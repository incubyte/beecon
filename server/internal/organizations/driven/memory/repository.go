// Package memory holds the in-memory driven adapter for the organizations
// module: the test-substitution Repository and the NewFacadeWithOverrides
// seam.
package memory

import (
	"context"
	"sync"

	"beecon/internal/organizations"
)

// Repository is an in-memory organizations.Repository and
// organizations.UserRepository for tests.
type Repository struct {
	mu        sync.RWMutex
	byID      map[organizations.OrgID]organizations.Organization
	usersByID map[organizations.UserID]organizations.User
}

var _ organizations.Repository = (*Repository)(nil)
var _ organizations.UserRepository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		byID:      map[organizations.OrgID]organizations.Organization{},
		usersByID: map[organizations.UserID]organizations.User{},
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
