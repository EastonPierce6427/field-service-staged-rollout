package infrai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdvanceRolloutSendsRequiredFields(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch requests {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/v1/flags/get/field-service-follow-up" {
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"default_value":false,"version":7},"error":null,"metadata":{}}`))
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/v1/flags/rollout/field-service-follow-up" {
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Idempotency-Key"); got != "rollout-step" {
				t.Fatalf("got idempotency key %q", got)
			}
			var got struct {
				Key        string `json:"key"`
				Percentage int    `json:"percentage"`
				Salt       string `json:"salt"`
				StickyUnit string `json:"sticky_unit"`
				Version    int    `json:"version"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Key != "field-service-follow-up" || got.Percentage != 100 || got.Salt != "field-service-follow-up" || got.StickyUnit != "user_id" || got.Version != 7 {
				t.Fatalf("unexpected rollout payload: %+v", got)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{},"error":null,"metadata":{}}`))
		default:
			t.Fatalf("unexpected extra request")
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "test-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AdvanceRollout(context.Background(), "field-service-follow-up", "rollout-step"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
}

func TestFlagEnabledHandlesEnvelopeBeforeStatus(t *testing.T) {
	tests := []struct {
		name       string
		responses  []string
		statuses   []int
		want       bool
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "business rejection keeps client status",
			responses:  []string{`{"ok":false,"data":null,"error":{"message":"flag input rejected"},"metadata":{}}`},
			statuses:   []int{http.StatusBadRequest},
			wantStatus: http.StatusBadRequest,
			wantCalls:  1,
		},
		{
			name:      "rate limit waits then returns decision",
			responses: []string{`{"ok":false,"data":null,"error":{"message":"retry later"},"metadata":{}}`, `{"ok":true,"data":{"default_value":true},"error":null,"metadata":{}}`},
			statuses:  []int{http.StatusTooManyRequests, http.StatusOK},
			want:      true,
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/flags/get/field-service-follow-up" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Fatal("missing Bearer authorization")
				}
				index := calls
				calls++
				if tt.statuses[index] == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "0")
				}
				w.WriteHeader(tt.statuses[index])
				_, _ = w.Write([]byte(tt.responses[index]))
			}))
			defer server.Close()

			client, err := New(server.URL, "test-key", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client.sleep = func(context.Context, time.Duration) error { return nil }
			got, err := client.FlagEnabled(context.Background(), "field-service-follow-up")
			if tt.wantStatus != 0 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Status != tt.wantStatus {
					t.Fatalf("got error %v, want API status %d", err, tt.wantStatus)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || calls != tt.wantCalls {
				t.Fatalf("got enabled=%v calls=%d, want enabled=%v calls=%d", got, calls, tt.want, tt.wantCalls)
			}
		})
	}
}
