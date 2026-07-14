// refreshOnce is PD36's per-connection refresh funnel: a hand-rolled keyed
// mutex serializes the scheduler, reconciliation, and request-path refresh so
// only one refresh_token grant runs per connection at a time; a caller that
// waits re-checks the freshly persisted state and reuses it instead of
// running a second grant.
package connections

import (
	"context"
	"sync"
	"time"

	"beecon/internal/organizations"
)

type refreshLocks struct {
	mu     sync.Mutex
	byConn map[ConnectionID]*sync.Mutex
}

func newRefreshLocks() *refreshLocks {
	return &refreshLocks{byConn: map[ConnectionID]*sync.Mutex{}}
}

func (l *refreshLocks) lockFor(id ConnectionID) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.byConn[id]
	if !ok {
		m = &sync.Mutex{}
		l.byConn[id] = m
	}
	return m
}

// refreshParams configures one refreshOnce call. Force skips the freshness
// gate entirely and always performs a grant (RefreshForExecution's reactive
// 401 refresh; ReconcileOnce's revocation check, reacting to direct evidence
// rather than a time-based predicate). When Force is false, Lead decides the
// gate: 0 means ResolveForExecution's strict check (only if the token has
// actually gone stale, needsRefresh); a positive Lead means the scheduler's
// own proactive window (needsProactiveRefresh) — so a caller that lost the
// lock race treats a connection another caller already refreshed past that
// same window as fresh too, keeping concurrent scheduler + request-path
// refreshes to exactly one grant regardless of which wins. DeniedReason is
// the connection.expired reason a permanent refusal records.
type refreshParams struct {
	force        bool
	lead         time.Duration
	deniedReason string
}

func (f *Facade) refreshOnce(ctx context.Context, org organizations.OrgID, id ConnectionID, params refreshParams) (Connection, error) {
	lock := f.refreshLocks.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	current, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return Connection{}, err
	}
	if current == nil {
		return Connection{}, ErrNotFound()
	}
	if current.Status != StatusActive {
		return *current, nil
	}
	if !params.force && !dueForRefresh(*current, f.now(), params.lead) {
		return *current, nil
	}
	return f.refreshConnection(ctx, *current, params.deniedReason)
}

// dueForRefresh is refreshOnce's non-forced gate: with no lead, the strict
// needsRefresh check; with a lead, the scheduler's own proactive
// needsProactiveRefresh check (see refreshParams's doc comment).
func dueForRefresh(connection Connection, now time.Time, lead time.Duration) bool {
	if lead <= 0 {
		return connection.needsRefresh(now)
	}
	return connection.needsProactiveRefresh(now, lead)
}
