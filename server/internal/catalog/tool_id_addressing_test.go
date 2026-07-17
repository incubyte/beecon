// tool_id_addressing_test.go (package catalog_test, reuses
// fakeDefinitions/assertDomainError/testOrgID from facade_test.go, the
// toolCatalogDefinitions/newToolCatalogFacade embedded-seed fixture also from
// facade_test.go, and the activationBundle/activationToolWithID builders from
// registry_activation_fixtures_test.go) exercises the Phase 5 registry
// sub-phase's Slice 5: tool_ ids surfaced by ListTools/ToolDetail alongside
// slug, empty for a tool never through the registry, stable across
// activating a later version that keeps its slug, and resolving a tool
// detail lookup by tool_ id to exactly the same tool a slug lookup returns.
package catalog_test

import (
	"context"
	"reflect"
	"testing"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/registrybundle"
)

// activatedFacadeWithSingleTool activates a one-tool "outlook" bundle at
// version carrying toolID for toolSlug, returning the facade already served
// on that version — the shared setup every real-tool_id test below needs.
func activatedFacadeWithSingleTool(t *testing.T, toolSlug, toolID, version string) *catalog.Facade {
	t.Helper()
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle(version, []registrybundle.Tool{
		activationToolWithID(toolSlug, toolID),
	}, nil))
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", version); err != nil {
		t.Fatalf("Activate %s: %v", version, err)
	}
	return f
}

// --- AC1: the tools catalog API returns id alongside slug ---

func TestListTools_SurfacesTheRealToolIDForAnActivatedRegistryTool(t *testing.T) {
	f := activatedFacadeWithSingleTool(t, "outlook-list-messages", "tool_real_cuid2", "1.0.0")

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{ProviderSlug: "outlook"}, "", 50)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].ID != "tool_real_cuid2" {
		t.Errorf("ID = %q, want the activated bundle's minted tool_ id %q", page.Items[0].ID, "tool_real_cuid2")
	}
	if page.Items[0].Slug != "outlook-list-messages" {
		t.Errorf("Slug = %q, want id surfaced alongside slug, not instead of it", page.Items[0].Slug)
	}
}

func TestListTools_SurfacesAnEmptyIDForAnEmbeddedPreRegistryTool(t *testing.T) {
	f := newToolCatalogFacade(t)

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{ProviderSlug: "outlook"}, "", 50)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("expected at least one tool from the embedded-seed fixture")
	}
	for _, item := range page.Items {
		if item.ID != "" {
			t.Errorf("ID = %q for embedded-seed tool %q, want empty (a tool never through the registry mints nothing here — Slice 6 backfills it)", item.ID, item.Slug)
		}
	}
}

func TestToolDetail_SurfacesTheRealToolIDForAnActivatedRegistryTool(t *testing.T) {
	f := activatedFacadeWithSingleTool(t, "outlook-list-messages", "tool_detail_real_id", "1.0.0")

	tool, err := f.ToolDetail(context.Background(), "outlook-list-messages")

	if err != nil {
		t.Fatalf("ToolDetail: %v", err)
	}
	if tool.ID != "tool_detail_real_id" {
		t.Errorf("ID = %q, want %q", tool.ID, "tool_detail_real_id")
	}
}

func TestToolDetail_SurfacesAnEmptyIDForAnEmbeddedPreRegistryTool(t *testing.T) {
	f := newToolCatalogFacade(t)

	tool, err := f.ToolDetail(context.Background(), "outlook-get-message")

	if err != nil {
		t.Fatalf("ToolDetail: %v", err)
	}
	if tool.ID != "" {
		t.Errorf("ID = %q for an embedded-seed tool, want empty", tool.ID)
	}
}

// --- AC6: a tool_ id detail lookup matches a slug lookup; unknown tool_ id is not-found ---

func TestToolDetail_ByToolIDReturnsTheSameToolASlugLookupReturns(t *testing.T) {
	f := activatedFacadeWithSingleTool(t, "outlook-list-messages", "tool_lookup_parity", "1.0.0")

	bySlug, err := f.ToolDetail(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("ToolDetail(slug): %v", err)
	}
	byID, err := f.ToolDetail(context.Background(), "tool_lookup_parity")
	if err != nil {
		t.Fatalf("ToolDetail(tool_ id): %v", err)
	}

	if !reflect.DeepEqual(bySlug, byID) {
		t.Errorf("ToolDetail(slug) = %+v, ToolDetail(tool_ id) = %+v, want the same tool", bySlug, byID)
	}
}

func TestToolDetail_UnknownToolIDReturnsTheCatalogNotFoundError(t *testing.T) {
	f := newToolCatalogFacade(t)

	_, err := f.ToolDetail(context.Background(), "tool_this_id_was_never_minted")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// --- AC4: a tool_ id is stable across activating a later version that keeps its slug ---

func TestActivate_AToolsToolIDIsUnchangedAfterActivatingALaterVersionThatKeepsItsSlug(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_stable_across_versions"),
	}, nil))
	client.Seed("outlook", activationBundle("1.1.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_stable_across_versions"),
		activationToolWithID("outlook-get-message", "tool_added_in_minor"),
	}, nil))
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}
	beforeUpgrade, err := f.ToolDetail(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("ToolDetail before upgrade: %v", err)
	}

	if _, err := f.Activate(context.Background(), "outlook", "1.1.0"); err != nil {
		t.Fatalf("Activate 1.1.0: %v", err)
	}
	afterUpgrade, err := f.ToolDetail(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("ToolDetail after upgrade: %v", err)
	}

	if afterUpgrade.ID != beforeUpgrade.ID {
		t.Errorf("tool_ id changed across activation: before=%q after=%q, want unchanged for a slug that didn't change", beforeUpgrade.ID, afterUpgrade.ID)
	}
	if afterUpgrade.ID != "tool_stable_across_versions" {
		t.Errorf("tool_ id after upgrade = %q, want %q", afterUpgrade.ID, "tool_stable_across_versions")
	}
}
