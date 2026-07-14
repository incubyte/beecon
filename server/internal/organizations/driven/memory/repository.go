// Package memory holds the in-memory driven adapter for the organizations
// module: the test-substitution Repository and the NewFacadeWithOverrides
// seam.
package memory

import (
	"context"
	"sort"
	"sync"

	"beecon/internal/organizations"
)

// Repository is an in-memory organizations.Repository,
// organizations.UserRepository, and organizations.GovernanceRepository for
// tests.
type Repository struct {
	mu              sync.RWMutex
	byID            map[organizations.OrgID]organizations.Organization
	usersByID       map[organizations.UserID]organizations.User
	governanceByOrg map[organizations.OrgID]organizations.Governance
}

var _ organizations.Repository = (*Repository)(nil)
var _ organizations.UserRepository = (*Repository)(nil)
var _ organizations.GovernanceRepository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		byID:            map[organizations.OrgID]organizations.Organization{},
		usersByID:       map[organizations.UserID]organizations.User{},
		governanceByOrg: map[organizations.OrgID]organizations.Governance{},
	}
}

func (r *Repository) Save(_ context.Context, org organizations.Organization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[org.ID] = org
	return nil
}

func (r *Repository) FindByID(_ context.Context, id organizations.OrgID) (*organizations.Organization, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	org, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	copied := org
	return &copied, nil
}

func (r *Repository) Update(_ context.Context, org organizations.Organization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[org.ID] = org
	return nil
}

// ListAll returns every organization known to the repository, newest first
// (created_at DESC, id DESC as a deterministic tiebreaker), matching cursor
// and limited to limit rows — mirroring the bun Repository's own ordering
// and cursor semantics (Slice 1, PD40).
func (r *Repository) ListAll(_ context.Context, cursor *organizations.ListAllCursor, limit int) ([]organizations.Organization, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]organizations.Organization, 0, len(r.byID))
	for _, org := range r.byID {
		if cursor == nil || isAfterListAllCursorNewestFirst(org, *cursor) {
			items = append(items, org)
		}
	}
	sortOrganizationsNewestFirst(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// isAfterListAllCursorNewestFirst reports whether org sorts strictly after
// cursor in the newest-first (created_at DESC, id DESC) ordering — i.e.
// belongs on the page following the one cursor was minted from.
func isAfterListAllCursorNewestFirst(org organizations.Organization, cursor organizations.ListAllCursor) bool {
	if org.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	if org.CreatedAt.Equal(cursor.CreatedAt) {
		return org.ID < cursor.ID
	}
	return false
}

func sortOrganizationsNewestFirst(items []organizations.Organization) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
}

func (r *Repository) SaveUser(_ context.Context, user organizations.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usersByID[user.ID] = user
	return nil
}

func (r *Repository) FindUserByID(_ context.Context, org organizations.OrgID, id organizations.UserID) (*organizations.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.usersByID[id]
	if !ok || user.OrgID != org {
		return nil, nil
	}
	copied := user
	return &copied, nil
}

// ListByOrg returns every user belonging to org, newest first (created_at
// DESC, id DESC as a deterministic tiebreaker), matching cursor and limited
// to limit rows — mirroring the bun Repository's own ordering and cursor
// semantics (Slice 4, PD40).
func (r *Repository) ListByOrg(_ context.Context, org organizations.OrgID, cursor *organizations.UserListCursor, limit int) ([]organizations.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]organizations.User, 0)
	for _, user := range r.usersByID {
		if user.OrgID != org {
			continue
		}
		if cursor == nil || isAfterUserListCursorNewestFirst(user, *cursor) {
			items = append(items, user)
		}
	}
	sortUsersNewestFirst(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// isAfterUserListCursorNewestFirst reports whether user sorts strictly
// after cursor in the newest-first (created_at DESC, id DESC) ordering —
// mirrors isAfterListAllCursorNewestFirst for organizations.
func isAfterUserListCursorNewestFirst(user organizations.User, cursor organizations.UserListCursor) bool {
	if user.CreatedAt.Before(cursor.CreatedAt) {
		return true
	}
	if user.CreatedAt.Equal(cursor.CreatedAt) {
		return user.ID < cursor.ID
	}
	return false
}

func sortUsersNewestFirst(items []organizations.User) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
}

// FindByOrg returns org's governance row, or (nil, nil) when org has never
// been configured (Slice 5) — mirrors the bun Repository's own semantics.
func (r *Repository) FindByOrg(_ context.Context, org organizations.OrgID) (*organizations.Governance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	governance, ok := r.governanceByOrg[org]
	if !ok {
		return nil, nil
	}
	copied := governance
	return &copied, nil
}

// SaveGovernance upserts org's governance row (Slice 5).
func (r *Repository) SaveGovernance(_ context.Context, governance organizations.Governance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.governanceByOrg[governance.OrgID] = governance
	return nil
}
