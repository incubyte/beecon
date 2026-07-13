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

// UpdateEncryptedClientSecret persists the boot backfill's re-sealed
// ciphertext for id (PD17) and flips ClientSecretEncrypted to true. A miss is
// a silent no-op, mirroring the connections memory repository's own
// best-effort Update.
func (r *Repository) UpdateEncryptedClientSecret(_ context.Context, id catalog.IntegrationID, encryptedClientSecret string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	integration, ok := r.byID[id]
	if !ok {
		return nil
	}
	integration.ClientSecret = encryptedClientSecret
	integration.ClientSecretEncrypted = true
	r.byID[id] = integration
	return nil
}

func sortByCreatedAt(integrations []catalog.Integration) {
	sort.Slice(integrations, func(i, j int) bool {
		if integrations[i].CreatedAt.Equal(integrations[j].CreatedAt) {
			return integrations[i].ID < integrations[j].ID
		}
		return integrations[i].CreatedAt.Before(integrations[j].CreatedAt)
	})
}
