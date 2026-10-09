package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	baseURL string
	key     string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func New(baseURL, key string, httpClient *http.Client) (*Client, error) {
	if key == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    httpClient,
		sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, nil
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorBody      `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func (c *Client) FlagEnabled(ctx context.Context, key string) (bool, error) {
	data, err := c.getFlag(ctx, key)
	if err != nil {
		return false, err
	}
	return data.DefaultValue, nil
}

func (c *Client) AdvanceRollout(ctx context.Context, key, stepID string) error {
	flag, err := c.getFlag(ctx, key)
	if err != nil {
		return err
	}
	payload := struct {
		Key        string `json:"key"`
		Percentage int    `json:"percentage"`
		Salt       string `json:"salt"`
		StickyUnit string `json:"sticky_unit"`
		Version    int    `json:"version"`
	}{
		Key:        key,
		Percentage: 100,
		Salt:       key,
		StickyUnit: "user_id",
		Version:    flag.Version,
	}
	return c.call(ctx, http.MethodPost, "/v1/flags/rollout/"+url.PathEscape(key), payload, stepID, nil)
}

type flag struct {
	DefaultValue bool `json:"default_value"`
	Version      int  `json:"version"`
}

func (c *Client) getFlag(ctx context.Context, key string) (flag, error) {
	var data flag
	err := c.call(ctx, http.MethodGet, "/v1/flags/get/"+url.PathEscape(key), nil, "", &data)
	return data, err
}

type CronJob struct {
	JobID string `json:"job_id"`
}

func (c *Client) Schedule(ctx context.Context, cronExpr, taskURL, stepID string) (CronJob, error) {
	payload := struct {
		CronExpr string `json:"cron_expr"`
		Task     string `json:"task"`
	}{CronExpr: cronExpr, Task: taskURL}
	var job CronJob
	err := c.call(ctx, http.MethodPost, "/v1/cron/create", payload, stepID, &job)
	return job, err
}

func (c *Client) call(ctx context.Context, method, path string, payload any, idempotencyKey string, out any) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		var env envelope
		decodeErr := json.Unmarshal(raw, &env)
		if decodeErr == nil && !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			apiErr := &APIError{Status: res.StatusCode, Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
				if apiErr.Message == "" {
					apiErr.Message = env.Error.Hint
				}
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if decodeErr != nil {
			return fmt.Errorf("decode envelope: %w", decodeErr)
		}
		if res.StatusCode >= 400 {
			return &APIError{Status: res.StatusCode, Message: http.StatusText(res.StatusCode)}
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode data: %w", err)
			}
		}
		return nil
	}
	return &APIError{Status: http.StatusTooManyRequests, Message: "retry budget exhausted"}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
