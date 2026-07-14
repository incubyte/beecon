// Package httpapi (in-package test) exercises the connections handlers
// through an actual chi router mounted with the same route patterns
// app/router.go uses, backed by the driven/memory facade and hand-written
// fakes for the narrow cross-module reader ports.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

type fakeOrgReader struct {
	orgs map[organizations.OrgID]organizations.Organization
}

func (f fakeOrgReader) Get(_ context.Context, id organizations.OrgID) (organizations.Organization, error) {
	org, ok := f.orgs[id]
	if !ok {
		return organizations.Organization{}, organizations.ErrNotFound()
	}
	return org, nil
}

type fakeUserReader struct {
	users map[organizations.UserID]organizations.User
}

func (f fakeUserReader) GetUser(_ context.Context, org organizations.OrgID, id organizations.UserID) (organizations.User, error) {
	user, ok := f.users[id]
	if !ok || user.OrgID != org {
		return organizations.User{}, organizations.ErrUserNotFound()
	}
	return user, nil
}

type fakeIntegrationReader struct {
	integrations map[catalog.IntegrationID]catalog.Integration
}

func (f fakeIntegrationReader) GetIntegration(_ context.Context, id catalog.IntegrationID) (catalog.Integration, error) {
	integration, ok := f.integrations[id]
	if !ok {
		return catalog.Integration{}, catalog.ErrIntegrationNotFound()
	}
	return integration, nil
}

// GetVisibleIntegration is governance-blind in this fake — it always
// answers exactly like GetIntegration, so every existing test built before
// Slice 5 keeps its original behavior unchanged.
func (f fakeIntegrationReader) GetVisibleIntegration(ctx context.Context, _ organizations.OrgID, id catalog.IntegrationID) (catalog.Integration, error) {
	return f.GetIntegration(ctx, id)
}

const (
	testOrg         = organizations.OrgID("org_1")
	otherOrg        = organizations.OrgID("org_2")
	testUser        = organizations.UserID("user_1")
	testIntegration = catalog.IntegrationID("intg_1")
	allowedRedirect = "https://consumer.example.com/callback"
)

func newTestRouter(t *testing.T) chi.Router {
	t.Helper()
	orgs := map[organizations.OrgID]organizations.Organization{
		testOrg:  {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
		otherOrg: {ID: otherOrg, Name: "Other", AllowedRedirectURIs: []string{allowedRedirect}},
	}
	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Organizations: fakeOrgReader{orgs: orgs},
		Users: fakeUserReader{users: map[organizations.UserID]organizations.User{
			testUser: {ID: testUser, OrgID: testOrg, Name: "Ada"},
		}},
		Integrations: fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{
			testIntegration: {ID: testIntegration, ProviderSlug: "outlook", ClientID: "cid", ClientSecret: "csecret"},
		}},
	})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/initiate", h.Initiate)
	r.Get("/", h.List)
	r.Get("/{connectionId}", h.Get)
	r.Post("/{connectionId}/disable", h.Disable)
	r.Delete("/{connectionId}", h.Delete)
	r.Post("/{connectionId}/reconnect", h.Reconnect)
	return r
}

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

func initiateBody(userID, integrationID, redirectURI string) string {
	return `{"userId":"` + userID + `","integrationId":"` + integrationID + `","redirectUri":"` + redirectURI + `"}`
}

func TestInitiate_Returns201WithIDStatusAndRedirectURL(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto initiatedConnectionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "conn_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "conn_")
	}
	if dto.Status != "INITIATED" {
		t.Errorf("status = %q, want %q", dto.Status, "INITIATED")
	}
	if !strings.Contains(dto.RedirectURL, "/connect/") {
		t.Errorf("redirectUrl = %q, want it to point at Beecon's own connect page", dto.RedirectURL)
	}
}

func TestInitiate_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", "", initiateBody(string(testUser), string(testIntegration), allowedRedirect))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestInitiate_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, `{"userId":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
	if issue, _ := env.Error.Details["issue"].(string); issue != "request body must be valid JSON" {
		t.Errorf("error.details.issue = %q, want %q", issue, "request body must be valid JSON")
	}
	if field, _ := env.Error.Details["field"].(string); field == "redirectUri" {
		t.Errorf("error.details.field = %q; a malformed body must not be reported as a redirectUri-not-allowed error", field)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "not in organization's allowed redirect uris") {
		t.Errorf("body %s claims the redirectUri is not allowed, but the request never made it past JSON decoding", w.Body.String())
	}
}

func TestInitiate_Returns422WhenRedirectURINotAllowed(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), "https://evil.example.com/callback"))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestInitiate_Returns404ForAnUnknownUserID(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody("user_missing", string(testIntegration), allowedRedirect))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestInitiate_Returns404ForAUserFromAnotherOrganization(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", otherOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestInitiate_Returns404ForAnUnknownIntegrationID(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), "intg_missing", allowedRedirect))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestGet_Returns200ForAConnectionInItsOwnOrg(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/"+createdDTO.ID, testOrg, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto connectionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.ID != createdDTO.ID {
		t.Errorf("id = %q, want %q", dto.ID, createdDTO.ID)
	}
	if dto.Status != "INITIATED" {
		t.Errorf("status = %q, want %q", dto.Status, "INITIATED")
	}
	if dto.ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", dto.ProviderSlug, "outlook")
	}
	if dto.UserID != string(testUser) {
		t.Errorf("userId = %q, want %q", dto.UserID, testUser)
	}
}

func TestGet_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/conn_1", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestGet_Returns404ForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/"+createdDTO.ID, otherOrg, "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestGet_Returns404ForAnUnknownID(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/conn_missing", testOrg, "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestGet_ResponseOmitsTheAccountFieldInThisSlice: account metadata arrives
// with Slice 4's OAuth callback; Phase 1's Get response for an INITIATED
// connection carries no "account" key at all.
func TestGet_ResponseOmitsTheAccountFieldInThisSlice(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/"+createdDTO.ID, testOrg, "")

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if _, present := raw["account"]; present {
		t.Errorf("response %s carries an \"account\" key; Phase 1 connections have no account metadata yet", w.Body.String())
	}
}

// TestGet_ResponseNeverIncludesTheConnectToken: the single-use connect token
// minted at Initiate must never be echoed back through Get.
func TestGet_ResponseNeverIncludesTheConnectToken(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	token := strings.TrimPrefix(createdDTO.RedirectURL, "http://localhost:8080/connect/")

	w := doRequestAsOrg(r, http.MethodGet, "/"+createdDTO.ID, testOrg, "")

	if strings.Contains(w.Body.String(), token) {
		t.Errorf("Get response %s contains the connect token %q — it must never be echoed back", w.Body.String(), token)
	}
}

// --- List (Slice 4, AC1) ---

func TestList_Returns200WithItemsScopedToTheCallersOrganization(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	doRequestAsOrg(r, http.MethodPost, "/initiate", otherOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))

	w := doRequestAsOrg(r, http.MethodGet, "/", testOrg, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page connectionsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Items) != 1 || page.Items[0].ID != createdDTO.ID {
		t.Fatalf("Items = %+v, want exactly the caller's own organization's connection %q", page.Items, createdDTO.ID)
	}
}

func TestList_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestList_Returns422ForAMalformedCursor(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/?cursor=not-valid-base64!!", testOrg, "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

// --- Disable (Slice 4, AC2, AC11) ---

func TestDisable_Returns200WithDisconnectedStatus(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/disable", testOrg, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto connectionStatusDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.ID != createdDTO.ID {
		t.Errorf("id = %q, want %q", dto.ID, createdDTO.ID)
	}
	if dto.Status != "DISCONNECTED" {
		t.Errorf("status = %q, want %q", dto.Status, "DISCONNECTED")
	}
}

func TestDisable_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/conn_1/disable", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestDisable_Returns404ForAConnectionBelongingToAnotherOrganization is AC11.
func TestDisable_Returns404ForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/disable", otherOrg, "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

// --- Delete (Slice 4, AC3, AC11) ---

func TestDelete_Returns204AndSubsequentGetIsNotFound(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodDelete, "/"+createdDTO.ID, testOrg, "")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	after := doRequestAsOrg(r, http.MethodGet, "/"+createdDTO.ID, testOrg, "")
	if after.Code != http.StatusNotFound {
		t.Fatalf("get-after-delete status = %d, want %d; body=%s", after.Code, http.StatusNotFound, after.Body.String())
	}
}

func TestDelete_Returns404ForAnUnknownID(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodDelete, "/conn_missing", testOrg, "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestDelete_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodDelete, "/conn_1", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestDelete_Returns404ForAConnectionBelongingToAnotherOrganization is AC11.
func TestDelete_Returns404ForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodDelete, "/"+createdDTO.ID, otherOrg, "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	stillThere := doRequestAsOrg(r, http.MethodGet, "/"+createdDTO.ID, testOrg, "")
	if stillThere.Code != http.StatusOK {
		t.Fatalf("connection was deleted via another organization's key; get status = %d, want %d", stillThere.Code, http.StatusOK)
	}
}

// --- Reconnect (Slice 4, AC4, AC11) ---

func reconnectBody(redirectURI string) string {
	return `{"redirectUri":"` + redirectURI + `"}`
}

func TestReconnect_Returns201WithTheSameIDAndAFreshRedirectURL(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	// A connection can only be reconnected once it is no longer INITIATED —
	// disable it first (Disable's own transition, already proven above).
	if disableResp := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/disable", testOrg, ""); disableResp.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d; body=%s", disableResp.Code, http.StatusOK, disableResp.Body.String())
	}

	w := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/reconnect", testOrg, reconnectBody(allowedRedirect))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto initiatedConnectionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.ID != createdDTO.ID {
		t.Errorf("id = %q, want the same stable id %q", dto.ID, createdDTO.ID)
	}
	if dto.RedirectURL == createdDTO.RedirectURL {
		t.Errorf("redirectUrl = %q, want a fresh redirectUrl distinct from the original", dto.RedirectURL)
	}
	if !strings.Contains(dto.RedirectURL, "/connect/") {
		t.Errorf("redirectUrl = %q, want it to point at Beecon's own connect page", dto.RedirectURL)
	}
}

func TestReconnect_Returns422ForAConnectionStillInitiated(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/reconnect", testOrg, reconnectBody(allowedRedirect))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "reconnect_not_allowed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "reconnect_not_allowed")
	}
}

func TestReconnect_Returns422WhenRedirectURINotAllowed(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/disable", testOrg, "")

	w := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/reconnect", testOrg, reconnectBody("https://evil.example.com/callback"))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

func TestReconnect_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter(t)

	w := doRequestAsOrg(r, http.MethodPost, "/conn_1/reconnect", "", reconnectBody(allowedRedirect))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestReconnect_Returns404ForAConnectionBelongingToAnotherOrganization is
// AC11.
func TestReconnect_Returns404ForAConnectionBelongingToAnotherOrganization(t *testing.T) {
	r := newTestRouter(t)
	created := doRequestAsOrg(r, http.MethodPost, "/initiate", testOrg, initiateBody(string(testUser), string(testIntegration), allowedRedirect))
	var createdDTO initiatedConnectionDTO
	if err := json.Unmarshal(created.Body.Bytes(), &createdDTO); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/disable", testOrg, "")

	w := doRequestAsOrg(r, http.MethodPost, "/"+createdDTO.ID+"/reconnect", otherOrg, reconnectBody(allowedRedirect))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}
