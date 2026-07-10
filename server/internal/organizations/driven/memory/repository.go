// Package memory holds the in-memory driven adapter for the organizations
// module: the test-substitution Repository and the NewFacadeWithOverrides
// seam.
package memory

import (
	"context"
	"sync"

	"beecon/internal/organizations"
)

// Repository is an in-memory organizations.Repository for tests.
type Repository struct {
	mu   sync.RWMutex
	byID map[organizations.OrgID]organizations.Organization
}

var _ organizations.Repository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{byID: map[organizations.OrgID]organizations.Organization{}}
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
