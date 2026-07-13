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

// Repository is an in-memory access.Repository, access.PrefixLookup, and
// access.ApiKeySecrets for tests.
type Repository struct {
	mu      sync.RWMutex
	keys    map[access.KeyID]access.ServerApiKey
	secrets map[access.ApiKeySecretID]access.ApiKeySecret
}

var _ access.Repository = (*Repository)(nil)
var _ access.PrefixLookup = (*Repository)(nil)
var _ access.ApiKeySecrets = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{
		keys:    map[access.KeyID]access.ServerApiKey{},
		secrets: map[access.ApiKeySecretID]access.ApiKeySecret{},
	}
}

func (r *Repository) SaveKey(_ context.Context, key access.ServerApiKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key.ID] = key
	return nil
}

func (r *Repository) ListByOrg(_ context.Context, org organizations.OrgID) ([]access.ServerApiKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]access.ServerApiKey, 0)
	for _, key := range r.keys {
		if key.OrgID == org {
			keys = append(keys, key)
		}
	}
	sortKeysByCreatedAt(keys)
	return keys, nil
}

func (r *Repository) FindByID(_ context.Context, org organizations.OrgID, id access.KeyID) (*access.ServerApiKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.keys[id]
	if !ok || key.OrgID != org {
		return nil, nil
	}
	copied := key
	return &copied, nil
}

func (r *Repository) MarkRevoked(_ context.Context, org organizations.OrgID, id access.KeyID, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[id]
	if !ok || key.OrgID != org {
		return nil
	}
	revokedAtCopy := revokedAt
	key.RevokedAt = &revokedAtCopy
	r.keys[id] = key
	return nil
}

// Save persists secret. org is accepted for the same reason every
// access.ApiKeySecrets method is org-scoped in shape (Repository's own
// convention) even though this in-memory adapter has no need to filter by
// it: by the time Issue or Rotate calls Save, secret.KeyID already names a
// key that was resolved (or just minted) within org.
func (r *Repository) Save(_ context.Context, _ organizations.OrgID, secret access.ApiKeySecret) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets[secret.ID] = secret
	return nil
}

func (r *Repository) ListByKeyID(_ context.Context, _ organizations.OrgID, keyID access.KeyID) ([]access.ApiKeySecret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matches := make([]access.ApiKeySecret, 0)
	for _, secret := range r.secrets {
		if secret.KeyID == keyID {
			matches = append(matches, secret)
		}
	}
	sortSecretsByCreatedAt(matches)
	return matches, nil
}

func (r *Repository) MarkExpiring(_ context.Context, _ organizations.OrgID, id access.ApiKeySecretID, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	secret, ok := r.secrets[id]
	if !ok {
		return nil
	}
	expiresAtCopy := expiresAt
	secret.ExpiresAt = &expiresAtCopy
	r.secrets[id] = secret
	return nil
}

func (r *Repository) FindByPrefix(_ context.Context, prefix string) ([]access.ApiKeySecretCandidate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matches := make([]access.ApiKeySecretCandidate, 0)
	for _, secret := range r.secrets {
		if secret.LookupPrefix != prefix {
			continue
		}
		key, ok := r.keys[secret.KeyID]
		if !ok {
			continue
		}
		matches = append(matches, access.ApiKeySecretCandidate{
			KeyID:     key.ID,
			OrgID:     key.OrgID,
			RevokedAt: key.RevokedAt,
			Secret:    secret,
		})
	}
	sortCandidatesBySecretCreatedAt(matches)
	return matches, nil
}

func sortKeysByCreatedAt(keys []access.ServerApiKey) {
	sort.Slice(keys, func(i, j int) bool { return keys[i].CreatedAt.Before(keys[j].CreatedAt) })
}

// sortSecretsByCreatedAt orders secrets oldest-first, breaking a tie between
// two secrets sharing a CreatedAt (e.g. a test clock that didn't advance
// between Issue and Rotate) by ID, so iterating r.secrets' map in random
// order never flips their relative position from one call to the next. The
// domain layer (secret.go's activeSecretOf/rotationState) no longer derives
// meaning from this order — it exists for deterministic, reproducible
// listings, not correctness.
func sortSecretsByCreatedAt(secrets []access.ApiKeySecret) {
	sort.Slice(secrets, func(i, j int) bool {
		if !secrets[i].CreatedAt.Equal(secrets[j].CreatedAt) {
			return secrets[i].CreatedAt.Before(secrets[j].CreatedAt)
		}
		return secrets[i].ID < secrets[j].ID
	})
}

func sortCandidatesBySecretCreatedAt(candidates []access.ApiKeySecretCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].Secret.CreatedAt.Equal(candidates[j].Secret.CreatedAt) {
			return candidates[i].Secret.CreatedAt.Before(candidates[j].Secret.CreatedAt)
		}
		return candidates[i].Secret.ID < candidates[j].Secret.ID
	})
}
