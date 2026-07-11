// Package httpapi (in-package test) exercises the catalog handlers through an
// actual chi router mounted with the same route patterns app/router.go uses,
// backed by the driven/memory facade with a fake provider definition so tests
// don't depend on the real embedded outlook.yaml.
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

func newTestRouter(t *testing.T) chi.Router {
	t.Helper()
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	return r
}

func doRequest(r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doRequestAsOrg mirrors organizations/driving/httpapi's helper: List reads
// the organization only from the request context, injected by OrgAuth in
// production.
func doRequestAsOrg(r chi.Router, method, path string, org organizations.OrgID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if org != "" {
		req = req.WithContext(organizations.WithOrgID(req.Context(), org))
	}
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

func TestCreate_Returns201WithTheIntegrationSummaryDTO(t *testing.T) {
	r := newTestRouter(t)

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto integrationSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "intg_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "intg_")
	}
	if dto.ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", dto.ProviderSlug, "outlook")
	}
	if dto.Name != "Outlook" {
		t.Errorf("name = %q, want %q", dto.Name, "Outlook")
	}
	if dto.AuthScheme != "oauth2" {
		t.Errorf("authScheme = %q, want %q", dto.AuthScheme, "oauth2")
	}
}

// TestCreate_ResponseBodyNeverContainsTheClientSecret is AC4, asserted at the
// wire level: the raw HTTP response bytes must never contain the secret
// string supplied at creation.
func TestCreate_ResponseBodyNeverContainsTheClientSecret(t *testing.T) {
	r := newTestRouter(t)
	const secret = "super-secret-oauth-client-secret"

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"`+secret+`"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("response body contains the client secret: %s", w.Body.String())
	}
}

func TestCreate_Returns422ForAnUnknownProviderSlug(t *testing.T) {
	r := newTestRouter(t)

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":"does-not-exist","clientId":"cid","clientSecret":"csecret"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestCreate_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newTestRouter(t)

	w := doRequest(r, http.MethodPost, "/", `{"providerSlug":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestList_Returns200WithEveryCreatedIntegrationSummary(t *testing.T) {
	r := newTestRouter(t)
	doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	w := doRequestAsOrg(r, http.MethodGet, "/", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dtos []integrationSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dtos); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dtos) != 1 {
		t.Fatalf("len(dtos) = %d, want 1", len(dtos))
	}
	if dtos[0].ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", dtos[0].ProviderSlug, "outlook")
	}
}

// TestList_IsIdenticalRegardlessOfWhichOrganizationAsks is PD7: Phase 1
// integrations are installation-level, visible to every organization.
func TestList_IsIdenticalRegardlessOfWhichOrganizationAsks(t *testing.T) {
	r := newTestRouter(t)
	doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	wA := doRequestAsOrg(r, http.MethodGet, "/", "org_a", "")
	wB := doRequestAsOrg(r, http.MethodGet, "/", "org_b", "")

	if wA.Body.String() != wB.Body.String() {
		t.Errorf("org A's list (%s) differs from org B's list (%s); PD7 says every organization sees the same installation-wide list", wA.Body.String(), wB.Body.String())
	}
}

func TestList_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
