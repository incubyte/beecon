// tool_id_dto_test.go (package httpapi, reuses fakeDefinitions/minimalSchema/
// doRequestAsOrg/decodeError/newToolsTestRouter from handler_test.go) pins
// the Phase 5 registry sub-phase's Slice 5 wire shape: GET /api/v1/tools and
// GET /api/v1/tools/{slug} carry the tool's id field alongside slug, empty
// for a tool that has never been through the registry, and a detail lookup
// by tool_ id renders exactly the same JSON a slug lookup does.
package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
)

const dtoTestToolID = "tool_wire_shape_check"

// fakeDefinitionsWithAnIDedTool is fakeDefinitions' outlook provider plus one
// tool already carrying a real tool_ id — standing in for a tool served from
// an activated registry bundle (the DTO-mapping concern this file tests does
// not depend on how the id got there).
func fakeDefinitionsWithAnIDedTool() []catalog.ProviderDefinition {
	defs := fakeDefinitions()
	defs[0].Tools = []catalog.ProviderTool{
		{ID: dtoTestToolID, Slug: "outlook-get-message", Name: "Get email message", Description: "Retrieves a message by id.", InputSchema: minimalSchema(), OutputSchema: minimalSchema()},
	}
	return defs
}

func newIDedToolsTestRouter(t *testing.T) chi.Router {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitionsWithAnIDedTool()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(nil)
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/tools", h.ListTools)
	r.Get("/tools/{slug}", h.GetTool)
	return r
}

func TestListTools_Returns200WithEachItemsRealToolIDAlongsideItsSlug(t *testing.T) {
	r := newIDedToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page toolsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].ID != dtoTestToolID {
		t.Errorf("id = %q, want %q", page.Items[0].ID, dtoTestToolID)
	}
	if page.Items[0].Slug != "outlook-get-message" {
		t.Errorf("slug = %q, want id surfaced alongside slug, not instead of it", page.Items[0].Slug)
	}
}

func TestListTools_ReturnsAnEmptyIDForAnEmbeddedPreRegistryTool(t *testing.T) {
	r := newToolsTestRouter(t) // fakeDefinitionsWithTools' tools carry no id

	w := doRequestAsOrg(r, http.MethodGet, "/tools?providerSlug=outlook", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page toolsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) == 0 {
		t.Fatal("expected at least one tool")
	}
	if page.Items[0].ID != "" {
		t.Errorf("id = %q, want empty for a tool never through the registry", page.Items[0].ID)
	}
}

func TestGetTool_Returns200WithTheToolsRealToolIDAlongsideItsSlug(t *testing.T) {
	r := newIDedToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools/outlook-get-message", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto toolSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.ID != dtoTestToolID {
		t.Errorf("id = %q, want %q", dto.ID, dtoTestToolID)
	}
}

// TestGetTool_ByToolIDReturnsTheIdenticalBodyASlugLookupReturns is AC6 at the
// wire level: addressing the same route by tool_ id instead of slug renders
// byte-identical JSON.
func TestGetTool_ByToolIDReturnsTheIdenticalBodyASlugLookupReturns(t *testing.T) {
	r := newIDedToolsTestRouter(t)

	bySlug := doRequestAsOrg(r, http.MethodGet, "/tools/outlook-get-message", "org_1", "")
	byID := doRequestAsOrg(r, http.MethodGet, "/tools/"+dtoTestToolID, "org_1", "")

	if bySlug.Code != http.StatusOK || byID.Code != http.StatusOK {
		t.Fatalf("status by slug = %d, by tool_ id = %d, want both 200", bySlug.Code, byID.Code)
	}
	if bySlug.Body.String() != byID.Body.String() {
		t.Errorf("GET by slug (%s) differs from GET by tool_ id (%s), want the identical tool", bySlug.Body.String(), byID.Body.String())
	}
}

func TestGetTool_Returns404ForAnUnknownToolID(t *testing.T) {
	r := newIDedToolsTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/tools/tool_never_minted", "org_1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}
