// retry.go is PD21's platform-side retry policy (ADR-0009): at most
// maxAttempts total calls against a normalized upstream rate limit,
// honoring the provider's own Retry-After when it sent one, falling back to
// a jittered backoff otherwise, and never waiting past retryBudget in total.
// A non-retriable response (a network failure, or any status/body that
// IsRateLimited does not recognize as a rate limit) passes through after
// exactly one attempt.
package execution

import (
	"context"
	"math/rand/v2"
	"time"

	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// maxAttempts is PD21's retry ceiling: the first call plus at most two
// retries against a normalized rate limit before Execute reports the AC3
// carve-out.
const maxAttempts = 3

// retryBudget is PD21's total wait ceiling across every attempt's backoff —
// a rate limit that would need longer than this to clear is reported as
// exhausted rather than making the caller wait indefinitely.
const retryBudget = 30 * time.Second

// jitterBackoffMin/Max bound the randomized wait retryDelay falls back to
// when a rate-limited response carries no Retry-After (PD21).
const (
	jitterBackoffMin = 500 * time.Millisecond
	jitterBackoffMax = 2 * time.Second
)

// sleepFunc pauses for d, or returns ctx's error if ctx is cancelled first.
// Facade.sleep defaults to realSleep; WithSleep overrides it so tests can
// drive the retry loop without a real sleep.
type sleepFunc func(ctx context.Context, d time.Duration) error

// realSleep is the sleepFunc production wiring uses.
func realSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryDelay is the wait before the next attempt (PD21): the provider's own
// Retry-After when the rate-limited response carried one, otherwise a
// jittered backoff.
func retryDelay(response ToolCallResponse, now time.Time) time.Duration {
	if delay, ok := ParseRetryAfter(response.RetryAfter, now); ok {
		return delay
	}
	return jitterBackoff()
}

func jitterBackoff() time.Duration {
	span := jitterBackoffMax - jitterBackoffMin
	return jitterBackoffMin + time.Duration(rand.Int64N(int64(span)))
}

// retryOutcome is what callWithRetry hands back to callProvider/
// retryAfterRefresh: the last attempt's response and call error (exactly the
// pair toolCallResult already knows how to turn into a Result), and — only
// when every attempt stayed rate-limited — exhausted plus the delay the loop
// would have waited next, for the AC3 429 carve-out's Retry-After header.
type retryOutcome struct {
	response   ToolCallResponse
	callErr    error
	exhausted  bool
	retryAfter time.Duration
}

// callWithRetry makes request against the provider up to maxAttempts times
// (AC1), retrying only while every attempt so far normalizes as a rate limit
// (AC4: a network failure or any non-rate-limited status/body — success or
// not — returns after exactly one attempt). Every attempt writes its own log
// entry via attemptCall, marked RateLimited where applicable (AC5), whether
// or not this call ultimately retries.
func (f *Facade) callWithRetry(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	attribution toolAttribution,
	providerSlug string,
	request ToolCallRequest,
) retryOutcome {
	return f.retryLoop(ctx, func() (ToolCallResponse, error) {
		return f.attemptCall(ctx, org, userID, connectionID, attribution, providerSlug, request)
	}, func() { f.recordRateLimitRetryMetric(providerSlug) })
}

// retryLoop is PD21's retry policy itself, shared by every caller that needs
// it: a tool execution (callWithRetry, which also logs and records metrics
// per attempt via its own attempt func) and, since Slice 4, trigger polling
// (execution/poll.go, which calls the provider directly with no
// tool-execution logging attached — a poll failure is logged by the
// triggers module instead, PD34). attempt makes one call; onRetry, when
// non-nil, is invoked once per attempt that will actually be retried (used
// for the rate-limit-retry metric — polling passes nil, since Slice 7 owns
// its own poll metrics).
func (f *Facade) retryLoop(ctx context.Context, attempt func() (ToolCallResponse, error), onRetry func()) retryOutcome {
	var waited time.Duration
	for i := 1; ; i++ {
		response, callErr := attempt()
		if callErr != nil || !IsRateLimited(response) {
			return retryOutcome{response: response, callErr: callErr}
		}

		delay := retryDelay(response, f.now())
		if i >= maxAttempts || waited+delay > retryBudget {
			return retryOutcome{response: response, exhausted: true, retryAfter: delay}
		}
		if onRetry != nil {
			onRetry()
		}
		if sleepErr := f.sleep(ctx, delay); sleepErr != nil {
			return retryOutcome{response: response, callErr: sleepErr}
		}
		waited += delay
	}
}

func (f *Facade) recordRateLimitRetryMetric(providerSlug string) {
	if f.metrics == nil {
		return
	}
	f.metrics.RecordRateLimitRetry(providerSlug)
}
