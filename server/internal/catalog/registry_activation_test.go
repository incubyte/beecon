// registry_activation_test.go (package catalog_test, same package as
// facade_test.go — reuses fakeDefinitions/assertDomainError). Exercises the
// registry sub-phase's Slice 1 installation-side walking skeleton: Activate
// pulling a bundle through the driven RegistryClient port and persisting it
// through the driven ActivatedDefinitions port (both exercised here via their
// in-memory fakes, memory.Overrides — the exact seam the coder's notes name),
// LoadActivatedDefinitions' boot-time rebuild surviving a restart with no
// registry reachable at all, and FindToolBySlug resolving a tool by both its
// slug and its immutable tool_ id to the very same tool.
package catalog_test

import (
	"context"
	"reflect"
	"testing"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/registrybundle"
)

// outlookBundleWithListMessagesTool's ContentHash is the real
// registrybundle.ContentHash of the rest of its own fields, computed below —
// not a hand-typed placeholder — because Slice 4's Activate now verifies it
// (PD67) exactly as a real registry-pulled bundle's would be.
func outlookBundleWithListMessagesTool(version, toolID string) registrybundle.Bundle {
	bundle := registrybundle.Bundle{
		FormatVersion: 1,
		ProviderSlug:  "outlook",
		Version:       version,
		Name:          "Outlook",
		AuthScheme:    "oauth2",
		OAuth: registrybundle.OAuthConfig{
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
			Scopes:       []string{"Mail.Read"},
		},
		Tools: []registrybundle.Tool{
			{
				ID: toolID, Slug: "outlook-list-messages", Name: "List messages",
				InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
				Mapping: registrybundle.ToolMapping{Method: "GET", Path: "/v1.0/me/messages"},
			},
		},
	}
	bundle.ContentHash, _ = registrybundle.ContentHash(bundle)
	return bundle
}

func TestActivate_PullsPersistsAndImmediatelyServesTheActivatedVersionsTools(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", outlookBundleWithListMessagesTool("1.1.0", "tool_abc123"))
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:          fakeDefinitions(),
		RegistryClient:       client,
		ActivatedDefinitions: memory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	activated, err := f.Activate(context.Background(), "outlook", "1.1.0")

	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if activated.ProviderSlug != "outlook" || activated.ActiveVersion != "1.1.0" {
		t.Errorf("Activate result = %+v, want {outlook 1.1.0}", activated)
	}

	_, tool, err := f.FindToolBySlug(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("FindToolBySlug(slug): %v", err)
	}
	if tool.ID != "tool_abc123" {
		t.Errorf("tool.ID = %q, want %q", tool.ID, "tool_abc123")
	}
}

func TestActivate_WithNoRegistryClientWiredFailsClearlyInsteadOfPanicking(t *testing.T) {
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	_, err = f.Activate(context.Background(), "outlook", "1.0.0")

	assertDomainError(t, err, catalog.CodeRegistryUnavailable, 503)
}

func TestActivate_AVersionTheRegistryNeverPublishedSurfacesBundleVersionNotFound(t *testing.T) {
	client := memory.NewRegistryClient() // seeded with nothing
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:          fakeDefinitions(),
		RegistryClient:       client,
		ActivatedDefinitions: memory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	_, err = f.Activate(context.Background(), "outlook", "9.9.9")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

func TestLoadActivatedDefinitions_SurvivesARestartWithNoRegistryReachableAtAll(t *testing.T) {
	sharedActivatedDefinitions := memory.NewActivatedDefinitionRepository()
	client := memory.NewRegistryClient()
	client.Seed("outlook", outlookBundleWithListMessagesTool("1.2.0", "tool_survives_restart"))

	firstBoot, err := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:          fakeDefinitions(),
		RegistryClient:       client,
		ActivatedDefinitions: sharedActivatedDefinitions,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides (first boot): %v", err)
	}
	if _, err := firstBoot.Activate(context.Background(), "outlook", "1.2.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// A fresh facade, standing in for the installation restarting: no
	// RegistryClient at all (BEECON_REGISTRY_URL unset — PD59, offline), just
	// the same DB-backed ActivatedDefinitions store LoadActivatedDefinitions
	// rebuilds from at boot (mirroring app/wiring.go's own boot call).
	afterRestart, err := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:          fakeDefinitions(),
		ActivatedDefinitions: sharedActivatedDefinitions,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides (after restart): %v", err)
	}

	_, toolBySlug, err := afterRestart.FindToolBySlug(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("FindToolBySlug(slug) after restart: %v", err)
	}
	if toolBySlug.ID != "tool_survives_restart" {
		t.Errorf("tool.ID after restart = %q, want %q", toolBySlug.ID, "tool_survives_restart")
	}

	_, toolByID, err := afterRestart.FindToolBySlug(context.Background(), "tool_survives_restart")
	if err != nil {
		t.Fatalf("FindToolBySlug(id) after restart: %v", err)
	}
	if !reflect.DeepEqual(toolBySlug, toolByID) {
		t.Errorf("resolving by slug %+v and by id %+v after restart returned different tools", toolBySlug, toolByID)
	}
}

func TestFindToolBySlug_ResolvesTheSameToolByItsSlugAndByItsImmutableToolID(t *testing.T) {
	definitions := []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook",
			Tools: []catalog.ProviderTool{
				{ID: "tool_xyz789", Slug: "outlook-list-messages", Name: "List messages", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
			},
		},
	}
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: definitions})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	_, toolBySlug, err := f.FindToolBySlug(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("FindToolBySlug(slug): %v", err)
	}
	_, toolByID, err := f.FindToolBySlug(context.Background(), "tool_xyz789")
	if err != nil {
		t.Fatalf("FindToolBySlug(id): %v", err)
	}

	if !reflect.DeepEqual(toolBySlug, toolByID) {
		t.Errorf("resolving by slug %+v and by id %+v returned different tools, want the same one", toolBySlug, toolByID)
	}
}

func TestFindToolBySlug_AnUnknownToolIDIsANotFoundDistinctFromAToolLevelFailure(t *testing.T) {
	definitions := []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook",
			Tools: []catalog.ProviderTool{
				{ID: "tool_xyz789", Slug: "outlook-list-messages", Name: "List messages", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
			},
		},
	}
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: definitions})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	_, _, err = f.FindToolBySlug(context.Background(), "tool_this_id_was_never_minted")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// TestNewFacadeWithOverrides_EmbeddedSeedStillLoadsAndResolvesAfterTheRegistrySyncBootStep
// pins the exact regression the registry sub-phase's boot changes (PD65:
// LoadActivatedDefinitions now runs at boot, ahead of every other read) must
// never cause: with nothing ever activated (an empty ActivatedDefinitions
// store, mirroring a fresh installation's first boot), the real embedded
// provider seed (outlook, hubspot, ...) still loads and every embedded
// provider still resolves by its slug exactly as before this sub-phase
// existed.
func TestNewFacadeWithOverrides_EmbeddedSeedStillLoadsAndResolvesAfterTheRegistrySyncBootStep(t *testing.T) {
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{
		ActivatedDefinitions: memory.NewActivatedDefinitionRepository(), // empty: nothing ever activated
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	if _, err := f.GetProviderDefinition(context.Background(), "outlook"); err != nil {
		t.Errorf("GetProviderDefinition(outlook): %v, want the embedded seed still resolves it", err)
	}
	if _, err := f.GetProviderDefinition(context.Background(), "hubspot"); err != nil {
		t.Errorf("GetProviderDefinition(hubspot): %v, want the embedded seed still resolves it", err)
	}
}
