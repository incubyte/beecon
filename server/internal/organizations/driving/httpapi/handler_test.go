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
	r.Get("/", h.List)
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

// TestList_Returns200WithAnEmptyPageWhenNoOrganizationsExist pins the empty
// case of the Slice 1/PD40 page shape: an items array (never omitted/null)
// and no nextCursor field at all (organizationsPageDTO's own
// `omitempty` — the last page never carries a cursor).
func TestList_Returns200WithAnEmptyPageWhenNoOrganizationsExist(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodGet, "/", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page organizationsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if page.Items == nil {
		t.Error("items = nil, want a (possibly empty) array")
	}
	if len(page.Items) != 0 {
		t.Errorf("items = %v, want empty", page.Items)
	}
	if page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want empty on a single, complete page", page.NextCursor)
	}
}

// TestList_Returns200WithEveryCreatedOrganizationNewestFirstAndTheOrganizationDTOShape
// pins the DTO shape List's page carries: each item mirrors
// toOrganizationDTO exactly (id/name/allowedRedirectUris/createdAt), and the
// page comes back newest-created first.
func TestList_Returns200WithEveryCreatedOrganizationNewestFirstAndTheOrganizationDTOShape(t *testing.T) {
	r := newTestRouter()
	doRequest(r, http.MethodPost, "/", `{"name":"First"}`)
	doRequest(r, http.MethodPost, "/", `{"name":"Second"}`)

	w := doRequest(r, http.MethodGet, "/", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page organizationsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(page.Items))
	}
	if page.Items[0].Name != "Second" || page.Items[1].Name != "First" {
		t.Fatalf("names = [%q, %q], want [%q, %q] (newest-created first)", page.Items[0].Name, page.Items[1].Name, "Second", "First")
	}
	first := page.Items[0]
	if !strings.HasPrefix(first.ID, "org_") {
		t.Errorf("id = %q, want it to start with %q", first.ID, "org_")
	}
	if first.AllowedRedirectUris == nil {
		t.Error("allowedRedirectUris = nil, want a (possibly empty) array")
	}
	if first.CreatedAt == "" {
		t.Error("createdAt = \"\", want a non-empty timestamp")
	}
}

// TestList_LoadMoreUsesNextCursorToFetchTheRemainingPage exercises the same
// cursor round trip an admin-ui "Load more" click performs: a first page
// requested with a small limit returns a nextCursor, and feeding that cursor
// back on the following request resumes exactly where the first page left
// off (Slice 1's headline AC).
func TestList_LoadMoreUsesNextCursorToFetchTheRemainingPage(t *testing.T) {
	r := newTestRouter()
	doRequest(r, http.MethodPost, "/", `{"name":"First"}`)
	doRequest(r, http.MethodPost, "/", `{"name":"Second"}`)
	doRequest(r, http.MethodPost, "/", `{"name":"Third"}`)

	firstPageResp := doRequest(r, http.MethodGet, "/?limit=2", "")
	if firstPageResp.Code != http.StatusOK {
		t.Fatalf("first page status = %d, want %d; body=%s", firstPageResp.Code, http.StatusOK, firstPageResp.Body.String())
	}
	var firstPage organizationsPageDTO
	if err := json.Unmarshal(firstPageResp.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage.Items) != 2 {
		t.Fatalf("first page has %d items, want 2", len(firstPage.Items))
	}
	if firstPage.NextCursor == "" {
		t.Fatal("first page's nextCursor is empty, want a cursor for the third organization")
	}

	secondPageResp := doRequest(r, http.MethodGet, "/?limit=2&cursor="+firstPage.NextCursor, "")

	if secondPageResp.Code != http.StatusOK {
		t.Fatalf("second page status = %d, want %d; body=%s", secondPageResp.Code, http.StatusOK, secondPageResp.Body.String())
	}
	var secondPage organizationsPageDTO
	if err := json.Unmarshal(secondPageResp.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if len(secondPage.Items) != 1 {
		t.Fatalf("second page has %d items, want 1 (the remaining organization)", len(secondPage.Items))
	}
	if secondPage.Items[0].Name != "First" {
		t.Errorf("second page's item name = %q, want %q", secondPage.Items[0].Name, "First")
	}
	if secondPage.NextCursor != "" {
		t.Errorf("second page's nextCursor = %q, want empty (it was the last page)", secondPage.NextCursor)
	}
	for _, item := range secondPage.Items {
		for _, seen := range firstPage.Items {
			if item.ID == seen.ID {
				t.Errorf("id %q appeared on both pages, want each organization exactly once", item.ID)
			}
		}
	}
}

// TestList_Returns422ForAMalformedCursorWithThePD5Envelope pins the
// query-param validation path List adds on top of Create/Get's own body
// validation.
func TestList_Returns422ForAMalformedCursorWithThePD5Envelope(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodGet, "/?cursor=not-valid-base64!!", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

// TestList_Returns422ForANonIntegerLimit pins parseIntQueryParam's own
// rejection path: a limit that doesn't parse as an integer at all (as
// opposed to one that's merely out of range, which normalizeListAllLimit
// silently clamps) is a validation error.
func TestList_Returns422ForANonIntegerLimit(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodGet, "/?limit=not-a-number", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}
