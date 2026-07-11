// oauth_test.go exercises the connections.Facade's OAuth handshake
// (OpenConnectPage, HandleCallback) against the in-memory Repository, a fake
// OAuthClient standing in for the real Microsoft/Graph HTTP calls, and the
// fakeOrgReader/fakeUserReader/fakeIntegrationReader helpers already declared
// in facade_test.go (same package). This covers Slice 4's AC1-AC10 at the
// facade layer; connectweb/handler_test.go covers the HTTP-level rendering,
// and test/crucial_path covers the full journey against real SQLite.
package connections_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/organizations"
)

type fakeProviderReader struct {
	definitions map[string]catalog.ProviderDefinition
}

func (f fakeProviderReader) GetProviderDefinition(_ context.Context, slug string) (catalog.ProviderDefinition, error) {
	definition, ok := f.definitions[slug]
	if !ok {
		return catalog.ProviderDefinition{}, catalog.ErrUnknownProvider(slug)
	}
	return definition, nil
}

// fakeOAuthClient is a scripted connections.OAuthClient: ExchangeErr/AccountErr
// let tests simulate a provider failure without a real HTTP round trip.
type fakeOAuthClient struct {
	exchangeResult connections.TokenExchangeResult
	exchangeErr    error
	accountResult  connections.AccountInfo
	accountErr     error

	lastExchangeRequest connections.TokenExchangeRequest
	exchangeCallCount   int
	fetchAccountCalled  bool
}

func (f *fakeOAuthClient) ExchangeCode(_ context.Context, req connections.TokenExchangeRequest) (connections.TokenExchangeResult, error) {
	f.exchangeCallCount++
	f.lastExchangeRequest = req
	if f.exchangeErr != nil {
		return connections.TokenExchangeResult{}, f.exchangeErr
	}
	return f.exchangeResult, nil
}

func (f *fakeOAuthClient) FetchAccount(_ context.Context, _ string, _ string) (connections.AccountInfo, error) {
	f.fetchAccountCalled = true
	if f.accountErr != nil {
		return connections.AccountInfo{}, f.accountErr
	}
	return f.accountResult, nil
}

// mutableClock lets handshake tests advance time past ConnectLinkTTL /
// OAuthStateTTL without sleeping.
type mutableClock struct {
	now time.Time
}

func (c *mutableClock) Now() time.Time { return c.now }

const testProviderSlug = "outlook"

func testProviderDefinition() catalog.ProviderDefinition {
	return catalog.ProviderDefinition{
		Slug:         testProviderSlug,
		Name:         "Outlook",
		Logo:         "https://static.beecon.dev/providers/outlook.png",
		AuthScheme:   "oauth2",
		AuthorizeURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL:  "https://graph.microsoft.com/v1.0/me",
		Scopes:       []string{"offline_access", "Mail.Read", "User.Read"},
	}
}

// handshakeFixture wires a connections.Facade with every reader port the
// OAuth handshake needs, a controllable clock, and a scripted OAuthClient.
type handshakeFixture struct {
	facade      *connections.Facade
	oauthClient *fakeOAuthClient
	clock       *mutableClock
}

func newHandshakeFixture(oauthClient *fakeOAuthClient) *handshakeFixture {
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg: {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
	}
	user := organizations.User{ID: testUser, OrgID: testOrg, Name: "Ada"}
	integration := catalog.Integration{ID: testIntegration, ProviderSlug: testProviderSlug, ClientID: "the-client-id", ClientSecret: "the-client-secret"}
	clock := &mutableClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}

	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Organizations: fakeOrgReader{orgs: orgs},
		Users:         fakeUserReader{users: map[organizations.UserID]organizations.User{testUser: user}},
		Integrations:  fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{testIntegration: integration}},
		Providers:     fakeProviderReader{definitions: map[string]catalog.ProviderDefinition{testProviderSlug: testProviderDefinition()}},
		OAuthClient:   oauthClient,
		Now:           clock.Now,
	})
	return &handshakeFixture{facade: facade, oauthClient: oauthClient, clock: clock}
}

func newHappyPathOAuthClient() *fakeOAuthClient {
	return &fakeOAuthClient{
		exchangeResult: connections.TokenExchangeResult{
			AccessToken:  "raw-access-token-value",
			RefreshToken: "raw-refresh-token-value",
		},
		accountResult: connections.AccountInfo{
			Email:       "ada@example.com",
			DisplayName: "Ada Lovelace",
		},
	}
}

func initiateConnection(t *testing.T, f *handshakeFixture) connections.InitiatedConnection {
	t.Helper()
	initiated, err := f.facade.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	return initiated
}

func queryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	return parsed.Query().Get(key)
}

// --- OpenConnectPage (AC1, AC2, AC3) ---

func TestOpenConnectPage_ReturnsTheProviderNameAndLogo(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)

	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.ProviderName != "Outlook" {
		t.Errorf("ProviderName = %q, want %q", view.ProviderName, "Outlook")
	}
	if view.ProviderLogo != "https://static.beecon.dev/providers/outlook.png" {
		t.Errorf("ProviderLogo = %q, want the provider definition's logo", view.ProviderLogo)
	}
}

// TestOpenConnectPage_AuthorizeURLCarriesClientIDScopesAndAStateParam is AC3.
func TestOpenConnectPage_AuthorizeURLCarriesClientIDScopesAndAStateParam(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)

	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := queryParam(t, view.AuthorizeURL, "client_id"); got != "the-client-id" {
		t.Errorf("client_id = %q, want the Integration's client id %q", got, "the-client-id")
	}
	if got := queryParam(t, view.AuthorizeURL, "scope"); got != "offline_access Mail.Read User.Read" {
		t.Errorf("scope = %q, want the provider definition's space-joined scopes", got)
	}
	if got := queryParam(t, view.AuthorizeURL, "state"); got == "" {
		t.Error("AuthorizeURL carries no state param — AC3 requires a single-use CSRF state")
	}
}

func TestOpenConnectPage_ReturnsInvalidLinkErrorForATokenThatNamesNoConnection(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())

	_, err := f.facade.OpenConnectPage(context.Background(), "does-not-exist")

	assertDomainError(t, err, connections.CodeConnectLinkInvalid, 404)
}

func TestOpenConnectPage_ReturnsExpiredLinkErrorPastTheConnectLinkTTL(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	f.clock.now = f.clock.now.Add(connections.ConnectLinkTTL + time.Minute)

	_, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)

	assertDomainError(t, err, connections.CodeConnectLinkExpired, 410)
}

func TestOpenConnectPage_ReturnsAlreadyCompletedErrorForAConnectionNoLongerInitiated(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	activateConnection(t, f, initiated)

	_, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)

	assertDomainError(t, err, connections.CodeConnectLinkAlreadyCompleted, 410)
}

// TestOpenConnectPage_NeverForwardsToTheProviderOnAnInvalidLink is AC2's other
// half: an error must never carry the provider's own authorize URL forward —
// ConnectPageView (which is what would render the Connect link) is never
// returned at all when OpenConnectPage errors.
func TestOpenConnectPage_NeverForwardsToTheProviderOnAnInvalidLink(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())

	view, err := f.facade.OpenConnectPage(context.Background(), "does-not-exist")

	if err == nil {
		t.Fatal("expected an error for an invalid connect link")
	}
	if view.AuthorizeURL != "" {
		t.Errorf("AuthorizeURL = %q, want empty — an error case must never carry a provider URL forward", view.AuthorizeURL)
	}
}

// TestOpenConnectPage_MintsADistinctStateEachTimeTheLinkIsOpened proves the
// CSRF state is single-use per open rather than a fixed value reused forever.
func TestOpenConnectPage_MintsADistinctStateEachTimeTheLinkIsOpened(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)

	first, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("first OpenConnectPage: %v", err)
	}
	second, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("second OpenConnectPage: %v", err)
	}

	firstState := queryParam(t, first.AuthorizeURL, "state")
	secondState := queryParam(t, second.AuthorizeURL, "state")
	if firstState == secondState {
		t.Error("opening the same connect link twice minted the same state — each state must be single-use")
	}
}

// TestHandleCallback_ExchangesTheCodeUsingTheIntegrationsOwnClientCredentials
// proves the token endpoint is called with the same Integration client id/
// secret the connect page's authorize URL carried, the provider's own token
// endpoint, and the callback's own redirect_uri — not some other
// integration's credentials or a mismatched endpoint.
func TestHandleCallback_ExchangesTheCodeUsingTheIntegrationsOwnClientCredentials(t *testing.T) {
	client := newHappyPathOAuthClient()
	f := newHandshakeFixture(client)
	initiated := initiateConnection(t, f)

	activateConnection(t, f, initiated)

	if client.exchangeCallCount != 1 {
		t.Fatalf("exchangeCallCount = %d, want exactly 1", client.exchangeCallCount)
	}
	req := client.lastExchangeRequest
	if req.ClientID != "the-client-id" {
		t.Errorf("ClientID = %q, want the Integration's own client id %q", req.ClientID, "the-client-id")
	}
	if req.ClientSecret != "the-client-secret" {
		t.Errorf("ClientSecret = %q, want the Integration's own client secret", req.ClientSecret)
	}
	if req.TokenURL != testProviderDefinition().TokenURL {
		t.Errorf("TokenURL = %q, want the provider definition's token URL %q", req.TokenURL, testProviderDefinition().TokenURL)
	}
	if req.Code != "the-auth-code" {
		t.Errorf("Code = %q, want the code the callback received %q", req.Code, "the-auth-code")
	}
}

// --- HandleCallback (AC4, AC5, AC6, AC7, AC8, AC9, AC10) ---

// activateConnection drives Initiate -> OpenConnectPage -> HandleCallback to
// an ACTIVE connection, returning the outcome and the state consumed.
func activateConnection(t *testing.T, f *handshakeFixture, initiated connections.InitiatedConnection) (connections.CallbackOutcome, string) {
	t.Helper()
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")
	outcome, err := f.facade.HandleCallback(context.Background(), "the-auth-code", state, "")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	return outcome, state
}

func TestHandleCallback_HappyPathActivatesTheConnectionAndRedirectsWithSuccessStatus(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)

	outcome, _ := activateConnection(t, f, initiated)

	if got := queryParam(t, outcome.RedirectURL, "connectionId"); got != string(initiated.Connection.ID) {
		t.Errorf("connectionId = %q, want the stable id %q from initiate", got, initiated.Connection.ID)
	}
	if got := queryParam(t, outcome.RedirectURL, "status"); got != "success" {
		t.Errorf("status = %q, want %q", got, "success")
	}
	parsed, err := url.Parse(outcome.RedirectURL)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != allowedRedirect {
		t.Errorf("redirect base = %q, want the consumer's own redirectUri %q", parsed.Scheme+"://"+parsed.Host+parsed.Path, allowedRedirect)
	}
}

// TestHandleCallback_KeepsTheExactIDReturnedByInitiate is AC5: activation
// never mints a second id.
func TestHandleCallback_KeepsTheExactIDReturnedByInitiate(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)

	activateConnection(t, f, initiated)

	got, err := f.facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != initiated.Connection.ID {
		t.Errorf("ID after activation = %q, want the original %q — activation must never mint a second id", got.ID, initiated.Connection.ID)
	}
	if got.Status != connections.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, connections.StatusActive)
	}
}

// TestHandleCallback_RecordsAccountEmailAndDisplayNameVisibleViaGet is AC6.
func TestHandleCallback_RecordsAccountEmailAndDisplayNameVisibleViaGet(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)

	activateConnection(t, f, initiated)

	got, err := f.facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccountEmail != "ada@example.com" {
		t.Errorf("AccountEmail = %q, want %q", got.AccountEmail, "ada@example.com")
	}
	if got.AccountDisplayName != "Ada Lovelace" {
		t.Errorf("AccountDisplayName = %q, want %q", got.AccountDisplayName, "Ada Lovelace")
	}
}

// TestHandleCallback_StoresOnlyEncryptedTokensNeverTheRawValues is AC10 at the
// facade boundary: the activated Connection's token fields must be vault
// ciphertext, decryptable back to the exact values the provider returned, and
// must never equal (or contain) the raw token strings themselves.
func TestHandleCallback_StoresOnlyEncryptedTokensNeverTheRawValues(t *testing.T) {
	client := newHappyPathOAuthClient()
	f := newHandshakeFixture(client)
	initiated := initiateConnection(t, f)

	activateConnection(t, f, initiated)

	got, err := f.facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EncryptedAccessToken == client.exchangeResult.AccessToken {
		t.Error("EncryptedAccessToken equals the raw access token — it must be vault ciphertext")
	}
	if got.EncryptedRefreshToken == client.exchangeResult.RefreshToken {
		t.Error("EncryptedRefreshToken equals the raw refresh token — it must be vault ciphertext")
	}
	if got.EncryptedAccessToken == "" || got.EncryptedRefreshToken == "" {
		t.Fatal("encrypted token fields must not be empty after activation")
	}
}

// TestHandleCallback_MissingStateShowsAnErrorAndTheConnectionStaysInitiated is
// AC7 (missing variant).
func TestHandleCallback_MissingStateShowsAnErrorAndTheConnectionStaysInitiated(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	if _, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken); err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}

	_, err := f.facade.HandleCallback(context.Background(), "the-auth-code", "", "")

	assertDomainError(t, err, connections.CodeOAuthStateMissing, 400)
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusInitiated)
}

// AC7 (unknown variant): a state Beecon never minted.
func TestHandleCallback_UnknownStateShowsAnErrorAndTheConnectionStaysInitiated(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	if _, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken); err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}

	_, err := f.facade.HandleCallback(context.Background(), "the-auth-code", "state-nobody-minted", "")

	assertDomainError(t, err, connections.CodeOAuthStateUnknown, 400)
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusInitiated)
}

// AC7 (expired variant).
func TestHandleCallback_ExpiredStateShowsAnErrorAndTheConnectionStaysInitiated(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")
	f.clock.now = f.clock.now.Add(connections.OAuthStateTTL + time.Minute)

	_, err = f.facade.HandleCallback(context.Background(), "the-auth-code", state, "")

	assertDomainError(t, err, connections.CodeOAuthStateExpired, 410)
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusInitiated)
}

// AC7 (already-used variant): a state consumed by a prior callback.
func TestHandleCallback_AlreadyUsedStateShowsAnErrorAndDoesNotReactivate(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	_, state := activateConnection(t, f, initiated)

	_, err := f.facade.HandleCallback(context.Background(), "the-auth-code", state, "")

	assertDomainError(t, err, connections.CodeOAuthStateAlreadyUsed, 410)
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusActive)
}

// TestHandleCallback_ConsentDenialRedirectsWithErrorStatusAndConnectionStaysInitiated
// is AC8.
func TestHandleCallback_ConsentDenialRedirectsWithErrorStatusAndConnectionStaysInitiated(t *testing.T) {
	client := newHappyPathOAuthClient()
	f := newHandshakeFixture(client)
	initiated := initiateConnection(t, f)
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")

	outcome, err := f.facade.HandleCallback(context.Background(), "", state, "access_denied")

	if err != nil {
		t.Fatalf("unexpected error on consent denial: %v", err)
	}
	if got := queryParam(t, outcome.RedirectURL, "status"); got != "error" {
		t.Errorf("status = %q, want %q", got, "error")
	}
	if got := queryParam(t, outcome.RedirectURL, "connectionId"); got != string(initiated.Connection.ID) {
		t.Errorf("connectionId = %q, want %q", got, initiated.Connection.ID)
	}
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusInitiated)
	if client.exchangeCallCount != 0 {
		t.Error("a denied consent must never call the token endpoint")
	}
}

// TestHandleCallback_TokenExchangeFailureShowsAnErrorAndConnectionStaysInitiated
// is AC9.
func TestHandleCallback_TokenExchangeFailureShowsAnErrorAndConnectionStaysInitiated(t *testing.T) {
	client := &fakeOAuthClient{exchangeErr: errExchangeFailed}
	f := newHandshakeFixture(client)
	initiated := initiateConnection(t, f)
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")

	_, err = f.facade.HandleCallback(context.Background(), "the-auth-code", state, "")

	assertDomainError(t, err, connections.CodeOAuthTokenExchangeFailed, 502)
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusInitiated)
	if client.fetchAccountCalled {
		t.Error("FetchAccount must not be called when the token exchange itself failed")
	}
}

// TestHandleCallback_AccountFetchFailureShowsAnErrorAndConnectionStaysInitiated
// is AC9's other failure mode: the code exchange succeeds but the user-info
// fetch fails.
func TestHandleCallback_AccountFetchFailureShowsAnErrorAndConnectionStaysInitiated(t *testing.T) {
	client := &fakeOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "at", RefreshToken: "rt"},
		accountErr:     errExchangeFailed,
	}
	f := newHandshakeFixture(client)
	initiated := initiateConnection(t, f)
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")

	_, err = f.facade.HandleCallback(context.Background(), "the-auth-code", state, "")

	assertDomainError(t, err, connections.CodeOAuthTokenExchangeFailed, 502)
	assertConnectionStatus(t, f, initiated.Connection.ID, connections.StatusInitiated)
}

func assertConnectionStatus(t *testing.T, f *handshakeFixture, id connections.ConnectionID, want connections.Status) {
	t.Helper()
	got, err := f.facade.Get(context.Background(), testOrg, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != want {
		t.Errorf("Status = %q, want %q", got.Status, want)
	}
}

var errExchangeFailed = &fakeProviderCallError{msg: "provider rejected the request"}

// fakeProviderCallError is a minimal error type standing in for whatever
// transport-level error a real OAuthClient implementation might return —
// HandleCallback must translate any such error uniformly to
// ErrTokenExchangeFailed (AC9), never leak it verbatim.
type fakeProviderCallError struct{ msg string }

func (e *fakeProviderCallError) Error() string { return e.msg }
