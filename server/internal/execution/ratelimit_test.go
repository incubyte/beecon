// Package execution_test exercises IsRateLimited and ParseRetryAfter
// directly (PD21, Slice 6) — retry.go's only detection/parsing primitives:
// normalizing Graph's nested throttle codes and Hubspot's RATE_LIMITS
// category into one rate-limit signal, independent of the response's own
// HTTP status, and turning a Retry-After header (whole seconds or an
// HTTP-date) into a wait duration.
package execution_test

import (
	"net/http"
	"testing"
	"time"

	"beecon/internal/execution"
)

func TestIsRateLimited_ABareHTTP429IsRateLimited(t *testing.T) {
	got := execution.IsRateLimited(execution.ToolCallResponse{StatusCode: http.StatusTooManyRequests, Body: ""})

	if !got {
		t.Fatal("IsRateLimited = false, want true for a bare HTTP 429")
	}
}

func TestIsRateLimited_GraphsTopLevelThrottleCodeIsRateLimitedRegardlessOfStatus(t *testing.T) {
	body := `{"error":{"code":"TooManyRequests"}}`

	got := execution.IsRateLimited(execution.ToolCallResponse{StatusCode: http.StatusOK, Body: body})

	if !got {
		t.Fatal("IsRateLimited = false, want true for Graph's top-level throttle code, even with a non-429 status")
	}
}

func TestIsRateLimited_GraphsNestedInnerErrorThrottleCodeIsRateLimited(t *testing.T) {
	body := `{"error":{"code":"ServiceUnavailable","innerError":{"code":"activityLimitReached"}}}`

	got := execution.IsRateLimited(execution.ToolCallResponse{StatusCode: http.StatusServiceUnavailable, Body: body})

	if !got {
		t.Fatal("IsRateLimited = false, want true for Graph's nested error.innerError.code throttle shape")
	}
}

func TestIsRateLimited_HubspotsRateLimitsCategoryIsRateLimitedRegardlessOfStatus(t *testing.T) {
	body := `{"category":"RATE_LIMITS","message":"You have reached your daily limit."}`

	got := execution.IsRateLimited(execution.ToolCallResponse{StatusCode: http.StatusOK, Body: body})

	if !got {
		t.Fatal("IsRateLimited = false, want true for Hubspot's RATE_LIMITS category, even with a 200 status")
	}
}

func TestIsRateLimited_ANonRateLimitedResponseIsNotRateLimited(t *testing.T) {
	responses := map[string]execution.ToolCallResponse{
		"200 with a normal payload":        {StatusCode: http.StatusOK, Body: `{"value":[]}`},
		"400 with an unrelated error body": {StatusCode: http.StatusBadRequest, Body: `{"error":"invalid_request"}`},
		"404 with no body":                 {StatusCode: http.StatusNotFound, Body: ""},
		"500 with a non-JSON body":         {StatusCode: http.StatusInternalServerError, Body: "not json at all"},
	}
	for name, response := range responses {
		t.Run(name, func(t *testing.T) {
			if execution.IsRateLimited(response) {
				t.Errorf("IsRateLimited(%+v) = true, want false", response)
			}
		})
	}
}

func TestParseRetryAfter_ParsesAWholeSecondsValue(t *testing.T) {
	delay, ok := execution.ParseRetryAfter("5", time.Now())

	if !ok {
		t.Fatal("ok = false, want true for a whole-seconds Retry-After value")
	}
	if delay != 5*time.Second {
		t.Errorf("delay = %v, want %v", delay, 5*time.Second)
	}
}

func TestParseRetryAfter_ParsesAnHTTPDateRelativeToNow(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(90 * time.Second)

	delay, ok := execution.ParseRetryAfter(future.Format(http.TimeFormat), now)

	if !ok {
		t.Fatal("ok = false, want true for an HTTP-date Retry-After value")
	}
	if delay < 89*time.Second || delay > 90*time.Second {
		t.Errorf("delay = %v, want ~90s (HTTP-date has only whole-second precision)", delay)
	}
}

func TestParseRetryAfter_AnHTTPDateAlreadyInThePastReturnsZeroDelayButStillOK(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-90 * time.Second)

	delay, ok := execution.ParseRetryAfter(past.Format(http.TimeFormat), now)

	if !ok {
		t.Fatal("ok = false, want true — a past HTTP-date is still a recognized Retry-After shape")
	}
	if delay != 0 {
		t.Errorf("delay = %v, want 0 for a Retry-After date already in the past", delay)
	}
}

func TestParseRetryAfter_AnEmptyHeaderIsNotRecognized(t *testing.T) {
	_, ok := execution.ParseRetryAfter("", time.Now())

	if ok {
		t.Fatal("ok = true, want false for an empty Retry-After header")
	}
}

func TestParseRetryAfter_AMalformedHeaderIsNotRecognized(t *testing.T) {
	_, ok := execution.ParseRetryAfter("not-a-valid-value", time.Now())

	if ok {
		t.Fatal("ok = true, want false for a Retry-After header that is neither whole seconds nor an HTTP-date")
	}
}

func TestParseRetryAfter_ANegativeSecondsValueIsNotRecognized(t *testing.T) {
	_, ok := execution.ParseRetryAfter("-5", time.Now())

	if ok {
		t.Fatal("ok = true, want false for a negative Retry-After seconds value")
	}
}
