package delivery

import "time"

// MaxAttempts is PD30's exhaustion point (the Standard Webhooks
// recommended schedule): the 10th failed attempt marks an Event FAILED
// rather than scheduling an 11th.
const MaxAttempts = 10

// afterFailureDelays is PD30's fixed backoff table: afterFailureDelays[i]
// is how long to wait, after the (i+1)th attempt fails, before making the
// (i+2)th — 5s, 5m, 30m, 2h, 5h, 10h, 14h, 20h, 24h, spanning roughly
// three days across all nine retries. The very first attempt is never
// scheduled through this table: Enqueue sets it for "now" directly (PD30's
// "immediately").
var afterFailureDelays = []time.Duration{
	5 * time.Second,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	5 * time.Hour,
	10 * time.Hour,
	14 * time.Hour,
	20 * time.Hour,
	24 * time.Hour,
}

// jitterFraction is how much NextAttempt randomizes each delay (±10%), so
// many simultaneously failing deliveries don't all retry in lockstep
// (PD30: "jittered").
const jitterFraction = 0.1

// IsExhausted reports whether attempts has reached MaxAttempts —
// DispatchOnce marks the event FAILED instead of scheduling another
// attempt.
func IsExhausted(attempts int) bool {
	return attempts >= MaxAttempts
}

// NextAttempt returns when the attempt after attemptsSoFar failed attempts
// should run, per PD30's schedule, jittered by ±jitterFraction. jitter is
// injected (a func returning a float64 in [0,1)) so tests stay
// deterministic; production passes math/rand.Float64. Callers must not
// call this once IsExhausted(attemptsSoFar) is already true.
func NextAttempt(now time.Time, attemptsSoFar int, jitter func() float64) time.Time {
	delay := afterFailureDelays[delayIndex(attemptsSoFar)]
	spread := float64(delay) * jitterFraction
	offset := (jitter()*2 - 1) * spread
	return now.Add(delay + time.Duration(offset))
}

func delayIndex(attemptsSoFar int) int {
	index := attemptsSoFar - 1
	if index < 0 {
		index = 0
	}
	if index >= len(afterFailureDelays) {
		index = len(afterFailureDelays) - 1
	}
	return index
}
