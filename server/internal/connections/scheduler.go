package connections

import (
	"context"
	"time"

	"beecon/internal/metrics"
)

const refreshClaimBatchLimit = 50

const refreshLeaseTTL = 30 * time.Second

// RefreshDueOnce claims ACTIVE connections whose access token expires within
// BEECON_REFRESH_LEAD and refreshes each through refreshOnce (PD36).
func (f *Facade) RefreshDueOnce(ctx context.Context) error {
	now := f.now()
	due, err := f.refreshQueue.ClaimDueRefresh(ctx, now, f.refreshLead, refreshLeaseTTL, refreshClaimBatchLimit)
	if err != nil {
		return err
	}
	for _, connection := range due {
		f.refreshDueOne(ctx, connection)
	}
	return nil
}

// refreshDueOne swallows a per-connection failure: transient errors retry on
// the next scan; a permanent refusal has already expired the connection and
// emitted its event. lead (not force) is the freshness gate here: a claimed
// connection is due because it is nearing expiry within BEECON_REFRESH_LEAD,
// not because it has necessarily gone stale yet (needsRefresh alone would
// silently no-op on every not-yet-expired connection in the lead window,
// defeating the whole point of proactive refresh) — and the same lead-aware
// check lets a caller that lost the lock race (e.g. a concurrent
// request-path refresh already ran) recognize the connection as already
// fresh, so only one grant happens either way.
func (f *Facade) refreshDueOne(ctx context.Context, connection Connection) {
	refreshed, err := f.refreshOnce(ctx, connection.OrgID, connection.ID, refreshParams{lead: f.refreshLead, deniedReason: ExpiredReasonRefreshDenied})
	f.recordScheduledRefreshOutcome(err, refreshed)
}

// recordScheduledRefreshOutcome records PD38d's scheduled-refresh outcome
// counter, distinct from RecordTokenRefresh's own provider-keyed counter
// (which already fires for both scheduled and PD18 request-path refreshes
// alike via the shared refreshConnection funnel): error means a transient
// failure retried on a later scan; a connection left EXPIRED means the
// provider permanently refused the grant; anything else means the
// connection is refreshed (or was already fresh — the lock-race loser's
// no-op counts the same, since nothing went wrong).
func (f *Facade) recordScheduledRefreshOutcome(err error, connection Connection) {
	if f.metrics == nil {
		return
	}
	f.metrics.RecordScheduledRefreshOutcome(scheduledRefreshOutcomeLabel(err, connection))
}

func scheduledRefreshOutcomeLabel(err error, connection Connection) string {
	if err != nil {
		return metrics.ScheduledRefreshOutcomeError
	}
	if connection.Status == StatusExpired {
		return metrics.ScheduledRefreshOutcomeExpired
	}
	return metrics.ScheduledRefreshOutcomeRefreshed
}
