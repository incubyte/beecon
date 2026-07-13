// lifecycle_test.go exercises connections.Facade's Slice 4 lifecycle
// operations — List, Disable, Delete, Reconnect — against the in-memory
// Repository, reusing fakeOrgReader/fakeUserReader/fakeIntegrationReader and
// the testOrg/otherOrg/testUser/testIntegration/allowedRedirect fixtures
// already declared in facade_test.go, and the handshakeFixture/
// fakeOAuthClient/mutableClock helpers already declared in oauth_test.go
// (same package). Handler-level coverage (HTTP status codes, PD5 not-found
// shapes) lives in connections/driving/httpapi/handler_test.go; the full
// journey against real SQLite lives in test/crucial_path.
package connections_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/organizations"
)

const testUser2 = organizations.UserID("user_2")

// newLifecycleFacade wires a facade with testOrg/otherOrg, testUser and
// testUser2 (both in testOrg), and testIntegration, backed by an explicit
// Repository handle and a mutable clock — so List's newest-first ordering
// and cursor pagination can use distinct createdAt timestamps, and a test
// can seed a Connection directly at a specific Status via repo.Update
// without going through a full OAuth handshake.
func newLifecycleFacade(t *testing.T) (*connections.Facade, *memory.Repository, *mutableClock) {
	t.Helper()
	repo := memory.NewRepository()
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg:  {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
		otherOrg: {ID: otherOrg, Name: "Other", AllowedRedirectURIs: []string{allowedRedirect}},
	}
	users := map[organizations.UserID]organizations.User{
		testUser:  {ID: testUser, OrgID: testOrg, Name: "Ada"},
		testUser2: {ID: testUser2, OrgID: testOrg, Name: "Bea"},
	}
	integration := catalog.Integration{ID: testIntegration, ProviderSlug: "outlook", ClientID: "cid", ClientSecret: "csecret"}
	clock := &mutableClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Repository:    repo,
		Organizations: fakeOrgReader{orgs: orgs},
		Users:         fakeUserReader{users: users},
		Integrations:  fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{testIntegration: integration}},
		Now:           clock.Now,
	})
	return facade, repo, clock
}

func mustInitiateAs(t *testing.T, f *connections.Facade, org organizations.OrgID, user organizations.UserID) connections.InitiatedConnection {
	t.Helper()
	initiated, err := f.Initiate(context.Background(), org, user, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	return initiated
}

// --- List (AC1) ---

func TestList_ReturnsConnectionsNewestFirst(t *testing.T) {
	f, _, clock := newLifecycleFacade(t)
	first := mustInitiateAs(t, f, testOrg, testUser)
	clock.now = clock.now.Add(time.Minute)
	second := mustInitiateAs(t, f, testOrg, testUser)

	result, err := f.List(context.Background(), testOrg, connections.ListParams{})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Connections) != 2 {
		t.Fatalf("len(Connections) = %d, want 2", len(result.Connections))
	}
	if result.Connections[0].ID != second.Connection.ID {
		t.Errorf("Connections[0].ID = %q, want the newest %q", result.Connections[0].ID, second.Connection.ID)
	}
	if result.Connections[1].ID != first.Connection.ID {
		t.Errorf("Connections[1].ID = %q, want the oldest %q", result.Connections[1].ID, first.Connection.ID)
	}
}

func TestList_ScopesToTheCallersOrganization(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)
	mustInitiateAs(t, f, testOrg, testUser)

	result, err := f.List(context.Background(), otherOrg, connections.ListParams{})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Connections) != 0 {
		t.Errorf("len(Connections) = %d, want 0 — another organization's connections must never appear", len(result.Connections))
	}
}

func TestList_FiltersByUserIDWhenSupplied(t *testing.T) {
	f, _, clock := newLifecycleFacade(t)
	mustInitiateAs(t, f, testOrg, testUser)
	clock.now = clock.now.Add(time.Minute)
	forUser2 := mustInitiateAs(t, f, testOrg, testUser2)

	result, err := f.List(context.Background(), testOrg, connections.ListParams{UserID: string(testUser2)})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Connections) != 1 || result.Connections[0].ID != forUser2.Connection.ID {
		t.Fatalf("Connections = %+v, want exactly user_2's own connection %q", result.Connections, forUser2.Connection.ID)
	}
}

func TestList_EmptyUserIDReturnsEveryUsersConnectionsInTheOrg(t *testing.T) {
	f, _, clock := newLifecycleFacade(t)
	mustInitiateAs(t, f, testOrg, testUser)
	clock.now = clock.now.Add(time.Minute)
	mustInitiateAs(t, f, testOrg, testUser2)

	result, err := f.List(context.Background(), testOrg, connections.ListParams{UserID: ""})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Connections) != 2 {
		t.Fatalf("len(Connections) = %d, want 2 (an empty userId means every user in the org)", len(result.Connections))
	}
}

func TestList_PaginatesAndOnlyReportsANextCursorWhenMorePagesRemain(t *testing.T) {
	f, _, clock := newLifecycleFacade(t)
	var ids []connections.ConnectionID
	for i := 0; i < 3; i++ {
		ids = append(ids, mustInitiateAs(t, f, testOrg, testUser).Connection.ID)
		clock.now = clock.now.Add(time.Minute)
	}

	firstPage, err := f.List(context.Background(), testOrg, connections.ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("List (first page): %v", err)
	}
	if len(firstPage.Connections) != 2 {
		t.Fatalf("len(first page) = %d, want 2", len(firstPage.Connections))
	}
	if firstPage.NextCursor == "" {
		t.Fatal("NextCursor is empty, want a cursor — a third connection remains")
	}
	if firstPage.Connections[0].ID != ids[2] || firstPage.Connections[1].ID != ids[1] {
		t.Fatalf("first page ids = [%q, %q], want the two newest [%q, %q]", firstPage.Connections[0].ID, firstPage.Connections[1].ID, ids[2], ids[1])
	}

	secondPage, err := f.List(context.Background(), testOrg, connections.ListParams{Limit: 2, Cursor: firstPage.NextCursor})
	if err != nil {
		t.Fatalf("List (second page): %v", err)
	}
	if len(secondPage.Connections) != 1 || secondPage.Connections[0].ID != ids[0] {
		t.Fatalf("second page = %+v, want exactly the oldest connection %q", secondPage.Connections, ids[0])
	}
	if secondPage.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty — this was the last page", secondPage.NextCursor)
	}
}

func TestList_RejectsAMalformedCursor(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)

	_, err := f.List(context.Background(), testOrg, connections.ListParams{Cursor: "not-valid-base64!!"})

	assertDomainError(t, err, connections.CodeValidationFailed, 422)
}

// --- Disable (AC2, AC11) ---

func TestDisable_TransitionsTheConnectionToDisconnected(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)

	disabled, err := f.Disable(context.Background(), testOrg, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if disabled.Status != connections.StatusDisconnected {
		t.Errorf("Status = %q, want %q", disabled.Status, connections.StatusDisconnected)
	}
	got, err := f.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != connections.StatusDisconnected {
		t.Errorf("persisted Status = %q, want %q", got.Status, connections.StatusDisconnected)
	}
}

func TestDisable_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)

	_, err := f.Disable(context.Background(), testOrg, connections.ConnectionID("conn_missing"))

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

// TestDisable_ReturnsNotFoundForAConnectionBelongingToAnotherOrganization is
// AC11.
func TestDisable_ReturnsNotFoundForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)

	_, err := f.Disable(context.Background(), otherOrg, initiated.Connection.ID)

	assertDomainError(t, err, connections.CodeNotFound, 404)
	got, getErr := f.Get(context.Background(), testOrg, initiated.Connection.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.Status == connections.StatusDisconnected {
		t.Error("connection was disabled via another organization's id — cross-org access must be a no-op")
	}
}

// --- Delete (AC3, AC11) ---

func TestDelete_RemovesTheConnectionSoASubsequentGetIsNotFound(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)

	if err := f.Delete(context.Background(), testOrg, initiated.Connection.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := f.Get(context.Background(), testOrg, initiated.Connection.ID)
	assertDomainError(t, err, connections.CodeNotFound, 404)
}

func TestDelete_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)

	err := f.Delete(context.Background(), testOrg, connections.ConnectionID("conn_missing"))

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

// TestDelete_ReturnsNotFoundForAConnectionBelongingToAnotherOrganizationAndLeavesItIntact
// is AC11: a cross-org delete must be rejected as not-found, and must never
// silently remove the other organization's row.
func TestDelete_ReturnsNotFoundForAConnectionBelongingToAnotherOrganizationAndLeavesItIntact(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)

	err := f.Delete(context.Background(), otherOrg, initiated.Connection.ID)

	assertDomainError(t, err, connections.CodeNotFound, 404)
	if _, getErr := f.Get(context.Background(), testOrg, initiated.Connection.ID); getErr != nil {
		t.Fatalf("connection was deleted via another organization's id: %v", getErr)
	}
}

// --- Reconnect (AC4, AC6, AC10, AC11) ---

func TestReconnect_RejectsAConnectionStillInitiated(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)

	_, err := f.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)

	assertDomainError(t, err, connections.CodeReconnectNotAllowed, 422)
}

func TestReconnect_AllowedFromDisconnected(t *testing.T) {
	f, repo, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)
	if err := repo.Update(context.Background(), initiated.Connection.Disable()); err != nil {
		t.Fatalf("seed disabled connection: %v", err)
	}

	_, err := f.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)

	if err != nil {
		t.Fatalf("unexpected error reconnecting a DISCONNECTED connection: %v", err)
	}
}

func TestReconnect_AllowedFromExpired(t *testing.T) {
	f, repo, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)
	if err := repo.Update(context.Background(), initiated.Connection.MarkExpired()); err != nil {
		t.Fatalf("seed expired connection: %v", err)
	}

	_, err := f.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)

	if err != nil {
		t.Fatalf("unexpected error reconnecting an EXPIRED connection: %v", err)
	}
}

func TestReconnect_RejectsARedirectURINotOnTheAllowList(t *testing.T) {
	f, repo, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)
	if err := repo.Update(context.Background(), initiated.Connection.Disable()); err != nil {
		t.Fatalf("seed disabled connection: %v", err)
	}

	_, err := f.Reconnect(context.Background(), testOrg, initiated.Connection.ID, disallowedRedirect)

	assertDomainError(t, err, connections.CodeValidationFailed, 422)
}

func TestReconnect_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f, _, _ := newLifecycleFacade(t)

	_, err := f.Reconnect(context.Background(), testOrg, connections.ConnectionID("conn_missing"), allowedRedirect)

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

// TestReconnect_ReturnsNotFoundForAConnectionBelongingToAnotherOrganization is
// AC11.
func TestReconnect_ReturnsNotFoundForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	f, repo, _ := newLifecycleFacade(t)
	initiated := mustInitiateAs(t, f, testOrg, testUser)
	if err := repo.Update(context.Background(), initiated.Connection.Disable()); err != nil {
		t.Fatalf("seed disabled connection: %v", err)
	}

	_, err := f.Reconnect(context.Background(), otherOrg, initiated.Connection.ID, allowedRedirect)

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

// TestReconnect_KeepsTheSameIDButMintsAFreshSingleUseConnectToken is AC4: the
// stable id survives, and the fresh connect token is distinct from — and
// usable independently of — the original one, which HandleCallback already
// marked used at activation.
func TestReconnect_KeepsTheSameIDButMintsAFreshSingleUseConnectToken(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	activateConnection(t, f, initiated)

	reconnected, err := f.facade.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)

	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if reconnected.Connection.ID != initiated.Connection.ID {
		t.Errorf("ID = %q, want the same stable id %q", reconnected.Connection.ID, initiated.Connection.ID)
	}
	if reconnected.Connection.ConnectToken == initiated.Connection.ConnectToken {
		t.Error("Reconnect reused the original (already-used) connect token — it must mint a fresh one")
	}
	if reconnected.Connection.ConnectTokenUsed {
		t.Error("ConnectTokenUsed = true, want false — the fresh handshake must be able to complete")
	}
}

// TestReconnect_ClearsPreviouslyCollectedParamsForReCollection is the
// reconnect note's params half: a provider that declares expectedParams must
// collect them again on a reconnect.
func TestReconnect_ClearsPreviouslyCollectedParamsForReCollection(t *testing.T) {
	f := newParamsHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	if _, err := f.facade.SubmitParams(context.Background(), initiated.Connection.ConnectToken, map[string]string{"region": "eu", "apiKey": "k"}); err != nil {
		t.Fatalf("SubmitParams: %v", err)
	}
	activateConnection(t, f, initiated)

	reconnected, err := f.facade.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)

	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if reconnected.Connection.EncryptedParams != "" {
		t.Errorf("EncryptedParams = %q, want empty — a reconnect must re-collect expected params", reconnected.Connection.EncryptedParams)
	}
}

// TestReconnect_LeavesThePreviousConnectionsStatusAndTokensUntouchedUntilCallbackCompletes
// is AC6's core: starting a reconnect must not disturb the connection's
// current ACTIVE status or its existing tokens — only a completed callback
// (Activate) does that. ResolveForExecution is asserted too, proving the
// still-ACTIVE connection keeps a usable access token throughout.
func TestReconnect_LeavesThePreviousConnectionsStatusAndTokensUntouchedUntilCallbackCompletes(t *testing.T) {
	f := newHandshakeFixture(newHappyPathOAuthClient())
	initiated := initiateConnection(t, f)
	activateConnection(t, f, initiated)
	before, err := f.facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get (before): %v", err)
	}

	if _, err := f.facade.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}

	after, err := f.facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get (after): %v", err)
	}
	if after.Status != connections.StatusActive {
		t.Errorf("Status after starting (but not completing) a reconnect = %q, want it to remain %q", after.Status, connections.StatusActive)
	}
	if after.EncryptedAccessToken != before.EncryptedAccessToken {
		t.Error("EncryptedAccessToken changed before any reconnect callback completed")
	}
	if after.EncryptedRefreshToken != before.EncryptedRefreshToken {
		t.Error("EncryptedRefreshToken changed before any reconnect callback completed")
	}

	access, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("ResolveForExecution: %v", err)
	}
	if access.Status != connections.StatusActive || access.AccessToken == "" {
		t.Errorf("ExecutionAccess = %+v, want an ACTIVE connection with a usable access token — an abandoned reconnect must not interrupt execution", access)
	}
}

// TestReconnectCompletion_ReactivatesUnderTheSameIDWithFreshTokensAndRefreshedAccountMetadata
// is AC5: completing a reconnect's handshake must activate the *same*
// connection id with the tokens/account metadata *this* handshake captured —
// not the original ones.
func TestReconnectCompletion_ReactivatesUnderTheSameIDWithFreshTokensAndRefreshedAccountMetadata(t *testing.T) {
	client := newHappyPathOAuthClient()
	f := newHandshakeFixture(client)
	initiated := initiateConnection(t, f)
	activateConnection(t, f, initiated)

	reconnected, err := f.facade.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}

	// Script the reconnect's own handshake with different tokens and account
	// metadata than the original activation returned.
	client.exchangeResult = connections.TokenExchangeResult{AccessToken: "fresh-access-token", RefreshToken: "fresh-refresh-token"}
	client.accountResult = connections.AccountInfo{Email: "ada.new@example.com", DisplayName: "Ada L."}

	view, err := f.facade.OpenConnectPage(context.Background(), reconnected.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")
	if _, err := f.facade.HandleCallback(context.Background(), "the-auth-code", state, ""); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}

	got, err := f.facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != initiated.Connection.ID {
		t.Errorf("ID = %q, want the same stable id %q", got.ID, initiated.Connection.ID)
	}
	if got.Status != connections.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, connections.StatusActive)
	}
	if got.AccountEmail != "ada.new@example.com" || got.AccountDisplayName != "Ada L." {
		t.Errorf("account metadata = (%q, %q), want the reconnect handshake's own refreshed values", got.AccountEmail, got.AccountDisplayName)
	}
	plaintext, err := f.vault.Decrypt(got.EncryptedAccessToken)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "fresh-access-token" {
		t.Errorf("decrypted access token = %q, want the reconnect handshake's own fresh token", plaintext)
	}
}

// TestReconnect_ReconnectingAnExpiredConnectionRestoresItToActiveWithTheSameID
// is AC10. It wires its own facade (rather than newHandshakeFixture) so the
// test can reach in via the Repository to seed the EXPIRED status directly,
// exactly as refresh.go's own MarkExpired transition would leave it after a
// failed refresh (AC9's other half, covered end to end in
// test/crucial_path/connection_lifecycle_journey_integration_test.go).
func TestReconnect_ReconnectingAnExpiredConnectionRestoresItToActiveWithTheSameID(t *testing.T) {
	repo := memory.NewRepository()
	client := newHappyPathOAuthClient()
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg: {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
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
	})

	initiated, err := facade.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	view, err := facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")
	if _, err := facade.HandleCallback(context.Background(), "the-auth-code", state, ""); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	active, err := facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := repo.Update(context.Background(), active.MarkExpired()); err != nil {
		t.Fatalf("seed expired connection: %v", err)
	}

	reconnected, err := facade.Reconnect(context.Background(), testOrg, initiated.Connection.ID, allowedRedirect)
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if reconnected.Connection.ID != initiated.Connection.ID {
		t.Fatalf("Reconnect ID = %q, want the same stable id %q", reconnected.Connection.ID, initiated.Connection.ID)
	}

	view2, err := facade.OpenConnectPage(context.Background(), reconnected.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage (reconnect): %v", err)
	}
	state2 := queryParam(t, view2.AuthorizeURL, "state")
	if _, err := facade.HandleCallback(context.Background(), "the-auth-code", state2, ""); err != nil {
		t.Fatalf("HandleCallback (reconnect): %v", err)
	}

	got, err := facade.Get(context.Background(), testOrg, initiated.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != connections.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, connections.StatusActive)
	}
	if got.ID != initiated.Connection.ID {
		t.Errorf("ID = %q, want the same stable id %q", got.ID, initiated.Connection.ID)
	}
}
