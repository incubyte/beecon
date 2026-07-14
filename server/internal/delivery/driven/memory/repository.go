// Package memory holds the in-memory driven adapter for the delivery
// module: the test-substitution Repository/WorkQueue and the
// NewFacadeWithOverrides seam.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"beecon/internal/delivery"
	"beecon/internal/organizations"
)

// eventRecord pairs a delivery.Event with its claim lease — leaseUntil is
// deliberately not part of delivery.Event itself (SaveEvent always means
// "this attempt is over," releasing any lease in the same breath; only
// ClaimDue ever sets one), mirroring driven/bun/repository.go's own
// EventRow.LeaseUntil convention.
type eventRecord struct {
	event      delivery.Event
	leaseUntil *time.Time
}

// Repository is an in-memory delivery.Repository and delivery.WorkQueue for
// tests.
type Repository struct {
	mu        sync.RWMutex
	endpoints map[organizations.OrgID]delivery.Endpoint
	events    map[delivery.EventID]*eventRecord
}

var _ delivery.Repository = (*Repository)(nil)
var _ delivery.WorkQueue = (*Repository)(nil)
var _ delivery.OutboxStats = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		endpoints: map[organizations.OrgID]delivery.Endpoint{},
		events:    map[delivery.EventID]*eventRecord{},
	}
}

func (r *Repository) SaveEndpoint(_ context.Context, endpoint delivery.Endpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endpoints[endpoint.OrgID] = endpoint
	return nil
}

func (r *Repository) FindEndpoint(_ context.Context, org organizations.OrgID) (*delivery.Endpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	endpoint, ok := r.endpoints[org]
	if !ok {
		return nil, nil
	}
	copied := endpoint
	return &copied, nil
}

func (r *Repository) SaveEvent(_ context.Context, event delivery.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.ID] = &eventRecord{event: event}
	return nil
}

func (r *Repository) FindEvent(_ context.Context, org organizations.OrgID, id delivery.EventID) (*delivery.Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.events[id]
	if !ok || record.event.OrgID != org {
		return nil, nil
	}
	copied := record.event
	return &copied, nil
}

// ListEventsPage returns Events scoped to org, optionally narrowed to
// filter.Type and/or filter.DeliveryStatus, newest first (created_at DESC,
// id DESC as a deterministic tiebreaker), limited to filter.Limit rows —
// mirroring the bun Repository's own ordering and cursor semantics.
func (r *Repository) ListEventsPage(_ context.Context, org organizations.OrgID, filter delivery.ListFilter) ([]delivery.Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]delivery.Event, 0, len(r.events))
	for _, record := range r.events {
		if matchesEventFilter(record.event, org, filter) {
			matches = append(matches, record.event)
		}
	}
	sortEventsNewestFirst(matches)
	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return matches, nil
}

// ClaimDue leases up to limit due events, oldest-created first, mirroring
// the bun Repository's dialect-agnostic claim predicate: PENDING, due
// (NextAttemptAt <= now), and not currently leased by anyone else.
func (r *Repository) ClaimDue(_ context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]delivery.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := make([]*eventRecord, 0)
	for _, record := range r.events {
		if isDueForClaim(record, now) {
			due = append(due, record)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].event.CreatedAt.Before(due[j].event.CreatedAt) })
	if len(due) > limit {
		due = due[:limit]
	}

	leaseUntil := now.Add(leaseTTL)
	claimed := make([]delivery.Event, 0, len(due))
	for _, record := range due {
		record.leaseUntil = &leaseUntil
		claimed = append(claimed, record.event)
	}
	return claimed, nil
}

func isDueForClaim(record *eventRecord, now time.Time) bool {
	if record.event.Status != delivery.StatusPending {
		return false
	}
	if record.event.NextAttemptAt.After(now) {
		return false
	}
	if record.leaseUntil != nil && record.leaseUntil.After(now) {
		return false
	}
	return true
}

func matchesEventFilter(event delivery.Event, org organizations.OrgID, filter delivery.ListFilter) bool {
	if event.OrgID != org {
		return false
	}
	if filter.Type != "" && event.Type != filter.Type {
		return false
	}
	if filter.DeliveryStatus != "" && event.Status != filter.DeliveryStatus {
		return false
	}
	if filter.Cursor != nil && !isAfterCursorInNewestFirstOrder(event, *filter.Cursor) {
		return false
	}
	return true
}

// isAfterCursorInNewestFirstOrder reports whether event sorts strictly
// after cursor in the newest-first (created_at DESC, id DESC) ordering —
// i.e. belongs on the page following the one cursor was minted from.
func isAfterCursorInNewestFirstOrder(event delivery.Event, cursor delivery.ListCursor) bool {
	if event.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	if event.CreatedAt.Equal(cursor.CreatedAt) {
		return event.ID < cursor.ID
	}
	return false
}

// PendingDepthAndOldestAge returns how many PENDING events exist right now
// and the age of the oldest one (PD38d) — mirroring the bun Repository's own
// installation-wide, deliberately unscoped query.
func (r *Repository) PendingDepthAndOldestAge(_ context.Context, now time.Time) (int, time.Duration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	depth := 0
	var oldest time.Time
	for _, record := range r.events {
		if record.event.Status != delivery.StatusPending {
			continue
		}
		depth++
		if oldest.IsZero() || record.event.CreatedAt.Before(oldest) {
			oldest = record.event.CreatedAt
		}
	}
	if depth == 0 {
		return 0, 0, nil
	}
	return depth, now.Sub(oldest), nil
}

func sortEventsNewestFirst(events []delivery.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.After(events[j].CreatedAt)
		}
		return events[i].ID > events[j].ID
	})
}
