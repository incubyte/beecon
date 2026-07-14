// Package worker is Beecon's shared background-loop runtime (PD29): dumb
// interval tickers that call an owning module's own Run func — every
// module's actual worker logic lives in its own facade as a public
// RunOnce-style method (delivery.Facade.DispatchOnce, and later
// triggers.Facade.PollOnce, connections.Facade.RefreshDueOnce/
// ReconcileOnce); this package only sleeps, jitters, and drains. It
// imports no domain module, like httpx or idgen.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// Loop is one background worker's schedule and logic: Run is called every
// Every, jittered by up to ±Jitter, until the owning Group is stopped.
type Loop struct {
	Name   string
	Every  time.Duration
	Jitter time.Duration
	Run    func(ctx context.Context) error
}

// Group owns a set of named Loops: Start runs each on its own goroutine
// (a jittered sleep, then Run, forever, until stopped — a Run error is
// logged and the loop continues to its next tick, PD29), Stop cancels
// every loop's sleep and waits (bounded by ctx's own deadline) for
// in-flight Runs to finish, RunOnce invokes one loop's Run directly, and
// Nudge wakes one loop's sleep early without blocking.
type Group struct {
	loops  map[string]Loop
	wake   map[string]chan struct{}
	logger *slog.Logger

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// drainOnce and drained are PD38d's drain-behavior hardening: exactly
	// one goroutine ever cancels the loops and waits on wg, no matter how
	// many times or how concurrently Stop is called, and drained (closed
	// once that wait completes) is what every caller's own select actually
	// waits on — so a second, concurrent Stop call blocks until the drain
	// genuinely finishes (or its own ctx expires) instead of wrongly
	// returning the instant it observes stopped already set.
	drainOnce sync.Once
	drained   chan struct{}
}

// NewGroup builds a Group over loops, keyed by each Loop's own Name.
// logger records a loop's Run error (PD29: "run-error -> slog +
// continue"); a nil logger falls back to slog.Default().
func NewGroup(logger *slog.Logger, loops ...Loop) *Group {
	if logger == nil {
		logger = slog.Default()
	}
	byName := make(map[string]Loop, len(loops))
	wake := make(map[string]chan struct{}, len(loops))
	for _, loop := range loops {
		byName[loop.Name] = loop
		wake[loop.Name] = make(chan struct{}, 1)
	}
	return &Group{loops: byName, wake: wake, logger: logger, drained: make(chan struct{})}
}

// Start runs every Loop on its own goroutine. It is a no-op if the Group
// has already been started or stopped — production calls this exactly
// once (cmd/beecon/main.go, after the HTTP listener is up); Wire itself
// never calls it, so every existing test and journey composes the app
// without background nondeterminism (section 3 of the architecture doc).
func (g *Group) Start(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started || g.stopped {
		return
	}
	g.started = true
	runCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	for name, loop := range g.loops {
		g.wg.Add(1)
		go g.runLoop(runCtx, name, loop)
	}
}

func (g *Group) runLoop(ctx context.Context, name string, loop Loop) {
	defer g.wg.Done()
	for g.sleep(ctx, name, loop) {
		if err := loop.Run(ctx); err != nil {
			g.logger.Error("worker loop run failed", "loop", name, "err", err)
		}
	}
}

// sleep waits Every ± Jitter, or until ctx is cancelled or Nudge(name)
// fires — whichever comes first. It returns false when ctx was cancelled
// (the loop should stop), true otherwise (proceed to the next Run).
func (g *Group) sleep(ctx context.Context, name string, loop Loop) bool {
	timer := time.NewTimer(jittered(loop.Every, loop.Jitter))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-g.wake[name]:
		return true
	}
}

func jittered(every, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return every
	}
	offset := time.Duration(rand.Int63n(int64(jitter)*2)) - jitter
	delay := every + offset
	if delay < 0 {
		return 0
	}
	return delay
}

// RunOnce invokes name's Run directly, bypassing its schedule entirely —
// the shape every unit and journey test drives instead of starting real
// loops (PD29's "deterministic tests, zero sleeps").
func (g *Group) RunOnce(ctx context.Context, name string) error {
	loop, ok := g.loops[name]
	if !ok {
		return fmt.Errorf("worker: no loop named %q", name)
	}
	return loop.Run(ctx)
}

// Nudge wakes name's sleep early, non-blocking: if the loop isn't
// currently sleeping (or a nudge is already pending in its single-slot
// wake channel), this is a no-op — the loop still runs on its next
// natural tick. An unknown name is also a no-op.
func (g *Group) Nudge(name string) {
	ch, ok := g.wake[name]
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Stop cancels every loop's sleep and waits for in-flight Runs to finish,
// bounded by ctx's own deadline; anything still running when ctx expires
// is simply abandoned — its lease-claimed work is re-claimed by the next
// boot (PD29's crash-safety story, restated as a test in PD38d). Safe to
// call more than once, including concurrently: every call blocks until the
// same drain completes (or its own ctx expires) rather than a second call
// wrongly returning early just because a first call already marked the
// Group stopped while its own drain was still in flight — the caller that
// actually needs to know work has settled (e.g. Wired.Close closing the
// database only after workers.Stop returns) must be able to trust that.
// Safe to call on a Group that was never Started (a no-op: there is nothing
// to drain).
func (g *Group) Stop(ctx context.Context) {
	g.mu.Lock()
	if !g.started {
		g.stopped = true
		g.mu.Unlock()
		return
	}
	g.stopped = true
	cancel := g.cancel
	g.mu.Unlock()

	g.drainOnce.Do(func() {
		cancel()
		go func() {
			g.wg.Wait()
			close(g.drained)
		}()
	})

	select {
	case <-g.drained:
	case <-ctx.Done():
	}
}
