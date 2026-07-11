// Package memory holds the in-memory driven adapter for the access module:
// the test-substitution Repository and the NewFacadeWithOverrides seam.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// Repository is an in-memory access.Repository and access.PrefixLookup for
// tests.
type Repository struct {
	mu   sync.RWMutex
	byID map[access.KeyID]access.ServerApiKey
}

var _ access.Repository = (*Repository)(nil)
var _ access.PrefixLookup = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{byID: map[access.KeyID]access.ServerApiKey{}}
}

func (r *Repository) Save(_ context.Context, key access.ServerApiKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[key.ID] = key
	return nil
}

func (r *Repository) ListByOrg(_ context.Context, org organizations.OrgID) ([]access.ServerApiKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]access.ServerApiKey, 0)
	for _, key := range r.byID {
		if key.OrgID == org {
			keys = append(keys, key)
		}
	}
	sortByCreatedAt(keys)
	return keys, nil
}

func (r *Repository) FindByID(_ context.Context, org organizations.OrgID, id access.KeyID) (*access.ServerApiKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.byID[id]
	if !ok || key.OrgID != org {
		return nil, nil
	}
	copied := key
	return &copied, nil
}

func (r *Repository) MarkRevoked(_ context.Context, org organizations.OrgID, id access.KeyID, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.byID[id]
	if !ok || key.OrgID != org {
		return nil
	}
	revokedAtCopy := revokedAt
	key.RevokedAt = &revokedAtCopy
	r.byID[id] = key
	return nil
}

func (r *Repository) FindByPrefix(_ context.Context, prefix string) ([]access.ServerApiKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matches := make([]access.ServerApiKey, 0)
	for _, key := range r.byID {
		if key.LookupPrefix == prefix {
			matches = append(matches, key)
		}
	}
	sortByCreatedAt(matches)
	return matches, nil
}

func sortByCreatedAt(keys []access.ServerApiKey) {
	sort.Slice(keys, func(i, j int) bool { return keys[i].CreatedAt.Before(keys[j].CreatedAt) })
}
