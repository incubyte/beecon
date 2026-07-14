package app

import (
	"context"
	"log/slog"
	"time"

	"beecon/internal/connections"
	"beecon/internal/delivery"
	"beecon/internal/logging"
	"beecon/internal/triggers"
	"beecon/internal/worker"
)

// dispatcherLoopName is the dispatcher's Loop name — worker.Group.RunOnce
// and Nudge key off this.
const dispatcherLoopName = "dispatcher"

// pollerLoopName is the trigger poller's Loop name (Phase 3 Slice 4) —
// worker.Group.RunOnce keys off this.
const pollerLoopName = "poller"

// refresherLoopName and reconcilerLoopName are the Phase 3 Slice 5 refresh
// scheduler's and reconciliation job's Loop names.
const (
	refresherLoopName  = "refresher"
	reconcilerLoopName = "reconciler"
)

// purgerLoopName is the retention purge worker's Loop name (Phase 4 Slice
// 7, PD44) — worker.Group.RunOnce keys off this.
const purgerLoopName = "purger"

// dispatcherScanInterval and dispatcherJitter are FD5's "unexported
// constants, not config": the dispatcher loop scans for due outbox events
// every 5s. Per-event scheduling already lives in data (next_attempt_at) —
// the scan interval only bounds how quickly a second binary instance, or a
// missed Nudge, notices new or newly-due work.
const (
	dispatcherScanInterval = 5 * time.Second
	dispatcherJitter       = 500 * time.Millisecond
)

// pollerScanInterval and pollerJitter are FD5's second "unexported
// constant, not config" scan tick: the poller loop scans for due
// TriggerInstances every 5s. Per-instance scheduling already lives in data
// (next_poll_at) — the scan interval only bounds how quickly a second
// binary instance notices new or newly-due polls.
const (
	pollerScanInterval = 5 * time.Second
	pollerJitter       = 500 * time.Millisecond
)

// buildWorkers assembles every Phase 3 background loop shipped so far (the
// outbox dispatcher, Slice 3; the trigger poller, Slice 4; the refresh
// scheduler and reconciliation job, Slice 5) into one worker.Group. Wire
// itself never starts it — cmd/beecon's main.go does, after the HTTP
// listener is up, and stops it before the listener's own shutdown completes
// (section 3 of the architecture doc) — so every existing test and journey
// composes the app without background nondeterminism. Unlike the
// dispatcher/poller's fixed 5s scan (FD5 — per-item scheduling lives in
// data), the refresher and reconciler ticks are the configured
// BEECON_REFRESH_SCAN_INTERVAL/BEECON_RECONCILE_INTERVAL themselves, since
// neither claim query has a per-row "next attempt" column to schedule
// against.
func buildWorkers(
	logger *slog.Logger,
	deliveryFacade *delivery.Facade,
	triggersFacade *triggers.Facade,
	connectionsFacade *connections.Facade,
	loggingFacade *logging.Facade,
	refreshScanInterval time.Duration,
	reconcileInterval time.Duration,
	purgeInterval time.Duration,
) *worker.Group {
	return worker.NewGroup(logger,
		worker.Loop{
			Name:   dispatcherLoopName,
			Every:  dispatcherScanInterval,
			Jitter: dispatcherJitter,
			Run:    deliveryFacade.DispatchOnce,
		},
		worker.Loop{
			Name:   pollerLoopName,
			Every:  pollerScanInterval,
			Jitter: pollerJitter,
			Run:    triggersFacade.PollOnce,
		},
		worker.Loop{
			Name:   refresherLoopName,
			Every:  refreshScanInterval,
			Jitter: refreshScanInterval / 10,
			Run:    connectionsFacade.RefreshDueOnce,
		},
		worker.Loop{
			Name:   reconcilerLoopName,
			Every:  reconcileInterval,
			Jitter: reconcileInterval / 10,
			Run:    connectionsFacade.ReconcileOnce,
		},
		worker.Loop{
			Name:   purgerLoopName,
			Every:  purgeInterval,
			Jitter: purgeInterval / 10,
			Run:    purgeOnce(loggingFacade, deliveryFacade),
		},
	)
}

// purgeOnce is the purger Loop's Run func (Slice 7, PD44/§7 of the
// architecture doc): logging.PurgeOnce then delivery.PurgeOnce, in that
// order — each is independently org-scoped and independently a no-op for
// any org whose effective window is unlimited, so running logging's purge
// first never affects delivery's own outcome.
func purgeOnce(loggingFacade *logging.Facade, deliveryFacade *delivery.Facade) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if err := loggingFacade.PurgeOnce(ctx); err != nil {
			return err
		}
		return deliveryFacade.PurgeOnce(ctx)
	}
}
