//go:build integration

// Package support: SleepSpy is a test double for the execution facade's
// retry-loop sleep func (PD21, Slice 6, app.Deps.Sleep): it never actually
// waits, but records every duration it was asked to sleep for, so a
// rate-limit journey can assert the retry loop honored a provider's
// Retry-After (or fell back to a jittered backoff) without a real delay.
package support

import (
	"context"
	"sync"
	"time"
)

// SleepSpy records every duration Sleep was asked to wait, in call order.
type SleepSpy struct {
	mu        sync.Mutex
	durations []time.Duration
}

// Sleep is the sleepFunc shape app.Deps.Sleep expects. It records d and
// returns immediately (ctx cancellation aside) — no real delay.
func (s *SleepSpy) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.durations = append(s.durations, d)
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// Durations returns every duration Sleep has been asked to wait for so far,
// in call order.
func (s *SleepSpy) Durations() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]time.Duration, len(s.durations))
	copy(out, s.durations)
	return out
}
