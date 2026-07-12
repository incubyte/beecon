package connections

import (
	"context"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

// Repository is the connections module's org-scoped driven port. Every
// method takes the owning OrgID as its second parameter, so a query without
// org scope cannot be expressed. FindByID returns (nil, nil) on a miss
// (including a connection that belongs to a different organization); the
// facade translates that into ErrNotFound. Update persists a previously
// initiated Connection's mutable fields — in this slice, only the OAuth
// callback's activation (status, encrypted tokens, account metadata).
type Repository interface {
	Save(ctx context.Context, connection Connection) error
	FindByID(ctx context.Context, org organizations.OrgID, id ConnectionID) (*Connection, error)
	Update(ctx context.Context, connection Connection) error
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

// TokenExchangeRequest carries everything ExchangeCode needs to complete the
// authorization_code grant against a provider's token endpoint.
type TokenExchangeRequest struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Code         string
	RedirectURI  string
}

// TokenExchangeResult is the provider's authorization_code grant response —
// the raw values the vault encrypts before they are ever persisted, returned
// to a DTO, or written to a log line (AC10).
type TokenExchangeResult struct {
	AccessToken  string
	RefreshToken string
}

// AccountInfo is the authenticated account's profile the callback captures
// via User.Read (PD9): email and display name, later visible via
// get-connection (AC6).
type AccountInfo struct {
	Email       string
	DisplayName string
}

// OAuthClient is a narrow driven port for exchanging an authorization code
// for tokens and fetching the authenticated account's profile, so tests can
// substitute a fake provider (a fake Microsoft + Graph httptest server)
// instead of calling the real internet.
type OAuthClient interface {
	ExchangeCode(ctx context.Context, req TokenExchangeRequest) (TokenExchangeResult, error)
	FetchAccount(ctx context.Context, userInfoURL, accessToken string) (AccountInfo, error)
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
