// provider_definitions_handler_test.go exercises the catalog module's
// installation-wide, operator-only provider-definitions handlers (PD40, Slice
// 6): ListProviderDefinitions and GetProviderDefinition. Reuses
// fakeDefinitions/doRequest/decodeError from handler_test.go (same package).
// These handlers read no organization from context at all — unlike every
// other handler in this file — so tests exercise them with a plain
// doRequest, never doRequestAsOrg; the admin-key guard itself is proven at
// the router level (internal/app/provider_definitions_route_admin_guard_test.go),
// since AdminAuth is middleware this package's own router-less tests never
// mount.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
)

func newProviderDefinitionsTestRouter(t *testing.T) chi.Router {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/provider-definitions", h.ListProviderDefinitions)
	r.Get("/provider-definitions/{slug}", h.GetProviderDefinition)
	return r
}

func TestListProviderDefinitions_Returns200WithTheProviderDefinitionsPageShape(t *testing.T) {
	r := newProviderDefinitionsTestRouter(t)

	w := doRequest(r, http.MethodGet, "/provider-definitions", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page providerDefinitionsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if item.Slug != "outlook" || item.Name != "Outlook" {
		t.Errorf("slug/name = %q/%q, want %q/%q", item.Slug, item.Name, "outlook", "Outlook")
	}
	if item.AuthScheme != "oauth2" {
		t.Errorf("authScheme = %q, want %q", item.AuthScheme, "oauth2")
	}
	if item.FormatVersion != 1 {
		t.Errorf("formatVersion = %d, want 1", item.FormatVersion)
	}
}

func TestListProviderDefinitions_Returns422ForAnInvalidCursor(t *testing.T) {
	r := newProviderDefinitionsTestRouter(t)

	w := doRequest(r, http.MethodGet, "/provider-definitions?cursor=not-valid-base64!!", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestGetProviderDefinition_Returns200WithTheFullBundle(t *testing.T) {
	r := newProviderDefinitionsTestRouter(t)

	w := doRequest(r, http.MethodGet, "/provider-definitions/outlook", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto providerDefinitionDetailDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Slug != "outlook" {
		t.Errorf("slug = %q, want %q", dto.Slug, "outlook")
	}
	if dto.FormatVersion != 1 {
		t.Errorf("formatVersion = %d, want 1", dto.FormatVersion)
	}
	if dto.Bundle["authScheme"] != "oauth2" {
		t.Errorf("bundle.authScheme = %v, want %q", dto.Bundle["authScheme"], "oauth2")
	}
	if _, ok := dto.Bundle["oauth"]; !ok {
		t.Error("bundle is missing an \"oauth\" block")
	}
}

func TestGetProviderDefinition_Returns404ForAnUnknownSlug(t *testing.T) {
	r := newProviderDefinitionsTestRouter(t)

	w := doRequest(r, http.MethodGet, "/provider-definitions/does-not-exist", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}
