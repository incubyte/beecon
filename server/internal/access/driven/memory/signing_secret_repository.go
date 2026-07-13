package memory

import (
	"context"
	"sort"
	"sync"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// SigningSecretRepository is an in-memory access.SigningSecrets and
// access.SigningSecretLookup for tests. It is a separate type from
// Repository (not additional methods on it): access.Repository and
// access.SigningSecrets both declare a Save(ctx, entity) method, and
// access.PrefixLookup/access.SigningSecretLookup both key off a different id
// shape — Go has no method overloading, so the two storage shapes need their
// own types.
type SigningSecretRepository struct {
	mu   sync.RWMutex
	byID map[access.SigningSecretID]access.SigningSecret
}

var _ access.SigningSecrets = (*SigningSecretRepository)(nil)
var _ access.SigningSecretLookup = (*SigningSecretRepository)(nil)

func NewSigningSecretRepository() *SigningSecretRepository {
	return &SigningSecretRepository{byID: map[access.SigningSecretID]access.SigningSecret{}}
}

func (r *SigningSecretRepository) Save(_ context.Context, secret access.SigningSecret) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[secret.ID] = secret
	return nil
}

func (r *SigningSecretRepository) ListByOrg(_ context.Context, org organizations.OrgID) ([]access.SigningSecret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	secrets := make([]access.SigningSecret, 0)
	for _, secret := range r.byID {
		if secret.OrgID == org {
			secrets = append(secrets, secret)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].CreatedAt.Before(secrets[j].CreatedAt) })
	return secrets, nil
}

func (r *SigningSecretRepository) FindByKid(_ context.Context, id access.SigningSecretID) (*access.SigningSecret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	secret, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	copied := secret
	return &copied, nil
}
