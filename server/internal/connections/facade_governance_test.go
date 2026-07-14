// facade_governance_test.go exercises Initiate's governance guard (Slice 5's
// AC5, PD42): "an org can never initiate a connection to an integration it
// cannot see." Unlike facade_test.go's fakeIntegrationReader (deliberately
// governance-blind, so every pre-Slice-5 test keeps its original behavior),
// this file wires the REAL catalog.Facade backed by a REAL organizations.Facade
// GovernanceReader as connections.Facade's own IntegrationReader — connections
// already depends on both modules (BOUNDARIES) — so Initiate's call to
// GetVisibleIntegration (not GetIntegration) is exercised exactly as
// production composes it, entirely in memory. This is the fast, package-level
// half of AC5's isolation proof; test/crucial_path/governance_journey_integration_test.go
// exercises the identical story over real HTTP against the full composition
// root.
package connections_test

import (
	"context"
	"errors"
	"testing"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
	orgsmemory "beecon/internal/organizations/driven/memory"
)

func governedFakeDefinitions() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "outlook",
			Name:         "Outlook",
			AuthScheme:   "oauth2",
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
		},
	}
}

// governedFixture wires a connections.Facade whose IntegrationReader is a
// real catalog.Facade, itself governed by a real organizations.Facade — the
// exact multi-module composition app/wiring.go builds in production.
type governedFixture struct {
	connections *connections.Facade
	catalog     *catalog.Facade
	orgs        *organizations.Facade
}

func newGovernedFixture(t *testing.T) governedFixture {
	t.Helper()
	orgs := orgsmemory.NewFacadeWithOverrides(orgsmemory.Overrides{})
	catalogFacade, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: governedFakeDefinitions(),
		Governance:  orgs,
	})
	if err != nil {
		t.Fatalf("catalog NewFacadeWithOverrides: %v", err)
	}
	connectionsFacade := memory.NewFacadeWithOverrides(memory.Overrides{
		Organizations: orgs,
		Users:         orgs,
		Integrations:  catalogFacade,
	})
	return governedFixture{connections: connectionsFacade, catalog: catalogFacade, orgs: orgs}
}

func (f governedFixture) mustCreateOrgAllowingRedirect(t *testing.T, name, redirectURI string) organizations.OrgID {
	t.Helper()
	org, err := f.orgs.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("create org %q: %v", name, err)
	}
	if _, err := f.orgs.SetAllowedRedirectURIs(context.Background(), org.ID, []string{redirectURI}); err != nil {
		t.Fatalf("set allowed redirect uris for %q: %v", name, err)
	}
	return org.ID
}

func (f governedFixture) mustCreateUser(t *testing.T, org organizations.OrgID, name string) organizations.UserID {
	t.Helper()
	user, err := f.orgs.CreateUser(context.Background(), org, name, "")
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	return user.ID
}

func (f governedFixture) mustCreateIntegration(t *testing.T) catalog.IntegrationID {
	t.Helper()
	created, err := f.catalog.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("create integration: %v", err)
	}
	return created.ID
}

func (f governedFixture) mustSetGovernance(t *testing.T, org organizations.OrgID, update organizations.GovernanceUpdate) {
	t.Helper()
	if _, err := f.orgs.SetGovernance(context.Background(), org, update); err != nil {
		t.Fatalf("SetGovernance(%s): %v", org, err)
	}
}

const governedRedirect = "https://consumer.example.com/callback"

// TestInitiate_AnUnconfiguredOrgCanInitiateToAnyIntegration_PD42Continuity
// pins PD42's continuity guarantee through Initiate specifically: an org
// that has never had its governance configured behaves exactly as every
// pre-Slice-5 Initiate test already proved.
func TestInitiate_AnUnconfiguredOrgCanInitiateToAnyIntegration_PD42Continuity(t *testing.T) {
	f := newGovernedFixture(t)
	org := f.mustCreateOrgAllowingRedirect(t, "Acme", governedRedirect)
	user := f.mustCreateUser(t, org, "Ada")
	integration := f.mustCreateIntegration(t)

	_, err := f.connections.Initiate(context.Background(), org, user, integration, governedRedirect)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestInitiate_RejectsInitiatingToAHiddenIntegration is AC5's hidden branch:
// the initiate call must fail exactly as if the integration did not exist.
func TestInitiate_RejectsInitiatingToAHiddenIntegration(t *testing.T) {
	f := newGovernedFixture(t)
	org := f.mustCreateOrgAllowingRedirect(t, "Acme", governedRedirect)
	user := f.mustCreateUser(t, org, "Ada")
	integration := f.mustCreateIntegration(t)
	f.mustSetGovernance(t, org, organizations.GovernanceUpdate{Hidden: []string{string(integration)}})

	_, err := f.connections.Initiate(context.Background(), org, user, integration, governedRedirect)

	if de, ok := errorsAsDomainError(err); !ok || de.Status != 404 {
		t.Fatalf("expected a 404 not-found error, got: %v", err)
	}
}

// TestInitiate_RejectsInitiatingToANonAllowListedIntegration is AC5's
// allow-list branch.
func TestInitiate_RejectsInitiatingToANonAllowListedIntegration(t *testing.T) {
	f := newGovernedFixture(t)
	org := f.mustCreateOrgAllowingRedirect(t, "Acme", governedRedirect)
	user := f.mustCreateUser(t, org, "Ada")
	integration := f.mustCreateIntegration(t)
	f.mustSetGovernance(t, org, organizations.GovernanceUpdate{AllowList: &[]string{"intg_someone_else"}})

	_, err := f.connections.Initiate(context.Background(), org, user, integration, governedRedirect)

	if de, ok := errorsAsDomainError(err); !ok || de.Status != 404 {
		t.Fatalf("expected a 404 not-found error, got: %v", err)
	}
}

// TestInitiate_ASubsequentGetVisibleIntegrationIsWhatIsCalled_NotTheGovernanceBlindGetIntegration
// pins that Initiate's not-found for a hidden integration is indistinguishable
// from an unknown integration id — the same not-found code/status either way,
// so a caller can never use the response to infer "this integration exists
// but I can't see it" versus "this integration doesn't exist at all".
func TestInitiate_HiddenIntegrationNotFoundIsIndistinguishableFromAnUnknownIntegrationID(t *testing.T) {
	f := newGovernedFixture(t)
	org := f.mustCreateOrgAllowingRedirect(t, "Acme", governedRedirect)
	user := f.mustCreateUser(t, org, "Ada")
	integration := f.mustCreateIntegration(t)
	f.mustSetGovernance(t, org, organizations.GovernanceUpdate{Hidden: []string{string(integration)}})

	_, hiddenErr := f.connections.Initiate(context.Background(), org, user, integration, governedRedirect)
	_, unknownErr := f.connections.Initiate(context.Background(), org, user, catalog.IntegrationID("intg_does_not_exist"), governedRedirect)

	hiddenDE, ok1 := errorsAsDomainError(hiddenErr)
	unknownDE, ok2 := errorsAsDomainError(unknownErr)
	if !ok1 || !ok2 {
		t.Fatalf("expected both errors to be domain errors: hidden=%v unknown=%v", hiddenErr, unknownErr)
	}
	if hiddenDE.Code != unknownDE.Code || hiddenDE.Status != unknownDE.Status {
		t.Errorf("hidden error = {%s %d}, unknown error = {%s %d}; want identical", hiddenDE.Code, hiddenDE.Status, unknownDE.Code, unknownDE.Status)
	}
}

// TestInitiate_GovernanceIsStrictlyOrgScoped_TwoOrgsDifferentGovernanceNoCrossOrgBleed
// is AC5's headline isolation proof at the connections facade: two
// organizations independently curate the same installation catalog, and
// Initiate must obey only the calling org's own governance — never bleed
// into, or be blocked by, the other's rules.
func TestInitiate_GovernanceIsStrictlyOrgScoped_TwoOrgsDifferentGovernanceNoCrossOrgBleed(t *testing.T) {
	f := newGovernedFixture(t)
	orgA := f.mustCreateOrgAllowingRedirect(t, "Org A", governedRedirect)
	orgB := f.mustCreateOrgAllowingRedirect(t, "Org B", governedRedirect)
	userA := f.mustCreateUser(t, orgA, "Ada (org A)")
	userB := f.mustCreateUser(t, orgB, "Bea (org B)")
	intgX := f.mustCreateIntegration(t)
	intgY := f.mustCreateIntegration(t)
	// Org A allow-lists only X; org B hides X (so B can only see/initiate Y).
	f.mustSetGovernance(t, orgA, organizations.GovernanceUpdate{AllowList: &[]string{string(intgX)}})
	f.mustSetGovernance(t, orgB, organizations.GovernanceUpdate{Hidden: []string{string(intgX)}})

	t.Run("org A can initiate to its own visible X", func(t *testing.T) {
		_, err := f.connections.Initiate(context.Background(), orgA, userA, intgX, governedRedirect)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("org A cannot initiate to Y (never allow-listed for A)", func(t *testing.T) {
		_, err := f.connections.Initiate(context.Background(), orgA, userA, intgY, governedRedirect)
		if de, ok := errorsAsDomainError(err); !ok || de.Status != 404 {
			t.Fatalf("expected a 404 not-found error, got: %v", err)
		}
	})

	t.Run("org B can initiate to its own visible Y", func(t *testing.T) {
		_, err := f.connections.Initiate(context.Background(), orgB, userB, intgY, governedRedirect)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("org B cannot initiate to X (hidden for B)", func(t *testing.T) {
		_, err := f.connections.Initiate(context.Background(), orgB, userB, intgX, governedRedirect)
		if de, ok := errorsAsDomainError(err); !ok || de.Status != 404 {
			t.Fatalf("expected a 404 not-found error, got: %v", err)
		}
	})
}

// errorsAsDomainError is a small local helper mirroring assertDomainError's
// own errors.As unwrap, returning ok=false rather than failing the test
// immediately so callers can compose sub-assertions (used by table-style
// t.Run cases in this file).
func errorsAsDomainError(err error) (*httpx.DomainError, bool) {
	if err == nil {
		return nil, false
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		return nil, false
	}
	return de, true
}
