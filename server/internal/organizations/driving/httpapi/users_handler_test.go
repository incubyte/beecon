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
