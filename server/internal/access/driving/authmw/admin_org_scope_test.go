package authmw_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access/driving/authmw"
	"beecon/internal/organizations"
)

// orgProbeHandler reports what AdminOrgScope put into the request context
// (Slice 1, FD3): the same probe shape org_test.go uses for OrgAuth, so a
// test can assert the {orgId} path param actually lands in context rather
// than just that the request passed through.
func orgProbeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := organizations.OrgIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"orgId": string(org), "hasOrg": ok})
	})
}

// newAdminOrgScopeRouter mounts orgProbeHandler behind AdminOrgScope on a
// real chi router with an {orgId} path param, mirroring how app/router.go
// mounts the console's org-scoped routes under
// /api/v1/organizations/{orgId}/… (FD3) — a plain http.Handler wired
// directly (bypassing chi's URLParam) would never see the path param at all.
func newAdminOrgScopeRouter() chi.Router {
	r := chi.NewRouter()
	r.With(authmw.AdminOrgScope(testAdminKey)).Get("/organizations/{orgId}/probe", orgProbeHandler().ServeHTTP)
	return r
}

func doAdminOrgScopeRequest(r chi.Router, orgID, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/organizations/"+orgID+"/probe", nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminOrgScope_RejectsARequestWithNoAuthorizationHeader(t *testing.T) {
	w := doAdminOrgScopeRequest(newAdminOrgScopeRouter(), "org_1", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestAdminOrgScope_RejectsAWrongAdminKey(t *testing.T) {
	w := doAdminOrgScopeRequest(newAdminOrgScopeRouter(), "org_1", "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestAdminOrgScope_PassesThroughAndInjectsTheOrgIDFromThePathForAValidAdminKey
// is FD3's core guarantee: a handler behind AdminOrgScope reads the org from
// context exactly like every existing org-key-guarded handler does, so it
// can be reused verbatim under the admin-key console mount.
func TestAdminOrgScope_PassesThroughAndInjectsTheOrgIDFromThePathForAValidAdminKey(t *testing.T) {
	w := doAdminOrgScopeRequest(newAdminOrgScopeRouter(), "org_42", "Bearer "+testAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		OrgID  string `json:"orgId"`
		HasOrg bool   `json:"hasOrg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode probe response: %v; body=%s", err, w.Body.String())
	}
	if !body.HasOrg {
		t.Fatal("expected OrgIDFromContext to report ok=true after AdminOrgScope passed the request through")
	}
	if body.OrgID != "org_42" {
		t.Errorf("orgId in context = %q, want %q (from the {orgId} path param)", body.OrgID, "org_42")
	}
}

// TestAdminOrgScope_DifferentPathOrgIdsProduceDifferentContextOrgs guards
// against a hardcoded or stale org id leaking across requests — each
// request's own {orgId} path segment must be the one injected.
func TestAdminOrgScope_DifferentPathOrgIdsProduceDifferentContextOrgs(t *testing.T) {
	router := newAdminOrgScopeRouter()

	firstResp := doAdminOrgScopeRequest(router, "org_a", "Bearer "+testAdminKey)
	secondResp := doAdminOrgScopeRequest(router, "org_b", "Bearer "+testAdminKey)

	var first, second struct {
		OrgID string `json:"orgId"`
	}
	if err := json.Unmarshal(firstResp.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(secondResp.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if first.OrgID != "org_a" {
		t.Errorf("first request's orgId = %q, want %q", first.OrgID, "org_a")
	}
	if second.OrgID != "org_b" {
		t.Errorf("second request's orgId = %q, want %q", second.OrgID, "org_b")
	}
}

// --- GET /admin/verify (FD3): a thin route mounted behind the unchanged
// AdminAuth, guarding VerifyAdminKey itself. ---

func newVerifyAdminKeyRouter() chi.Router {
	r := chi.NewRouter()
	r.With(authmw.AdminAuth(testAdminKey)).Get("/admin/verify", authmw.VerifyAdminKey)
	return r
}

func doVerifyRequest(r chi.Router, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/admin/verify", nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestVerifyAdminKey_Returns204ForAValidAdminKey(t *testing.T) {
	w := doVerifyRequest(newVerifyAdminKeyRouter(), "Bearer "+testAdminKey)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty for a 204", w.Body.String())
	}
}

func TestVerifyAdminKey_Returns401ForAMissingAdminKey(t *testing.T) {
	w := doVerifyRequest(newVerifyAdminKeyRouter(), "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestVerifyAdminKey_Returns401ForAWrongAdminKey(t *testing.T) {
	w := doVerifyRequest(newVerifyAdminKeyRouter(), "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
