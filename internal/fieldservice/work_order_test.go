package fieldservice

import (
	"context"
	"testing"
	"time"
)

type fixedFlag bool

func (f fixedFlag) FlagEnabled(context.Context, string) (bool, error) { return bool(f), nil }

func TestProcessChoosesFieldServiceTrack(t *testing.T) {
	now := time.Date(2026, time.September, 12, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		enabled    bool
		wantStatus string
		wantPhotos int
		wantDue    string
	}{
		{name: "flag off keeps baseline dispatch", enabled: false, wantStatus: "queued"},
		{name: "flag on records evidence and follow-up", enabled: true, wantStatus: "technician_dispatched", wantPhotos: 2, wantDue: "2026-09-13T08:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewProcessor(fixedFlag(tt.enabled), func() time.Time { return now })
			got, err := processor.Process(context.Background(), Intake{
				WorkOrderID: "WO-1842",
				PhotoRefs:   []string{"meter-before.jpg", "meter-after.jpg"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Dispatch != tt.wantStatus || len(got.PhotoRefs) != tt.wantPhotos || got.FollowUpDue != tt.wantDue {
				t.Fatalf("got status=%q photos=%d due=%q", got.Dispatch, len(got.PhotoRefs), got.FollowUpDue)
			}
		})
	}
}
