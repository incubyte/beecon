package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitUntil polls check every 2ms until it returns true or timeout elapses,
// failing the test if it never does — used instead of a single fixed sleep
// so these real-goroutine tests stay both fast (on the happy path) and
// reliable (under load) without depending on ExactTiming.
func waitUntil(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !check() {
		t.Fatalf("condition not met within %v", timeout)
	}
}

// --- RunOnce: the shape every unit and journey test drives (PD29's
// "deterministic tests, zero sleeps"). ---

func TestGroup_RunOnce_InvokesTheNamedLoopsRunDirectlyWithoutSleeping(t *testing.T) {
	var calls int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: time.Hour, // would never fire on its own within a test timeout
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	if err := group.RunOnce(context.Background(), "dispatcher"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want exactly 1", got)
	}
}

func TestGroup_RunOnce_ReturnsAnErrorForAnUnknownLoopName(t *testing.T) {
	group := NewGroup(nil, Loop{Name: "dispatcher", Every: time.Hour, Run: func(context.Context) error { return nil }})

	err := group.RunOnce(context.Background(), "does-not-exist")

	if err == nil {
		t.Fatal("expected an error for an unknown loop name, got nil")
	}
}

func TestGroup_RunOnce_PropagatesTheRunFuncsOwnError(t *testing.T) {
	wantErr := errors.New("boom")
	group := NewGroup(nil, Loop{Name: "dispatcher", Every: time.Hour, Run: func(context.Context) error { return wantErr }})

	err := group.RunOnce(context.Background(), "dispatcher")

	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// --- Start/Stop: the real-loop semantics (PD29's Loop mechanics). ---

func TestGroup_Start_RunsTheLoopRepeatedlyUntilStopped(t *testing.T) {
	var calls int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: 3 * time.Millisecond,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	group.Start(context.Background())
	waitUntil(t, time.Second, func() bool { return atomic.LoadInt32(&calls) >= 3 })

	group.Stop(context.Background())
	afterStop := atomic.LoadInt32(&calls)
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != afterStop {
		t.Errorf("calls kept increasing after Stop: %d -> %d", afterStop, got)
	}
}

// TestGroup_Start_ARunErrorIsLoggedAndTheLoopContinuesToItsNextTick pins
// PD29's "run-error -> slog + continue": a Run that always fails must keep
// being invoked on schedule rather than killing its own loop.
func TestGroup_Start_ARunErrorIsLoggedAndTheLoopContinuesToItsNextTick(t *testing.T) {
	var calls int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: 3 * time.Millisecond,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return errors.New("always fails")
		},
	})

	group.Start(context.Background())
	waitUntil(t, time.Second, func() bool { return atomic.LoadInt32(&calls) >= 3 })
	group.Stop(context.Background())
}

func TestGroup_Start_IsANoOpWhenCalledASecondTime(t *testing.T) {
	var calls int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: 3 * time.Millisecond,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	group.Start(context.Background())
	group.Start(context.Background()) // must not spawn a second goroutine for the same loop
	waitUntil(t, time.Second, func() bool { return atomic.LoadInt32(&calls) >= 2 })
	group.Stop(context.Background())
}

func TestGroup_Stop_IsANoOpOnAGroupThatWasNeverStarted(t *testing.T) {
	group := NewGroup(nil, Loop{Name: "dispatcher", Every: time.Hour, Run: func(context.Context) error { return nil }})

	done := make(chan struct{})
	go func() {
		group.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop on a never-started Group hung")
	}
}

func TestGroup_Stop_IsSafeToCallMoreThanOnce(t *testing.T) {
	group := NewGroup(nil, Loop{Name: "dispatcher", Every: time.Hour, Run: func(context.Context) error { return nil }})
	group.Start(context.Background())

	group.Stop(context.Background())
	done := make(chan struct{})
	go func() {
		group.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a second Stop call hung")
	}
}

// TestGroup_Stop_ConcurrentCallsBothWaitForTheSameInFlightRunToFinish pins
// PD38d's own drain-hardening fix directly: two Stop calls racing each other
// while a Run is genuinely in flight must BOTH block until that Run actually
// finishes — neither may return early just because the other call already
// flipped the Group's own "stopped" bookkeeping (the bug the drainOnce/
// drained channel fixes; see worker.go's own doc comment on Stop).
func TestGroup_Stop_ConcurrentCallsBothWaitForTheSameInFlightRunToFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var finished int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: time.Millisecond,
		Run: func(ctx context.Context) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			atomic.StoreInt32(&finished, 1)
			return nil
		},
	})

	group.Start(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Run was never invoked")
	}

	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() {
		group.Stop(context.Background()) // no deadline: must wait for Run to finish
		close(firstDone)
	}()
	go func() {
		group.Stop(context.Background()) // races the first call; must wait too
		close(secondDone)
	}()

	// Give both calls a moment to (wrongly) return early if either doesn't
	// actually wait for the drain.
	select {
	case <-firstDone:
		t.Fatal("the first concurrent Stop call returned before the in-flight Run finished")
	case <-secondDone:
		t.Fatal("the second concurrent Stop call returned before the in-flight Run finished")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	for name, done := range map[string]chan struct{}{"first": firstDone, "second": secondDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("the %s concurrent Stop call never returned after the in-flight Run finished", name)
		}
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Error("expected the in-flight Run to have completed before either Stop call returned")
	}
}

// TestGroup_Stop_WaitsForAnInFlightRunToFinishBeforeReturning pins the
// "waits for in-flight Runs to finish" half of Stop's contract: Stop must
// not return while a Run invoked before cancellation is still executing.
func TestGroup_Stop_WaitsForAnInFlightRunToFinishBeforeReturning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var finished int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: time.Millisecond,
		Run: func(ctx context.Context) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			atomic.StoreInt32(&finished, 1)
			return nil
		},
	})

	group.Start(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Run was never invoked")
	}

	stopDone := make(chan struct{})
	go func() {
		group.Stop(context.Background()) // no deadline: must wait for Run to finish
		close(stopDone)
	}()

	// Give Stop a moment to (wrongly) return early if it doesn't actually wait.
	select {
	case <-stopDone:
		t.Fatal("Stop returned before the in-flight Run finished")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop never returned after the in-flight Run finished")
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Error("expected the in-flight Run to have completed before Stop returned")
	}
}

// TestGroup_Stop_AbandonsAnInFlightRunOnceItsContextDeadlinePasses pins the
// other half: a Run that never finishes must not hang Stop forever — Stop
// gives up once the ctx passed to it expires, leaving the work abandoned
// (PD29's crash-safety story: the lease simply expires and is re-claimed).
func TestGroup_Stop_AbandonsAnInFlightRunOnceItsContextDeadlinePasses(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{}) // never closed: Run hangs forever
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: time.Millisecond,
		Run: func(ctx context.Context) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-block
			return nil
		},
	})

	group.Start(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Run was never invoked")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	stopDone := make(chan struct{})
	go func() {
		group.Stop(stopCtx)
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return once its own context deadline passed, even though the Run never finished")
	}
}

// --- Nudge: the non-blocking wake (PD30's "Enqueue's first attempt runs
// promptly"). ---

// TestGroup_Nudge_TriggersAnImmediateRunWithoutWaitingOutALongInterval is the
// real Nudge behavior test: with a long Every, no run happens until Nudge is
// called, and Nudge causes one promptly.
func TestGroup_Nudge_TriggersAnImmediateRunWithoutWaitingOutALongInterval(t *testing.T) {
	var calls int32
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: time.Hour,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	group.Start(context.Background())
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("calls = %d before any Nudge, want 0 (Every is 1 hour)", got)
	}

	group.Nudge("dispatcher")
	waitUntil(t, time.Second, func() bool { return atomic.LoadInt32(&calls) >= 1 })
	group.Stop(context.Background())
}

func TestGroup_Nudge_OnAnUnknownLoopNameIsANoOp(t *testing.T) {
	group := NewGroup(nil, Loop{Name: "dispatcher", Every: time.Hour, Run: func(context.Context) error { return nil }})

	done := make(chan struct{})
	go func() {
		group.Nudge("does-not-exist")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Nudge on an unknown loop name hung")
	}
}

// TestGroup_Nudge_IsNonBlockingWhenTheLoopIsNotCurrentlySleeping proves
// Nudge never blocks the caller even when its single-slot wake channel is
// already full (a nudge already pending) or the loop isn't sleeping at all
// (never started) — Enqueue must never wait on the dispatcher loop.
func TestGroup_Nudge_IsNonBlockingWhenTheLoopIsNotCurrentlySleeping(t *testing.T) {
	group := NewGroup(nil, Loop{Name: "dispatcher", Every: time.Hour, Run: func(context.Context) error { return nil }})

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				group.Nudge("dispatcher") // never started: must still not block
			}()
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent Nudge calls on a not-yet-started Group blocked")
	}
}

func TestNewGroup_ANilLoggerFallsBackToSlogDefaultWithoutPanicking(t *testing.T) {
	group := NewGroup(nil, Loop{
		Name:  "dispatcher",
		Every: 3 * time.Millisecond,
		Run:   func(context.Context) error { return errors.New("logged via slog.Default()") },
	})

	group.Start(context.Background())
	time.Sleep(10 * time.Millisecond)
	group.Stop(context.Background())
}
