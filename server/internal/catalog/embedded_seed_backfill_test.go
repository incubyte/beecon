// embedded_seed_backfill_test.go (package catalog_test, reuses
// fakeDefinitions/toolCatalogDefinitions/assertDomainError/minimalSchema
// from facade_test.go and activationBundle/activationTool/
// activationToolWithID from registry_activation_fixtures_test.go) exercises
// the Phase 5 registry sub-phase's Slice 6 boot backfill
// (catalog.Facade.BackfillEmbeddedSeed): every embedded provider becomes its
// own initially-activated version with a minted tool_ id per tool (AC2), a
// second run mints nothing new and leaves every previously-minted id
// unchanged (AC3, idempotency — the load-bearing AC), a provider already
// activated through the registry is left completely alone (AC3), existing
// connections and trigger-instances are never touched by any of this
// (AC4/AC5), and every backfilled tool is addressable by both its slug and
// its new tool_ id, resolving to the identical tool (AC6).
package catalog_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/connections"
	connectionsmemory "beecon/internal/connections/driven/memory"
	"beecon/internal/organizations"
	"beecon/internal/registrybundle"
	"beecon/internal/triggers"
	triggersmemory "beecon/internal/triggers/driven/memory"
)

// backfillFacade builds a facade over definitions with an ActivatedDefinitions
// store wired (and no RegistryClient — the boot backfill runs identically
// whether or not a registry is configured, PD59) and returns it alongside the
// store, so a test can inspect exactly what backfill persisted.
func backfillFacade(t *testing.T, definitions []catalog.ProviderDefinition) (*catalog.Facade, *catalogmemory.ActivatedDefinitionRepository) {
	t.Helper()
	store := catalogmemory.NewActivatedDefinitionRepository()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: definitions, ActivatedDefinitions: store,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f, store
}

// --- AC2: every embedded provider becomes its own initially-activated version, minting tool_ ids ---

func TestBackfillEmbeddedSeed_MintsAToolIDForEveryEmbeddedToolAndRecordsEachProviderAsActivated(t *testing.T) {
	f, store := backfillFacade(t, toolCatalogDefinitions())

	minted, err := f.BackfillEmbeddedSeed(context.Background())
	if err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}
	if minted != 4 {
		t.Fatalf("minted = %d, want 4 (outlook's 3 tools + slack's 1)", minted)
	}

	for _, tt := range []struct{ providerSlug, toolSlug string }{
		{"outlook", "outlook-get-message"},
		{"outlook", "outlook-list-messages"},
		{"outlook", "outlook-legacy-tool"},
		{"slack", "slack-post-message"},
	} {
		tool, err := f.ToolDetail(context.Background(), tt.toolSlug)
		if err != nil {
			t.Fatalf("ToolDetail(%q): %v", tt.toolSlug, err)
		}
		if tool.ID == "" {
			t.Errorf("tool %q: ID is empty after backfill, want a minted tool_ id", tt.toolSlug)
		}

		row, err := store.FindByProviderSlug(context.Background(), tt.providerSlug)
		if err != nil {
			t.Fatalf("FindByProviderSlug(%q): %v", tt.providerSlug, err)
		}
		if row == nil {
			t.Fatalf("no ActivatedDefinition row for %q after backfill", tt.providerSlug)
		}
		if row.Version != "1.0.0" {
			t.Errorf("provider %q: ActivatedDefinition.Version = %q, want %q (the embedded seed's own initial version)", tt.providerSlug, row.Version, "1.0.0")
		}
	}
}

func TestBackfillEmbeddedSeed_MintsDistinctToolIDsAcrossEveryTool(t *testing.T) {
	f, _ := backfillFacade(t, toolCatalogDefinitions())
	if _, err := f.BackfillEmbeddedSeed(context.Background()); err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}

	seen := map[string]string{}
	for _, slug := range []string{"outlook-get-message", "outlook-list-messages", "outlook-legacy-tool", "slack-post-message"} {
		tool, err := f.ToolDetail(context.Background(), slug)
		if err != nil {
			t.Fatalf("ToolDetail(%q): %v", slug, err)
		}
		if owner, dup := seen[tool.ID]; dup {
			t.Fatalf("tool_ id %q minted for both %q and %q, want each tool to get its own", tool.ID, owner, slug)
		}
		seen[tool.ID] = slug
	}
}

// --- AC3 (load-bearing): idempotent re-run mints nothing new, ids stable ---

func TestBackfillEmbeddedSeed_ASecondRunMintsNothingNewAndLeavesEveryToolIDUnchanged(t *testing.T) {
	f, _ := backfillFacade(t, toolCatalogDefinitions())
	firstRunMinted, err := f.BackfillEmbeddedSeed(context.Background())
	if err != nil {
		t.Fatalf("first BackfillEmbeddedSeed: %v", err)
	}
	if firstRunMinted == 0 {
		t.Fatal("first run minted 0, want > 0 (a fresh embedded seed with no prior ActivatedDefinition rows)")
	}
	idsBefore := map[string]string{}
	for _, slug := range []string{"outlook-get-message", "outlook-list-messages", "slack-post-message"} {
		tool, err := f.ToolDetail(context.Background(), slug)
		if err != nil {
			t.Fatalf("ToolDetail(%q) before second run: %v", slug, err)
		}
		idsBefore[slug] = tool.ID
	}

	secondRunMinted, err := f.BackfillEmbeddedSeed(context.Background())
	if err != nil {
		t.Fatalf("second BackfillEmbeddedSeed: %v", err)
	}
	if secondRunMinted != 0 {
		t.Errorf("second run minted %d, want 0 (every provider already caught up)", secondRunMinted)
	}

	for slug, before := range idsBefore {
		tool, err := f.ToolDetail(context.Background(), slug)
		if err != nil {
			t.Fatalf("ToolDetail(%q) after second run: %v", slug, err)
		}
		if tool.ID != before {
			t.Errorf("tool %q: ID changed across an idempotent re-run: before=%q after=%q", slug, before, tool.ID)
		}
	}
}

// --- AC3: a provider already activated through the registry is skipped entirely (partial-set edge case) ---

func TestBackfillEmbeddedSeed_SkipsAProviderAlreadyActivatedThroughTheRegistry_NoDoubleActivationIDPreserved(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("2.0.0", []registrybundle.Tool{
		activationToolWithID("outlook-list-messages", "tool_already_activated"),
	}, nil))
	store := catalogmemory.NewActivatedDefinitionRepository()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: toolCatalogDefinitions(), RegistryClient: client, ActivatedDefinitions: store,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate outlook 2.0.0: %v", err)
	}

	// Partial-set precondition: outlook has a real registry-activated row;
	// slack (still purely embedded) does not.
	minted, err := f.BackfillEmbeddedSeed(context.Background())
	if err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}
	if minted != 1 {
		t.Fatalf("minted = %d, want 1 (only slack's one embedded tool — outlook was already activated)", minted)
	}

	outlookRow, err := store.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug(outlook): %v", err)
	}
	if outlookRow.Version != "2.0.0" {
		t.Errorf("outlook's ActivatedDefinition.Version = %q after backfill, want it to remain %q (untouched, not overwritten with the embedded-seed's own 1.0.0)", outlookRow.Version, "2.0.0")
	}

	tool, err := f.ToolDetail(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("ToolDetail(outlook-list-messages): %v", err)
	}
	if tool.ID != "tool_already_activated" {
		t.Errorf("outlook-list-messages ID = %q after backfill, want the id Activate already set, %q, preserved untouched", tool.ID, "tool_already_activated")
	}

	slackTool, err := f.ToolDetail(context.Background(), "slack-post-message")
	if err != nil {
		t.Fatalf("ToolDetail(slack-post-message): %v", err)
	}
	if slackTool.ID == "" {
		t.Error("slack-post-message ID is empty after backfill, want a freshly minted tool_ id for the still-purely-embedded provider")
	}
}

// --- AC4: existing connections keep resolving/executing by slug, unaffected ---

func TestBackfillEmbeddedSeed_LeavesToolRoutingMetadataForAnExistingSlugUnchanged(t *testing.T) {
	definitions := []catalog.ProviderDefinition{{
		Slug: "outlook", Name: "Outlook", AuthScheme: "oauth2",
		AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token",
		Tools: []catalog.ProviderTool{{
			Slug: "outlook-list-messages", Name: "List messages", Method: "GET", Path: "https://graph.example.com/v1.0/messages",
			InputSchema: minimalSchema(), OutputSchema: minimalSchema(),
		}},
	}}
	f, _ := backfillFacade(t, definitions)
	_, before, err := f.FindToolBySlug(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("FindToolBySlug before backfill: %v", err)
	}

	if _, err := f.BackfillEmbeddedSeed(context.Background()); err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}

	_, after, err := f.FindToolBySlug(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("FindToolBySlug after backfill: %v", err)
	}
	if after.Method != before.Method || after.Path != before.Path {
		t.Errorf("execution routing changed: before={Method:%q Path:%q} after={Method:%q Path:%q}", before.Method, before.Path, after.Method, after.Path)
	}
	if before.ID != "" {
		t.Fatalf("test fixture bug: tool already carried an id (%q) before backfill ran", before.ID)
	}
	if after.ID == "" {
		t.Error("after backfill, ID is still empty — want a minted tool_ id")
	}
}

func TestBackfillEmbeddedSeed_LeavesAnExistingConnectionForThatProviderCompletelyUnchanged(t *testing.T) {
	f, _ := backfillFacade(t, fakeDefinitions())
	integrationSummary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	connRepo := connectionsmemory.NewRepository()
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	org := organizations.OrgID("org_1")
	connection := connections.NewConnection(
		connections.ConnectionID("conn_1"), org, organizations.UserID("user_1"),
		integrationSummary.ID, "outlook", "https://example.com/redirect", "connect_token_1", fixedTime,
	)
	activatedConnection := connection.Activate("encrypted-access-token", "encrypted-refresh-token", "user@example.com", "A User", fixedTime.Add(time.Hour))
	if err := connRepo.Save(context.Background(), activatedConnection); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	before, err := connRepo.FindByID(context.Background(), org, activatedConnection.ID)
	if err != nil {
		t.Fatalf("FindByID (before): %v", err)
	}

	if _, err := f.BackfillEmbeddedSeed(context.Background()); err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}

	after, err := connRepo.FindByID(context.Background(), org, activatedConnection.ID)
	if err != nil {
		t.Fatalf("FindByID (after): %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("connection changed after the boot backfill:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// --- AC5: existing trigger-instances keep resolving/polling their trigger definitions, unaffected ---

func embeddedDefinitionsWithOneTrigger() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{{
		Slug: "outlook", Name: "Outlook", AuthScheme: "oauth2",
		AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token",
		Triggers: []catalog.TriggerDefinition{{
			Slug: "outlook-message-received", Name: "New message received",
			ConfigSchema: minimalSchema(), PayloadSchema: minimalSchema(),
			Ingestion: "poll", PollIntervalSeconds: 60,
			Poll: catalog.TriggerPollMapping{Method: "GET", Path: "https://graph.example.com/v1.0/messages/delta", RecordsPath: "value", RecordIDPath: "id", RecordTimestampPath: "receivedDateTime"},
		}},
	}}
}

func TestBackfillEmbeddedSeed_LeavesTriggerDefinitionResolutionForAnExistingSlugUnchanged(t *testing.T) {
	f, _ := backfillFacade(t, embeddedDefinitionsWithOneTrigger())
	_, before, err := f.FindTriggerBySlug(context.Background(), "outlook-message-received")
	if err != nil {
		t.Fatalf("FindTriggerBySlug before backfill: %v", err)
	}

	if _, err := f.BackfillEmbeddedSeed(context.Background()); err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}

	_, after, err := f.FindTriggerBySlug(context.Background(), "outlook-message-received")
	if err != nil {
		t.Fatalf("FindTriggerBySlug after backfill: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("trigger definition changed after the boot backfill:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestBackfillEmbeddedSeed_LeavesAnExistingTriggerInstanceUnaffectedAndNeverCallsThePauser(t *testing.T) {
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	f, _ := backfillFacade(t, embeddedDefinitionsWithOneTrigger())
	pauser := catalogmemory.NewTriggerInstancePauser()
	f = f.WithTriggerInstancePauser(pauser)

	instanceRepo := triggersmemory.NewRepository()
	org := organizations.OrgID("org_a")
	instance := triggers.NewTriggerInstance("trg_a", org, "user_a", connections.ConnectionID("conn_a"), "outlook-message-received", nil, past)
	if err := instanceRepo.Save(context.Background(), instance); err != nil {
		t.Fatalf("seed trigger instance: %v", err)
	}

	if _, err := f.BackfillEmbeddedSeed(context.Background()); err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}

	updated, err := instanceRepo.FindByID(context.Background(), org, instance.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != triggers.StatusActive {
		t.Errorf("trigger instance status = %q after backfill, want it to remain %q — backfill must never pause a live instance", updated.Status, triggers.StatusActive)
	}
	if paused := pauser.Paused(); len(paused) != 0 {
		t.Errorf("backfill called the trigger-instance pauser for %v, want it never called — backfill only records the embedded seed, it never removes anything a live instance depends on", paused)
	}
}

// --- AC6: after backfill, a tool is addressable by both its slug and its new tool_ id, resolving to the same tool ---

func TestBackfillEmbeddedSeed_ToolDetailByToolIDReturnsTheSameToolAsBySlug(t *testing.T) {
	f, _ := backfillFacade(t, toolCatalogDefinitions())
	if _, err := f.BackfillEmbeddedSeed(context.Background()); err != nil {
		t.Fatalf("BackfillEmbeddedSeed: %v", err)
	}

	bySlug, err := f.ToolDetail(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("ToolDetail(slug): %v", err)
	}
	if bySlug.ID == "" {
		t.Fatal("test fixture bug: expected a minted tool_ id after backfill")
	}

	byID, err := f.ToolDetail(context.Background(), bySlug.ID)
	if err != nil {
		t.Fatalf("ToolDetail(tool_ id): %v", err)
	}

	if !reflect.DeepEqual(bySlug, byID) {
		t.Errorf("ToolDetail(slug) = %+v, ToolDetail(tool_ id) = %+v, want the identical tool", bySlug, byID)
	}
}

// --- Edge case: a tool with no output schema — backfill (unlike a real registry Publish) applies no schema gate ---

func TestBackfillEmbeddedSeed_MintsAToolIDEvenForAToolWithNoOutputSchema(t *testing.T) {
	definitions := []catalog.ProviderDefinition{{
		Slug: "acme", Name: "Acme", AuthScheme: "oauth2",
		AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token",
		Tools: []catalog.ProviderTool{{
			Slug: "acme-legacy-tool", Name: "Legacy tool", Method: "GET", Path: "https://example.com/legacy",
			InputSchema: minimalSchema(), // OutputSchema deliberately left nil
		}},
	}}
	f, _ := backfillFacade(t, definitions)

	minted, err := f.BackfillEmbeddedSeed(context.Background())
	if err != nil {
		t.Fatalf("BackfillEmbeddedSeed must not fail for a tool with no output schema (unlike a real registry Publish, the boot backfill applies no output-schema-vs-sample gate): %v", err)
	}
	if minted != 1 {
		t.Fatalf("minted = %d, want 1", minted)
	}

	tool, err := f.ToolDetail(context.Background(), "acme-legacy-tool")
	if err != nil {
		t.Fatalf("ToolDetail: %v", err)
	}
	if tool.ID == "" {
		t.Error("ID is empty, want a minted tool_ id even though the tool has no output schema")
	}
}

// --- Wiring no-op: a facade with no ActivatedDefinitions/minter wired treats this as a no-op ---

func TestBackfillEmbeddedSeed_IsANoOpWhenNoActivatedDefinitionsStoreIsWired(t *testing.T) {
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{Definitions: toolCatalogDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	minted, err := f.BackfillEmbeddedSeed(context.Background())

	if err != nil {
		t.Fatalf("BackfillEmbeddedSeed on an unwired facade must not error, got: %v", err)
	}
	if minted != 0 {
		t.Errorf("minted = %d, want 0 (no-op)", minted)
	}
	tool, err := f.ToolDetail(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("ToolDetail: %v", err)
	}
	if tool.ID != "" {
		t.Errorf("ID = %q, want empty — an unwired facade must never mint a tool_ id", tool.ID)
	}
}
