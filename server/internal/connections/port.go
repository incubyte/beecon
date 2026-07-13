package connections

import (
	"context"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

// ListCursor is the decoded pagination cursor List's driven port accepts
// (Slice 4, AC1): the created_at/id pair of the last connection on the
// previous page, so the next page resumes strictly after it in the
// newest-first ordering.
type ListCursor struct {
	CreatedAt time.Time
	ID        ConnectionID
}

// ListFilter is List's org-scoped driven port query shape: UserID optionally
// restricts the page to one user's connections (empty means every user in
// org), plus the decoded pagination cursor and the page size to fetch.
type ListFilter struct {
	UserID organizations.UserID
	Cursor *ListCursor
	Limit  int
}

// Repository is the connections module's org-scoped driven port. Every
// method takes the owning OrgID as its second parameter, so a query without
// org scope cannot be expressed. FindByID returns (nil, nil) on a miss
// (including a connection that belongs to a different organization); the
// facade translates that into ErrNotFound. Update persists a Connection's
// mutable fields (status, connect token/redirect uri, encrypted tokens and
// their expiry, account metadata, encrypted params) — every lifecycle
// operation (activation, disable, refresh, reconnect) goes through it. List
// returns connections scoped to org (AC1), matching filter, newest first.
// Delete permanently removes a connection and its row — including its
// encrypted credentials — scoped to org (AC3); deleting an id that does not
// exist, or belongs to another organization, is a no-op the facade has
// already turned into ErrNotFound via a preceding FindByID.
type Repository interface {
	Save(ctx context.Context, connection Connection) error
	FindByID(ctx context.Context, org organizations.OrgID, id ConnectionID) (*Connection, error)
	Update(ctx context.Context, connection Connection) error
	List(ctx context.Context, org organizations.OrgID, filter ListFilter) ([]Connection, error)
	Delete(ctx context.Context, org organizations.OrgID, id ConnectionID) error
}

// OAuthRepository is deliberately installation-level, not org-scoped: the
// connect page and OAuth callback authenticate a connection attempt through
// its single-use connect token or its CSRF state — before any organization
// API key is presented — mirroring access.PrefixLookup's own pre-auth lookup
// shape. FindByConnectToken and FindConnectionForCallback return (nil, nil)
// on a miss; FindState returns (nil, nil) when the state is unknown.
type OAuthRepository interface {
	FindByConnectToken(ctx context.Context, token string) (*Connection, error)
	FindConnectionForCallback(ctx context.Context, id ConnectionID) (*Connection, error)
	SaveState(ctx context.Context, state OAuthState) error
	FindState(ctx context.Context, state string) (*OAuthState, error)
	MarkStateConsumed(ctx context.Context, state string, consumedAt time.Time) error
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

// ProviderDefinitionReader is a narrow, consumer-defined port satisfied by
// *catalog.Facade: the connect page and OAuth callback need the provider's
// OAuth authorize/token/user-info URLs and scopes to build the consent
// redirect and complete the token exchange.
type ProviderDefinitionReader interface {
	GetProviderDefinition(ctx context.Context, providerSlug string) (catalog.ProviderDefinition, error)
}

// Credential styles a provider's token endpoint may declare (PD13): FormBody
// carries client_id/client_secret in the token request's form body (Phase
// 1's original, and still Outlook's and Hubspot's, behavior); BasicAuth
// carries them in an HTTP Basic Authorization header instead, per RFC 6749
// section 2.3.1. A TokenExchangeRequest with no CredentialStyle set behaves
// as FormBody — see catalog.CredentialStyleFormBody's doc comment for why
// that is the definition format's own default.
const (
	CredentialStyleFormBody  = "formBody"
	CredentialStyleBasicAuth = "basicAuth"
)

// TokenExchangeRequest carries everything ExchangeCode needs to complete the
// authorization_code grant against a provider's token endpoint.
type TokenExchangeRequest struct {
	TokenURL        string
	ClientID        string
	ClientSecret    string
	Code            string
	RedirectURI     string
	CredentialStyle string
}

// TokenExchangeResult is a provider's authorization_code or refresh_token
// grant response — the raw values the vault encrypts before they are ever
// persisted, returned to a DTO, or written to a log line (AC10). ExpiresIn
// is the access token's lifetime in seconds (PD18); RefreshToken is empty on
// a refresh_token grant response when the provider did not rotate it — the
// caller keeps the one it already has (Slice 4, AC8's other branch).
type TokenExchangeResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// RefreshGrantRequest carries everything RefreshGrant needs to complete a
// refresh_token grant against a provider's token endpoint (PD18): the same
// credential-style-aware client authentication ExchangeCode uses, applied to
// a stored refresh token instead of a fresh authorization code.
type RefreshGrantRequest struct {
	TokenURL        string
	ClientID        string
	ClientSecret    string
	RefreshToken    string
	CredentialStyle string
}

// AccountInfo is the authenticated account's profile the callback captures
// via the provider's user-info/token-metadata endpoint (PD9): email and
// display name, later visible via get-connection (AC6).
type AccountInfo struct {
	Email       string
	DisplayName string
}

// AccountFetchRequest carries everything FetchAccount needs to call a
// provider's user-info/token-metadata endpoint and extract email/display
// name generically, via the definition's own declared field names (PD13's
// userInfo mapping) — this is what lets Hubspot's differently-shaped
// token-metadata response (Slice 2) reuse the same driven adapter Outlook's
// GET /v1.0/me already used, with no provider-specific Go code.
type AccountFetchRequest struct {
	UserInfoURL      string
	AccessToken      string
	EmailField       string
	DisplayNameField string
}

// OAuthClient is a narrow driven port for exchanging an authorization code
// for tokens, fetching the authenticated account's profile, and refreshing
// an access token via a stored refresh token (PD18), so tests can substitute
// a fake provider (a fake Microsoft + Graph, or Hubspot, httptest server)
// instead of calling the real internet.
type OAuthClient interface {
	ExchangeCode(ctx context.Context, req TokenExchangeRequest) (TokenExchangeResult, error)
	FetchAccount(ctx context.Context, req AccountFetchRequest) (AccountInfo, error)
	RefreshGrant(ctx context.Context, req RefreshGrantRequest) (TokenExchangeResult, error)
}

// LogEntry is what the OAuth token exchange hands to a Recorder after
// completing (or failing) an authorization_code exchange with the provider
// (Slice 5, AC8).
type LogEntry struct {
	OrgID        organizations.OrgID
	UserID       organizations.UserID
	ConnectionID ConnectionID
	Status       int
	DurationMs   int64
	RequestBody  string
	ResponseBody string
}

// Recorder is a narrow, consumer-defined port for writing an OAuth
// token-exchange log entry (AC8), so tests can substitute a fake instead of
// depending on the logging module directly (BOUNDARIES: connections does not
// depend on logging — the composition root wires a logging-backed adapter).
type Recorder interface {
	Record(ctx context.Context, entry LogEntry) error
}
