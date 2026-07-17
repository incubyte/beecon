// Package memory holds the in-memory driven adapter for the triggers
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
	"beecon/internal/triggers"
)

// Repository is an in-memory triggers.Repository and triggers.PollQueue for
// tests.
type Repository struct {
	mu   sync.RWMutex
	byID map[triggers.TriggerInstanceID]triggers.TriggerInstance

	leaseUntil map[triggers.TriggerInstanceID]time.Time
}

var _ triggers.Repository = (*Repository)(nil)
var _ triggers.PollQueue = (*Repository)(nil)
var _ triggers.TriggerSlugIndex = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		byID:       map[triggers.TriggerInstanceID]triggers.TriggerInstance{},
		leaseUntil: map[triggers.TriggerInstanceID]time.Time{},
	}
}

// Save inserts a freshly created TriggerInstance, or overwrites an existing
// one by id — the same upsert shape the bun adapter provides via "ON
// CONFLICT (id) DO UPDATE" (triggers.Repository declares no separate
// Update). A facade-level Save always releases whatever poll lease
// ClaimDuePolls last set, mirroring the bun adapter's own convention.
func (r *Repository) Save(_ context.Context, instance triggers.TriggerInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[instance.ID] = instance
	delete(r.leaseUntil, instance.ID)
	return nil
}

// ClaimDuePolls leases up to limit due ACTIVE TriggerInstances, oldest-
// created first — the in-memory mirror of the bun adapter's dual-dialect
// claim query (section 3 of the architecture doc).
func (r *Repository) ClaimDuePolls(_ context.Context, now time.Time, leaseTTL time.Duration, limit int) ([]triggers.TriggerInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var due []triggers.TriggerInstance
	for _, instance := range r.byID {
		if r.isDueForPoll(instance, now) {
			due = append(due, instance)
		}
	}
	sortInstancesOldestFirst(due)
	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	for _, instance := range due {
		r.leaseUntil[instance.ID] = now.Add(leaseTTL)
	}
	return due, nil
}

func (r *Repository) isDueForPoll(instance triggers.TriggerInstance, now time.Time) bool {
	if instance.Status != triggers.StatusActive || instance.NextPollAt == nil {
		return false
	}
	if instance.NextPollAt.After(now) {
		return false
	}
	leased, ok := r.leaseUntil[instance.ID]
	return !ok || leased.Before(now)
}

func sortInstancesOldestFirst(items []triggers.TriggerInstance) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
}

// ListByTriggerSlug returns every TriggerInstance bound to triggerSlug,
// across every organization (Phase 5 registry sub-phase Slice 4, PD66) —
// mirrors the bun Repository's own installation-level query.
func (r *Repository) ListByTriggerSlug(_ context.Context, triggerSlug string) ([]triggers.TriggerInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matches []triggers.TriggerInstance
	for _, instance := range r.byID {
		if instance.TriggerSlug == triggerSlug {
			matches = append(matches, instance)
		}
	}
	sortInstancesOldestFirst(matches)
	return matches, nil
}

func (r *Repository) FindByID(_ context.Context, org organizations.OrgID, id triggers.TriggerInstanceID) (*triggers.TriggerInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	instance, ok := r.byID[id]
	if !ok || instance.OrgID != org {
		return nil, nil
	}
	copied := instance
	return &copied, nil
}

// ListPage returns TriggerInstances scoped to org, optionally narrowed to
// filter.ConnectionID and/or filter.UserID, newest first (created_at DESC,
// id DESC as a deterministic tiebreaker), limited to filter.Limit rows —
// mirroring the bun Repository's own ordering and cursor semantics.
func (r *Repository) ListPage(_ context.Context, org organizations.OrgID, filter triggers.ListFilter) ([]triggers.TriggerInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]triggers.TriggerInstance, 0, len(r.byID))
	for _, instance := range r.byID {
		if matchesListFilter(instance, org, filter) {
			matches = append(matches, instance)
		}
	}
	sortInstancesNewestFirst(matches)
	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return matches, nil
}

func matchesListFilter(instance triggers.TriggerInstance, org organizations.OrgID, filter triggers.ListFilter) bool {
	if instance.OrgID != org {
		return false
	}
	if filter.ConnectionID != "" && instance.ConnectionID != filter.ConnectionID {
		return false
	}
	if filter.UserID != "" && instance.UserID != filter.UserID {
		return false
	}
	if filter.Cursor != nil && !isAfterCursorInNewestFirstOrder(instance, *filter.Cursor) {
		return false
	}
	return true
}

// isAfterCursorInNewestFirstOrder reports whether instance sorts strictly
// after cursor in the newest-first (created_at DESC, id DESC) ordering —
// i.e. belongs on the page following the one cursor was minted from.
func isAfterCursorInNewestFirstOrder(instance triggers.TriggerInstance, cursor triggers.ListCursor) bool {
	if instance.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	if instance.CreatedAt.Equal(cursor.CreatedAt) {
		return instance.ID < cursor.ID
	}
	return false
}

func sortInstancesNewestFirst(items []triggers.TriggerInstance) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
}

// Delete permanently removes the row for id scoped to org (PD33): a
// cross-org or unknown id is a no-op — the facade has already turned that
// into ErrNotFound via a preceding FindByID.
func (r *Repository) Delete(_ context.Context, org organizations.OrgID, id triggers.TriggerInstanceID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if instance, ok := r.byID[id]; ok && instance.OrgID == org {
		delete(r.byID, id)
		delete(r.leaseUntil, id)
	}
	return nil
}

// DeleteByConnection permanently removes every instance bound to connID
// scoped to org (PD33's connection-delete cascade); a connection with no
// instances is a no-op.
func (r *Repository) DeleteByConnection(_ context.Context, org organizations.OrgID, connID connections.ConnectionID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, instance := range r.byID {
		if instance.OrgID == org && instance.ConnectionID == connID {
			delete(r.byID, id)
			delete(r.leaseUntil, id)
		}
	}
	return nil
}
