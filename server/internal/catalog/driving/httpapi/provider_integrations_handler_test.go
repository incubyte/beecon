// provider_integrations_handler_test.go exercises the catalog module's
// installation-wide, operator-only ListIntegrationsForProvider handler.
// Reuses fakeDefinitions/toolCatalogDefinitions/doRequest/decodeError from
// handler_test.go and newProviderDefinitionsTestRouter's router-building
// convention from provider_definitions_handler_test.go (same package). The
// consoleAuth guard itself is proven at the router level
// (internal/app/provider_definitions_route_admin_guard_test.go), so — like
// its sibling handlers in this file — these tests exercise the handler with
// a plain doRequest.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
)

// twoProviderDefinitionsFixture gives the filtering test a second provider
// (slack) alongside this file's package-level fakeDefinitions' outlook, so an
// integration created against each provider proves ListIntegrationsForProvider
// actually filters rather than merely returning everything.
func twoProviderDefinitionsFixture() []catalog.ProviderDefinition {
	defs := fakeDefinitions()
	return append(defs, catalog.ProviderDefinition{
		Slug:         "slack",
		Name:         "Slack",
		Logo:         "https://static.beecon.dev/providers/slack.png",
		AuthScheme:   "oauth2",
		AuthorizeURL: "https://slack.example.com/authorize",
		TokenURL:     "https://slack.example.com/token",
		Scopes:       []string{"chat:write"},
	})
}

func TestListIntegrationsForProvider_Returns200WithItemsFilteredToTheSlug(t *testing.T) {
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: twoProviderDefinitionsFixture()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := facade.CreateIntegration(context.Background(), "outlook", "outlook-client", "outlook-secret"); err != nil {
		t.Fatalf("CreateIntegration(outlook): %v", err)
	}
	if _, err := facade.CreateIntegration(context.Background(), "slack", "slack-client", "slack-secret"); err != nil {
		t.Fatalf("CreateIntegration(slack): %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)
	r := chi.NewRouter()
	r.Get("/provider-definitions/{slug}/integrations", h.ListIntegrationsForProvider)

	w := doRequest(r, http.MethodGet, "/provider-definitions/outlook/integrations", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto integrationSummaryListDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dto.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1 (only outlook's integration)", len(dto.Items))
	}
	item := dto.Items[0]
	if item.ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", item.ProviderSlug, "outlook")
	}
	if item.Name != "Outlook" {
		t.Errorf("name = %q, want %q", item.Name, "Outlook")
	}
	if item.AuthScheme != "oauth2" {
		t.Errorf("authScheme = %q, want %q", item.AuthScheme, "oauth2")
	}
	if item.ID == "" {
		t.Error("id is empty, want the integration's id")
	}
}

func TestListIntegrationsForProvider_Returns404ForAnUnknownSlug(t *testing.T) {
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)
	r := chi.NewRouter()
	r.Get("/provider-definitions/{slug}/integrations", h.ListIntegrationsForProvider)

	w := doRequest(r, http.MethodGet, "/provider-definitions/does-not-exist/integrations", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

// TestListIntegrationsForProvider_ResponseBodyNeverContainsTheClientSecret is
// the handler-level, JSON-bytes proof mirroring
// TestListIntegrationsForProvider_SummaryNeverSerializesTheClientSecret at
// the facade layer: the raw HTTP response body must never carry the client
// secret or a clientSecret-shaped field.
func TestListIntegrationsForProvider_ResponseBodyNeverContainsTheClientSecret(t *testing.T) {
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	const distinctiveSecret = "super-secret-oauth-client-secret-value"
	if _, err := facade.CreateIntegration(context.Background(), "outlook", "client-id", distinctiveSecret); err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)
	r := chi.NewRouter()
	r.Get("/provider-definitions/{slug}/integrations", h.ListIntegrationsForProvider)

	w := doRequest(r, http.MethodGet, "/provider-definitions/outlook/integrations", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, distinctiveSecret) {
		t.Fatalf("response body %s contains the client secret", body)
	}
	if strings.Contains(body, "clientSecret") || strings.Contains(body, "client_secret") {
		t.Fatalf("response body %s carries a client-secret-shaped field at all", body)
	}
}
