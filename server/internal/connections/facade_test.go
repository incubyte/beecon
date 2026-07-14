// Package connections_test exercises connections.Facade against the
// in-memory Repository and hand-written fakes for the three narrow
// cross-module reader ports (OrganizationReader, UserReader,
// IntegrationReader) — this keeps Initiate's own logic (allow-list check,
// not-found propagation, connect-token minting) isolated from
// organizations/catalog's own domain logic, which is covered by their own
// package tests.
package connections_test

import (
	"context"
	"errors"
	"testing"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

type fakeOrgReader struct {
	orgs map[organizations.OrgID]organizations.Organization
}

func (f fakeOrgReader) Get(_ context.Context, id organizations.OrgID) (organizations.Organization, error) {
	org, ok := f.orgs[id]
	if !ok {
		return organizations.Organization{}, organizations.ErrNotFound()
	}
	return org, nil
}

type fakeUserReader struct {
	users map[organizations.UserID]organizations.User
}

func (f fakeUserReader) GetUser(_ context.Context, org organizations.OrgID, id organizations.UserID) (organizations.User, error) {
	user, ok := f.users[id]
	if !ok || user.OrgID != org {
		return organizations.User{}, organizations.ErrUserNotFound()
	}
	return user, nil
}

type fakeIntegrationReader struct {
	integrations map[catalog.IntegrationID]catalog.Integration
}

func (f fakeIntegrationReader) GetIntegration(_ context.Context, id catalog.IntegrationID) (catalog.Integration, error) {
	integration, ok := f.integrations[id]
	if !ok {
		return catalog.Integration{}, catalog.ErrIntegrationNotFound()
	}
	return integration, nil
}

// GetVisibleIntegration is governance-blind in this fake (it has no
// governance concept of its own) — it always answers exactly like
// GetIntegration, so every existing test built before Slice 5 keeps its
// original behavior unchanged.
func (f fakeIntegrationReader) GetVisibleIntegration(ctx context.Context, _ organizations.OrgID, id catalog.IntegrationID) (catalog.Integration, error) {
	return f.GetIntegration(ctx, id)
}

const (
	testOrg            = organizations.OrgID("org_1")
	otherOrg           = organizations.OrgID("org_2")
	testUser           = organizations.UserID("user_1")
	testIntegration    = catalog.IntegrationID("intg_1")
	allowedRedirect    = "https://consumer.example.com/callback"
	disallowedRedirect = "https://evil.example.com/callback"
)

// newFacadeWithAllowList registers both testOrg and otherOrg with the given
// allow-list (otherOrg exists so cross-org tests exercise the user/connection
// lookup's own org check, rather than failing earlier at "org not found").
// testUser and testIntegration belong only to testOrg.
func newFacadeWithAllowList(allowList []string) *connections.Facade {
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg:  {ID: testOrg, Name: "Acme", AllowedRedirectURIs: allowList},
		otherOrg: {ID: otherOrg, Name: "Other", AllowedRedirectURIs: allowList},
	}
	user := organizations.User{ID: testUser, OrgID: testOrg, Name: "Ada"}
	integration := catalog.Integration{ID: testIntegration, ProviderSlug: "outlook", ClientID: "cid", ClientSecret: "csecret"}

	return memory.NewFacadeWithOverrides(memory.Overrides{
		Organizations: fakeOrgReader{orgs: orgs},
		Users:         fakeUserReader{users: map[organizations.UserID]organizations.User{testUser: user}},
		Integrations:  fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{testIntegration: integration}},
	})
}

func assertDomainError(t *testing.T, err error, wantCode string, wantStatus int) *httpx.DomainError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected domain error with code %q, got nil", wantCode)
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Code != wantCode {
		t.Fatalf("error code = %q, want %q", de.Code, wantCode)
	}
	if de.Status != wantStatus {
		t.Fatalf("error status = %d, want %d", de.Status, wantStatus)
	}
	return de
}

func TestInitiate_MintsAConnPrefixedIDAndStartsInitiated(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	initiated, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if initiated.Connection.ID != "conn_1" {
		t.Errorf("ID = %q, want %q (deterministic sequential id from the memory fake)", initiated.Connection.ID, "conn_1")
	}
	if initiated.Connection.Status != connections.StatusInitiated {
		t.Errorf("Status = %q, want %q", initiated.Connection.Status, connections.StatusInitiated)
	}
}

func TestInitiate_RecordsTheIntegrationsProviderSlugOnTheConnection(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	initiated, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if initiated.Connection.ProviderSlug != "outlook" {
		t.Errorf("ProviderSlug = %q, want %q", initiated.Connection.ProviderSlug, "outlook")
	}
}

// TestInitiate_RedirectURLIsBoundToExactlyThisConnectionAttempt is AC8: the
// redirectUrl points at Beecon's own connect page carrying the single-use
// token minted for THIS connection — not a shared/reusable token, and not
// another connection's token.
func TestInitiate_RedirectURLIsBoundToExactlyThisConnectionAttempt(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})
	ctx := context.Background()

	first, err := f.Initiate(ctx, testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.Initiate(ctx, testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantURL := "http://localhost:8080/connect/" + first.Connection.ConnectToken
	if first.RedirectURL != wantURL {
		t.Errorf("RedirectURL = %q, want %q (baseURL + connect path + this connection's own token)", first.RedirectURL, wantURL)
	}
	if first.Connection.ConnectToken == "" {
		t.Fatal("ConnectToken must not be empty")
	}
	if first.Connection.ConnectToken == second.Connection.ConnectToken {
		t.Error("two separate Initiate calls minted the same connect token — tokens must be single-use and bound to one connection attempt")
	}
	if first.RedirectURL == second.RedirectURL {
		t.Error("two separate Initiate calls produced the same redirectUrl — each must be bound to its own connection attempt")
	}
}

func TestInitiate_AllowsAnExactRedirectURIMatch(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	_, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitiate_RejectsARedirectURINotOnTheAllowList(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	_, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, disallowedRedirect)

	de := assertDomainError(t, err, connections.CodeValidationFailed, 422)
	if de.Details["field"] != "redirectUri" {
		t.Errorf("error details field = %v, want %q", de.Details["field"], "redirectUri")
	}
}

// TestInitiate_RejectsEveryRedirectURIWhenTheAllowListIsEmpty is PD4's
// secure default: no open redirect.
func TestInitiate_RejectsEveryRedirectURIWhenTheAllowListIsEmpty(t *testing.T) {
	f := newFacadeWithAllowList(nil)

	_, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)

	assertDomainError(t, err, connections.CodeValidationFailed, 422)
}

func TestInitiate_AllowsAnOriginOnlyAllowListEntryToMatchAnyPathUnderThatOrigin(t *testing.T) {
	f := newFacadeWithAllowList([]string{"https://consumer.example.com"})

	_, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, "https://consumer.example.com/some/deep/callback")

	if err != nil {
		t.Fatalf("unexpected error: %v (an origin-only allow-list entry must match any path under that origin)", err)
	}
}

func TestInitiate_ReturnsNotFoundForAnUnknownUserID(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	_, err := f.Initiate(context.Background(), testOrg, organizations.UserID("user_missing"), testIntegration, allowedRedirect)

	assertDomainError(t, err, "not_found", 404)
}

// TestInitiate_ReturnsNotFoundForAUserBelongingToAnotherOrganization covers
// the cross-org branch of AC10: a userID that exists, but in a different
// organization, must surface identically to an unknown userID. otherOrg
// itself resolves fine (it's a registered, allow-listed organization) so this
// exercises the user lookup's own org check, not an earlier org-not-found
// short-circuit.
func TestInitiate_ReturnsNotFoundForAUserBelongingToAnotherOrganization(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	_, err := f.Initiate(context.Background(), otherOrg, testUser, testIntegration, allowedRedirect)

	assertDomainError(t, err, "not_found", 404)
}

func TestInitiate_ReturnsNotFoundForAnUnknownIntegrationID(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	_, err := f.Initiate(context.Background(), testOrg, testUser, catalog.IntegrationID("intg_missing"), allowedRedirect)

	assertDomainError(t, err, "not_found", 404)
}

func TestGet_ReturnsAPreviouslyInitiatedConnection(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})
	initiated, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.Get(context.Background(), testOrg, initiated.Connection.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != initiated.Connection.ID {
		t.Errorf("ID = %q, want %q", got.ID, initiated.Connection.ID)
	}
	if got.Status != connections.StatusInitiated {
		t.Errorf("Status = %q, want %q", got.Status, connections.StatusInitiated)
	}
}

func TestGet_ReturnsNotFoundForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})
	initiated, err := f.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = f.Get(context.Background(), otherOrg, initiated.Connection.ID)

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

func TestGet_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f := newFacadeWithAllowList([]string{allowedRedirect})

	_, err := f.Get(context.Background(), testOrg, connections.ConnectionID("conn_missing"))

	assertDomainError(t, err, connections.CodeNotFound, 404)
}
