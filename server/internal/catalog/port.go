package catalog

import "context"

// Repository is the catalog module's driven port for the Integration entity.
// Integrations are installation-level, not org-scoped (PD7: visible to every
// organization in the installation) — there is no organization id to filter
// by, mirroring organizations.Repository's own installation-level scope.
// FindByID returns (nil, nil) on a miss; the facade translates that into
// ErrIntegrationNotFound.
type Repository interface {
	Save(ctx context.Context, integration Integration) error
	FindByID(ctx context.Context, id IntegrationID) (*Integration, error)
	ListAll(ctx context.Context) ([]Integration, error)
}
