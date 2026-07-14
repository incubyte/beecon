// governance_visibility_test.go exercises catalog.Facade's governance
// enforcement (Slice 5's core-risk seam, PD42/PD43): ListIntegrations,
// GetVisibleIntegration, ListIntegrationsWithVisibility,
// ListFeaturedIntegrations, and the previously-ignored org param in
// ListTools/ListTriggerDefinitions. It wires the REAL organizations.Facade
// (in-memory) as the GovernanceReader — not a hand-rolled stub — so
// GetGovernance's own continuity-preserving default (PD42) and SetGovernance's
// validation are exercised exactly as production wires them (catalog already
// depends on organizations, BOUNDARIES), while still running fully in
// memory. Reuses fakeDefinitions, toolCatalogDefinitions,
// triggerCatalogDefinitions, minimalSchema, and assertDomainError from
// facade_test.go (same package).
package catalog_test

import (
	"context"
	"testing"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/organizations"
	orgsmemory "beecon/internal/organizations/driven/memory"
)

// newOrgsFacade builds a real organizations.Facade over its own in-memory
// repository — the same GovernanceReader production wires into catalog
// (app/wiring.go).
func newOrgsFacade() *organizations.Facade {
	return orgsmemory.NewFacadeWithOverrides(orgsmemory.Overrides{})
}

// newGovernedCatalogFacade wires a catalog.Facade against defs with orgs (a
// real organizations.Facade) as its GovernanceReader.
func newGovernedCatalogFacade(t *testing.T, orgs *organizations.Facade, defs []catalog.ProviderDefinition) *catalog.Facade {
	t.Helper()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{Definitions: defs, Governance: orgs})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
}

func mustCreateOrg(t *testing.T, orgs *organizations.Facade, name string) organizations.OrgID {
	t.Helper()
	org, err := orgs.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("create organization %q: %v", name, err)
	}
	return org.ID
}

func mustSetGovernance(t *testing.T, orgs *organizations.Facade, org organizations.OrgID, update organizations.GovernanceUpdate) {
	t.Helper()
	if _, err := orgs.SetGovernance(context.Background(), org, update); err != nil {
		t.Fatalf("SetGovernance(%s): %v", org, err)
	}
}

// --- PD42 continuity: an org with no governance row sees the full catalog,
// exactly Phase 1's PD7 behavior, across every governance-aware read. ---

func TestListIntegrations_AnUnconfiguredOrgSeesTheFullInstallationCatalog_PD42Continuity(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	first, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	second, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	summaries, err := f.ListIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 2 || summaries[0].ID != first.ID || summaries[1].ID != second.ID {
		t.Fatalf("summaries = %+v, want both created integrations in creation order — an unconfigured org's governance must never restrict anything (PD42)", summaries)
	}
}

func TestGetVisibleIntegration_AnUnconfiguredOrgCanSeeEveryIntegration_PD42Continuity(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	got, err := f.GetVisibleIntegration(context.Background(), org, created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestListTools_AnUnconfiguredOrgSeesEveryProvidersTools_PD42Continuity(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, toolCatalogDefinitions())

	page, err := f.ListTools(context.Background(), org, catalog.ToolFilter{}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("len(Items) = %d, want 3 (both providers' non-deprecated tools, unrestricted)", len(page.Items))
	}
}

func TestListTriggerDefinitions_AnUnconfiguredOrgSeesEveryProvidersTriggers_PD42Continuity(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, triggerCatalogDefinitions())

	page, err := f.ListTriggerDefinitions(context.Background(), org, catalog.TriggerDefinitionFilter{}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 (both providers' triggers, unrestricted)", len(page.Items))
	}
}

// --- Allow-list enforcement ---

func TestListIntegrations_WithAnAllowListSetReturnsOnlyTheAllowedIntegrations(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	allowed, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	notAllowed, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{AllowList: &[]string{string(allowed.ID)}})

	summaries, err := f.ListIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != allowed.ID {
		t.Fatalf("summaries = %+v, want only %q", summaries, allowed.ID)
	}
	for _, s := range summaries {
		if s.ID == notAllowed.ID {
			t.Fatalf("non-allow-listed integration %q present in the result", notAllowed.ID)
		}
	}
}

func TestGetVisibleIntegration_ReturnsNotFoundForANonAllowListedIntegration(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	allowed, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	notAllowed, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{AllowList: &[]string{string(allowed.ID)}})

	_, err = f.GetVisibleIntegration(ctx, org, notAllowed.ID)

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

func TestListTools_ReturnsAnEmptyPageNotAnErrorWhenFilteredByANonAllowListedIntegration(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, toolCatalogDefinitions())
	ctx := context.Background()
	slackIntegration, err := f.CreateIntegration(ctx, "slack", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	// Allow-list some other integration id, so slackIntegration is excluded.
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{AllowList: &[]string{"intg_someone_else"}})

	page, err := f.ListTools(ctx, org, catalog.ToolFilter{IntegrationID: slackIntegration.ID}, "", 0)

	if err != nil {
		t.Fatalf("expected an empty page, not an error, got: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty", page.Items)
	}
}

func TestListTriggerDefinitions_ReturnsAnEmptyPageNotAnErrorWhenFilteredByANonAllowListedIntegration(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, triggerCatalogDefinitions())
	ctx := context.Background()
	hubspotIntegration, err := f.CreateIntegration(ctx, "hubspot", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{AllowList: &[]string{"intg_someone_else"}})

	page, err := f.ListTriggerDefinitions(ctx, org, catalog.TriggerDefinitionFilter{IntegrationID: hubspotIntegration.ID}, "", 0)

	if err != nil {
		t.Fatalf("expected an empty page, not an error, got: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty", page.Items)
	}
}

// --- Hidden enforcement: hidden always wins, even over an allow-list. ---

func TestListIntegrations_ExcludesAHiddenIntegrationEvenWhenItIsAllowListed(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	integration, err := f.CreateIntegration(ctx, "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{
		AllowList: &[]string{string(integration.ID)},
		Hidden:    []string{string(integration.ID)},
	})

	summaries, err := f.ListIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want empty — hidden must win over an explicit allow-list entry", summaries)
	}
}

func TestGetVisibleIntegration_ReturnsNotFoundForAHiddenIntegration(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	integration, err := f.CreateIntegration(ctx, "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{Hidden: []string{string(integration.ID)}})

	_, err = f.GetVisibleIntegration(ctx, org, integration.ID)

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

func TestListTools_ReturnsAnEmptyPageWhenFilteredByAHiddenIntegration(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, toolCatalogDefinitions())
	ctx := context.Background()
	slackIntegration, err := f.CreateIntegration(ctx, "slack", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{Hidden: []string{string(slackIntegration.ID)}})

	page, err := f.ListTools(ctx, org, catalog.ToolFilter{IntegrationID: slackIntegration.ID}, "", 0)

	if err != nil {
		t.Fatalf("expected an empty page, not an error, got: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty", page.Items)
	}
}

func TestListTriggerDefinitions_ReturnsAnEmptyPageWhenFilteredByAHiddenIntegration(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, triggerCatalogDefinitions())
	ctx := context.Background()
	hubspotIntegration, err := f.CreateIntegration(ctx, "hubspot", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{Hidden: []string{string(hubspotIntegration.ID)}})

	page, err := f.ListTriggerDefinitions(ctx, org, catalog.TriggerDefinitionFilter{IntegrationID: hubspotIntegration.ID}, "", 0)

	if err != nil {
		t.Fatalf("expected an empty page, not an error, got: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty", page.Items)
	}
}

// TestListTools_UnfilteredListDropsAProviderWhoseEveryIntegrationIsHiddenForOrg
// is visibleProviderSlugs' documented behavior: with no explicit
// filter.IntegrationID, a provider all of whose integrations org cannot see
// disappears from the unfiltered list entirely.
func TestListTools_UnfilteredListDropsAProviderWhoseEveryIntegrationIsHiddenForOrg(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, toolCatalogDefinitions())
	ctx := context.Background()
	outlookIntegration, err := f.CreateIntegration(ctx, "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{Hidden: []string{string(outlookIntegration.ID)}})

	page, err := f.ListTools(ctx, org, catalog.ToolFilter{}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, item := range page.Items {
		if item.ProviderSlug == "outlook" {
			t.Fatalf("outlook tool %q present, want outlook dropped entirely — its only integration is hidden for this org", item.Slug)
		}
	}
	if len(page.Items) != 1 || page.Items[0].ProviderSlug != "slack" {
		t.Fatalf("Items = %+v, want only slack's tool (slack has no integration at all, so it stays visible)", page.Items)
	}
}

// TestListTools_AProviderWithNoIntegrationsAtAllStaysVisibleRegardlessOfGovernance
// pins visibleProviderSlugs' other documented branch: a provider nobody has
// created an Integration for has nothing concrete to hide, so it remains
// visible even under a restrictive allow-list — preserving ListTools'
// pre-governance "providerSlug filter works with zero created integrations"
// behavior (existing tests built before Slice 5 rely on this).
func TestListTools_AProviderWithNoIntegrationsAtAllStaysVisibleRegardlessOfGovernance(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, toolCatalogDefinitions())
	// An allow-list naming an integration id that doesn't exist: neither
	// outlook nor slack has ANY created Integration, so both providers must
	// stay visible per the "no integration to hide" rule.
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{AllowList: &[]string{"intg_does_not_exist"}})

	page, err := f.ListTools(context.Background(), org, catalog.ToolFilter{}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("len(Items) = %d, want 3 (both providers stay visible: neither has a created integration to restrict)", len(page.Items))
	}
}

// --- ListIntegrationsWithVisibility (AC1: the operator's unfiltered view) ---

func TestListIntegrationsWithVisibility_AnnotatesEachIntegrationsEffectiveVisibilityForOrg(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	visible, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	hidden, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	notAllowed, err := f.CreateIntegration(ctx, "outlook", "client-3", "secret-3")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{
		AllowList: &[]string{string(visible.ID), string(hidden.ID)},
		Hidden:    []string{string(hidden.ID)},
	})

	items, err := f.ListIntegrationsWithVisibility(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3 (every installation integration, unfiltered)", len(items))
	}
	byID := map[catalog.IntegrationID]string{}
	for _, item := range items {
		byID[item.Integration.ID] = item.Visibility
	}
	if byID[visible.ID] != catalog.VisibilityVisible {
		t.Errorf("visible integration's visibility = %q, want %q", byID[visible.ID], catalog.VisibilityVisible)
	}
	if byID[hidden.ID] != catalog.VisibilityHidden {
		t.Errorf("hidden integration's visibility = %q, want %q", byID[hidden.ID], catalog.VisibilityHidden)
	}
	if byID[notAllowed.ID] != catalog.VisibilityNotAllowed {
		t.Errorf("non-allow-listed integration's visibility = %q, want %q", byID[notAllowed.ID], catalog.VisibilityNotAllowed)
	}
}

// --- ListFeaturedIntegrations (AC7, PD43) ---

func TestListFeaturedIntegrations_ReturnsTheOrderedFeaturedSubset(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	first, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	second, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	// Feature them in the reverse of creation order, to prove the operator's
	// order wins over ListIntegrations' own creation-order default.
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{Featured: []string{string(second.ID), string(first.ID)}})

	featured, err := f.ListFeaturedIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(featured) != 2 || featured[0].ID != second.ID || featured[1].ID != first.ID {
		t.Fatalf("featured = %+v, want [%q %q] in the configured order", featured, second.ID, first.ID)
	}
}

func TestListFeaturedIntegrations_FallsBackToTheFirstNVisibleWhenNoneAreFeatured(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	first, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	second, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	// No SetGovernance call at all: continuity default, FeaturedCap 8, no
	// Featured list configured.

	featured, err := f.ListFeaturedIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(featured) != 2 || featured[0].ID != first.ID || featured[1].ID != second.ID {
		t.Fatalf("featured = %+v, want the first-N visible integrations (creation order) as the onboarding fallback", featured)
	}
}

// TestListFeaturedIntegrations_FallbackIsTruncatedToTheConfiguredCap proves
// the fallback path (firstNIntegrations) actually honors FeaturedCap, not
// just "return everything visible".
func TestListFeaturedIntegrations_FallbackIsTruncatedToTheConfiguredCap(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	var firstID catalog.IntegrationID
	for i := 0; i < 3; i++ {
		created, err := f.CreateIntegration(ctx, "outlook", "client", "secret")
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		if i == 0 {
			firstID = created.ID
		}
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{FeaturedCap: 1})

	featured, err := f.ListFeaturedIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(featured) != 1 {
		t.Fatalf("len(featured) = %d, want 1 (truncated to the configured cap)", len(featured))
	}
	if featured[0].ID != firstID {
		t.Errorf("featured[0].ID = %q, want the first-created integration %q", featured[0].ID, firstID)
	}
}

// TestListFeaturedIntegrations_SkipsAFeaturedIDNoLongerVisible pins
// orderByFeaturedList's documented skip behavior: a featured id that has
// since been hidden must not resurface.
func TestListFeaturedIntegrations_SkipsAFeaturedIDNoLongerVisible(t *testing.T) {
	orgs := newOrgsFacade()
	org := mustCreateOrg(t, orgs, "Acme")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	stillVisible, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	nowHidden, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	mustSetGovernance(t, orgs, org, organizations.GovernanceUpdate{
		Featured: []string{string(nowHidden.ID), string(stillVisible.ID)},
		Hidden:   []string{string(nowHidden.ID)},
	})

	featured, err := f.ListFeaturedIntegrations(ctx, org)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(featured) != 1 || featured[0].ID != stillVisible.ID {
		t.Fatalf("featured = %+v, want only %q (the hidden featured id must be silently skipped)", featured, stillVisible.ID)
	}
}

// --- AC5 / isolation: governance is strictly org-scoped through the whole
// catalog facade — no cross-org bleed. ---

// TestCatalogGovernance_IsStrictlyOrgScoped_TwoOrgsDifferentGovernanceNoCrossOrgBleed
// is the seam's headline isolation test: two organizations independently
// curate a shared installation catalog, and every governance-aware read
// (ListIntegrations, GetVisibleIntegration) must reflect only the org asked
// about — never the other's rules.
func TestCatalogGovernance_IsStrictlyOrgScoped_TwoOrgsDifferentGovernanceNoCrossOrgBleed(t *testing.T) {
	orgs := newOrgsFacade()
	orgA := mustCreateOrg(t, orgs, "Org A")
	orgB := mustCreateOrg(t, orgs, "Org B")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	ctx := context.Background()
	intgX, err := f.CreateIntegration(ctx, "outlook", "client-x", "secret-x")
	if err != nil {
		t.Fatalf("CreateIntegration X: %v", err)
	}
	intgY, err := f.CreateIntegration(ctx, "outlook", "client-y", "secret-y")
	if err != nil {
		t.Fatalf("CreateIntegration Y: %v", err)
	}
	// Org A allow-lists only X (so A cannot see Y). Org B hides X (so B sees
	// only Y, since it has no allow-list restricting anything else).
	mustSetGovernance(t, orgs, orgA, organizations.GovernanceUpdate{AllowList: &[]string{string(intgX.ID)}})
	mustSetGovernance(t, orgs, orgB, organizations.GovernanceUpdate{Hidden: []string{string(intgX.ID)}})

	t.Run("org A sees only X, never B's differently-governed view of Y", func(t *testing.T) {
		summaries, err := f.ListIntegrations(ctx, orgA)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(summaries) != 1 || summaries[0].ID != intgX.ID {
			t.Fatalf("org A's summaries = %+v, want only %q", summaries, intgX.ID)
		}
	})

	t.Run("org B sees only Y", func(t *testing.T) {
		summaries, err := f.ListIntegrations(ctx, orgB)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(summaries) != 1 || summaries[0].ID != intgY.ID {
			t.Fatalf("org B's summaries = %+v, want only %q", summaries, intgY.ID)
		}
	})

	t.Run("org A cannot see or resolve Y (B's only-visible integration)", func(t *testing.T) {
		_, err := f.GetVisibleIntegration(ctx, orgA, intgY.ID)
		assertDomainError(t, err, catalog.CodeNotFound, 404)
	})

	t.Run("org B cannot see or resolve X (A's only-visible integration)", func(t *testing.T) {
		_, err := f.GetVisibleIntegration(ctx, orgB, intgX.ID)
		assertDomainError(t, err, catalog.CodeNotFound, 404)
	})

	t.Run("org A can still resolve its own visible X", func(t *testing.T) {
		got, err := f.GetVisibleIntegration(ctx, orgA, intgX.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != intgX.ID {
			t.Errorf("ID = %q, want %q", got.ID, intgX.ID)
		}
	})

	t.Run("org B can still resolve its own visible Y", func(t *testing.T) {
		got, err := f.GetVisibleIntegration(ctx, orgB, intgY.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != intgY.ID {
			t.Errorf("ID = %q, want %q", got.ID, intgY.ID)
		}
	})
}
