// Package httpapi (in-package test, same package as handler_test.go — reuses
// its wireErrorEnvelope/decodeError helpers). CreateUser and GetUser read the
// organization only from the request context (injected by OrgAuth in
// production); these tests set that context directly with
// organizations.WithOrgID rather than routing through real authmw, since
// organizations does not depend on access (BOUNDARIES.md).
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

	"beecon/internal/organizations"
)

func newUsersTestRouter() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/users", h.CreateUser)
	r.Get("/users/{userId}", h.GetUser)
	// The admin console's org-scoped mount (Slice 4, PD40): org comes from
	// context exactly like the org-key routes above — this test router
	// injects it directly (doRequestAsOrg), the same shortcut this file's own
	// header explains, standing in for the real AdminOrgScope/InjectOrgFromPath
	// middleware chain.
	r.Get("/organizations/{orgId}/users", h.ListUsersByOrg)
	return r
}

// doRequestAsOrg simulates what OrgAuth does once a key has verified: inject
// the authenticated OrgID into the request context. An empty org simulates a
// request that never passed through org authentication at all.
func doRequestAsOrg(r chi.Router, method, path string, org organizations.OrgID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if org != "" {
		req = req.WithContext(organizations.WithOrgID(req.Context(), org))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateUser_Returns201WithTheUserDTOShapeIncludingTheOptionalExternalID(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":"Ada Lovelace","externalId":"ext-1"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto userDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "user_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "user_")
	}
	if dto.Name != "Ada Lovelace" {
		t.Errorf("name = %q, want %q", dto.Name, "Ada Lovelace")
	}
	if dto.ExternalID != "ext-1" {
		t.Errorf("externalId = %q, want %q", dto.ExternalID, "ext-1")
	}
	if dto.CreatedAt == "" {
		t.Error("createdAt is empty, want a non-empty timestamp")
	}
}

func TestCreateUser_ExternalIDIsOptionalAndDefaultsToEmptyString(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":"Ada Lovelace"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto userDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.ExternalID != "" {
		t.Errorf("externalId = %q, want empty string when omitted from the request", dto.ExternalID)
	}
}

func TestCreateUser_Returns422ForAnEmptyNameWithThePD5Envelope(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":""}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestCreateUser_Returns401WhenTheRequestNeverPassedThroughOrgAuthentication(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/users", "", `{"name":"Ada Lovelace"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestGetUser_Returns200ForAUserFetchedFromItsOwnOrg(t *testing.T) {
	r := newUsersTestRouter()
	created := doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":"Ada Lovelace"}`)
	var createdDTO userDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/users/"+createdDTO.ID, "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto userDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.ID != createdDTO.ID {
		t.Errorf("id = %q, want %q", dto.ID, createdDTO.ID)
	}
}

func TestGetUser_Returns404ForAUserBelongingToAnotherOrgWithNoExistenceLeak(t *testing.T) {
	r := newUsersTestRouter()
	created := doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":"Ada Lovelace"}`)
	var createdDTO userDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/users/"+createdDTO.ID, "org_2", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestGetUser_Returns404ForAnUnknownIDWithThePD5NotFoundEnvelope(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/users/user_missing", "org_1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// --- ListUsersByOrg (Slice 4, PD40): the Admin UI's new list-users-per-org
// read, mounted behind the admin key with org injected from the path in
// production (AdminOrgScope/InjectOrgFromPath) — these tests inject org via
// context directly, the same doRequestAsOrg shortcut CreateUser/GetUser's own
// tests above use, since the handler itself reads org only from context. ---

func TestListUsersByOrg_Returns200WithAPageOfUsersBelongingToTheOrg(t *testing.T) {
	r := newUsersTestRouter()
	doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":"Ada Lovelace"}`)
	doRequestAsOrg(r, http.MethodPost, "/users", "org_1", `{"name":"Grace Hopper"}`)

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/users", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page usersPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(page.Items))
	}
}

func TestListUsersByOrg_NeverIncludesAnotherOrgsUsers(t *testing.T) {
	r := newUsersTestRouter()
	doRequestAsOrg(r, http.MethodPost, "/users", "org_a", `{"name":"Ada Lovelace"}`)
	doRequestAsOrg(r, http.MethodPost, "/users", "org_b", `{"name":"Grace Hopper"}`)

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_a/users", "org_a", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page usersPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1 (org B's user must not leak into org A's page)", len(page.Items))
	}
	if page.Items[0].Name != "Ada Lovelace" {
		t.Errorf("item name = %q, want %q", page.Items[0].Name, "Ada Lovelace")
	}
}

func TestListUsersByOrg_ReturnsAnEmptyPageWhenTheOrgHasNoUsersYet(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_empty/users", "org_empty", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page usersPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(page.Items))
	}
	if page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want empty for a single, unfull page", page.NextCursor)
	}
}

func TestListUsersByOrg_Returns401WhenTheRequestNeverPassedThroughOrgContextInjection(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/users", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestListUsersByOrg_RejectsAMalformedCursorWithThePD5ValidationEnvelope(t *testing.T) {
	r := newUsersTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/users?cursor=not-valid!!", "org_1", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}
