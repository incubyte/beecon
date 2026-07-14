package organizations

import (
	"context"
	"time"
)

// ListAllCursor is the decoded pagination cursor ListAll's driven port
// accepts (Slice 1, PD40): the created_at/id pair of the last organization
// on the previous page, so the next page resumes strictly after it in the
// newest-first ordering.
type ListAllCursor struct {
	CreatedAt time.Time
	ID        OrgID
}

// Repository is the organizations module's driven port for the
// installation-level Organization entity. FindByID returns (nil, nil) on a
// miss; the facade translates that into ErrNotFound. Organization lookup is
// installation-level, not org-scoped — there is no wider scope to filter by.
// ListAll is the same installation-level shape (Slice 1, PD40): an
// operator-only view over every organization, ordered created_at,id newest
// first, matching the pagination cursor and limit.
type Repository interface {
	Save(ctx context.Context, org Organization) error
	FindByID(ctx context.Context, id OrgID) (*Organization, error)
	Update(ctx context.Context, org Organization) error
	ListAll(ctx context.Context, cursor *ListAllCursor, limit int) ([]Organization, error)
}

// UserListCursor is the decoded pagination cursor ListByOrg's driven port
// accepts (Slice 4, PD40): the created_at/id pair of the last user on the
// previous page, so the next page resumes strictly after it in the
// newest-first ordering — mirroring ListAllCursor's own shape.
type UserListCursor struct {
	CreatedAt time.Time
	ID        UserID
}

// UserRepository is the organizations module's driven port for the
// org-scoped User entity. Every method takes the owning OrgID as its second
// parameter, so a query without org scope cannot be expressed. FindUserByID
// returns (nil, nil) on a miss (including a user that belongs to a different
// organization); the facade translates that into ErrUserNotFound. ListByOrg
// (Slice 4, PD40) is the org-scoped end-user listing the Admin UI's
// end-users area reads: every organization's members, ordered
// created_at,id newest first, matching the pagination cursor and limit —
// the same installation-level shape as Repository.ListAll, but scoped to
// one organization instead of the whole installation.
type UserRepository interface {
	SaveUser(ctx context.Context, user User) error
	FindUserByID(ctx context.Context, org OrgID, id UserID) (*User, error)
	ListByOrg(ctx context.Context, org OrgID, cursor *UserListCursor, limit int) ([]User, error)
}

// GovernanceRepository is the organizations module's driven port for the
// per-org Governance settings record (Slice 5, PD42/PD43). FindByOrg returns
// (nil, nil) when org has never been configured — GetGovernance synthesizes
// the continuity-preserving default in that case (PD42), so callers never
// see a nil Governance. Save is an upsert (create-or-replace): every org
// starts with no row, and SetGovernance always replaces the whole record,
// mirroring SetAllowedRedirectURIs' own whole-replace convention.
type GovernanceRepository interface {
	FindByOrg(ctx context.Context, org OrgID) (*Governance, error)
	SaveGovernance(ctx context.Context, governance Governance) error
}

// GovernanceReader is a narrow port satisfied directly by *Facade, consumed
// by catalog (Slice 5's core-risk seam): catalog already depends on
// organizations (it already takes organizations.OrgID in ListTools/
// ListTriggerDefinitions), so catalog references this interface directly —
// no consumer-defined-port-plus-app-adapter indirection is needed, unlike
// connections' narrow reader ports, which exist precisely to avoid a new
// module-dependency edge organizations->connections would otherwise require.
type GovernanceReader interface {
	GetGovernance(ctx context.Context, org OrgID) (Governance, error)
}

// EndpointPorter is organizations' consumer-defined port onto delivery's
// webhook endpoints (Slice 9, PD46): organizations never imports delivery
// (BOUNDARIES — the module dependency graph has no organizations->delivery
// edge) — the composition root wires a concrete adapter over
// *delivery.Facade (app/endpoint_porter.go), the same
// consumer-defined-port-plus-app-adapter shape triggersEventSink/
// connectionsEventSink already use for the opposite direction. ExportConfig
// reads org's endpoints through ListEndpoints — URL and event-type filter
// only, never a secret; ImportConfig creates/updates/deletes through the
// other three, always minting a fresh secret at creation (returned exactly
// once — an import document never carries a secret to reuse, PD46).
type EndpointPorter interface {
	ListEndpoints(ctx context.Context, org OrgID) ([]PortedEndpoint, error)
	CreateEndpoint(ctx context.Context, org OrgID, url string, eventTypes []string) (PortedEndpointSecret, error)
	UpdateEndpoint(ctx context.Context, org OrgID, endpointID, url string, eventTypes []string) error
	DeleteEndpoint(ctx context.Context, org OrgID, endpointID string) error
}

// PortedEndpoint is EndpointPorter.ListEndpoints' per-endpoint shape (Slice
// 9): never a secret. ID is carried as a plain string — organizations
// cannot reference delivery.EndpointID (BOUNDARIES) — mirroring how
// delivery.SecretIssuer already carries its own endpointID as a plain
// string across the module boundary in the other direction.
type PortedEndpoint struct {
	ID         string
	URL        string
	EventTypes []string
}

// PortedEndpointSecret is EndpointPorter.CreateEndpoint's response: the new
// endpoint's id and its freshly minted signing secret, returned exactly
// once (Slice 9, PD46).
type PortedEndpointSecret struct {
	ID     string
	Secret string
}

// IntegrationExistenceChecker is organizations' consumer-defined port onto
// catalog's installed integrations (Slice 9, PD46): organizations cannot
// import catalog (BOUNDARIES — the dependency points the other way, catalog
// already depends on organizations) — the composition root wires a concrete
// adapter over *catalog.Facade (app/integration_checker.go).
// ImportConfig's dry-run uses it to flag an allow-listed/hidden/featured
// integration id that doesn't exist in this installation, rather than
// silently dropping it (Slice 9's AC).
type IntegrationExistenceChecker interface {
	IntegrationExists(ctx context.Context, id string) (bool, error)
}
