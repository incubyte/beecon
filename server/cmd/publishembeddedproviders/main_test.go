//go:build integration

// main_test.go (package main) exercises the Phase 5 registry sub-phase's
// Slice 6 AC1: the one-time embedded-provider migration script publishes
// this installation's real embedded Outlook and Hubspot definitions to a
// real registry service (support.NewTestRegistryServer, standing in for the
// separately-deployed cmd/registry binary, PD59) and each becomes a valid
// 1.0.0 bundle with a tool_ id minted per tool. Getting a 201 from the real
// publish endpoint IS the "parses under the strict loader" and
// "output-schema-vs-sample gate" assertions (registryservice.Facade.Publish
// runs both before it ever stores anything, schemagate.go/strict_parse.go)
// — this file additionally pulls the stored bundle back and re-validates
// every tool's sample against its own output schema directly, so the claim
// is checked here too, not just trusted from the far side of an HTTP call.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"beecon/internal/catalog"
	"beecon/internal/registrybundle"
	"beecon/internal/schema"
	"beecon/test/support"
)

// pullBundle fetches providerSlug's bundle at version from registryServer's
// real pull API (installation-facing, API-key authenticated — the same
// route BootAppWithProviderDefinitionsAndRegistry's installation pulls
// through), decoded into the shared wire shape.
func pullBundle(t *testing.T, registryServer *support.TestRegistryServer, providerSlug, version string) registrybundle.Bundle {
	t.Helper()
	url := fmt.Sprintf("%s/registry/v1/providers/%s/bundles/%s", registryServer.URL, providerSlug, version)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build pull request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+registryServer.APIKey)

	resp, err := registryServer.Client().Do(req)
	if err != nil {
		t.Fatalf("pull request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pull status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, body)
	}
	var bundle registrybundle.Bundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode pulled bundle: %v; body=%s", err, body)
	}
	return bundle
}

func embeddedDefinitionBySlug(t *testing.T, slug string) catalog.ProviderDefinition {
	t.Helper()
	definitions, err := catalog.DefaultProviderDefinitions()
	if err != nil {
		t.Fatalf("DefaultProviderDefinitions: %v", err)
	}
	for _, d := range definitions {
		if d.Slug == slug {
			return d
		}
	}
	t.Fatalf("no embedded provider definition found for slug %q", slug)
	return catalog.ProviderDefinition{}
}

func TestPublishEmbeddedProviders_PublishesTheRealEmbeddedOutlookAndHubspotDefinitionsAsValidInitialBundles(t *testing.T) {
	registryServer := support.NewTestRegistryServer(t, "test-publish-token", "test-registry-api-key")
	ctx := context.Background()
	client := &http.Client{}

	for slug := range migratedProviderSlugs {
		t.Run(slug, func(t *testing.T) {
			definition := embeddedDefinitionBySlug(t, slug)
			if len(definition.Tools) == 0 {
				t.Fatalf("test fixture bug: embedded definition %q carries no tools", slug)
			}

			if err := publishDefinition(ctx, client, registryServer.URL, registryServer.PublishToken, definition); err != nil {
				t.Fatalf("publishDefinition(%q): %v", slug, err)
			}

			bundle := pullBundle(t, registryServer, slug, "1.0.0")

			if bundle.FormatVersion != 1 {
				t.Errorf("formatVersion = %d, want 1", bundle.FormatVersion)
			}
			if bundle.Version != "1.0.0" {
				t.Errorf("version = %q, want %q (a provider's first bundle)", bundle.Version, "1.0.0")
			}
			if len(bundle.Tools) != len(definition.Tools) {
				t.Fatalf("published bundle carries %d tools, want %d (every embedded tool)", len(bundle.Tools), len(definition.Tools))
			}
			for _, tool := range bundle.Tools {
				if !strings.HasPrefix(tool.ID, "tool_") {
					t.Errorf("tool %q: id = %q, want a tool_-prefixed id", tool.Slug, tool.ID)
				}
				if err := schema.Validate(tool.OutputSchema, tool.Sample); err != nil {
					t.Errorf("tool %q: its own generated sample does not validate against its own output schema: %v (sample=%+v)", tool.Slug, err, tool.Sample)
				}
			}
		})
	}
}

func TestPublishEmbeddedProviders_APublishedProvidersToolIDsAreDistinct(t *testing.T) {
	registryServer := support.NewTestRegistryServer(t, "test-publish-token", "test-registry-api-key")
	definition := embeddedDefinitionBySlug(t, "outlook")
	if err := publishDefinition(context.Background(), &http.Client{}, registryServer.URL, registryServer.PublishToken, definition); err != nil {
		t.Fatalf("publishDefinition: %v", err)
	}

	bundle := pullBundle(t, registryServer, "outlook", "1.0.0")

	seen := map[string]string{}
	for _, tool := range bundle.Tools {
		if owner, dup := seen[tool.ID]; dup {
			t.Fatalf("tool_ id %q minted for both %q and %q", tool.ID, owner, tool.Slug)
		}
		seen[tool.ID] = tool.Slug
	}
}

// TestPublishEmbeddedProviders_ATooLWithNoOutputSchemaIsRejectedNotSilentlyPublished is
// an adversarial edge case: a tool declaring no output schema at all has
// nothing for schema.Example to synthesize a meaningful sample from, and the
// registry's own publish-time gate (PD63) requires both an output schema and
// a sample that validates against it. The migration script must surface that
// rejection as an error, not publish a bundle carrying a tool nobody could
// ever validate a real response against.
func TestPublishEmbeddedProviders_AToolWithNoOutputSchemaIsRejectedNotSilentlyPublished(t *testing.T) {
	registryServer := support.NewTestRegistryServer(t, "test-publish-token", "test-registry-api-key")
	definition := catalog.ProviderDefinition{
		Slug: "acme-crm", Name: "Acme CRM", AuthScheme: "oauth2",
		AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token",
		Tools: []catalog.ProviderTool{{
			Slug: "acme-legacy-tool", Name: "Legacy tool", Method: "GET", Path: "/v1/legacy",
			InputSchema: map[string]any{"type": "object"}, // OutputSchema deliberately left nil
		}},
	}

	err := publishDefinition(context.Background(), &http.Client{}, registryServer.URL, registryServer.PublishToken, definition)

	if err == nil {
		t.Fatal("publishDefinition succeeded for a tool with no output schema, want it rejected")
	}
}
