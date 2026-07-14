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
// tests. endpoints is keyed by EndpointID, not OrgID (Slice 8: many
// endpoints per org now — the persisted row's own primary key), mirroring
// the bun Repository's own table schema.
type Repository struct {
	mu        sync.RWMutex
	endpoints map[delivery.EndpointID]delivery.Endpoint
	events    map[delivery.EventID]*eventRecord
}

var _ delivery.Repository = (*Repository)(nil)
var _ delivery.WorkQueue = (*Repository)(nil)
var _ delivery.OutboxStats = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		endpoints: map[delivery.EndpointID]delivery.Endpoint{},
		events:    map[delivery.EventID]*eventRecord{},
	}
}

func (r *Repository) SaveEndpoint(_ context.Context, endpoint delivery.Endpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endpoints[endpoint.ID] = endpoint
	return nil
}

// FindEndpoint returns org's first/oldest endpoint — mirrors the bun
// Repository's own "order by created_at, id, take the first" alias target
// (Slice 8).
func (r *Repository) FindEndpoint(_ context.Context, org organizations.OrgID) (*delivery.Endpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	endpoints := endpointsForOrg(r.endpoints, org)
	if len(endpoints) == 0 {
		return nil, nil
	}
	sortEndpointsOldestFirst(endpoints)
	first := endpoints[0]
	return &first, nil
}

func (r *Repository) FindEndpointByID(_ context.Context, org organizations.OrgID, id delivery.EndpointID) (*delivery.Endpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	endpoint, ok := r.endpoints[id]
	if !ok || endpoint.OrgID != org {
		return nil, nil
	}
	copied := endpoint
	return &copied, nil
}

func (r *Repository) ListEndpoints(_ context.Context, org organizations.OrgID) ([]delivery.Endpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	endpoints := endpointsForOrg(r.endpoints, org)
	sortEndpointsOldestFirst(endpoints)
	return endpoints, nil
}

func (r *Repository) DeleteEndpoint(_ context.Context, org organizations.OrgID, id delivery.EndpointID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if endpoint, ok := r.endpoints[id]; ok && endpoint.OrgID == org {
		delete(r.endpoints, id)
	}
	return nil
}

func endpointsForOrg(all map[delivery.EndpointID]delivery.Endpoint, org organizations.OrgID) []delivery.Endpoint {
	matches := make([]delivery.Endpoint, 0, len(all))
	for _, endpoint := range all {
		if endpoint.OrgID == org {
			matches = append(matches, endpoint)
		}
	}
	return matches
}

func sortEndpointsOldestFirst(endpoints []delivery.Endpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		if !endpoints[i].CreatedAt.Equal(endpoints[j].CreatedAt) {
			return endpoints[i].CreatedAt.Before(endpoints[j].CreatedAt)
		}
		return endpoints[i].ID < endpoints[j].ID
	})
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

// PurgeTerminalOlderThan hard-deletes org's own events whose Status is one
// of delivery.TerminalStatuses AND whose CreatedAt is strictly before
// cutoff (Slice 7, PD44) — mirroring the bun Repository's own predicate: a
// PENDING event's status never matches delivery.IsTerminal, so it is never
// removed here, at any age.
func (r *Repository) PurgeTerminalOlderThan(_ context.Context, org organizations.OrgID, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	purged := 0
	for id, record := range r.events {
		if record.event.OrgID != org {
			continue
		}
		if !delivery.IsTerminal(record.event.Status) {
			continue
		}
		if !record.event.CreatedAt.Before(cutoff) {
			continue
		}
		delete(r.events, id)
		purged++
	}
	return purged, nil
}

func sortEventsNewestFirst(events []delivery.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.After(events[j].CreatedAt)
		}
		return events[i].ID > events[j].ID
	})
}
