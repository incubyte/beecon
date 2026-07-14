// Package catalog_test exercises catalog.Facade against the in-memory
// Repository, with a fake ProviderDefinition set so tests do not depend on
// the real embedded outlook.yaml (definition_test.go and embed_test-style
// coverage already exercises that file directly).
package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

func fakeDefinitions() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "outlook",
			Name:         "Outlook",
			Logo:         "https://static.beecon.dev/providers/outlook.png",
			AuthScheme:   "oauth2",
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
			Scopes:       []string{"Mail.Read"},
		},
	}
}

func newCatalogFacade(t *testing.T) *catalog.Facade {
	t.Helper()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
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

func TestCreateIntegration_MintsAnIntgPrefixedIDDeterministically(t *testing.T) {
	f := newCatalogFacade(t)

	summary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(summary.ID) != "intg_1" {
		t.Errorf("ID = %q, want %q (deterministic sequential id from the memory fake)", summary.ID, "intg_1")
	}
}

func TestCreateIntegration_RejectsAnUnknownProviderSlug(t *testing.T) {
	f := newCatalogFacade(t)

	_, err := f.CreateIntegration(context.Background(), "does-not-exist", "client-id", "client-secret")

	assertDomainError(t, err, catalog.CodeValidationFailed, 422)
}

func TestCreateIntegration_SummaryCarriesProviderNameLogoAndAuthSchemeFromTheDefinition(t *testing.T) {
	f := newCatalogFacade(t)

	summary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.ProviderSlug != "outlook" {
		t.Errorf("ProviderSlug = %q, want %q", summary.ProviderSlug, "outlook")
	}
	if summary.ProviderName != "Outlook" {
		t.Errorf("ProviderName = %q, want %q", summary.ProviderName, "Outlook")
	}
	if summary.Logo != "https://static.beecon.dev/providers/outlook.png" {
		t.Errorf("Logo = %q, want the provider definition's logo", summary.Logo)
	}
	if summary.AuthScheme != "oauth2" {
		t.Errorf("AuthScheme = %q, want %q", summary.AuthScheme, "oauth2")
	}
}

// TestCreateIntegration_SummaryNeverSerializesTheClientSecret is a
// belt-and-suspenders behavior test for AC4: even though IntegrationSummary
// carries no ClientSecret field at the type level, this test guards against a
// future field addition silently leaking the secret by asserting on the
// actual JSON bytes a caller would see.
func TestCreateIntegration_SummaryNeverSerializesTheClientSecret(t *testing.T) {
	f := newCatalogFacade(t)
	const secret = "super-secret-oauth-client-secret"

	summary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("IntegrationSummary JSON %s contains the client secret — AC4 requires it never appear in any API response after creation", encoded)
	}
}

func TestListIntegrations_ReturnsEverySummaryOrderedByCreation(t *testing.T) {
	f := newCatalogFacade(t)
	ctx := context.Background()
	first, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summaries, err := f.ListIntegrations(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}
	if summaries[0].ID != first.ID || summaries[1].ID != second.ID {
		t.Errorf("order = [%q, %q], want creation order [%q, %q]", summaries[0].ID, summaries[1].ID, first.ID, second.ID)
	}
}

func TestListIntegrations_ReturnsEmptySliceWhenNoneExist(t *testing.T) {
	f := newCatalogFacade(t)

	summaries, err := f.ListIntegrations(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %+v, want empty", summaries)
	}
}

func TestGetIntegration_ReturnsAPreviouslyCreatedIntegrationIncludingItsClientSecret(t *testing.T) {
	f := newCatalogFacade(t)
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.GetIntegration(context.Background(), created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientSecret != "client-secret" {
		t.Errorf("ClientSecret = %q, want %q (internal reads need the secret to run OAuth; only the summary hides it)", got.ClientSecret, "client-secret")
	}
	if !reflect.DeepEqual(got.ID, created.ID) {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetIntegration_ReturnsTypedNotFoundForAnUnknownID(t *testing.T) {
	f := newCatalogFacade(t)

	_, err := f.GetIntegration(context.Background(), catalog.IntegrationID("intg_missing"))

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// --- GetExpectedParams (Slice 3, AC2) ---

// fakeDefinitionsWithExpectedParams is fakeDefinitions' Outlook provider plus
// a required non-secret "region" and a required secret "apiKey" expected
// param, so GetExpectedParams has something to return besides an empty list.
func fakeDefinitionsWithExpectedParams() []catalog.ProviderDefinition {
	defs := fakeDefinitions()
	defs[0].ExpectedParams = []catalog.ExpectedParam{
		{Name: "region", DisplayName: "Region", Description: "Your account's region.", Required: true, Secret: false},
		{Name: "apiKey", DisplayName: "API Key", Description: "Your account's API key.", Required: true, Secret: true},
	}
	return defs
}

func TestGetExpectedParams_ReturnsTheProvidersNameAndDeclaredFields(t *testing.T) {
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitionsWithExpectedParams()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	view, err := f.GetExpectedParams(context.Background(), created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.ProviderName != "Outlook" {
		t.Errorf("ProviderName = %q, want %q", view.ProviderName, "Outlook")
	}
	if len(view.Fields) != 2 {
		t.Fatalf("len(Fields) = %d, want 2", len(view.Fields))
	}
	if view.Fields[0].Name != "region" || view.Fields[0].Required != true || view.Fields[0].Secret != false {
		t.Errorf("Fields[0] = %+v, want the region field (required, non-secret)", view.Fields[0])
	}
	if view.Fields[1].Name != "apiKey" || view.Fields[1].Secret != true {
		t.Errorf("Fields[1] = %+v, want the apiKey field flagged secret", view.Fields[1])
	}
}

// TestGetExpectedParams_ReturnsEmptyFieldsForAProviderWithNoExpectedParams is
// AC6's read-path half: Outlook/Hubspot-shaped integrations report no fields
// to collect.
func TestGetExpectedParams_ReturnsEmptyFieldsForAProviderWithNoExpectedParams(t *testing.T) {
	f := newCatalogFacade(t)
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	view, err := f.GetExpectedParams(context.Background(), created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(view.Fields) != 0 {
		t.Errorf("Fields = %+v, want empty for a provider with no expectedParams", view.Fields)
	}
}

func TestGetExpectedParams_ReturnsNotFoundForAnUnknownIntegrationID(t *testing.T) {
	f := newCatalogFacade(t)

	_, err := f.GetExpectedParams(context.Background(), catalog.IntegrationID("intg_missing"))

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// --- Client-secret encryption (PD17, Slice 2) ---

// newCatalogFacadeWithRepo is newCatalogFacade plus a handle on the
// in-memory Repository itself, so a test can seed a row directly (bypassing
// the facade's own encryption, to simulate a Phase 1 row created before the
// vault existed) or inspect exactly what landed in storage rather than what
// the facade chooses to decrypt back out.
func newCatalogFacadeWithRepo(t *testing.T) (*catalog.Facade, *memory.Repository) {
	t.Helper()
	repo := memory.NewRepository()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Repository: repo, Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f, repo
}

// TestCreateIntegration_PersistsTheClientSecretEncryptedNeverInPlaintext is
// PD17's write-path half: the repository row CreateIntegration hands to
// storage must already be vault ciphertext, not the plaintext the caller
// supplied — asserted directly against the repository, independent of
// GetIntegration's own decryption.
func TestCreateIntegration_PersistsTheClientSecretEncryptedNeverInPlaintext(t *testing.T) {
	f, repo := newCatalogFacadeWithRepo(t)
	const secret = "super-secret-oauth-client-secret"

	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repo.FindByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !stored.ClientSecretEncrypted {
		t.Error("ClientSecretEncrypted = false, want true — CreateIntegration must persist ciphertext")
	}
	if stored.ClientSecret == secret || strings.Contains(stored.ClientSecret, secret) {
		t.Errorf("stored ClientSecret %q contains the raw secret — it must be vault ciphertext", stored.ClientSecret)
	}
}

// TestEncryptPlaintextClientSecrets_EncryptsAPlaintextRowAndPersistsItsCiphertext
// is AC3/PD17's boot backfill: a row persisted before the vault existed
// (ClientSecretEncrypted: false, a plaintext ClientSecret — Phase 1's
// Outlook rows) must come out of the backfill re-sealed as ciphertext, still
// decryptable back to the exact original plaintext via GetIntegration.
func TestEncryptPlaintextClientSecrets_EncryptsAPlaintextRowAndPersistsItsCiphertext(t *testing.T) {
	f, repo := newCatalogFacadeWithRepo(t)
	const legacyID = catalog.IntegrationID("intg_phase1_legacy")
	const legacySecret = "phase1-plaintext-outlook-client-secret"
	if err := repo.Save(context.Background(), catalog.Integration{
		ID: legacyID, ProviderSlug: "outlook", ClientID: "legacy-client-id",
		ClientSecret: legacySecret, ClientSecretEncrypted: false, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed legacy plaintext row: %v", err)
	}

	if err := f.EncryptPlaintextClientSecrets(context.Background()); err != nil {
		t.Fatalf("EncryptPlaintextClientSecrets: %v", err)
	}

	stored, err := repo.FindByID(context.Background(), legacyID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !stored.ClientSecretEncrypted {
		t.Error("ClientSecretEncrypted = false after backfill, want true")
	}
	if stored.ClientSecret == legacySecret || strings.Contains(stored.ClientSecret, legacySecret) {
		t.Errorf("stored ClientSecret %q contains the raw legacy secret after backfill — it must be ciphertext", stored.ClientSecret)
	}

	got, err := f.GetIntegration(context.Background(), legacyID)
	if err != nil {
		t.Fatalf("GetIntegration after backfill: %v", err)
	}
	if got.ClientSecret != legacySecret {
		t.Errorf("GetIntegration.ClientSecret after backfill = %q, want the original plaintext %q", got.ClientSecret, legacySecret)
	}
}

// TestEncryptPlaintextClientSecrets_IsIdempotentAndLeavesAnAlreadyEncryptedRowsCiphertextUntouched
// is the backfill's idempotency guarantee (PD17): every boot calls this, so a
// row already marked ClientSecretEncrypted must be left exactly as it was —
// re-sealing it again would still be correct but is wasted work the facade
// deliberately skips.
func TestEncryptPlaintextClientSecrets_IsIdempotentAndLeavesAnAlreadyEncryptedRowsCiphertextUntouched(t *testing.T) {
	f, repo := newCatalogFacadeWithRepo(t)
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	before, err := repo.FindByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	if err := f.EncryptPlaintextClientSecrets(context.Background()); err != nil {
		t.Fatalf("EncryptPlaintextClientSecrets: %v", err)
	}

	after, err := repo.FindByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if after.ClientSecret != before.ClientSecret {
		t.Errorf("ClientSecret changed after a no-op backfill: before=%q after=%q, want it left untouched", before.ClientSecret, after.ClientSecret)
	}
}

// withCapturedDefaultLog temporarily replaces slog.Default() with a handler
// writing to an in-memory buffer, restoring the original on cleanup — the
// only seam available to assert on EncryptPlaintextClientSecrets' own
// slog.Default().Info(...) call (PD38c, Slice 7) without a testability
// refactor to the production code (facade.go has no injected logger for this
// one-time boot backfill).
func withCapturedDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &buf
}

// TestEncryptPlaintextClientSecrets_LogsASuccessLineWithTheRowCountEncrypted
// is PD38c (Slice 7, Phase 2 review carry-forward): the backfill logs a
// success line naming exactly how many rows it encrypted this run, so an
// operator can confirm the one-time migration ran from the boot log alone.
func TestEncryptPlaintextClientSecrets_LogsASuccessLineWithTheRowCountEncrypted(t *testing.T) {
	f, repo := newCatalogFacadeWithRepo(t)
	for i, id := range []string{"intg_legacy_1", "intg_legacy_2"} {
		if err := repo.Save(context.Background(), catalog.Integration{
			ID: catalog.IntegrationID(id), ProviderSlug: "outlook", ClientID: fmt.Sprintf("client-%d", i),
			ClientSecret: "plaintext-secret", ClientSecretEncrypted: false, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("seed legacy plaintext row %s: %v", id, err)
		}
	}
	log := withCapturedDefaultLog(t)

	if err := f.EncryptPlaintextClientSecrets(context.Background()); err != nil {
		t.Fatalf("EncryptPlaintextClientSecrets: %v", err)
	}

	if !strings.Contains(log.String(), "encrypted 2 plaintext client secrets") {
		t.Errorf("log output = %q, want it to contain %q", log.String(), "encrypted 2 plaintext client secrets")
	}
}

// TestEncryptPlaintextClientSecrets_LogsZeroWhenNothingWasLeftToEncrypt is
// PD38c's own "including zero" clause: a run that finds nothing left to
// backfill still logs a success line naming zero, so silence never has to be
// interpreted as "did this even run?".
func TestEncryptPlaintextClientSecrets_LogsZeroWhenNothingWasLeftToEncrypt(t *testing.T) {
	f, _ := newCatalogFacadeWithRepo(t)
	log := withCapturedDefaultLog(t)

	if err := f.EncryptPlaintextClientSecrets(context.Background()); err != nil {
		t.Fatalf("EncryptPlaintextClientSecrets: %v", err)
	}

	if !strings.Contains(log.String(), "encrypted 0 plaintext client secrets") {
		t.Errorf("log output = %q, want it to contain %q", log.String(), "encrypted 0 plaintext client secrets")
	}
}

// --- ListTools / ToolDetail (Slice 1's catalog API) ---

const testOrgID = organizations.OrgID("org_1")

func minimalSchema() map[string]any {
	return map[string]any{"type": "object"}
}

// toolCatalogDefinitions is two providers' worth of tools: enough to prove
// ListTools' providerSlug filter actually narrows (not just "return
// everything"), plus one deprecated tool to exercise the deprecation filter.
func toolCatalogDefinitions() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook", Logo: "https://static.beecon.dev/providers/outlook.png", AuthScheme: "oauth2",
			AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token", Scopes: []string{"Mail.Read"},
			Tools: []catalog.ProviderTool{
				{Slug: "outlook-get-message", Name: "Get email message", Description: "Retrieves a message by id.", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
				{Slug: "outlook-list-messages", Name: "List messages", Description: "Lists mailbox messages.", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
				{Slug: "outlook-legacy-tool", Name: "Legacy tool", Description: "Deprecated.", InputSchema: minimalSchema(), OutputSchema: minimalSchema(), Deprecated: true},
			},
		},
		{
			Slug: "slack", Name: "Slack", Logo: "https://static.beecon.dev/providers/slack.png", AuthScheme: "oauth2",
			AuthorizeURL: "https://slack.example.com/authorize", TokenURL: "https://slack.example.com/token", Scopes: []string{"chat:write"},
			Tools: []catalog.ProviderTool{
				{Slug: "slack-post-message", Name: "Post message", Description: "Posts a chat message.", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
			},
		},
	}
}

func newToolCatalogFacade(t *testing.T) *catalog.Facade {
	t.Helper()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: toolCatalogDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
}

func TestListTools_FiltersByProviderSlug(t *testing.T) {
	f := newToolCatalogFacade(t)

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{ProviderSlug: "outlook"}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 (outlook's two non-deprecated tools)", len(page.Items))
	}
	for _, item := range page.Items {
		if item.ProviderSlug != "outlook" {
			t.Errorf("ProviderSlug = %q, want %q", item.ProviderSlug, "outlook")
		}
	}
}

func TestListTools_FiltersByIntegrationIDResolvedToItsProvider(t *testing.T) {
	f := newToolCatalogFacade(t)
	summary, err := f.CreateIntegration(context.Background(), "slack", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("unexpected error creating the integration: %v", err)
	}

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{IntegrationID: summary.ID}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].Slug != "slack-post-message" {
		t.Errorf("Slug = %q, want %q", page.Items[0].Slug, "slack-post-message")
	}
}

// TestListTools_UnknownIntegrationIDReturnsNotFound is the coder's flag #1:
// integrations are installation-level (PD7), so there is no cross-org
// semantics to invent here — an integrationId that names no integration at
// all is simply not-found.
func TestListTools_UnknownIntegrationIDReturnsNotFound(t *testing.T) {
	f := newToolCatalogFacade(t)

	_, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{IntegrationID: catalog.IntegrationID("intg_does_not_exist")}, "", 0)

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

func TestListTools_UnknownProviderSlugReturnsNotFound(t *testing.T) {
	f := newToolCatalogFacade(t)

	_, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{ProviderSlug: "does-not-exist"}, "", 0)

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// TestListTools_ExcludesDeprecatedToolsByDefault pins the coder's flag #2:
// includeDeprecated defaults to excluded.
func TestListTools_ExcludesDeprecatedToolsByDefault(t *testing.T) {
	f := newToolCatalogFacade(t)

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{ProviderSlug: "outlook"}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, item := range page.Items {
		if item.Slug == "outlook-legacy-tool" {
			t.Fatalf("deprecated tool %q present, want it excluded by default", item.Slug)
		}
	}
}

func TestListTools_IncludesDeprecatedToolsWhenOptedIn(t *testing.T) {
	f := newToolCatalogFacade(t)

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{ProviderSlug: "outlook", IncludeDeprecated: true}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("len(Items) = %d, want 3 (including the deprecated tool)", len(page.Items))
	}
	found := false
	for _, item := range page.Items {
		if item.Slug == "outlook-legacy-tool" {
			found = true
			if !item.Deprecated {
				t.Error("Deprecated = false, want true for outlook-legacy-tool")
			}
		}
	}
	if !found {
		t.Error("outlook-legacy-tool missing from the includeDeprecated=true result")
	}
}

func TestListTools_CursorPaginationWalksEveryNonDeprecatedToolSortedBySlugWithoutDuplicatesOrGaps(t *testing.T) {
	f := newToolCatalogFacade(t)
	ctx := context.Background()

	var order []string
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 10; i++ {
		page, err := f.ListTools(ctx, testOrgID, catalog.ToolFilter{}, cursor, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, item := range page.Items {
			if seen[item.Slug] {
				t.Fatalf("slug %q seen more than once while paginating", item.Slug)
			}
			seen[item.Slug] = true
			order = append(order, item.Slug)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	want := []string{"outlook-get-message", "outlook-list-messages", "slack-post-message"}
	if len(order) != len(want) {
		t.Fatalf("walked %d tools across all pages, want exactly %d (no duplicates or gaps): %v", len(order), len(want), order)
	}
	for i, slug := range want {
		if order[i] != slug {
			t.Errorf("order[%d] = %q, want %q (sorted by slug)", i, order[i], slug)
		}
	}
}

// manyToolDefinitions returns one provider with count non-deprecated tools,
// sorted-by-slug-friendly names (tool-000, tool-001, ...), so tests can prove
// normalizeToolLimit's clamp at maxToolPageLimit (200) with an observable
// page size rather than testing the unexported function directly.
func manyToolDefinitions(count int) []catalog.ProviderDefinition {
	tools := make([]catalog.ProviderTool, count)
	for i := range tools {
		slug := fmt.Sprintf("tool-%03d", i)
		tools[i] = catalog.ProviderTool{Slug: slug, Name: slug, Description: "generated", InputSchema: minimalSchema(), OutputSchema: minimalSchema()}
	}
	return []catalog.ProviderDefinition{
		{Slug: "many", Name: "Many", AuthScheme: "oauth2", AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token", Tools: tools},
	}
}

// TestListTools_ClampsARequestedLimitAboveTheMaximumTo200 covers
// normalizeToolLimit's upper clamp (facade.go): a caller-requested limit
// above maxToolPageLimit must not be honored as-is.
func TestListTools_ClampsARequestedLimitAboveTheMaximumTo200(t *testing.T) {
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: manyToolDefinitions(205)})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	page, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{}, "", 250)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 200 {
		t.Fatalf("len(Items) = %d, want 200 (a requested limit of 250 must clamp to the maximum)", len(page.Items))
	}
	if page.NextCursor == "" {
		t.Error("NextCursor is empty, want a cursor since 205 tools exceed the clamped page of 200")
	}
}

func TestListTools_InvalidCursorReturnsAValidationError(t *testing.T) {
	f := newToolCatalogFacade(t)

	_, err := f.ListTools(context.Background(), testOrgID, catalog.ToolFilter{}, "not-valid-base64!!", 0)

	assertDomainError(t, err, catalog.CodeValidationFailed, 422)
}

func TestToolDetail_ReturnsTheToolBySlugWithItsProviderIdentity(t *testing.T) {
	f := newToolCatalogFacade(t)

	tool, err := f.ToolDetail(context.Background(), "outlook-get-message")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.Name != "Get email message" {
		t.Errorf("Name = %q, want %q", tool.Name, "Get email message")
	}
	if tool.ProviderSlug != "outlook" {
		t.Errorf("ProviderSlug = %q, want %q", tool.ProviderSlug, "outlook")
	}
	if tool.ProviderName != "Outlook" {
		t.Errorf("ProviderName = %q, want %q", tool.ProviderName, "Outlook")
	}
}

func TestToolDetail_UnknownSlugReturnsNotFound(t *testing.T) {
	f := newToolCatalogFacade(t)

	_, err := f.ToolDetail(context.Background(), "does-not-exist")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

func TestToolDetail_ADeprecatedToolIsStillRetrievableBySlug(t *testing.T) {
	f := newToolCatalogFacade(t)

	tool, err := f.ToolDetail(context.Background(), "outlook-legacy-tool")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tool.Deprecated {
		t.Error("Deprecated = false, want true")
	}
}

// --- ListTriggerDefinitions / TriggerDefinitionDetail (Slice 1's catalog API) ---

// triggerCatalogDefinitions is two providers' worth of trigger definitions —
// enough to prove ListTriggerDefinitions' providerSlug filter actually
// narrows (not just "return everything") and that cross-provider cursor
// pagination walks in slug order.
func triggerCatalogDefinitions() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook", Logo: "https://static.beecon.dev/providers/outlook.png", AuthScheme: "oauth2",
			AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token", Scopes: []string{"Mail.Read"},
			Triggers: []catalog.TriggerDefinition{
				{
					Slug: "outlook-message-received", Name: "New message received",
					Description: "Triggered when a new message arrives.",
					ConfigSchema: map[string]any{"type": "object", "properties": map[string]any{
						"folderId": map[string]any{"type": "string", "default": "Inbox"},
					}},
					PayloadSchema: map[string]any{"type": "object", "properties": map[string]any{
						"id": map[string]any{"type": "string"},
					}},
					Ingestion: "poll", PollIntervalSeconds: 60,
				},
			},
		},
		{
			Slug: "hubspot", Name: "Hubspot", Logo: "https://static.beecon.dev/providers/hubspot.png", AuthScheme: "oauth2",
			AuthorizeURL: "https://hubspot.example.com/authorize", TokenURL: "https://hubspot.example.com/token", Scopes: []string{"crm.objects.contacts.read"},
			Triggers: []catalog.TriggerDefinition{
				{
					Slug: "hubspot-contact-created", Name: "New contact created",
					Description:   "Triggered when a new CRM contact is created.",
					ConfigSchema:  minimalSchema(),
					PayloadSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
					Ingestion:     "poll", PollIntervalSeconds: 60,
				},
			},
		},
	}
}

func newTriggerCatalogFacade(t *testing.T) *catalog.Facade {
	t.Helper()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: triggerCatalogDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
}

// TestListTriggerDefinitions_FiltersByProviderSlugAndCarriesTheFullShapeAndProviderIdentity
// is AC1: slug, name, description, config schema, payload schema, ingestion
// mode, and the owning provider's identity, narrowed by providerSlug.
func TestListTriggerDefinitions_FiltersByProviderSlugAndCarriesTheFullShapeAndProviderIdentity(t *testing.T) {
	f := newTriggerCatalogFacade(t)

	page, err := f.ListTriggerDefinitions(context.Background(), testOrgID, catalog.TriggerDefinitionFilter{ProviderSlug: "outlook"}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 (only outlook's trigger)", len(page.Items))
	}
	item := page.Items[0]
	if item.Slug != "outlook-message-received" {
		t.Errorf("Slug = %q, want %q", item.Slug, "outlook-message-received")
	}
	if item.Name != "New message received" {
		t.Errorf("Name = %q, want %q", item.Name, "New message received")
	}
	if item.Description == "" {
		t.Error("Description is empty, want the trigger's description")
	}
	if len(item.ConfigSchema) == 0 {
		t.Error("ConfigSchema is empty, want the parsed folderId schema")
	}
	if len(item.PayloadSchema) == 0 {
		t.Error("PayloadSchema is empty, want the parsed id schema")
	}
	if item.Ingestion != "poll" {
		t.Errorf("Ingestion = %q, want %q", item.Ingestion, "poll")
	}
	if item.ProviderSlug != "outlook" || item.ProviderName != "Outlook" || item.ProviderLogo == "" {
		t.Errorf("provider identity = {%q %q %q}, want outlook/Outlook/<logo>", item.ProviderSlug, item.ProviderName, item.ProviderLogo)
	}
}

func TestListTriggerDefinitions_FiltersByIntegrationIDResolvedToItsProvider(t *testing.T) {
	f := newTriggerCatalogFacade(t)
	summary, err := f.CreateIntegration(context.Background(), "hubspot", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("unexpected error creating the integration: %v", err)
	}

	page, err := f.ListTriggerDefinitions(context.Background(), testOrgID, catalog.TriggerDefinitionFilter{IntegrationID: summary.ID}, "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].Slug != "hubspot-contact-created" {
		t.Errorf("Slug = %q, want %q", page.Items[0].Slug, "hubspot-contact-created")
	}
}

// TestListTriggerDefinitions_UnknownIntegrationIDReturnsNotFound is Slice 1's
// AC6 read through resolveProviderSlugFilter: an integrationId naming no
// integration at all is not-found (integrations are installation-level, no
// cross-org semantics to invent — mirrors ListTools' own documented decision).
func TestListTriggerDefinitions_UnknownIntegrationIDReturnsNotFound(t *testing.T) {
	f := newTriggerCatalogFacade(t)

	_, err := f.ListTriggerDefinitions(context.Background(), testOrgID, catalog.TriggerDefinitionFilter{IntegrationID: catalog.IntegrationID("intg_does_not_exist")}, "", 0)

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// TestListTriggerDefinitions_UnknownProviderSlugReturnsNotFound is AC6's
// other half.
func TestListTriggerDefinitions_UnknownProviderSlugReturnsNotFound(t *testing.T) {
	f := newTriggerCatalogFacade(t)

	_, err := f.ListTriggerDefinitions(context.Background(), testOrgID, catalog.TriggerDefinitionFilter{ProviderSlug: "does-not-exist"}, "", 0)

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

// TestListTriggerDefinitions_CursorPaginationWalksEveryTriggerSortedBySlugWithoutDuplicatesOrGaps
// is AC1's "cursor-paginated" half, proven across both providers' triggers.
func TestListTriggerDefinitions_CursorPaginationWalksEveryTriggerSortedBySlugWithoutDuplicatesOrGaps(t *testing.T) {
	f := newTriggerCatalogFacade(t)
	ctx := context.Background()

	var order []string
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 10; i++ {
		page, err := f.ListTriggerDefinitions(ctx, testOrgID, catalog.TriggerDefinitionFilter{}, cursor, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, item := range page.Items {
			if seen[item.Slug] {
				t.Fatalf("slug %q seen more than once while paginating", item.Slug)
			}
			seen[item.Slug] = true
			order = append(order, item.Slug)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	want := []string{"hubspot-contact-created", "outlook-message-received"}
	if len(order) != len(want) {
		t.Fatalf("walked %d triggers across all pages, want exactly %d (no duplicates or gaps): %v", len(order), len(want), order)
	}
	for i, slug := range want {
		if order[i] != slug {
			t.Errorf("order[%d] = %q, want %q (sorted by slug)", i, order[i], slug)
		}
	}
}

func TestTriggerDefinitionDetail_ReturnsTheTriggerBySlugWithItsProviderIdentityAndSchemas(t *testing.T) {
	f := newTriggerCatalogFacade(t)

	trigger, err := f.TriggerDefinitionDetail(context.Background(), "hubspot-contact-created")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trigger.Name != "New contact created" {
		t.Errorf("Name = %q, want %q", trigger.Name, "New contact created")
	}
	if trigger.ProviderSlug != "hubspot" || trigger.ProviderName != "Hubspot" {
		t.Errorf("provider identity = {%q %q}, want hubspot/Hubspot", trigger.ProviderSlug, trigger.ProviderName)
	}
	if len(trigger.PayloadSchema) == 0 {
		t.Error("PayloadSchema is empty, want the parsed id schema")
	}
}

func TestTriggerDefinitionDetail_UnknownSlugReturnsNotFound(t *testing.T) {
	f := newTriggerCatalogFacade(t)

	_, err := f.TriggerDefinitionDetail(context.Background(), "does-not-exist")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}
