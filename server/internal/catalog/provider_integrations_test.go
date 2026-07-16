// provider_integrations_test.go exercises catalog.Facade's operator-only,
// installation-wide read of one provider's created integrations:
// ListIntegrationsForProvider. Reuses fakeDefinitions/toolCatalogDefinitions
// (facade_test.go) and newOrgsFacade/mustCreateOrg/mustSetGovernance
// (governance_visibility_test.go, same package) for the AC-equivalent
// unfiltered proof this mirrors from provider_definitions_test.go.
package catalog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

func TestListIntegrationsForProvider_ReturnsOnlyIntegrationsMatchingTheGivenProviderSlug(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())
	ctx := context.Background()
	outlookIntegration, err := f.CreateIntegration(ctx, "outlook", "outlook-client", "outlook-secret")
	if err != nil {
		t.Fatalf("CreateIntegration(outlook): %v", err)
	}
	if _, err := f.CreateIntegration(ctx, "slack", "slack-client", "slack-secret"); err != nil {
		t.Fatalf("CreateIntegration(slack): %v", err)
	}

	summaries, err := f.ListIntegrationsForProvider(ctx, "outlook")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1 (only the outlook integration, not slack's)", len(summaries))
	}
	if summaries[0].ID != outlookIntegration.ID {
		t.Errorf("summaries[0].ID = %q, want %q", summaries[0].ID, outlookIntegration.ID)
	}
	if summaries[0].ProviderSlug != "outlook" {
		t.Errorf("summaries[0].ProviderSlug = %q, want %q", summaries[0].ProviderSlug, "outlook")
	}
}

func TestListIntegrationsForProvider_ReturnsEmptySliceForAValidProviderWithNoIntegrations(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())

	summaries, err := f.ListIntegrationsForProvider(context.Background(), "slack")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summaries == nil {
		t.Error("summaries = nil, want a non-nil empty slice")
	}
	if len(summaries) != 0 {
		t.Fatalf("len(summaries) = %d, want 0", len(summaries))
	}
}

func TestListIntegrationsForProvider_ReturnsNotFoundForAnUnknownProviderSlug(t *testing.T) {
	f := newProviderDefinitionsFacade(t, fakeDefinitions())

	_, err := f.ListIntegrationsForProvider(context.Background(), "does-not-exist")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// TestListIntegrationsForProvider_IsNotFilteredByAnOrgsRestrictiveGovernance
// mirrors TestListProviderDefinitions_IsNotFilteredByAnOrgsRestrictiveGovernance:
// ListIntegrationsForProvider takes no organization parameter at all, so an
// org's most restrictive governance (empty allow-list plus an explicit hide)
// must have zero effect on this operator-only, installation-wide view.
func TestListIntegrationsForProvider_IsNotFilteredByAnOrgsRestrictiveGovernance(t *testing.T) {
	orgs := newOrgsFacade()
	restrictedOrg := mustCreateOrg(t, orgs, "Restricted Co")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, restrictedOrg, organizations.GovernanceUpdate{
		AllowList: &[]string{},
		Hidden:    []string{string(created.ID)},
	})

	// Sanity: the org-facing, governance-filtered view really does see
	// nothing — proves the governance actually took effect.
	orgFacing, err := f.ListIntegrations(context.Background(), restrictedOrg)
	if err != nil {
		t.Fatalf("ListIntegrations: %v", err)
	}
	if len(orgFacing) != 0 {
		t.Fatalf("org-facing ListIntegrations = %+v, want empty — governance setup did not take effect, test is invalid", orgFacing)
	}

	summaries, err := f.ListIntegrationsForProvider(context.Background(), "outlook")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != created.ID {
		t.Fatalf("ListIntegrationsForProvider = %+v, want the outlook integration present despite %s's restrictive governance", summaries, restrictedOrg)
	}
}

// TestListIntegrationsForProvider_SummaryNeverSerializesTheClientSecret is a
// belt-and-suspenders JSON-bytes proof, the same style as
// TestCreateIntegration_SummaryNeverSerializesTheClientSecret: even though
// IntegrationSummary carries no ClientSecret field, this guards against a
// future field addition silently leaking the secret through this endpoint's
// response shape.
func TestListIntegrationsForProvider_SummaryNeverSerializesTheClientSecret(t *testing.T) {
	f := newProviderDefinitionsFacade(t, fakeDefinitions())
	const distinctiveSecret = "super-secret-oauth-client-secret-value"
	if _, err := f.CreateIntegration(context.Background(), "outlook", "client-id", distinctiveSecret); err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	summaries, err := f.ListIntegrationsForProvider(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	encoded, err := json.Marshal(summaries)
	if err != nil {
		t.Fatalf("marshal summaries: %v", err)
	}
	if strings.Contains(string(encoded), distinctiveSecret) {
		t.Fatalf("ListIntegrationsForProvider JSON %s contains the client secret", encoded)
	}
	if strings.Contains(string(encoded), "clientSecret") || strings.Contains(string(encoded), "client_secret") {
		t.Fatalf("ListIntegrationsForProvider JSON %s carries a client-secret-shaped field at all", encoded)
	}
}
