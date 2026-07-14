package organizations

import (
	"context"
	"fmt"
	"time"

	"beecon/internal/httpx"
)

// Facade is the organizations module's only public surface.
type Facade struct {
	repo           Repository
	users          UserRepository
	governance     GovernanceRepository
	endpointPorter EndpointPorter
	integrations   IntegrationExistenceChecker
	newOrgID       func() string
	newUserID      func() string
	now            func() time.Time
}

// NewFacade wires the facade with an injected id minter per entity and a
// clock so tests can supply deterministic ids and a fixed time. governance is
// the driven port GetGovernance/SetGovernance persist through (Slice 5).
func NewFacade(repo Repository, users UserRepository, governance GovernanceRepository, newOrgID, newUserID func() string, now func() time.Time) *Facade {
	return &Facade{repo: repo, users: users, governance: governance, newOrgID: newOrgID, newUserID: newUserID, now: now}
}

// Create validates name and persists a new Organization.
func (f *Facade) Create(ctx context.Context, name string) (Organization, error) {
	org, err := NewOrganization(OrgID(f.newOrgID()), name, f.now())
	if err != nil {
		return Organization{}, err
	}
	if err := f.repo.Save(ctx, org); err != nil {
		return Organization{}, err
	}
	return org, nil
}

// Get fetches an Organization by id, translating a repository miss into
// ErrNotFound.
func (f *Facade) Get(ctx context.Context, id OrgID) (Organization, error) {
	org, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return Organization{}, err
	}
	if org == nil {
		return Organization{}, ErrNotFound()
	}
	return *org, nil
}

// CreateUser validates and persists a new User scoped to org (PD2: the
// consumer's own server provisions its users with its org API key).
func (f *Facade) CreateUser(ctx context.Context, org OrgID, name, externalID string) (User, error) {
	user, err := NewUser(UserID(f.newUserID()), org, name, externalID, f.now())
	if err != nil {
		return User{}, err
	}
	if err := f.users.SaveUser(ctx, user); err != nil {
		return User{}, err
	}
	return user, nil
}

// SetAllowedRedirectURIs replaces org's redirect-uri allow-list (PD4),
// settable only by the installation admin (PATCH
// /api/v1/organizations/{orgId}).
func (f *Facade) SetAllowedRedirectURIs(ctx context.Context, id OrgID, uris []string) (Organization, error) {
	org, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return Organization{}, err
	}
	if org == nil {
		return Organization{}, ErrNotFound()
	}
	updated := org.WithAllowedRedirectURIs(uris)
	if err := f.repo.Update(ctx, updated); err != nil {
		return Organization{}, err
	}
	return updated, nil
}

// defaultListAllLimit and maxListAllLimit bound ListAll's page size when a
// caller supplies none, or supplies one larger than Beecon allows — the same
// PD10-style bounds every other list endpoint applies.
const (
	defaultListAllLimit = 50
	maxListAllLimit     = 200
)

// ListAllParams is ListAll's caller-facing shape (Slice 1, PD40): Cursor is
// the opaque cursor a consumer sends back exactly as a previous page's
// NextCursor returned it.
type ListAllParams struct {
	Cursor string
	Limit  int
}

// ListAllResult is one cursor-paginated page of every Organization in the
// installation (Slice 1, PD40), newest first; NextCursor is empty when this
// was the last page.
type ListAllResult struct {
	Organizations []Organization
	NextCursor    string
}

// ListAll returns a page of every organization in the installation (Slice 1,
// PD40) — an operator-only, installation-wide view guarded by AdminAuth;
// there is no org id to scope this query by since Organization is itself
// the isolation unit.
func (f *Facade) ListAll(ctx context.Context, params ListAllParams) (ListAllResult, error) {
	cursor, err := decodeListAllCursor(params.Cursor)
	if err != nil {
		return ListAllResult{}, err
	}
	limit := normalizeListAllLimit(params.Limit)

	items, err := f.repo.ListAll(ctx, cursor, limit+1)
	if err != nil {
		return ListAllResult{}, err
	}
	return paginateOrganizations(items, limit), nil
}

func paginateOrganizations(items []Organization, limit int) ListAllResult {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := ListAllResult{Organizations: items}
	if hasMore {
		last := items[len(items)-1]
		result.NextCursor = encodeListAllCursor(last.CreatedAt, last.ID)
	}
	return result
}

func normalizeListAllLimit(requested int) int {
	if requested <= 0 {
		return defaultListAllLimit
	}
	if requested > maxListAllLimit {
		return maxListAllLimit
	}
	return requested
}

func encodeListAllCursor(createdAt time.Time, id OrgID) string {
	return httpx.EncodeCursor(createdAt.UTC().Format(time.RFC3339Nano), string(id))
}

func decodeListAllCursor(raw string) (*ListAllCursor, error) {
	fields, err := httpx.DecodeCursor(raw, 2)
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	if fields == nil {
		return nil, nil
	}
	createdAt, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	return &ListAllCursor{CreatedAt: createdAt, ID: OrgID(fields[1])}, nil
}

// GetUser fetches a User scoped to org, translating a repository miss (or a
// cross-org match) into ErrUserNotFound.
func (f *Facade) GetUser(ctx context.Context, org OrgID, id UserID) (User, error) {
	user, err := f.users.FindUserByID(ctx, org, id)
	if err != nil {
		return User{}, err
	}
	if user == nil {
		return User{}, ErrUserNotFound()
	}
	return *user, nil
}

// ListUsersParams is ListUsers' caller-facing shape (Slice 4, PD40): Cursor
// is the opaque cursor a consumer sends back exactly as a previous page's
// NextCursor returned it — the same shape ListAllParams uses.
type ListUsersParams struct {
	Cursor string
	Limit  int
}

// ListUsersResult is one cursor-paginated page of one organization's
// end-users (Slice 4, PD40), newest first; NextCursor is empty when this was
// the last page.
type ListUsersResult struct {
	Users      []User
	NextCursor string
}

// ListUsers returns a page of org's end-users (Slice 4, PD40) — the Admin
// UI's new list-users-per-org read, mounted behind the admin key with org
// injected from the path (AdminOrgScope/InjectOrgFromPath), org-scoped at
// the persistence port like every other org-scoped query.
func (f *Facade) ListUsers(ctx context.Context, org OrgID, params ListUsersParams) (ListUsersResult, error) {
	cursor, err := decodeUserListCursor(params.Cursor)
	if err != nil {
		return ListUsersResult{}, err
	}
	limit := normalizeListAllLimit(params.Limit)

	items, err := f.users.ListByOrg(ctx, org, cursor, limit+1)
	if err != nil {
		return ListUsersResult{}, err
	}
	return paginateUsers(items, limit), nil
}

func paginateUsers(items []User, limit int) ListUsersResult {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := ListUsersResult{Users: items}
	if hasMore {
		last := items[len(items)-1]
		result.NextCursor = encodeUserListCursor(last.CreatedAt, last.ID)
	}
	return result
}

func encodeUserListCursor(createdAt time.Time, id UserID) string {
	return httpx.EncodeCursor(createdAt.UTC().Format(time.RFC3339Nano), string(id))
}

func decodeUserListCursor(raw string) (*UserListCursor, error) {
	fields, err := httpx.DecodeCursor(raw, 2)
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	if fields == nil {
		return nil, nil
	}
	createdAt, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	return &UserListCursor{CreatedAt: createdAt, ID: UserID(fields[1])}, nil
}

// GetGovernance returns org's governance settings (Slice 5, PD42/PD43),
// synthesizing the continuity-preserving default (NewDefaultGovernance) for
// an organization that has never been configured — this is the read half of
// the seam catalog.Facade calls through GovernanceReader to filter every
// integration/tool/trigger-definition listing and connections.Facade.Initiate
// calls (via catalog.GetVisibleIntegration) to guard connection creation.
func (f *Facade) GetGovernance(ctx context.Context, org OrgID) (Governance, error) {
	governance, err := f.governance.FindByOrg(ctx, org)
	if err != nil {
		return Governance{}, err
	}
	if governance == nil {
		return NewDefaultGovernance(org), nil
	}
	return *governance, nil
}

// GovernanceUpdate is SetGovernance's caller-facing shape (Slice 5): it
// replaces org's entire governance record, mirroring
// SetAllowedRedirectURIs' own whole-replace convention. AllowList nil means
// "inherit the full installation catalog" (PD42); FeaturedCap <= 0 falls
// back to DefaultFeaturedCap.
type GovernanceUpdate struct {
	AllowList   *[]string
	Hidden      []string
	Featured    []string
	FeaturedCap int
}

// SetGovernance validates and replaces org's governance record (admin-only:
// the Admin UI's governance editor, PUT /api/v1/organizations/{orgId}/governance).
// Only the governance half of the record (AllowList/Hidden/Featured/
// FeaturedCap) is replaced from update — org's own retention windows (Slice
// 7, set independently through SetRetention) are read first and carried
// through unchanged, so a governance-only PUT never disturbs a
// separately-configured retention window (org_governance is one shared
// settings row, FD8, but these are two independently-replaceable halves).
func (f *Facade) SetGovernance(ctx context.Context, org OrgID, update GovernanceUpdate) (Governance, error) {
	existing, err := f.GetGovernance(ctx, org)
	if err != nil {
		return Governance{}, err
	}
	governance, err := NewGovernance(org, update.AllowList, update.Hidden, update.Featured, update.FeaturedCap)
	if err != nil {
		return Governance{}, err
	}
	governance, err = governance.WithRetention(existing.LogRetentionDays, existing.EventRetentionDays)
	if err != nil {
		return Governance{}, err
	}
	if err := f.governance.SaveGovernance(ctx, governance); err != nil {
		return Governance{}, err
	}
	return governance, nil
}

// RetentionView is GetRetention's response shape (Slice 7, PD44): nil means
// org inherits the installation's own BEECON_RETENTION_DAYS default; 0
// means unlimited/disabled for that entity kind. RetentionView carries no
// opinion about what the installation default actually is — the driving
// httpapi layer (which already has the configured value) combines it into
// the response, keeping this facade free of any config-package dependency.
type RetentionView struct {
	OrgID              OrgID
	LogRetentionDays   *int
	EventRetentionDays *int
}

// GetRetention returns org's own retention overrides (Slice 7, PD44):
// synthesizes the continuity-preserving default (both nil, "inherit") for
// an organization that has never configured either governance or retention,
// via the same GetGovernance seam SetGovernance/SetRetention both read
// through.
func (f *Facade) GetRetention(ctx context.Context, org OrgID) (RetentionView, error) {
	governance, err := f.GetGovernance(ctx, org)
	if err != nil {
		return RetentionView{}, err
	}
	return RetentionView{OrgID: org, LogRetentionDays: governance.LogRetentionDays, EventRetentionDays: governance.EventRetentionDays}, nil
}

// RetentionUpdate is SetRetention's caller-facing shape (Slice 7, PD44): it
// replaces org's entire retention record — mirroring GovernanceUpdate's own
// whole-replace convention, but scoped to just the two retention fields.
// LogRetentionDays/EventRetentionDays nil means "inherit the installation
// default" (absent/JSON null in the request); 0 means unlimited/disabled.
type RetentionUpdate struct {
	LogRetentionDays   *int
	EventRetentionDays *int
}

// SetRetention validates and replaces org's retention windows (admin-only:
// the Admin UI's retention settings, PUT /api/v1/organizations/{orgId}/retention).
// Only the retention half of the record is replaced from update — org's own
// governance (AllowList/Hidden/Featured/FeaturedCap, set independently
// through SetGovernance) is read first and carried through unchanged, the
// mirror image of SetGovernance's own preservation of retention.
func (f *Facade) SetRetention(ctx context.Context, org OrgID, update RetentionUpdate) (RetentionView, error) {
	existing, err := f.GetGovernance(ctx, org)
	if err != nil {
		return RetentionView{}, err
	}
	updated, err := existing.WithRetention(update.LogRetentionDays, update.EventRetentionDays)
	if err != nil {
		return RetentionView{}, err
	}
	if err := f.governance.SaveGovernance(ctx, updated); err != nil {
		return RetentionView{}, err
	}
	return RetentionView{OrgID: org, LogRetentionDays: updated.LogRetentionDays, EventRetentionDays: updated.EventRetentionDays}, nil
}

// WithEndpointPorter wires organizations' consumer-defined EndpointPorter
// port (Slice 9, PD46): ExportConfig reads an org's webhook endpoints
// through it and ImportConfig creates/updates/deletes through it —
// organizations never imports delivery (BOUNDARIES). Wired by the
// composition root over *delivery.Facade (app/endpoint_porter.go).
func (f *Facade) WithEndpointPorter(porter EndpointPorter) *Facade {
	f.endpointPorter = porter
	return f
}

// WithIntegrationChecker wires organizations' consumer-defined
// IntegrationExistenceChecker port (Slice 9, PD46): ImportConfig's dry-run
// uses it to flag an allow-listed/hidden/featured integration id that
// doesn't exist in this installation, without organizations importing
// catalog (BOUNDARIES: the dependency points the other way). Wired by the
// composition root over *catalog.Facade (app/integration_checker.go).
func (f *Facade) WithIntegrationChecker(checker IntegrationExistenceChecker) *Facade {
	f.integrations = checker
	return f
}

// ExportConfig assembles org's versioned config document (Slice 9, PD46):
// its governance, webhook endpoints (URL + event-type filter, reached
// through EndpointPorter — never a secret), and retention config. It never
// includes an API-key/webhook secret, connection, credential, user token,
// or provider definition — there is no field on ConfigDocument for any of
// them.
func (f *Facade) ExportConfig(ctx context.Context, org OrgID) (ConfigDocument, error) {
	governance, err := f.GetGovernance(ctx, org)
	if err != nil {
		return ConfigDocument{}, err
	}
	endpoints, err := f.endpointPorter.ListEndpoints(ctx, org)
	if err != nil {
		return ConfigDocument{}, err
	}
	return ConfigDocument{
		SchemaVersion: CurrentConfigSchemaVersion,
		Governance: ConfigGovernance{
			AllowList:   governance.AllowList,
			Hidden:      governance.Hidden,
			Featured:    governance.Featured,
			FeaturedCap: governance.FeaturedCap,
		},
		Endpoints: toConfigEndpoints(endpoints),
		Retention: ConfigRetention{
			LogRetentionDays:   governance.LogRetentionDays,
			EventRetentionDays: governance.EventRetentionDays,
		},
	}, nil
}

func toConfigEndpoints(endpoints []PortedEndpoint) []ConfigEndpoint {
	items := make([]ConfigEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		items = append(items, ConfigEndpoint{URL: endpoint.URL, EventTypes: endpoint.EventTypes})
	}
	return items
}

// ImportConfig validates document's schema version first — an unknown or
// incompatible version is rejected and nothing is read or written further
// (Slice 9's AC) — then computes the plan/diff and any
// unknown-integration-id warnings. A dry-run (opts.DryRun, the driving
// httpapi layer's default) stops there and returns the plan; an apply
// additionally writes governance and retention through Facade's own
// SetGovernance/SetRetention (each already preserving the other's half, as
// they do for their own direct callers) and endpoints through
// EndpointPorter, and returns what it applied plus any freshly minted
// endpoint secrets (shown once — secrets are never part of the import
// document).
func (f *Facade) ImportConfig(ctx context.Context, org OrgID, document ConfigDocument, opts ImportOptions) (ImportResult, error) {
	if err := ValidateConfigSchemaVersion(document.SchemaVersion); err != nil {
		return ImportResult{}, err
	}
	mode, err := normalizeImportMode(opts.Mode)
	if err != nil {
		return ImportResult{}, err
	}

	existingGovernance, err := f.GetGovernance(ctx, org)
	if err != nil {
		return ImportResult{}, err
	}
	existingRetention, err := f.GetRetention(ctx, org)
	if err != nil {
		return ImportResult{}, err
	}
	existingEndpoints, err := f.endpointPorter.ListEndpoints(ctx, org)
	if err != nil {
		return ImportResult{}, err
	}

	warnings, err := f.unknownIntegrationWarnings(ctx, document.Governance)
	if err != nil {
		return ImportResult{}, err
	}

	governanceUpdate := resolveGovernanceUpdate(existingGovernance, document.Governance, mode)
	retentionUpdate := resolveRetentionUpdate(existingRetention, document.Retention, mode)
	endpointActions := planEndpoints(existingEndpoints, document.Endpoints, mode)
	changes := combineConfigChanges(existingGovernance, governanceUpdate, existingRetention, retentionUpdate, endpointActions)

	if opts.DryRun {
		return ImportResult{Plan: changes, Warnings: warnings}, nil
	}

	secrets, err := f.applyImport(ctx, org, governanceUpdate, retentionUpdate, endpointActions)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Applied: changes, Secrets: secrets}, nil
}

// unknownIntegrationWarnings checks every integration id doc's governance
// section references against IntegrationExistenceChecker (Slice 9's AC):
// flagged, not silently dropped, so an operator moving config between
// installations sees exactly which referenced integrations don't exist here
// yet.
func (f *Facade) unknownIntegrationWarnings(ctx context.Context, doc ConfigGovernance) ([]string, error) {
	var warnings []string
	for _, id := range referencedIntegrationIDs(doc) {
		exists, err := f.integrations.IntegrationExists(ctx, id)
		if err != nil {
			return nil, err
		}
		if !exists {
			warnings = append(warnings, fmt.Sprintf("integration %q referenced in governance does not exist in this installation", id))
		}
	}
	return warnings, nil
}

// applyImport is ImportConfig's non-dry-run half: writes governance and
// retention (each through Facade's own half-preserving Set method) and
// applies every endpoint action through EndpointPorter, collecting the
// freshly minted secret for each newly created endpoint.
func (f *Facade) applyImport(ctx context.Context, org OrgID, governanceUpdate GovernanceUpdate, retentionUpdate RetentionUpdate, endpointActions []endpointAction) ([]ImportedEndpointSecret, error) {
	if _, err := f.SetGovernance(ctx, org, governanceUpdate); err != nil {
		return nil, err
	}
	if _, err := f.SetRetention(ctx, org, retentionUpdate); err != nil {
		return nil, err
	}

	var secrets []ImportedEndpointSecret
	for _, action := range endpointActions {
		switch action.Action {
		case "create":
			created, err := f.endpointPorter.CreateEndpoint(ctx, org, action.URL, action.EventTypes)
			if err != nil {
				return nil, err
			}
			secrets = append(secrets, ImportedEndpointSecret{EndpointID: created.ID, Secret: created.Secret})
		case "update":
			if err := f.endpointPorter.UpdateEndpoint(ctx, org, action.EndpointID, action.URL, action.EventTypes); err != nil {
				return nil, err
			}
		case "delete":
			if err := f.endpointPorter.DeleteEndpoint(ctx, org, action.EndpointID); err != nil {
				return nil, err
			}
		}
	}
	return secrets, nil
}
