package delivery

import (
	"testing"
	"time"
)

// midpointJitter is the neutral jitter source (offset = (0.5*2-1)*spread =
// 0): with it, NextAttempt returns exactly now.Add(delay) — the pure PD30
// schedule value, with jitter's own randomization factored out so the exact
// table can be pinned per attempt count.
func midpointJitter() float64 { return 0.5 }

// TestNextAttempt_PinsTheExactPD30ScheduleAtEachAttemptCount is the pure
// retry-table test: afterFailureDelays[i] (schedule.go) for attempts 1
// through 9 is exactly the Standard Webhooks recommended schedule — 5s, 5m,
// 30m, 2h, 5h, 10h, 14h, 20h, 24h.
func TestNextAttempt_PinsTheExactPD30ScheduleAtEachAttemptCount(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		attemptsSoFar int
		wantDelay     time.Duration
	}{
		{1, 5 * time.Second},
		{2, 5 * time.Minute},
		{3, 30 * time.Minute},
		{4, 2 * time.Hour},
		{5, 5 * time.Hour},
		{6, 10 * time.Hour},
		{7, 14 * time.Hour},
		{8, 20 * time.Hour},
		{9, 24 * time.Hour},
	}
	for _, tc := range cases {
		got := NextAttempt(now, tc.attemptsSoFar, midpointJitter)
		want := now.Add(tc.wantDelay)
		if !got.Equal(want) {
			t.Errorf("NextAttempt(attemptsSoFar=%d) = %v, want %v (delay %v)", tc.attemptsSoFar, got, want, tc.wantDelay)
		}
	}
}

// TestNextAttempt_JitterStaysWithinPlusMinusTenPercentOfTheScheduledDelay
// covers PD30's "jittered": the extreme jitter values (0 and near-1) must
// bound the returned delay to delay*0.9 and delay*1.1 respectively — the
// ±10% jitterFraction schedule.go documents.
func TestNextAttempt_JitterStaysWithinPlusMinusTenPercentOfTheScheduledDelay(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const firstRetryDelay = 5 * time.Second

	lowest := NextAttempt(now, 1, func() float64 { return 0 })
	wantLowest := now.Add(firstRetryDelay - firstRetryDelay/10)
	if !lowest.Equal(wantLowest) {
		t.Errorf("NextAttempt with jitter()=0 = %v, want %v (delay - 10%%)", lowest, wantLowest)
	}

	highest := NextAttempt(now, 1, func() float64 { return 1 })
	wantHighest := now.Add(firstRetryDelay + firstRetryDelay/10)
	if !highest.Equal(wantHighest) {
		t.Errorf("NextAttempt with jitter()=1 = %v, want %v (delay + 10%%)", highest, wantHighest)
	}

	if lowest.After(now.Add(firstRetryDelay)) || highest.Before(now.Add(firstRetryDelay)) {
		t.Errorf("jittered bounds [%v, %v] must straddle the unjittered delay %v", lowest, highest, now.Add(firstRetryDelay))
	}
}

// TestNextAttempt_AtAttemptsSoFarZeroUsesTheFirstScheduleEntry pins
// delayIndex's clamp for attemptsSoFar below 1 (defensive: production never
// calls NextAttempt for the very first attempt — Enqueue sets it to "now"
// directly per PD30's "immediately" — but the pure function itself must not
// panic or index out of range if ever called at 0).
func TestNextAttempt_AtAttemptsSoFarZeroUsesTheFirstScheduleEntry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got := NextAttempt(now, 0, midpointJitter)

	want := now.Add(5 * time.Second)
	if !got.Equal(want) {
		t.Errorf("NextAttempt(attemptsSoFar=0) = %v, want %v (clamped to the first schedule entry)", got, want)
	}
}

// TestIsExhausted_FalseBelowMaxAttempts covers every attempt count strictly
// below MaxAttempts (10): DispatchOnce must keep scheduling another attempt.
func TestIsExhausted_FalseBelowMaxAttempts(t *testing.T) {
	for attempts := 0; attempts < MaxAttempts; attempts++ {
		if IsExhausted(attempts) {
			t.Errorf("IsExhausted(%d) = true, want false (MaxAttempts is %d)", attempts, MaxAttempts)
		}
	}
}

// TestIsExhausted_TrueAtExactlyMaxAttempts pins the exhaustion boundary
// itself: the 10th failed attempt marks the event FAILED rather than
// scheduling an 11th.
func TestIsExhausted_TrueAtExactlyMaxAttempts(t *testing.T) {
	if !IsExhausted(MaxAttempts) {
		t.Errorf("IsExhausted(%d) = false, want true — MaxAttempts itself must be exhausted", MaxAttempts)
	}
}

func TestIsExhausted_TrueAboveMaxAttempts(t *testing.T) {
	if !IsExhausted(MaxAttempts + 1) {
		t.Errorf("IsExhausted(%d) = false, want true", MaxAttempts+1)
	}
}

// TestMaxAttempts_IsTen pins the exact PD30 count so a future refactor of
// afterFailureDelays' length cannot silently change the exhaustion point
// without a failing test naming it.
func TestMaxAttempts_IsTen(t *testing.T) {
	if MaxAttempts != 10 {
		t.Errorf("MaxAttempts = %d, want 10 (PD30: 10 attempts spanning roughly three days)", MaxAttempts)
	}
}
