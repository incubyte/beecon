package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// WebhookSecretRepository is an in-memory access.WebhookSecrets for tests.
type WebhookSecretRepository struct {
	mu      sync.RWMutex
	secrets map[access.WebhookSecretID]access.WebhookSigningSecret
}

var _ access.WebhookSecrets = (*WebhookSecretRepository)(nil)

func NewWebhookSecretRepository() *WebhookSecretRepository {
	return &WebhookSecretRepository{secrets: map[access.WebhookSecretID]access.WebhookSigningSecret{}}
}

func (r *WebhookSecretRepository) Save(_ context.Context, secret access.WebhookSigningSecret) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets[secret.ID] = secret
	return nil
}

// ListByEndpoint returns org's secrets scoped to one specific endpoint
// (Slice 8), mirroring the bun repository's own filter.
func (r *WebhookSecretRepository) ListByEndpoint(_ context.Context, org organizations.OrgID, endpoint access.EndpointID) ([]access.WebhookSigningSecret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matches := make([]access.WebhookSigningSecret, 0)
	for _, secret := range r.secrets {
		if secret.OrgID == org && secret.EndpointID == endpoint {
			matches = append(matches, secret)
		}
	}
	sortWebhookSecretsByCreatedAt(matches)
	return matches, nil
}

func (r *WebhookSecretRepository) MarkExpiring(_ context.Context, _ organizations.OrgID, id access.WebhookSecretID, expiresAt time.Time) error {
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

// sortWebhookSecretsByCreatedAt orders secrets oldest-first, breaking a tie
// between two secrets sharing a CreatedAt by ID, mirroring
// repository.go's sortSecretsByCreatedAt for ApiKeySecret.
func sortWebhookSecretsByCreatedAt(secrets []access.WebhookSigningSecret) {
	sort.Slice(secrets, func(i, j int) bool {
		if !secrets[i].CreatedAt.Equal(secrets[j].CreatedAt) {
			return secrets[i].CreatedAt.Before(secrets[j].CreatedAt)
		}
		return secrets[i].ID < secrets[j].ID
	})
}
