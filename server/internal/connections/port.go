package connections

import (
	"context"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

// Repository is the connections module's org-scoped driven port. Every
// method takes the owning OrgID as its second parameter, so a query without
// org scope cannot be expressed. FindByID returns (nil, nil) on a miss
// (including a connection that belongs to a different organization); the
// facade translates that into ErrNotFound.
type Repository interface {
	Save(ctx context.Context, connection Connection) error
	FindByID(ctx context.Context, org organizations.OrgID, id ConnectionID) (*Connection, error)
}

// OrganizationReader is a narrow, consumer-defined port satisfied by
// *organizations.Facade: Initiate needs the organization's redirect-uri
// allow-list (PD4) to validate the requested redirectUri.
type OrganizationReader interface {
	Get(ctx context.Context, id organizations.OrgID) (organizations.Organization, error)
}

// UserReader is a narrow, consumer-defined port satisfied by
// *organizations.Facade: Initiate must reject an unknown userId, or a userId
// belonging to another organization, as not-found (PD5).
type UserReader interface {
	GetUser(ctx context.Context, org organizations.OrgID, id organizations.UserID) (organizations.User, error)
}

// IntegrationReader is a narrow, consumer-defined port satisfied by
// *catalog.Facade: Initiate must reject an unknown integrationId, and needs
// the integration's provider slug to record on the Connection. Integrations
// are installation-level (PD7), so this reader takes no organization id.
type IntegrationReader interface {
	GetIntegration(ctx context.Context, id catalog.IntegrationID) (catalog.Integration, error)
}
