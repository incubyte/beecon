package connections

import (
	"context"
	"strings"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/httpx"
	"beecon/internal/metrics"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// Facade is the connections module's only public surface.
type Facade struct {
	repo         Repository
	oauthRepo    OAuthRepository
	orgs         OrganizationReader
	users        UserReader
	integrations IntegrationReader
	providers    ProviderDefinitionReader
	vault        *vault.Vault
	oauthClient  OAuthClient
	recorder     Recorder
	metrics      *metrics.Registry
	newID        func() string
	newToken     func() string
	newState     func() string
	baseURL      string
	now          func() time.Time
}

// NewFacade wires the facade with the narrow cross-module reader ports, the
// vault and OAuth client the handshake (Slice 4) needs, the narrow Recorder
// port every token exchange logs through (Slice 5, AC8 — nil is safe: a
// facade built without a recorder simply skips logging), injected
// id/token/state minters, the public base URL used to build connect-page and
// callback URLs (PD12), and a clock so tests can supply deterministic ids and
// a fixed time.
func NewFacade(
	repo Repository,
	oauthRepo OAuthRepository,
	orgs OrganizationReader,
	users UserReader,
	integrations IntegrationReader,
	providers ProviderDefinitionReader,
	tokenVault *vault.Vault,
	oauthClient OAuthClient,
	recorder Recorder,
	newID func() string,
	newToken func() string,
	newState func() string,
	baseURL string,
	now func() time.Time,
) *Facade {
	return &Facade{
		repo:         repo,
		oauthRepo:    oauthRepo,
		orgs:         orgs,
		users:        users,
		integrations: integrations,
		providers:    providers,
		vault:        tokenVault,
		oauthClient:  oauthClient,
		recorder:     recorder,
		newID:        newID,
		newToken:     newToken,
		newState:     newState,
		baseURL:      baseURL,
		now:          now,
	}
}

// WithMetrics wires this facade's Prometheus recording (PD24): OAuth
// handshake and token-refresh outcomes. A facade built without one (the nil
// zero value NewFacade leaves it at) makes every metrics call a silent
// no-op, exactly like a nil Recorder already does for logging.
func (f *Facade) WithMetrics(registry *metrics.Registry) *Facade {
	f.metrics = registry
	return f
}

// InitiatedConnection is Initiate's result: the newly created Connection and
// the connect-page URL its single-use token resolves to.
type InitiatedConnection struct {
	Connection  Connection
	RedirectURL string
}

// Initiate starts a connection attempt: it validates redirectURI against
// org's allow-list (PD4), confirms userID and integrationID both exist (an
// unknown or cross-org userID/integrationID surfaces as not-found, PD5), and
// mints a Connection bound to a single-use connect token.
func (f *Facade) Initiate(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	integrationID catalog.IntegrationID,
	redirectURI string,
) (InitiatedConnection, error) {
	orgEntity, err := f.orgs.Get(ctx, org)
	if err != nil {
		return InitiatedConnection{}, err
	}
	if !orgEntity.AllowsRedirectURI(redirectURI) {
		return InitiatedConnection{}, ErrRedirectURINotAllowed()
	}
	if _, err := f.users.GetUser(ctx, org, userID); err != nil {
		return InitiatedConnection{}, err
	}
	integration, err := f.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return InitiatedConnection{}, err
	}

	connection := NewConnection(
		ConnectionID(f.newID()),
		org,
		userID,
		integrationID,
		integration.ProviderSlug,
		redirectURI,
		f.newToken(),
		f.now(),
	)
	if err := f.repo.Save(ctx, connection); err != nil {
		return InitiatedConnection{}, err
	}
	return InitiatedConnection{
		Connection:  connection,
		RedirectURL: buildConnectURL(f.baseURL, connection.ConnectToken),
	}, nil
}

// Get fetches a Connection by id, translating a repository miss (or a
// cross-org match) into ErrNotFound.
func (f *Facade) Get(ctx context.Context, org organizations.OrgID, id ConnectionID) (Connection, error) {
	connection, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return Connection{}, err
	}
	if connection == nil {
		return Connection{}, ErrNotFound()
	}
	return *connection, nil
}

// buildConnectURL joins baseURL with the connect page's path and the
// single-use token (AC8: the redirectUrl points at Beecon's own connect
// page, bound to exactly this connection attempt).
func buildConnectURL(baseURL, token string) string {
	return strings.TrimRight(baseURL, "/") + "/connect/" + token
}

// defaultListLimit and maxListLimit bound List's page size when a caller
// supplies none, or supplies one larger than Beecon allows — the same
// PD10-style bounds every other list endpoint applies.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// ListParams is List's caller-facing filter shape (Slice 4, AC1): UserID
// optionally restricts the page to one user's connections (empty means
// every user in org); Cursor is the opaque cursor a consumer sends back
// exactly as a previous page's NextCursor returned it.
type ListParams struct {
	UserID string
	Cursor string
	Limit  int
}

// ListResult is one cursor-paginated page of Connections, newest first;
// NextCursor is empty when this was the last page.
type ListResult struct {
	Connections []Connection
	NextCursor  string
}

// List returns a page of Connections scoped to org (AC1), optionally
// narrowed to one user, newest first.
func (f *Facade) List(ctx context.Context, org organizations.OrgID, params ListParams) (ListResult, error) {
	cursor, err := decodeConnectionCursor(params.Cursor)
	if err != nil {
		return ListResult{}, err
	}
	limit := normalizeListLimit(params.Limit)

	items, err := f.repo.List(ctx, org, ListFilter{
		UserID: organizations.UserID(params.UserID),
		Cursor: cursor,
		Limit:  limit + 1,
	})
	if err != nil {
		return ListResult{}, err
	}
	return paginateConnections(items, limit), nil
}

// Disable transitions a Connection to DISCONNECTED (AC2, PD19): its stored
// tokens are retained, so Reconnect can bring it back to ACTIVE later, but
// execution against it now surfaces a status-explaining failure. An unknown
// id, or one belonging to another organization, is not-found (AC11).
func (f *Facade) Disable(ctx context.Context, org organizations.OrgID, id ConnectionID) (Connection, error) {
	connection, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return Connection{}, err
	}
	if connection == nil {
		return Connection{}, ErrNotFound()
	}
	disabled := connection.Disable()
	if err := f.repo.Update(ctx, disabled); err != nil {
		return Connection{}, err
	}
	return disabled, nil
}

// Delete permanently removes a Connection and its stored credentials (AC3,
// PD19): a hard row delete, so a subsequent Get returns not-found and no
// ciphertext belonging to this connection remains in the database. Log
// entries recorded against this connection's id are untouched — they live
// in their own table, keyed by the id string rather than a foreign key. An
// unknown id, or one belonging to another organization, is not-found (AC11).
func (f *Facade) Delete(ctx context.Context, org organizations.OrgID, id ConnectionID) error {
	connection, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return err
	}
	if connection == nil {
		return ErrNotFound()
	}
	return f.repo.Delete(ctx, org, id)
}

// Reconnect starts a fresh connect-page handshake against an existing
// Connection, reusing its immutable id (AC4, PD19): allowed from ACTIVE,
// EXPIRED, or DISCONNECTED — never from INITIATED, whose own initiate
// attempt is still open. redirectURI is validated against org's allow-list
// exactly as Initiate validates it (PD4). The connection's current status
// and existing tokens are left untouched until a completed callback
// activates it (Connection.Activate) — ResolveForExecution keeps serving a
// previously ACTIVE connection normally for the whole of a reconnect attempt
// (AC6). An unknown id, or one belonging to another organization, is
// not-found (AC11).
func (f *Facade) Reconnect(ctx context.Context, org organizations.OrgID, id ConnectionID, redirectURI string) (InitiatedConnection, error) {
	orgEntity, err := f.orgs.Get(ctx, org)
	if err != nil {
		return InitiatedConnection{}, err
	}
	if !orgEntity.AllowsRedirectURI(redirectURI) {
		return InitiatedConnection{}, ErrRedirectURINotAllowed()
	}
	connection, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return InitiatedConnection{}, err
	}
	if connection == nil {
		return InitiatedConnection{}, ErrNotFound()
	}
	if !connection.CanReconnect() {
		return InitiatedConnection{}, ErrReconnectNotAllowed(connection.Status)
	}

	prepared := connection.PrepareReconnect(f.newToken(), redirectURI, f.now().Add(ConnectLinkTTL))
	if err := f.repo.Update(ctx, prepared); err != nil {
		return InitiatedConnection{}, err
	}
	return InitiatedConnection{
		Connection:  prepared,
		RedirectURL: buildConnectURL(f.baseURL, prepared.ConnectToken),
	}, nil
}

func paginateConnections(items []Connection, limit int) ListResult {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := ListResult{Connections: items}
	if hasMore {
		last := items[len(items)-1]
		result.NextCursor = encodeConnectionCursor(last.CreatedAt, last.ID)
	}
	return result
}

func normalizeListLimit(requested int) int {
	if requested <= 0 {
		return defaultListLimit
	}
	if requested > maxListLimit {
		return maxListLimit
	}
	return requested
}

func encodeConnectionCursor(createdAt time.Time, id ConnectionID) string {
	return httpx.EncodeCursor(createdAt.UTC().Format(time.RFC3339Nano), string(id))
}

func decodeConnectionCursor(raw string) (*ListCursor, error) {
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
	return &ListCursor{CreatedAt: createdAt, ID: ConnectionID(fields[1])}, nil
}
