package connections

import (
	"context"
	"strings"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

// Facade is the connections module's only public surface.
type Facade struct {
	repo         Repository
	oauthRepo    OAuthRepository
	orgs         OrganizationReader
	users        UserReader
	integrations IntegrationReader
	providers    ProviderDefinitionReader
	vault        *Vault
	oauthClient  OAuthClient
	newID        func() string
	newToken     func() string
	newState     func() string
	baseURL      string
	now          func() time.Time
}

// NewFacade wires the facade with the narrow cross-module reader ports, the
// vault and OAuth client the handshake (Slice 4) needs, injected
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
	vault *Vault,
	oauthClient OAuthClient,
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
		vault:        vault,
		oauthClient:  oauthClient,
		newID:        newID,
		newToken:     newToken,
		newState:     newState,
		baseURL:      baseURL,
		now:          now,
	}
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
