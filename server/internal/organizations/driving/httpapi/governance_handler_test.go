// governance_handler_test.go (in-package test, same package as
// handler_test.go/users_handler_test.go — reuses their wireErrorEnvelope,
// decodeError, and doRequestAsOrg helpers). GetGovernance/UpdateGovernance
// read the organization only from request context, injected in production
// by the admin console's org-scoped mount (AdminOrgScope/InjectOrgFromPath)
// — these tests inject that context directly, the same shortcut
// users_handler_test.go's own header documents.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	memory "beecon/internal/organizations/driven/memory"
)

func newGovernanceTestRouter() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/organizations/{orgId}/governance", h.GetGovernance)
	r.Put("/organizations/{orgId}/governance", h.UpdateGovernance)
	return r
}

func TestGetGovernance_Returns401WhenNoOrgInContext(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestGetGovernance_ReturnsTheContinuityPreservingDefaultForAnUnconfiguredOrg
// is PD42's continuity guarantee at the HTTP boundary: an org that has never
// had its governance set sees allowList:null, empty hidden/featured, and the
// platform's default cap.
func TestGetGovernance_ReturnsTheContinuityPreservingDefaultForAnUnconfiguredOrg(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto governanceDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.AllowList != nil {
		t.Errorf("allowList = %v, want JSON null", dto.AllowList)
	}
	if len(dto.Hidden) != 0 {
		t.Errorf("hidden = %v, want empty", dto.Hidden)
	}
	if len(dto.Onboarding.Featured) != 0 {
		t.Errorf("onboarding.featured = %v, want empty", dto.Onboarding.Featured)
	}
	if dto.Onboarding.Cap != 8 {
		t.Errorf("onboarding.cap = %d, want the platform default 8", dto.Onboarding.Cap)
	}
}

// TestGetGovernance_NeverSerializesHiddenOrFeaturedAsJSONNull pins
// nonNilStrings: even an unconfigured org's empty slices must render as `[]`,
// matching what a PUT round-trip would send back — never `null`.
func TestGetGovernance_NeverSerializesHiddenOrFeaturedAsJSONNull(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `"hidden":null`) || strings.Contains(body, `"featured":null`) {
		t.Errorf("body = %s, want hidden/featured serialized as [] never null", body)
	}
}

func TestUpdateGovernance_Returns401WhenNoOrgInContext(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "", `{"allowList":null,"hidden":[],"onboarding":{"featured":[],"cap":8}}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestUpdateGovernance_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "org_1", `{"allowList":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

// TestUpdateGovernance_Returns422WhenFeaturedExceedsTheCap is AC7's
// validation half at the HTTP boundary.
func TestUpdateGovernance_Returns422WhenFeaturedExceedsTheCap(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "org_1",
		`{"allowList":null,"hidden":[],"onboarding":{"featured":["a","b","c"],"cap":2}}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

// TestUpdateGovernance_RoundTripsAnAllowListHiddenSetAndFeaturedOrderThroughGet
// is AC2/AC4/AC7 end to end at the handler layer: whatever UpdateGovernance
// persists, a subsequent GetGovernance must return exactly.
func TestUpdateGovernance_RoundTripsAnAllowListHiddenSetAndFeaturedOrderThroughGet(t *testing.T) {
	r := newGovernanceTestRouter()
	body := `{"allowList":["intg_1","intg_2"],"hidden":["intg_3"],"onboarding":{"featured":["intg_1"],"cap":5}}`

	putResp := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "org_1", body)

	if putResp.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d; body=%s", putResp.Code, http.StatusOK, putResp.Body.String())
	}

	getResp := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/governance", "org_1", "")
	var dto governanceDTO
	if err := json.Unmarshal(getResp.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode GET body: %v; body=%s", err, getResp.Body.String())
	}
	if dto.AllowList == nil || len(*dto.AllowList) != 2 {
		t.Fatalf("allowList = %v, want [intg_1 intg_2]", dto.AllowList)
	}
	if len(dto.Hidden) != 1 || dto.Hidden[0] != "intg_3" {
		t.Errorf("hidden = %v, want [intg_3]", dto.Hidden)
	}
	if len(dto.Onboarding.Featured) != 1 || dto.Onboarding.Featured[0] != "intg_1" {
		t.Errorf("onboarding.featured = %v, want [intg_1]", dto.Onboarding.Featured)
	}
	if dto.Onboarding.Cap != 5 {
		t.Errorf("onboarding.cap = %d, want 5", dto.Onboarding.Cap)
	}
}

// TestUpdateGovernance_AnAbsentAllowListFieldMeansInheritAllNotAnEmptyRestriction
// pins the tri-state PUT body contract (governance_dto.go): allowList absent
// (or JSON null) decodes to nil ("inherit all"), never coerced into an
// empty, everything-restricted allow-list.
func TestUpdateGovernance_AnAbsentAllowListFieldMeansInheritAllNotAnEmptyRestriction(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "org_1",
		`{"hidden":[],"onboarding":{"featured":[],"cap":8}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto governanceDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.AllowList != nil {
		t.Errorf("allowList = %v, want nil (absent field means inherit-all, PD42)", dto.AllowList)
	}
}

// TestUpdateGovernance_AnExplicitEmptyAllowListArrayRestrictsToNothing is the
// tri-state's other pole: `"allowList":[]` is a present, empty restriction —
// distinct from an absent/null field — and must round-trip as such.
func TestUpdateGovernance_AnExplicitEmptyAllowListArrayRestrictsToNothing(t *testing.T) {
	r := newGovernanceTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/governance", "org_1",
		`{"allowList":[],"hidden":[],"onboarding":{"featured":[],"cap":8}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto governanceDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.AllowList == nil {
		t.Fatal("allowList = nil, want a present (empty) array preserved distinctly from an absent field")
	}
	if len(*dto.AllowList) != 0 {
		t.Errorf("allowList = %v, want empty", *dto.AllowList)
	}
}

// TestGovernance_IsStrictlyOrgScopedAtTheHandlerLayer is Slice 5's isolation
// AC exercised through the HTTP handlers directly: writing one org's
// governance must never be observable through a different org's context.
func TestGovernance_IsStrictlyOrgScopedAtTheHandlerLayer(t *testing.T) {
	r := newGovernanceTestRouter()
	allowListA := `{"allowList":["intg_a_only"],"hidden":[],"onboarding":{"featured":[],"cap":8}}`

	putA := doRequestAsOrg(r, http.MethodPut, "/organizations/org_a/governance", "org_a", allowListA)
	if putA.Code != http.StatusOK {
		t.Fatalf("PUT org_a status = %d, want %d; body=%s", putA.Code, http.StatusOK, putA.Body.String())
	}

	getB := doRequestAsOrg(r, http.MethodGet, "/organizations/org_b/governance", "org_b", "")

	if getB.Code != http.StatusOK {
		t.Fatalf("GET org_b status = %d, want %d; body=%s", getB.Code, http.StatusOK, getB.Body.String())
	}
	var dtoB governanceDTO
	if err := json.Unmarshal(getB.Body.Bytes(), &dtoB); err != nil {
		t.Fatalf("decode org_b body: %v", err)
	}
	if dtoB.AllowList != nil {
		t.Errorf("org_b's allowList = %v, want nil — org_a's allow-list must never leak across", dtoB.AllowList)
	}
}
