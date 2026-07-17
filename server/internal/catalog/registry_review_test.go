// registry_review_test.go (package catalog_test, same package as
// facade_test.go/registry_activation_test.go — reuses fakeDefinitions,
// assertDomainError, and outlookBundleWithListMessagesTool). Exercises the
// registry sub-phase's Slice 3 "review before adopting" flow:
// Facade.ListRegistryVersions marking the currently-activated version,
// Facade.DiffRegistryVersion classifying added/changed/removed tools relative
// to the active bundle, and the invariant that neither call ever touches the
// activated definition or the served catalog.
package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/catalog/driven/registryhttp"
	"beecon/internal/registrybundle"
)

// registryReviewBundle builds a minimal, valid outlook bundle at version
// carrying exactly the tools supplied — enough shape for
// definitionFromBundle to convert cleanly, without repeating every other
// field ListRegistryVersions/DiffRegistryVersion never look at.
// registryReviewBundle's ContentHash is the real registrybundle.ContentHash
// of the rest of its own fields — not a hand-typed placeholder — because
// Activate (used as setup throughout this file) now verifies it (Slice 4,
// PD67) exactly as a real registry-pulled bundle's would be.
func registryReviewBundle(version string, tools ...registrybundle.Tool) registrybundle.Bundle {
	bundle := registrybundle.Bundle{
		FormatVersion: 1,
		ProviderSlug:  "outlook",
		Version:       version,
		Name:          "Outlook",
		AuthScheme:    "oauth2",
		OAuth: registrybundle.OAuthConfig{
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
		},
		Tools: tools,
	}
	bundle.ContentHash, _ = registrybundle.ContentHash(bundle)
	return bundle
}

func registryReviewTool(slug string, inputSchema, outputSchema map[string]any) registrybundle.Tool {
	return registrybundle.Tool{
		Slug: slug, Name: slug,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mapping:      registrybundle.ToolMapping{Method: "GET", Path: "/v1.0/" + slug},
	}
}

// registryReviewBundleWithTriggers mirrors registryReviewBundle, carrying a
// fixed non-empty tool set (so every activation stays otherwise valid) plus
// exactly the triggers supplied — the trigger-diff carryover coverage below
// (added/changed/removed) needs a bundle it can vary triggers on
// independently of tools, the same way registryReviewBundle already lets
// tests vary tools independently of triggers.
func registryReviewBundleWithTriggers(version string, triggers ...registrybundle.Trigger) registrybundle.Bundle {
	bundle := registrybundle.Bundle{
		FormatVersion: 1,
		ProviderSlug:  "outlook",
		Version:       version,
		Name:          "Outlook",
		AuthScheme:    "oauth2",
		OAuth: registrybundle.OAuthConfig{
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
		},
		Tools:    []registrybundle.Tool{registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())},
		Triggers: triggers,
	}
	bundle.ContentHash, _ = registrybundle.ContentHash(bundle)
	return bundle
}

func registryReviewTrigger(slug string, configSchema, payloadSchema map[string]any) registrybundle.Trigger {
	return registrybundle.Trigger{
		Slug: slug, Name: slug,
		ConfigSchema:  configSchema,
		PayloadSchema: payloadSchema,
		Ingestion:     "poll",
		Poll: registrybundle.TriggerPoll{
			Method: "GET", Path: "/v1.0/" + slug + "/poll",
			RecordsPath: "items", RecordIDPath: "id", RecordTimestampPath: "ts",
		},
	}
}

func newRegistryReviewFacade(t *testing.T, client catalog.RegistryClient, activated catalog.ActivatedDefinitions) *catalog.Facade {
	t.Helper()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:          fakeDefinitions(),
		RegistryClient:       client,
		ActivatedDefinitions: activated,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
}

// --- ListRegistryVersions ---

func TestListRegistryVersions_MarksTheCurrentlyActivatedVersionActive(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	client.Seed("outlook", registryReviewBundle("1.1.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	view, err := f.ListRegistryVersions(context.Background(), "outlook")

	if err != nil {
		t.Fatalf("ListRegistryVersions: %v", err)
	}
	if view.ActiveVersion != "1.0.0" {
		t.Errorf("ActiveVersion = %q, want %q", view.ActiveVersion, "1.0.0")
	}
	active := map[string]bool{}
	for _, item := range view.Items {
		active[item.Version] = item.Active
	}
	if !active["1.0.0"] {
		t.Errorf("version 1.0.0 must be marked active, items=%+v", view.Items)
	}
	if active["1.1.0"] {
		t.Errorf("version 1.1.0 must not be marked active, items=%+v", view.Items)
	}
}

func TestListRegistryVersions_ANeverActivatedProviderMarksNoVersionActive(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	client.Seed("outlook", registryReviewBundle("1.1.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())

	view, err := f.ListRegistryVersions(context.Background(), "outlook")

	if err != nil {
		t.Fatalf("ListRegistryVersions: %v", err)
	}
	if view.ActiveVersion != "" {
		t.Errorf("ActiveVersion = %q, want empty for a never-activated provider", view.ActiveVersion)
	}
	for _, item := range view.Items {
		if item.Active {
			t.Errorf("no version should be marked active for a never-activated provider, got %+v", item)
		}
	}
	if len(view.Items) != 2 {
		t.Fatalf("Items = %+v, want both seeded versions listed", view.Items)
	}
}

func TestListRegistryVersions_WithNoRegistryClientWiredFailsClearlyInsteadOfPanicking(t *testing.T) {
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	_, err = f.ListRegistryVersions(context.Background(), "outlook")

	assertDomainError(t, err, catalog.CodeRegistryUnavailable, http.StatusServiceUnavailable)
}

// --- DiffRegistryVersion ---

func TestDiffRegistryVersion_AToolWithAChangedSchemaIsClassifiedChangedNotAddedOrRemoved(t *testing.T) {
	fromSchema := map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}
	toSchema := map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "number"}}}
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", fromSchema, minimalSchema())))
	client.Seed("outlook", registryReviewBundle("1.1.0", registryReviewTool("outlook-list-messages", toSchema, minimalSchema())))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "1.1.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if diff.From != "1.0.0" || diff.To != "1.1.0" {
		t.Errorf("From/To = %q/%q, want 1.0.0/1.1.0", diff.From, diff.To)
	}
	if !containsSlug(diff.Changed.Tools, "outlook-list-messages") {
		t.Errorf("Changed.Tools = %v, want it to contain the tool whose input schema differs", diff.Changed.Tools)
	}
	if containsSlug(diff.Added.Tools, "outlook-list-messages") {
		t.Errorf("Added.Tools = %v, must not also list a schema-changed tool as added", diff.Added.Tools)
	}
	if containsSlug(diff.Removed.Tools, "outlook-list-messages") {
		t.Errorf("Removed.Tools = %v, must not also list a schema-changed tool as removed", diff.Removed.Tools)
	}
}

func TestDiffRegistryVersion_ANewToolInTheTargetVersionIsClassifiedAdded(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	client.Seed("outlook", registryReviewBundle("1.1.0",
		registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema()),
		registryReviewTool("outlook-get-message", minimalSchema(), minimalSchema()),
	))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "1.1.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if !containsSlug(diff.Added.Tools, "outlook-get-message") {
		t.Errorf("Added.Tools = %v, want it to contain the newly added tool", diff.Added.Tools)
	}
	if len(diff.Changed.Tools) != 0 {
		t.Errorf("Changed.Tools = %v, want empty — the unchanged tool must not be reported as changed", diff.Changed.Tools)
	}
	if len(diff.Removed.Tools) != 0 {
		t.Errorf("Removed.Tools = %v, want empty", diff.Removed.Tools)
	}
}

func TestDiffRegistryVersion_ATargetVersionMissingAnActiveToolIsClassifiedRemoved(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0",
		registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema()),
		registryReviewTool("outlook-get-message", minimalSchema(), minimalSchema()),
	))
	client.Seed("outlook", registryReviewBundle("2.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "2.0.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if !containsSlug(diff.Removed.Tools, "outlook-get-message") {
		t.Errorf("Removed.Tools = %v, want it to contain the tool dropped by the target version", diff.Removed.Tools)
	}
	if containsSlug(diff.Added.Tools, "outlook-get-message") || containsSlug(diff.Changed.Tools, "outlook-get-message") {
		t.Errorf("a removed tool must not also appear in Added or Changed: added=%v changed=%v", diff.Added.Tools, diff.Changed.Tools)
	}
}

func TestDiffRegistryVersion_ANeverActivatedProviderReportsEveryTargetToolAsAdded(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0",
		registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema()),
		registryReviewTool("outlook-get-message", minimalSchema(), minimalSchema()),
	))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "1.0.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if diff.From != "" {
		t.Errorf("From = %q, want empty for a never-activated provider", diff.From)
	}
	if !containsSlug(diff.Added.Tools, "outlook-list-messages") || !containsSlug(diff.Added.Tools, "outlook-get-message") {
		t.Errorf("Added.Tools = %v, want both target-version tools since nothing is active yet", diff.Added.Tools)
	}
	if len(diff.Changed.Tools) != 0 || len(diff.Removed.Tools) != 0 {
		t.Errorf("Changed/Removed must be empty when nothing was ever activated: changed=%v removed=%v", diff.Changed.Tools, diff.Removed.Tools)
	}
}

// TestDiffRegistryVersion_RegistryUnreachableSurfacesRegistryUnavailableAndLeavesActiveVersionUntouched
// drives the real registryhttp.Client adapter against a closed httptest
// server (a genuine connection failure, not a hand-rolled fake) to prove the
// facade surfaces catalog.ErrRegistryUnavailable — and, since Diff pulls
// before it ever reads or writes the activated-definition store, the version
// activated earlier through a working client stays exactly as it was.
func TestDiffRegistryVersion_RegistryUnreachableSurfacesRegistryUnavailableAndLeavesActiveVersionUntouched(t *testing.T) {
	activated := memory.NewActivatedDefinitionRepository()
	workingClient := memory.NewRegistryClient()
	workingClient.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	seedingFacade := newRegistryReviewFacade(t, workingClient, activated)
	if _, err := seedingFacade.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	unreachableServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableServer.Close() // closed before any request: every call now fails to connect
	brokenClient := registryhttp.NewClient(unreachableServer.URL, "test-api-key", nil)
	f := newRegistryReviewFacade(t, brokenClient, activated)

	_, err := f.DiffRegistryVersion(context.Background(), "outlook", "1.1.0")

	assertDomainError(t, err, catalog.CodeRegistryUnavailable, http.StatusServiceUnavailable)

	record, findErr := activated.FindByProviderSlug(context.Background(), "outlook")
	if findErr != nil {
		t.Fatalf("FindByProviderSlug: %v", findErr)
	}
	if record == nil || record.Version != "1.0.0" {
		t.Errorf("activated definition = %+v, want it left at version 1.0.0 despite the failed diff", record)
	}
}

func TestDiffRegistryVersion_AVersionTheRegistryDoesNotOfferReturnsBundleVersionNotFound(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	_, err := f.DiffRegistryVersion(context.Background(), "outlook", "9.9.9")

	assertDomainError(t, err, catalog.CodeNotFound, http.StatusNotFound)
}

// --- DiffRegistryVersion: triggers (mirrors the tool coverage above) ---

func TestDiffRegistryVersion_ATriggerWithAChangedPayloadSchemaIsClassifiedChangedNotAddedOrRemoved(t *testing.T) {
	fromPayload := minimalSchema()
	toPayload := map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundleWithTriggers("1.0.0", registryReviewTrigger("outlook-message-received", minimalSchema(), fromPayload)))
	client.Seed("outlook", registryReviewBundleWithTriggers("1.1.0", registryReviewTrigger("outlook-message-received", minimalSchema(), toPayload)))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "1.1.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if !containsSlug(diff.Changed.Triggers, "outlook-message-received") {
		t.Errorf("Changed.Triggers = %v, want it to contain the trigger whose payload schema differs", diff.Changed.Triggers)
	}
	if containsSlug(diff.Added.Triggers, "outlook-message-received") || containsSlug(diff.Removed.Triggers, "outlook-message-received") {
		t.Errorf("a schema-changed trigger must not also appear in Added or Removed: added=%v removed=%v", diff.Added.Triggers, diff.Removed.Triggers)
	}
}

func TestDiffRegistryVersion_ANewTriggerInTheTargetVersionIsClassifiedAdded(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundleWithTriggers("1.0.0"))
	client.Seed("outlook", registryReviewBundleWithTriggers("1.1.0", registryReviewTrigger("outlook-message-received", minimalSchema(), minimalSchema())))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "1.1.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if !containsSlug(diff.Added.Triggers, "outlook-message-received") {
		t.Errorf("Added.Triggers = %v, want it to contain the newly added trigger", diff.Added.Triggers)
	}
	if len(diff.Changed.Triggers) != 0 || len(diff.Removed.Triggers) != 0 {
		t.Errorf("Changed/Removed.Triggers must be empty when a trigger is only added: changed=%v removed=%v", diff.Changed.Triggers, diff.Removed.Triggers)
	}
}

func TestDiffRegistryVersion_ATargetVersionMissingAnActiveTriggerIsClassifiedRemoved(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundleWithTriggers("1.0.0", registryReviewTrigger("outlook-message-received", minimalSchema(), minimalSchema())))
	client.Seed("outlook", registryReviewBundleWithTriggers("2.0.0"))
	f := newRegistryReviewFacade(t, client, memory.NewActivatedDefinitionRepository())
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	diff, err := f.DiffRegistryVersion(context.Background(), "outlook", "2.0.0")

	if err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}
	if !containsSlug(diff.Removed.Triggers, "outlook-message-received") {
		t.Errorf("Removed.Triggers = %v, want it to contain the trigger dropped by the target version", diff.Removed.Triggers)
	}
	if containsSlug(diff.Added.Triggers, "outlook-message-received") || containsSlug(diff.Changed.Triggers, "outlook-message-received") {
		t.Errorf("a removed trigger must not also appear in Added or Changed: added=%v changed=%v", diff.Added.Triggers, diff.Changed.Triggers)
	}
}

// --- No side effects ---

// TestListAndDiffRegistryVersion_LeaveTheActiveVersionAndServedToolsUnchanged
// is Slice 3's blocking invariant (AC4): pulling a version list and a diff
// must apply nothing — the activated-definition row and the tools this
// facade actually serves must read identically before and after both calls,
// and a tool introduced only by the un-adopted target version must not be
// resolvable.
func TestListAndDiffRegistryVersion_LeaveTheActiveVersionAndServedToolsUnchanged(t *testing.T) {
	client := memory.NewRegistryClient()
	client.Seed("outlook", registryReviewBundle("1.0.0", registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema())))
	client.Seed("outlook", registryReviewBundle("2.0.0",
		registryReviewTool("outlook-list-messages", minimalSchema(), minimalSchema()),
		registryReviewTool("outlook-get-message", minimalSchema(), minimalSchema()),
	))
	activated := memory.NewActivatedDefinitionRepository()
	f := newRegistryReviewFacade(t, client, activated)
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	beforeDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (before): %v", err)
	}
	beforeRecord, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (before): %v", err)
	}

	if _, err := f.ListRegistryVersions(context.Background(), "outlook"); err != nil {
		t.Fatalf("ListRegistryVersions: %v", err)
	}
	if _, err := f.DiffRegistryVersion(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("DiffRegistryVersion: %v", err)
	}

	afterDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (after): %v", err)
	}
	if !reflect.DeepEqual(beforeDefinition, afterDefinition) {
		t.Errorf("served definition changed after list+diff:\nbefore=%+v\nafter=%+v", beforeDefinition, afterDefinition)
	}

	afterRecord, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (after): %v", err)
	}
	if !reflect.DeepEqual(beforeRecord, afterRecord) {
		t.Errorf("activated definition row changed after list+diff:\nbefore=%+v\nafter=%+v", beforeRecord, afterRecord)
	}

	if _, _, err := f.FindToolBySlug(context.Background(), "outlook-get-message"); err == nil {
		t.Errorf("outlook-get-message (only present in the un-adopted 2.0.0 diff target) must not resolve — diffing must activate nothing")
	}
}

func containsSlug(slugs []string, target string) bool {
	for _, slug := range slugs {
		if slug == target {
			return true
		}
	}
	return false
}
