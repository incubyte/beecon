// provider_definitions_test.go exercises catalog.Facade's operator-only,
// installation-wide provider-definitions read surface (PD40, Slice 6):
// ListProviderDefinitions and ProviderDefinitionDetail. Reuses
// fakeDefinitions/toolCatalogDefinitions/triggerCatalogDefinitions/
// minimalSchema/assertDomainError/testOrgID from facade_test.go, and
// newGovernedCatalogFacade/mustCreateOrg/mustSetGovernance from
// governance_visibility_test.go (same package) for the AC7 unfiltered proof.
package catalog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/organizations"
)

func newProviderDefinitionsFacade(t *testing.T, defs []catalog.ProviderDefinition) *catalog.Facade {
	t.Helper()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: defs})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
}

func TestListProviderDefinitions_ReturnsEveryLoadedDefinitionSortedBySlugWithNameAuthSchemeAndCounts(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())

	page, err := f.ListProviderDefinitions(context.Background(), "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 (outlook + slack)", len(page.Items))
	}
	// Sorted by slug ascending: "outlook" < "slack".
	outlook, slack := page.Items[0], page.Items[1]
	if outlook.Slug != "outlook" || slack.Slug != "slack" {
		t.Fatalf("slugs = [%q, %q], want [%q, %q] sorted ascending", outlook.Slug, slack.Slug, "outlook", "slack")
	}
	if outlook.Name != "Outlook" {
		t.Errorf("outlook.Name = %q, want %q", outlook.Name, "Outlook")
	}
	if outlook.AuthScheme != "oauth2" {
		t.Errorf("outlook.AuthScheme = %q, want %q", outlook.AuthScheme, "oauth2")
	}
	if outlook.FormatVersion != 1 {
		t.Errorf("outlook.FormatVersion = %d, want 1", outlook.FormatVersion)
	}
	// toolCatalogDefinitions gives outlook 3 tools (one deprecated, still
	// counted — the summary counts every declared tool, not just the
	// non-deprecated subset ListTools defaults to) and 0 triggers.
	if outlook.ToolCount != 3 {
		t.Errorf("outlook.ToolCount = %d, want 3", outlook.ToolCount)
	}
	if outlook.TriggerCount != 0 {
		t.Errorf("outlook.TriggerCount = %d, want 0", outlook.TriggerCount)
	}
	if slack.ToolCount != 1 {
		t.Errorf("slack.ToolCount = %d, want 1", slack.ToolCount)
	}
}

func TestListProviderDefinitions_ReturnsAnEmptyPageWhenNoDefinitionsAreLoaded(t *testing.T) {
	f := newProviderDefinitionsFacade(t, []catalog.ProviderDefinition{})

	page, err := f.ListProviderDefinitions(context.Background(), "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("len(Items) = %d, want 0", len(page.Items))
	}
	if page.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty", page.NextCursor)
	}
}

// TestListProviderDefinitions_CursorPaginationWalksEveryDefinitionSortedBySlugWithoutDuplicatesOrGaps
// mirrors TestListTools_CursorPaginationWalksEveryNonDeprecatedToolSortedBySlugWithoutDuplicatesOrGaps'
// own walk-the-whole-list convention, applied to provider definitions (PD15's
// cursor pagination default page size of 1 forces >1 page here).
func TestListProviderDefinitions_CursorPaginationWalksEveryDefinitionSortedBySlugWithoutDuplicatesOrGaps(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())

	var slugs []string
	cursor := ""
	for {
		page, err := f.ListProviderDefinitions(context.Background(), cursor, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, item := range page.Items {
			slugs = append(slugs, item.Slug)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(slugs) != 2 || slugs[0] != "outlook" || slugs[1] != "slack" {
		t.Fatalf("slugs = %v, want [outlook slack] walked page by page without duplicates or gaps", slugs)
	}
}

func TestListProviderDefinitions_ClampsARequestedLimitAboveTheMaximumTo200(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())

	page, err := f.ListProviderDefinitions(context.Background(), "", 10_000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2 — a limit above the maximum must still return everything (only 2 defined), never error", len(page.Items))
	}
}

func TestListProviderDefinitions_InvalidCursorReturnsAValidationError(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())

	_, err := f.ListProviderDefinitions(context.Background(), "not-valid-base64!!", 0)

	assertDomainError(t, err, catalog.CodeValidationFailed, 422)
}

// TestListProviderDefinitions_IsNotFilteredByAnOrgsRestrictiveGovernance is
// the CRITICAL AC7 proof: an integration hidden — and a provider entirely
// excluded by an allow-list — for one organization must still appear, in
// full, in the operator's installation-wide provider-definitions view.
// ListProviderDefinitions takes no organization parameter at all (unlike
// ListIntegrations/ListTools/ListTriggerDefinitions), so this wires the REAL
// organizations.Facade as the GovernanceReader (mirrors
// governance_visibility_test.go), sets the most restrictive governance
// possible for an org (an empty allow-list — nothing allowed at all), and
// confirms the operator's provider-definitions read is entirely unaffected:
// governance simply never intercepts this path.
func TestListProviderDefinitions_IsNotFilteredByAnOrgsRestrictiveGovernance(t *testing.T) {
	orgs := newOrgsFacade()
	restrictedOrg := mustCreateOrg(t, orgs, "Restricted Co")
	f := newGovernedCatalogFacade(t, orgs, fakeDefinitions())
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	// The most restrictive governance an operator can set for this org: an
	// empty (non-nil) allow-list excludes every integration, AND the same
	// integration is also explicitly hidden — belt-and-suspenders restrictive.
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

	// The operator's unfiltered view: outlook must still be present.
	page, err := f.ListProviderDefinitions(context.Background(), "", 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Slug != "outlook" {
		t.Fatalf("ListProviderDefinitions = %+v, want the outlook provider definition present despite %s's restrictive governance (AC7: operator views are never governance-filtered)", page.Items, restrictedOrg)
	}
}

func TestProviderDefinitionDetail_ReturnsTheFullBundleWithFormatVersionOauthToolsAndTriggers(t *testing.T) {
	f := newProviderDefinitionsFacade(t, triggerCatalogDefinitions())

	detail, err := f.ProviderDefinitionDetail(context.Background(), "outlook")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Slug != "outlook" || detail.Name != "Outlook" {
		t.Fatalf("Slug/Name = %q/%q, want %q/%q", detail.Slug, detail.Name, "outlook", "Outlook")
	}
	if detail.FormatVersion != 1 {
		t.Errorf("FormatVersion = %d, want 1", detail.FormatVersion)
	}
	oauth, ok := detail.Bundle["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("Bundle[\"oauth\"] = %T, want map[string]any", detail.Bundle["oauth"])
	}
	if oauth["authorizeUrl"] != "https://example.com/authorize" {
		t.Errorf("oauth.authorizeUrl = %v, want %q", oauth["authorizeUrl"], "https://example.com/authorize")
	}
	if oauth["tokenUrl"] != "https://example.com/token" {
		t.Errorf("oauth.tokenUrl = %v, want %q", oauth["tokenUrl"], "https://example.com/token")
	}
	triggers, ok := detail.Bundle["triggers"].([]map[string]any)
	if !ok || len(triggers) != 1 {
		t.Fatalf("Bundle[\"triggers\"] = %+v, want exactly outlook's one declared trigger", detail.Bundle["triggers"])
	}
	if triggers[0]["slug"] != "outlook-message-received" {
		t.Errorf("triggers[0].slug = %v, want %q", triggers[0]["slug"], "outlook-message-received")
	}
	if triggers[0]["configSchema"] == nil {
		t.Error("triggers[0].configSchema is nil, want the trigger's declared config schema")
	}
	if triggers[0]["payloadSchema"] == nil {
		t.Error("triggers[0].payloadSchema is nil, want the trigger's declared payload schema")
	}
}

func TestProviderDefinitionDetail_BundleFaithfullyRoundTripsEveryToolsInputAndOutputSchema(t *testing.T) {
	f := newProviderDefinitionsFacade(t, toolCatalogDefinitions())

	detail, err := f.ProviderDefinitionDetail(context.Background(), "outlook")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tools, ok := detail.Bundle["tools"].([]map[string]any)
	if !ok || len(tools) != 3 {
		t.Fatalf("Bundle[\"tools\"] = %+v, want outlook's 3 declared tools (including the deprecated one)", detail.Bundle["tools"])
	}
	var getMessage map[string]any
	for _, tool := range tools {
		if tool["slug"] == "outlook-get-message" {
			getMessage = tool
		}
	}
	if getMessage == nil {
		t.Fatalf("tools = %+v, want to find outlook-get-message", tools)
	}
	if _, ok := getMessage["inputSchema"].(map[string]any); !ok {
		t.Errorf("inputSchema = %T, want the tool's declared input JSON-Schema object", getMessage["inputSchema"])
	}
	if _, ok := getMessage["outputSchema"].(map[string]any); !ok {
		t.Errorf("outputSchema = %T, want the tool's declared output JSON-Schema object", getMessage["outputSchema"])
	}
	deprecatedFlag, ok := getDeprecatedFlag(tools, "outlook-legacy-tool")
	if !ok || deprecatedFlag != true {
		t.Errorf("outlook-legacy-tool.deprecated = %v (ok=%v), want true — the bundle carries every tool, deprecated or not", deprecatedFlag, ok)
	}
}

func getDeprecatedFlag(tools []map[string]any, slug string) (any, bool) {
	for _, tool := range tools {
		if tool["slug"] == slug {
			v, ok := tool["deprecated"]
			return v, ok
		}
	}
	return nil, false
}

// TestProviderDefinitionDetail_BundleNeverContainsIntegrationClientSecretMaterial
// pins the secrets-never-leak requirement: a provider definition's Bundle is
// declarative config the installation shipped with — it carries no
// credentials at all (ProviderDefinition itself has no client id/secret
// field; those live only on catalog.Integration, created separately per
// installation, PD17-vault-encrypted). This creates a real Integration
// carrying a distinctive secret value against the same provider slug and
// confirms the secret never appears anywhere in the marshaled bundle JSON —
// belt-and-suspenders, the same style as
// TestCreateIntegration_SummaryNeverSerializesTheClientSecret.
func TestProviderDefinitionDetail_BundleNeverContainsIntegrationClientSecretMaterial(t *testing.T) {
	f := newProviderDefinitionsFacade(t, fakeDefinitions())
	const distinctiveSecret = "super-secret-oauth-client-secret-value"
	if _, err := f.CreateIntegration(context.Background(), "outlook", "client-id", distinctiveSecret); err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	detail, err := f.ProviderDefinitionDetail(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if strings.Contains(string(encoded), distinctiveSecret) {
		t.Fatalf("provider definition bundle JSON %s contains an Integration's client secret — provider definitions must expose only declarative config, never credentials", encoded)
	}
	if strings.Contains(string(encoded), "clientSecret") || strings.Contains(string(encoded), "client_secret") {
		t.Fatalf("provider definition bundle JSON %s carries a client-secret-shaped field at all", encoded)
	}
}

func TestProviderDefinitionDetail_UnknownSlugReturnsNotFound(t *testing.T) {
	f := newProviderDefinitionsFacade(t, fakeDefinitions())

	_, err := f.ProviderDefinitionDetail(context.Background(), "does-not-exist")

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}
