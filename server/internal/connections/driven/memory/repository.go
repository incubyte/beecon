// Package memory holds the in-memory driven adapter for the connections
// module: the test-substitution Repository and the NewFacadeWithOverrides
// seam.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// Repository is an in-memory connections.Repository, connections.OAuthRepository,
// and connections.RefreshQueue for tests. refreshLeaseUntil/reconcileLeaseUntil
// track each claim's own lease, mirroring driven/bun's own lease columns.
type Repository struct {
	mu                  sync.RWMutex
	byID                map[connections.ConnectionID]connections.Connection
	states              map[string]connections.OAuthState
	refreshLeaseUntil   map[connections.ConnectionID]time.Time
	reconcileLeaseUntil map[connections.ConnectionID]time.Time
}

var _ connections.Repository = (*Repository)(nil)
var _ connections.OAuthRepository = (*Repository)(nil)
var _ connections.RefreshQueue = (*Repository)(nil)
var _ connections.StatusCounter = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		byID:                map[connections.ConnectionID]connections.Connection{},
		states:              map[string]connections.OAuthState{},
		refreshLeaseUntil:   map[connections.ConnectionID]time.Time{},
		reconcileLeaseUntil: map[connections.ConnectionID]time.Time{},
	}
}

func (r *Repository) Save(_ context.Context, connection connections.Connection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[connection.ID] = connection
	return nil
}

func (r *Repository) FindByID(_ context.Context, org organizations.OrgID, id connections.ConnectionID) (*connections.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	connection, ok := r.byID[id]
	if !ok || connection.OrgID != org {
		return nil, nil
	}
	copied := connection
	return &copied, nil
}

// Update persists connection and releases any refresh/reconcile claim lease
// held on it, mirroring driven/bun's own Update.
func (r *Repository) Update(_ context.Context, connection connections.Connection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[connection.ID] = connection
	delete(r.refreshLeaseUntil, connection.ID)
	delete(r.reconcileLeaseUntil, connection.ID)
	return nil
}

// TransitionStatus conditionally flips id from -> to (FD1).
func (r *Repository) TransitionStatus(_ context.Context, org organizations.OrgID, id connections.ConnectionID, from, to connections.Status) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connection, ok := r.byID[id]
	if !ok || connection.OrgID != org || connection.Status != from {
		return false, nil
	}
	connection.Status = to
	r.byID[id] = connection
	return true, nil
}

// ClaimDueRefresh leases up to limit ACTIVE connections whose TokenExpiresAt
// is within lead of now (or nil — a Phase-1-migrated row, treated as already
// due, mirroring needsRefresh's own self-heal rule), oldest-created first,
// not currently leased by anyone else.
func (r *Repository) ClaimDueRefresh(_ context.Context, now time.Time, lead, leaseTTL time.Duration, limit int) ([]connections.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := make([]connections.Connection, 0)
	for _, connection := range r.byID {
		if r.isDueForRefreshClaim(connection, now, lead) {
			due = append(due, connection)
		}
	}
	sortConnectionsOldestFirst(due)
	if len(due) > limit {
		due = due[:limit]
	}
	leaseUntil := now.Add(leaseTTL)
	for _, connection := range due {
		r.refreshLeaseUntil[connection.ID] = leaseUntil
	}
	return due, nil
}

func (r *Repository) isDueForRefreshClaim(connection connections.Connection, now time.Time, lead time.Duration) bool {
	if connection.Status != connections.StatusActive {
		return false
	}
	if connection.TokenExpiresAt != nil && connection.TokenExpiresAt.After(now.Add(lead)) {
		return false
	}
	if lease, leased := r.refreshLeaseUntil[connection.ID]; leased && lease.After(now) {
		return false
	}
	return true
}

// ClaimDueReconcile leases up to limit ACTIVE connections not probed within
// interval of now (ReconciledAt nil counts as due immediately), oldest-created
// first, not currently leased by anyone else.
func (r *Repository) ClaimDueReconcile(_ context.Context, now time.Time, interval, leaseTTL time.Duration, limit int) ([]connections.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := make([]connections.Connection, 0)
	for _, connection := range r.byID {
		if r.isDueForReconcileClaim(connection, now, interval) {
			due = append(due, connection)
		}
	}
	sortConnectionsOldestFirst(due)
	if len(due) > limit {
		due = due[:limit]
	}
	leaseUntil := now.Add(leaseTTL)
	for _, connection := range due {
		r.reconcileLeaseUntil[connection.ID] = leaseUntil
	}
	return due, nil
}

func (r *Repository) isDueForReconcileClaim(connection connections.Connection, now time.Time, interval time.Duration) bool {
	if connection.Status != connections.StatusActive {
		return false
	}
	if connection.ReconciledAt != nil && connection.ReconciledAt.After(now.Add(-interval)) {
		return false
	}
	if lease, leased := r.reconcileLeaseUntil[connection.ID]; leased && lease.After(now) {
		return false
	}
	return true
}

func sortConnectionsOldestFirst(items []connections.Connection) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
}

func (r *Repository) FindByConnectToken(_ context.Context, token string) (*connections.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, connection := range r.byID {
		if connection.ConnectToken == token {
			copied := connection
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *Repository) FindConnectionForCallback(_ context.Context, id connections.ConnectionID) (*connections.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	connection, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	copied := connection
	return &copied, nil
}

func (r *Repository) SaveState(_ context.Context, state connections.OAuthState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[state.State] = state
	return nil
}

func (r *Repository) FindState(_ context.Context, state string) (*connections.OAuthState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	found, ok := r.states[state]
	if !ok {
		return nil, nil
	}
	copied := found
	return &copied, nil
}

func (r *Repository) MarkStateConsumed(_ context.Context, state string, consumedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	found, ok := r.states[state]
	if !ok {
		return nil
	}
	found.ConsumedAt = &consumedAt
	r.states[state] = found
	return nil
}

// List returns Connections scoped to org (Slice 4, AC1), optionally
// narrowed to filter.UserID, newest first (created_at DESC, id DESC as a
// deterministic tiebreaker), limited to filter.Limit rows — mirroring the
// bun Repository's own ordering and cursor semantics.
func (r *Repository) List(_ context.Context, org organizations.OrgID, filter connections.ListFilter) ([]connections.Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]connections.Connection, 0, len(r.byID))
	for _, connection := range r.byID {
		if matchesListFilter(connection, org, filter) {
			matches = append(matches, connection)
		}
	}
	sortConnectionsNewestFirst(matches)
	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return matches, nil
}

func matchesListFilter(connection connections.Connection, org organizations.OrgID, filter connections.ListFilter) bool {
	if connection.OrgID != org {
		return false
	}
	if filter.UserID != "" && connection.UserID != filter.UserID {
		return false
	}
	if filter.Cursor != nil && !isAfterCursorInNewestFirstOrder(connection, *filter.Cursor) {
		return false
	}
	return true
}

// isAfterCursorInNewestFirstOrder reports whether connection sorts strictly
// after cursor in the newest-first (created_at DESC, id DESC) ordering —
// i.e. belongs on the page following the one cursor was minted from.
func isAfterCursorInNewestFirstOrder(connection connections.Connection, cursor connections.ListCursor) bool {
	if connection.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	if connection.CreatedAt.Equal(cursor.CreatedAt) {
		return connection.ID < cursor.ID
	}
	return false
}

func sortConnectionsNewestFirst(items []connections.Connection) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
}

// Delete permanently removes the row for id scoped to org (Slice 4, AC3): a
// cross-org or unknown id is a no-op — the facade has already turned that
// into ErrNotFound via a preceding FindByID.
func (r *Repository) Delete(_ context.Context, org organizations.OrgID, id connections.ConnectionID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if connection, ok := r.byID[id]; ok && connection.OrgID == org {
		delete(r.byID, id)
	}
	return nil
}

// CountByStatus returns the number of connections currently in each
// lifecycle status across every organization (PD38d) — mirroring the bun
// Repository's own installation-wide, deliberately unscoped query.
func (r *Repository) CountByStatus(_ context.Context) (map[connections.Status]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	counts := make(map[connections.Status]int)
	for _, connection := range r.byID {
		counts[connection.Status]++
	}
	return counts, nil
}
