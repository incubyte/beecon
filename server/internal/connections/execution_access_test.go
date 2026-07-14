// execution_access_test.go exercises PD18's on-demand refresh (refresh.go)
// through the only two exported entry points that reach it —
// ResolveForExecution's inline refresh and RefreshForExecution's forced
// refresh — since tokenExpiryFrom, needsRefresh, and refreshConnection are
// all unexported. A refreshScriptedOAuthClient (distinct from oauth_test.go's
// fakeOAuthClient) scripts the refresh_token grant independently of the
// authorization_code grant used to first activate a connection, so a test
// can prove rotated/non-rotated refresh tokens and refresh failure
// independently of the original activation's own tokens.
package connections_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// refreshScriptedOAuthClient is a connections.OAuthClient whose
// authorization_code exchange (ExchangeCode/FetchAccount) and refresh_token
// grant (RefreshGrant) are scripted independently, and whose refresh grant
// records how many times it was called and the request it last received.
type refreshScriptedOAuthClient struct {
	exchangeResult connections.TokenExchangeResult
	accountResult  connections.AccountInfo

	refreshResult      connections.TokenExchangeResult
	refreshErr         error
	refreshCallCount   int
	lastRefreshRequest connections.RefreshGrantRequest
}

func (c *refreshScriptedOAuthClient) ExchangeCode(_ context.Context, _ connections.TokenExchangeRequest) (connections.TokenExchangeResult, error) {
	return c.exchangeResult, nil
}

func (c *refreshScriptedOAuthClient) FetchAccount(_ context.Context, _ connections.AccountFetchRequest) (connections.AccountInfo, error) {
	return c.accountResult, nil
}

func (c *refreshScriptedOAuthClient) RefreshGrant(_ context.Context, req connections.RefreshGrantRequest) (connections.TokenExchangeResult, error) {
	c.refreshCallCount++
	c.lastRefreshRequest = req
	if c.refreshErr != nil {
		return connections.TokenExchangeResult{}, c.refreshErr
	}
	return c.refreshResult, nil
}

// FetchUserInfo is unused by this file's refresh-focused tests (Slice 5's
// reconciliation tests live in their own file); it satisfies
// connections.OAuthClient with a harmless default.
func (c *refreshScriptedOAuthClient) FetchUserInfo(_ context.Context, _, _ string) error {
	return nil
}

// executionAccessFixture wires a connections.Facade with an explicit
// Repository handle (so a test can seed a Connection's TokenExpiresAt
// directly — a nil-migrated-row simulation, AC7's self-heal case) and a
// mutable clock (so a test can travel time past a token's expiry without
// sleeping), backed by refreshScriptedOAuthClient.
type executionAccessFixture struct {
	facade *connections.Facade
	repo   *memory.Repository
	client *refreshScriptedOAuthClient
	clock  *mutableClock
	vault  *vault.Vault
}

var executionAccessFixtureVaultKey = []byte("exec-access-fixture-vault-key-00")

func newExecutionAccessFixture(t *testing.T, client *refreshScriptedOAuthClient) *executionAccessFixture {
	t.Helper()
	repo := memory.NewRepository()
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg: {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
	}
	clock := &mutableClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	tokenVault, err := vault.NewVault(executionAccessFixtureVaultKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Repository:    repo,
		Organizations: fakeOrgReader{orgs: orgs},
		Users:         fakeUserReader{users: map[organizations.UserID]organizations.User{testUser: {ID: testUser, OrgID: testOrg, Name: "Ada"}}},
		Integrations: fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{
			testIntegration: {ID: testIntegration, ProviderSlug: testProviderSlug, ClientID: "the-client-id", ClientSecret: "the-client-secret"},
		}},
		Providers:   fakeProviderReader{definitions: map[string]catalog.ProviderDefinition{testProviderSlug: testProviderDefinition()}},
		OAuthClient: client,
		Vault:       tokenVault,
		Now:         clock.Now,
	})
	return &executionAccessFixture{facade: facade, repo: repo, client: client, clock: clock, vault: tokenVault}
}

// activate drives Initiate -> OpenConnectPage -> HandleCallback to an ACTIVE
// connection using f.client's scripted ExchangeCode/FetchAccount result.
func (f *executionAccessFixture) activate(t *testing.T) connections.InitiatedConnection {
	t.Helper()
	initiated, err := f.facade.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")
	if _, err := f.facade.HandleCallback(context.Background(), "the-auth-code", state, ""); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	return initiated
}

func (f *executionAccessFixture) get(t *testing.T, id connections.ConnectionID) connections.Connection {
	t.Helper()
	got, err := f.facade.Get(context.Background(), testOrg, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got
}

func (f *executionAccessFixture) decrypt(t *testing.T, ciphertext string) string {
	t.Helper()
	plaintext, err := f.vault.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	return plaintext
}

// errTransientRefreshFailure stands in for a network error or a provider 5xx
// during a refresh_token grant (FD3): a plain error, carrying no typed
// connections.RefreshDenied — the only outcome PD36 treats as permanent.
var errTransientRefreshFailure = errors.New("connection reset by peer")

// --- ResolveForExecution's inline refresh (AC7) ---

func TestResolveForExecution_ReturnsTheDecryptedTokenWithoutRefreshingWhenNotYetExpired(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)

	access, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("ResolveForExecution: %v", err)
	}
	if access.Status != connections.StatusActive {
		t.Fatalf("Status = %q, want %q", access.Status, connections.StatusActive)
	}
	if access.AccessToken != "raw-access-token" {
		t.Errorf("AccessToken = %q, want the connection's own (unrefreshed) token", access.AccessToken)
	}
	if client.refreshCallCount != 0 {
		t.Errorf("refreshCallCount = %d, want 0 — a token not yet expired must never be refreshed", client.refreshCallCount)
	}
}

func TestResolveForExecution_RefreshesInlineWhenTheStoredAccessTokenHasExpiredAndCompletesTheCall(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 60},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshResult:  connections.TokenExchangeResult{AccessToken: "refreshed-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)
	f.clock.now = f.clock.now.Add(2 * time.Minute) // past the 60s ExpiresIn

	access, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("ResolveForExecution: %v", err)
	}
	if access.Status != connections.StatusActive {
		t.Fatalf("Status = %q, want %q — a successful refresh must let the call complete as a normal success", access.Status, connections.StatusActive)
	}
	if access.AccessToken != "refreshed-access-token" {
		t.Errorf("AccessToken = %q, want the freshly refreshed token", access.AccessToken)
	}
	if client.refreshCallCount != 1 {
		t.Errorf("refreshCallCount = %d, want exactly 1", client.refreshCallCount)
	}
	got := f.get(t, initiated.Connection.ID)
	if got.TokenExpiresAt == nil || !got.TokenExpiresAt.After(f.clock.now) {
		t.Errorf("TokenExpiresAt = %v, want an expiry in the future relative to the refreshed-at time", got.TokenExpiresAt)
	}
}

// TestResolveForExecution_TreatsAMigratedRowWithNoRecordedExpiryAsExpiredAndSelfHeals
// is PD18/ADR-0007's Phase-1 self-heal case: a connection persisted before
// token_expires_at existed (nil) must be refreshed on first use, exactly
// like a token that has genuinely expired.
func TestResolveForExecution_TreatsAMigratedRowWithNoRecordedExpiryAsExpiredAndSelfHeals(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshResult:  connections.TokenExchangeResult{AccessToken: "healed-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)
	migrated := f.get(t, initiated.Connection.ID)
	migrated.TokenExpiresAt = nil
	if err := f.repo.Update(context.Background(), migrated); err != nil {
		t.Fatalf("seed migrated row with nil expiry: %v", err)
	}

	access, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("ResolveForExecution: %v", err)
	}
	if client.refreshCallCount != 1 {
		t.Fatalf("refreshCallCount = %d, want exactly 1 — a nil expiry must be treated as already expired", client.refreshCallCount)
	}
	if access.AccessToken != "healed-access-token" {
		t.Errorf("AccessToken = %q, want the self-healed refreshed token", access.AccessToken)
	}
}

func TestResolveForExecution_UsesADefaultOneHourTTLWhenTheProviderReportsNoExpiresIn(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token"}, // ExpiresIn left 0
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
	}
	f := newExecutionAccessFixture(t, client)
	activatedAt := f.clock.now
	initiated := f.activate(t)

	got := f.get(t, initiated.Connection.ID)

	if got.TokenExpiresAt == nil {
		t.Fatal("TokenExpiresAt is nil, want the default 1h TTL applied")
	}
	wantExpiry := activatedAt.Add(1 * time.Hour)
	if !got.TokenExpiresAt.Equal(wantExpiry) {
		t.Errorf("TokenExpiresAt = %v, want %v (activation time + default 1h TTL)", got.TokenExpiresAt, wantExpiry)
	}
}

// --- Rotated vs. non-rotated refresh token (AC8) ---

func TestRefresh_RotatedRefreshTokenReplacesTheStoredOne(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "original-refresh-token", ExpiresIn: 3600},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshResult:  connections.TokenExchangeResult{AccessToken: "refreshed-access-token", RefreshToken: "rotated-refresh-token", ExpiresIn: 3600},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)

	if _, err := f.facade.RefreshForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID); err != nil {
		t.Fatalf("RefreshForExecution: %v", err)
	}

	got := f.get(t, initiated.Connection.ID)
	if plaintext := f.decrypt(t, got.EncryptedRefreshToken); plaintext != "rotated-refresh-token" {
		t.Errorf("decrypted refresh token = %q, want the provider's rotated value %q", plaintext, "rotated-refresh-token")
	}
}

func TestRefresh_EmptyRotatedRefreshTokenKeepsTheStoredOne(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "original-refresh-token", ExpiresIn: 3600},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshResult:  connections.TokenExchangeResult{AccessToken: "refreshed-access-token", RefreshToken: "", ExpiresIn: 3600},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)

	if _, err := f.facade.RefreshForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID); err != nil {
		t.Fatalf("RefreshForExecution: %v", err)
	}

	got := f.get(t, initiated.Connection.ID)
	if plaintext := f.decrypt(t, got.EncryptedRefreshToken); plaintext != "original-refresh-token" {
		t.Errorf("decrypted refresh token = %q, want the original value kept when the provider did not rotate it", plaintext)
	}
}

// --- Refresh failure (AC9, corrected by FD3/PD36 — Slice 5) ---

// TestRefresh_APermanentRefusalTransitionsTheConnectionToExpiredAndReturnsNoErrorFromResolve
// pins FD3's permanent half: only a typed connections.RefreshDenied (the
// provider's own "invalid_grant and kin" refusal, PD36) expires the
// connection — surfaced as a status, not an error, so a resolving caller
// reports it the same way it reports any other non-ACTIVE connection.
func TestRefresh_APermanentRefusalTransitionsTheConnectionToExpiredAndReturnsNoErrorFromResolve(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 60},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshErr:     connections.RefreshDenied{OAuthErrorCode: "invalid_grant"},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)
	f.clock.now = f.clock.now.Add(2 * time.Minute)

	access, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v — a permanent refusal must surface as a status, not an error", err)
	}
	if access.Status != connections.StatusExpired {
		t.Errorf("Status = %q, want %q", access.Status, connections.StatusExpired)
	}
	if access.AccessToken != "" {
		t.Errorf("AccessToken = %q, want empty for an EXPIRED connection", access.AccessToken)
	}
	got := f.get(t, initiated.Connection.ID)
	if got.Status != connections.StatusExpired {
		t.Errorf("persisted Status = %q, want %q", got.Status, connections.StatusExpired)
	}
}

// TestRefresh_ATransientFailureLeavesTheConnectionActiveAndSurfacesTheErrorFromResolve
// pins FD3's transient half — the behavior change this slice makes on top of
// Phase 2: a plain error (network failure, provider 5xx — anything that is
// not a typed connections.RefreshDenied) must NOT expire the connection; it
// is returned to the caller so a request-path resolve surfaces a retriable
// error instead of killing a connection whose token may still be refreshable
// on the very next attempt (PD36's "transient failures just retry").
func TestRefresh_ATransientFailureLeavesTheConnectionActiveAndSurfacesTheErrorFromResolve(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 60},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshErr:     errTransientRefreshFailure,
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)
	f.clock.now = f.clock.now.Add(2 * time.Minute)

	_, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err == nil {
		t.Fatal("expected a transient refresh failure to surface as an error, got nil")
	}
	if !errors.Is(err, errTransientRefreshFailure) {
		t.Errorf("err = %v, want it to wrap/equal the transient failure %v", err, errTransientRefreshFailure)
	}
	got := f.get(t, initiated.Connection.ID)
	if got.Status != connections.StatusActive {
		t.Errorf("persisted Status = %q, want %q — a transient failure must leave the connection untouched for the next scan to retry", got.Status, connections.StatusActive)
	}
	if client.refreshCallCount != 1 {
		t.Errorf("refreshCallCount = %d, want exactly 1", client.refreshCallCount)
	}
}

// --- RefreshForExecution's forced refresh (the 401-reactive half of PD18) ---

func TestRefreshForExecution_ForcesARefreshEvenWhenTheStoredTokenIsNotYetExpired(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
		refreshResult:  connections.TokenExchangeResult{AccessToken: "forced-refresh-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)

	access, err := f.facade.RefreshForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("RefreshForExecution: %v", err)
	}
	if client.refreshCallCount != 1 {
		t.Fatalf("refreshCallCount = %d, want exactly 1 even though the stored token had not expired", client.refreshCallCount)
	}
	if access.AccessToken != "forced-refresh-access-token" {
		t.Errorf("AccessToken = %q, want the forced refresh's own token", access.AccessToken)
	}
}

func TestRefreshForExecution_ReturnsANonActiveConnectionAsIsWithoutAttemptingARefresh(t *testing.T) {
	client := &refreshScriptedOAuthClient{}
	f := newExecutionAccessFixture(t, client)
	initiated, err := f.facade.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	access, err := f.facade.RefreshForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("RefreshForExecution: %v", err)
	}
	if access.Status != connections.StatusInitiated {
		t.Errorf("Status = %q, want %q (unchanged)", access.Status, connections.StatusInitiated)
	}
	if client.refreshCallCount != 0 {
		t.Errorf("refreshCallCount = %d, want 0 — a non-ACTIVE connection has nothing to refresh", client.refreshCallCount)
	}
}

func TestRefreshForExecution_ReturnsNotFoundForAConnectionBelongingToAnotherUser(t *testing.T) {
	client := &refreshScriptedOAuthClient{
		exchangeResult: connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600},
		accountResult:  connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"},
	}
	f := newExecutionAccessFixture(t, client)
	initiated := f.activate(t)

	_, err := f.facade.RefreshForExecution(context.Background(), testOrg, organizations.UserID("someone-else"), initiated.Connection.ID)

	assertDomainError(t, err, connections.CodeNotFound, 404)
}
