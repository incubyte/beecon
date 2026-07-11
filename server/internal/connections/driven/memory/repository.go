// Package memory holds the in-memory driven adapter for the connections
// module: the test-substitution Repository and the NewFacadeWithOverrides
// seam.
package memory

import (
	"context"
	"sync"
	"time"

	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// Repository is an in-memory connections.Repository and
// connections.OAuthRepository for tests.
type Repository struct {
	mu     sync.RWMutex
	byID   map[connections.ConnectionID]connections.Connection
	states map[string]connections.OAuthState
}

var _ connections.Repository = (*Repository)(nil)
var _ connections.OAuthRepository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		byID:   map[connections.ConnectionID]connections.Connection{},
		states: map[string]connections.OAuthState{},
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

func (r *Repository) Update(_ context.Context, connection connections.Connection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[connection.ID] = connection
	return nil
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
