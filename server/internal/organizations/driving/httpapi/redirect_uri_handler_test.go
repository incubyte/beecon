// Package httpapi (in-package test, same package as handler_test.go — reuses
// its doRequest/decodeError helpers). Covers PATCH
// /api/v1/organizations/{orgId} (AC5: installation admin sets an
// organization's allowed redirect URIs).
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	memory "beecon/internal/organizations/driven/memory"
)

// newTestRouterWithPatch mirrors newTestRouter (handler_test.go) but also
// mounts the PATCH route app/router.go wires for allowed-redirect-uri
// updates.
func newTestRouterWithPatch() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/{orgId}", h.Get)
	r.Patch("/{orgId}", h.UpdateAllowedRedirectURIs)
	return r
}

func TestUpdateAllowedRedirectURIs_Returns200AndReplacesTheAllowList(t *testing.T) {
	r := newTestRouterWithPatch()
	created := doRequest(r, http.MethodPost, "/", `{"name":"Acme"}`)
	var createdDTO organizationDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequest(r, http.MethodPatch, "/"+createdDTO.ID, `{"allowedRedirectUris":["https://consumer.example.com/callback"]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(dto.AllowedRedirectUris) != 1 || dto.AllowedRedirectUris[0] != "https://consumer.example.com/callback" {
		t.Errorf("allowedRedirectUris = %v, want [%q]", dto.AllowedRedirectUris, "https://consumer.example.com/callback")
	}
}

func TestUpdateAllowedRedirectURIs_Returns404ForAnUnknownOrgID(t *testing.T) {
	r := newTestRouterWithPatch()

	w := doRequest(r, http.MethodPatch, "/org_missing", `{"allowedRedirectUris":["https://consumer.example.com/callback"]}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestUpdateAllowedRedirectURIs_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newTestRouterWithPatch()
	created := doRequest(r, http.MethodPost, "/", `{"name":"Acme"}`)
	var createdDTO organizationDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequest(r, http.MethodPatch, "/"+createdDTO.ID, `{"allowedRedirectUris":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}
