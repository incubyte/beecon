// Package memory holds the in-memory driven adapter for the connections
// module: the test-substitution Repository and the NewFacadeWithOverrides
// seam.
package memory

import (
	"context"
	"sync"

	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// Repository is an in-memory connections.Repository for tests.
type Repository struct {
	mu   sync.RWMutex
	byID map[connections.ConnectionID]connections.Connection
}

var _ connections.Repository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{byID: map[connections.ConnectionID]connections.Connection{}}
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
