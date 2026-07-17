// registry_activation_deprecation_test.go (package catalog_test) exercises
// Slice 4's soft-deprecation guarantee (PD66): a tool the newly-activated
// version no longer declares stays resolvable — by both its slug and its
// immutable tool_ id — marked Deprecated, hidden from the default ListTools
// view, and durable across a restart (LoadActivatedDefinitions' boot-time
// rebuild, since the persisted row IS the served definition, always — PD65).
package catalog_test

import (
	"context"
	"testing"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/registrybundle"
)

func TestActivate_ARemovedToolStaysResolvableAsDeprecatedBySlugAndByItsToolIDAndIsHiddenFromTheDefaultToolList(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_list_messages"),
		activationToolWithID("outlook-get-message", "tool_get_message"),
	}, nil))
	client.Seed("outlook", activationBundle("2.0.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_list_messages"),
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

	if _, err := f.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate 2.0.0: %v", err)
	}

	_, bySlug, err := f.FindToolBySlug(context.Background(), "outlook-get-message")
	if err != nil {
		t.Fatalf("FindToolBySlug(slug) for a removed tool: %v", err)
	}
	if !bySlug.Deprecated {
		t.Errorf("a removed tool resolved by slug must be marked Deprecated, got %+v", bySlug)
	}

	_, byID, err := f.FindToolBySlug(context.Background(), "tool_get_message")
	if err != nil {
		t.Fatalf("FindToolBySlug(tool_ id) for a removed tool: %v", err)
	}
	if byID.Slug != "outlook-get-message" || !byID.Deprecated {
		t.Errorf("resolving the removed tool by its tool_ id = %+v, want slug outlook-get-message and Deprecated true", byID)
	}

	defaultPage, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{}, "", 50)
	if err != nil {
		t.Fatalf("ListTools (default): %v", err)
	}
	for _, item := range defaultPage.Items {
		if item.Slug == "outlook-get-message" {
			t.Errorf("the default ListTools view must exclude a deprecated tool, found %+v", item)
		}
	}

	includeDeprecatedPage, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{IncludeDeprecated: true}, "", 50)
	if err != nil {
		t.Fatalf("ListTools (IncludeDeprecated): %v", err)
	}
	found := false
	for _, item := range includeDeprecatedPage.Items {
		if item.Slug == "outlook-get-message" {
			found = true
		}
	}
	if !found {
		t.Errorf("ListTools with IncludeDeprecated:true must still surface the removed tool, items=%+v", includeDeprecatedPage.Items)
	}
}

func TestActivate_ARemovedToolsDeprecatedStatusSurvivesALoadActivatedDefinitionsRestart(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_list_messages"),
		activationToolWithID("outlook-get-message", "tool_get_message"),
	}, nil))
	client.Seed("outlook", activationBundle("2.0.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_list_messages"),
	}, nil))
	activated := catalogmemory.NewActivatedDefinitionRepository()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: activated,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate 2.0.0: %v", err)
	}

	// A fresh facade sharing the same DB-backed ActivatedDefinitions store,
	// with no registry reachable at all — standing in for the installation
	// restarting (LoadActivatedDefinitions runs once at boot, app/wiring.go).
	afterRestart, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), ActivatedDefinitions: activated,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides (after restart): %v", err)
	}

	_, bySlug, err := afterRestart.FindToolBySlug(context.Background(), "outlook-get-message")
	if err != nil {
		t.Fatalf("FindToolBySlug(slug) after restart: %v", err)
	}
	if !bySlug.Deprecated {
		t.Errorf("a removed tool must still be marked Deprecated after a restart, got %+v", bySlug)
	}

	_, byID, err := afterRestart.FindToolBySlug(context.Background(), "tool_get_message")
	if err != nil {
		t.Fatalf("FindToolBySlug(tool_ id) after restart: %v", err)
	}
	if byID.Slug != "outlook-get-message" {
		t.Errorf("resolving by tool_ id after restart = %+v, want slug outlook-get-message", byID)
	}
}
