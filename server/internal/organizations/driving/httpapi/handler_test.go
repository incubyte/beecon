// Package httpapi (in-package test) exercises the handlers through an actual
// chi router mounted with the same route patterns app/router.go uses
// ("POST /", "GET /{orgId}"), backed by the driven/memory facade, so
// chi.URLParam resolution and the PD5 error envelope are exercised exactly as
// production wires them.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	memory "beecon/internal/organizations/driven/memory"
)

func newTestRouter() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/{orgId}", h.Get)
	return r
}

func doRequest(r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type wireErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) wireErrorEnvelope {
	t.Helper()
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("response body is not the PD5 error envelope: %v\nbody: %s", err, w.Body.String())
	}
	return env
}

func TestCreate_Returns201WithTheOrganizationDTO(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodPost, "/", `{"name":"Acme"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "org_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "org_")
	}
	if dto.Name != "Acme" {
		t.Errorf("name = %q, want %q", dto.Name, "Acme")
	}
	if dto.CreatedAt == "" {
		t.Errorf("createdAt = %q, want a non-empty timestamp", dto.CreatedAt)
	}
}

func TestCreate_Returns422ForAnEmptyNameWithThePD5Envelope(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodPost, "/", `{"name":""}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
	if env.Error.Details["field"] != "name" {
		t.Errorf("error.details.field = %v, want %q", env.Error.Details["field"], "name")
	}
}

func TestCreate_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodPost, "/", `{"name":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestGet_Returns200ForAnOrganizationCreatedEarlier(t *testing.T) {
	r := newTestRouter()
	created := doRequest(r, http.MethodPost, "/", `{"name":"Acme"}`)
	var createdDTO organizationDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequest(r, http.MethodGet, "/"+createdDTO.ID, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.ID != createdDTO.ID {
		t.Errorf("id = %q, want %q", dto.ID, createdDTO.ID)
	}
	if dto.Name != "Acme" {
		t.Errorf("name = %q, want %q", dto.Name, "Acme")
	}
}

func TestGet_Returns404ForAnUnknownIDWithThePD5NotFoundEnvelope(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodGet, "/org_missing", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	wantBody := `{"error":{"code":"not_found","message":"organization not found"}}` + "\n"
	if got := w.Body.String(); got != wantBody {
		t.Errorf("body = %s, want %s", got, wantBody)
	}
}
