// Package memory holds the in-memory driven adapter for the logging module:
// the test-substitution Repository and the NewFacadeWithOverrides seam.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"beecon/internal/logging"
	"beecon/internal/organizations"
)

// Repository is an in-memory logging.Repository for tests.
type Repository struct {
	mu      sync.RWMutex
	entries []logging.EventLog
}

var _ logging.Repository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) Save(_ context.Context, entry logging.EventLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return nil
}

func (r *Repository) Query(_ context.Context, org organizations.OrgID, filter logging.Filter) ([]logging.EventLog, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]logging.EventLog, 0, len(r.entries))
	for _, entry := range r.entries {
		if matchesFilter(entry, org, filter) {
			matches = append(matches, entry)
		}
	}
	sortNewestFirst(matches)
	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return matches, nil
}

func matchesFilter(entry logging.EventLog, org organizations.OrgID, filter logging.Filter) bool {
	if entry.OrgID != org {
		return false
	}
	if filter.ConnectionID != "" && entry.ConnectionID != filter.ConnectionID {
		return false
	}
	if filter.UserID != "" && entry.UserID != filter.UserID {
		return false
	}
	if filter.ToolSlug != "" && entry.ToolSlug != filter.ToolSlug {
		return false
	}
	if filter.From != nil && entry.CreatedAt.Before(*filter.From) {
		return false
	}
	if filter.To != nil && entry.CreatedAt.After(*filter.To) {
		return false
	}
	if filter.Cursor != nil && !isAfterCursorInNewestFirstOrder(entry, *filter.Cursor) {
		return false
	}
	return true
}

// isAfterCursorInNewestFirstOrder reports whether entry sorts strictly after
// cursor in the newest-first (created_at DESC, id DESC) ordering — i.e.
// belongs on the page following the one cursor was minted from.
func isAfterCursorInNewestFirstOrder(entry logging.EventLog, cursor logging.Cursor) bool {
	if entry.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	if entry.CreatedAt.Equal(cursor.CreatedAt) {
		return entry.ID < cursor.ID
	}
	return false
}

// PurgeOlderThan hard-deletes org's own entries whose CreatedAt is strictly
// before cutoff (Slice 7, PD44), mirroring the bun Repository's own
// unconditional-by-age semantics.
func (r *Repository) PurgeOlderThan(_ context.Context, org organizations.OrgID, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := make([]logging.EventLog, 0, len(r.entries))
	purged := 0
	for _, entry := range r.entries {
		if entry.OrgID == org && entry.CreatedAt.Before(cutoff) {
			purged++
			continue
		}
		kept = append(kept, entry)
	}
	r.entries = kept
	return purged, nil
}

func sortNewestFirst(entries []logging.EventLog) {
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		}
		return entries[i].ID > entries[j].ID
	})
}
