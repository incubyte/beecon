// White-box (package execution) tests for retry.go's unexported
// callWithRetry and jitterBackoff (PD21, Slice 6, ADR-0009): at most
// maxAttempts total calls against a normalized rate limit, honoring the
// provider's own Retry-After when present, falling back to a jittered
// backoff otherwise, and never waiting past retryBudget in total — driven
// with an injected sleepFunc so these edges run without any real delay.
// facade_test.go covers the same policy through Execute's own public
// surface (AC1-AC5); these tests isolate retry.go's own loop and constants.
package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/connections"
	"beecon/internal/organizations"
)

const (
	retryTestOrg      = organizations.OrgID("org_retry")
	retryTestUser     = organizations.UserID("user_retry")
	retryTestConnID   = connections.ConnectionID("conn_retry")
	retryTestToolSlug = "retry-test-tool"
	retryTestProvider = "retry-test-provider"
)

// recordingSleep is a sleepFunc test double: it never actually waits, but
// records every duration it was asked to sleep for.
type recordingSleep struct {
	durations []time.Duration
}

func (r *recordingSleep) sleep(_ context.Context, d time.Duration) error {
	r.durations = append(r.durations, d)
	return nil
}

// sequencedRetryProvider is a minimal ProviderClient stub returning one
// scripted response per call, repeating the last one if called more times
// than scripted.
type sequencedRetryProvider struct {
	responses []ToolCallResponse
	callCount int
}

func (p *sequencedRetryProvider) Call(_ context.Context, _ ToolCallRequest) (ToolCallResponse, error) {
	index := p.callCount
	if index >= len(p.responses) {
		index = len(p.responses) - 1
	}
	p.callCount++
	return p.responses[index], nil
}

func retryTestFacade(provider ProviderClient, sleep sleepFunc) *Facade {
	return NewFacade(nil, nil, provider, nil, func() time.Time { return time.Now() }).WithSleep(sleep)
}

func rateLimited(retryAfter string) ToolCallResponse {
	return ToolCallResponse{StatusCode: 429, Body: "{}", RetryAfter: retryAfter}
}

func retrySuccess() ToolCallResponse {
	return ToolCallResponse{StatusCode: 200, Body: `{"ok":true}`}
}

func TestCallWithRetry_HonorsTheProvidersRetryAfterSecondsValue(t *testing.T) {
	provider := &sequencedRetryProvider{responses: []ToolCallResponse{rateLimited("5"), retrySuccess()}}
	sleep := &recordingSleep{}
	f := retryTestFacade(provider, sleep.sleep)

	outcome := f.callWithRetry(context.Background(), retryTestOrg, retryTestUser, retryTestConnID, retryTestToolSlug, retryTestProvider, ToolCallRequest{})

	if outcome.exhausted {
		t.Fatal("exhausted = true, want false — the retried call succeeded")
	}
	if len(sleep.durations) != 1 || sleep.durations[0] != 5*time.Second {
		t.Fatalf("sleep durations = %v, want exactly [5s]", sleep.durations)
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want 2", provider.callCount)
	}
}

func TestCallWithRetry_FallsBackToAJitteredBackoffWhenRetryAfterIsAbsent(t *testing.T) {
	provider := &sequencedRetryProvider{responses: []ToolCallResponse{rateLimited(""), retrySuccess()}}
	sleep := &recordingSleep{}
	f := retryTestFacade(provider, sleep.sleep)

	f.callWithRetry(context.Background(), retryTestOrg, retryTestUser, retryTestConnID, retryTestToolSlug, retryTestProvider, ToolCallRequest{})

	if len(sleep.durations) != 1 {
		t.Fatalf("sleep durations = %v, want exactly one jittered backoff", sleep.durations)
	}
	if sleep.durations[0] < jitterBackoffMin || sleep.durations[0] > jitterBackoffMax {
		t.Errorf("backoff = %v, want within [%v, %v]", sleep.durations[0], jitterBackoffMin, jitterBackoffMax)
	}
}

func TestJitterBackoff_StaysWithinTheDocumentedBounds(t *testing.T) {
	for i := 0; i < 50; i++ {
		got := jitterBackoff()
		if got < jitterBackoffMin || got > jitterBackoffMax {
			t.Fatalf("jitterBackoff() = %v, want within [%v, %v]", got, jitterBackoffMin, jitterBackoffMax)
		}
	}
}

func TestCallWithRetry_StopsAtMaxAttemptsAndReportsExhausted(t *testing.T) {
	provider := &sequencedRetryProvider{responses: []ToolCallResponse{rateLimited("1"), rateLimited("1"), rateLimited("1")}}
	sleep := &recordingSleep{}
	f := retryTestFacade(provider, sleep.sleep)

	outcome := f.callWithRetry(context.Background(), retryTestOrg, retryTestUser, retryTestConnID, retryTestToolSlug, retryTestProvider, ToolCallRequest{})

	if !outcome.exhausted {
		t.Fatal("exhausted = false, want true after maxAttempts rate-limited attempts")
	}
	if provider.callCount != maxAttempts {
		t.Errorf("provider was called %d times, want exactly maxAttempts (%d)", provider.callCount, maxAttempts)
	}
	if len(sleep.durations) != maxAttempts-1 {
		t.Errorf("slept %d times, want exactly maxAttempts-1 (%d) — no wait after the final attempt", len(sleep.durations), maxAttempts-1)
	}
	if outcome.retryAfter != time.Second {
		t.Errorf("retryAfter = %v, want the 1s the exhausted attempt itself carried", outcome.retryAfter)
	}
}

// TestCallWithRetry_StopsWhenTheTotalWaitWouldExceedTheRetryBudget: the first
// attempt's Retry-After (20s) fits under the 30s budget and is honored; the
// second attempt's own Retry-After (15s) would push the total to 35s, so the
// loop reports exhausted before a third attempt or a second sleep.
func TestCallWithRetry_StopsWhenTheTotalWaitWouldExceedTheRetryBudget(t *testing.T) {
	provider := &sequencedRetryProvider{responses: []ToolCallResponse{rateLimited("20"), rateLimited("15")}}
	sleep := &recordingSleep{}
	f := retryTestFacade(provider, sleep.sleep)

	outcome := f.callWithRetry(context.Background(), retryTestOrg, retryTestUser, retryTestConnID, retryTestToolSlug, retryTestProvider, ToolCallRequest{})

	if !outcome.exhausted {
		t.Fatal("exhausted = false, want true — the second attempt's delay would exceed the 30s total budget")
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want exactly 2 — the budget must stop the loop before a third attempt", provider.callCount)
	}
	if len(sleep.durations) != 1 {
		t.Fatalf("slept %d times, want exactly 1 (only the first attempt's delay was affordable within budget)", len(sleep.durations))
	}
}

func TestCallWithRetry_ANonRateLimitedResponseReturnsAfterExactlyOneAttempt(t *testing.T) {
	provider := &sequencedRetryProvider{responses: []ToolCallResponse{{StatusCode: 400, Body: "bad request"}}}
	sleep := &recordingSleep{}
	f := retryTestFacade(provider, sleep.sleep)

	outcome := f.callWithRetry(context.Background(), retryTestOrg, retryTestUser, retryTestConnID, retryTestToolSlug, retryTestProvider, ToolCallRequest{})

	if outcome.exhausted {
		t.Fatal("exhausted = true, want false — a non-rate-limited status must not be treated as exhaustion")
	}
	if provider.callCount != 1 {
		t.Errorf("provider was called %d times, want exactly 1 — a non-retriable response must not be retried", provider.callCount)
	}
	if len(sleep.durations) != 0 {
		t.Errorf("slept %d times, want 0 — a non-retriable response must never sleep", len(sleep.durations))
	}
}

func TestCallWithRetry_ANetworkFailureReturnsAfterExactlyOneAttemptWithoutSleeping(t *testing.T) {
	provider := &erroringRetryProvider{}
	sleep := &recordingSleep{}
	f := retryTestFacade(provider, sleep.sleep)

	outcome := f.callWithRetry(context.Background(), retryTestOrg, retryTestUser, retryTestConnID, retryTestToolSlug, retryTestProvider, ToolCallRequest{})

	if outcome.callErr == nil {
		t.Fatal("callErr = nil, want the provider's network error surfaced")
	}
	if outcome.exhausted {
		t.Fatal("exhausted = true, want false — a network failure is not a rate-limit exhaustion")
	}
	if provider.callCount != 1 {
		t.Errorf("provider was called %d times, want exactly 1", provider.callCount)
	}
	if len(sleep.durations) != 0 {
		t.Errorf("slept %d times, want 0 — a network failure must never sleep", len(sleep.durations))
	}
}

// erroringRetryProvider always fails to reach the provider at all (a network
// failure), distinct from a normal non-2xx ToolCallResponse.
type erroringRetryProvider struct {
	callCount int
}

func (p *erroringRetryProvider) Call(_ context.Context, _ ToolCallRequest) (ToolCallResponse, error) {
	p.callCount++
	return ToolCallResponse{}, errors.New("dial tcp: connection refused")
}
