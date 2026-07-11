// Package memory holds the in-memory driven adapter for the catalog module:
// the test-substitution Repository and the NewFacadeWithOverrides seam.
package memory

import (
	"context"
	"sort"
	"sync"

	"beecon/internal/catalog"
)

// Repository is an in-memory catalog.Repository for tests.
type Repository struct {
	mu   sync.RWMutex
	byID map[catalog.IntegrationID]catalog.Integration
}

var _ catalog.Repository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{byID: map[catalog.IntegrationID]catalog.Integration{}}
}

func (r *Repository) Save(_ context.Context, integration catalog.Integration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[integration.ID] = integration
	return nil
}

func (r *Repository) FindByID(_ context.Context, id catalog.IntegrationID) (*catalog.Integration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	integration, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	copied := integration
	return &copied, nil
}

func (r *Repository) ListAll(_ context.Context) ([]catalog.Integration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	integrations := make([]catalog.Integration, 0, len(r.byID))
	for _, integration := range r.byID {
		integrations = append(integrations, integration)
	}
	sortByCreatedAt(integrations)
	return integrations, nil
}

func sortByCreatedAt(integrations []catalog.Integration) {
	sort.Slice(integrations, func(i, j int) bool {
		if integrations[i].CreatedAt.Equal(integrations[j].CreatedAt) {
			return integrations[i].ID < integrations[j].ID
		}
		return integrations[i].CreatedAt.Before(integrations[j].CreatedAt)
	})
}
