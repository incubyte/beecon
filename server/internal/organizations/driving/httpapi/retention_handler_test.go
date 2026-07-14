// retention_handler_test.go (in-package test, same package as
// handler_test.go/users_handler_test.go/governance_handler_test.go — reuses
// their doRequestAsOrg/decodeError helpers). GetRetention/UpdateRetention
// read the organization only from request context, injected in production
// by the admin console's org-scoped mount — these tests inject that context
// directly, the same shortcut the sibling handler test files already
// document.
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

func newRetentionTestRouter() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/organizations/{orgId}/retention", h.GetRetention)
	r.Put("/organizations/{orgId}/retention", h.UpdateRetention)
	return r
}

func TestGetRetention_Returns401WhenNoOrgInContext(t *testing.T) {
	r := newRetentionTestRouter()

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/retention", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestGetRetention_AnUnconfiguredOrgSeesBothWindowsNullWithTheInstallationDefaultEchoed
// is AC1 at the HTTP boundary: an org that never set its own retention sees
// logDays/eventDays null ("inherit"), and installationDefaultDays names
// what that inherited default currently is.
func TestGetRetention_AnUnconfiguredOrgSeesBothWindowsNullWithTheInstallationDefaultEchoed(t *testing.T) {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer).WithInstallationDefaultRetentionDays(45)
	r := chi.NewRouter()
	r.Get("/organizations/{orgId}/retention", h.GetRetention)

	w := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/retention", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto retentionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.LogDays != nil {
		t.Errorf("logDays = %v, want null (inherit)", dto.LogDays)
	}
	if dto.EventDays != nil {
		t.Errorf("eventDays = %v, want null (inherit)", dto.EventDays)
	}
	if dto.InstallationDefaultDays != 45 {
		t.Errorf("installationDefaultDays = %d, want the configured 45", dto.InstallationDefaultDays)
	}
}

func TestUpdateRetention_Returns401WhenNoOrgInContext(t *testing.T) {
	r := newRetentionTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/retention", "", `{"logDays":14,"eventDays":null}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestUpdateRetention_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newRetentionTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/retention", "org_1", `{"logDays":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

// TestUpdateRetention_Returns422ForAWindowBelowTheMinimum is AC5 at the HTTP
// boundary.
func TestUpdateRetention_Returns422ForAWindowBelowTheMinimum(t *testing.T) {
	r := newRetentionTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/retention", "org_1", `{"logDays":-1}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

// TestUpdateRetention_RoundTripsBothWindowsThroughGet is AC1/AC4 end to end
// at the handler layer: whatever UpdateRetention persists, a subsequent
// GetRetention must return exactly, including 0 as a present (not null)
// unlimited value.
func TestUpdateRetention_RoundTripsBothWindowsThroughGet(t *testing.T) {
	r := newRetentionTestRouter()
	body := `{"logDays":14,"eventDays":0}`

	putResp := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/retention", "org_1", body)

	if putResp.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d; body=%s", putResp.Code, http.StatusOK, putResp.Body.String())
	}

	getResp := doRequestAsOrg(r, http.MethodGet, "/organizations/org_1/retention", "org_1", "")
	var dto retentionDTO
	if err := json.Unmarshal(getResp.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode GET body: %v; body=%s", err, getResp.Body.String())
	}
	if dto.LogDays == nil || *dto.LogDays != 14 {
		t.Fatalf("logDays = %v, want 14", dto.LogDays)
	}
	if dto.EventDays == nil || *dto.EventDays != 0 {
		t.Fatalf("eventDays = %v, want 0 (present, unlimited) not null", dto.EventDays)
	}
}

// TestUpdateRetention_AnAbsentFieldMeansInheritNotZero pins the tri-state
// PUT body contract: a field absent from the JSON body decodes to nil
// ("inherit the installation default"), never coerced to 0.
func TestUpdateRetention_AnAbsentFieldMeansInheritNotZero(t *testing.T) {
	r := newRetentionTestRouter()

	w := doRequestAsOrg(r, http.MethodPut, "/organizations/org_1/retention", "org_1", `{"logDays":14}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto retentionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if dto.EventDays != nil {
		t.Errorf("eventDays = %v, want nil (absent field means inherit, not 0)", dto.EventDays)
	}
}

// TestRetention_IsStrictlyOrgScopedAtTheHandlerLayer mirrors
// TestGovernance_IsStrictlyOrgScopedAtTheHandlerLayer for retention.
func TestRetention_IsStrictlyOrgScopedAtTheHandlerLayer(t *testing.T) {
	r := newRetentionTestRouter()

	putA := doRequestAsOrg(r, http.MethodPut, "/organizations/org_a/retention", "org_a", `{"logDays":7}`)
	if putA.Code != http.StatusOK {
		t.Fatalf("PUT org_a status = %d, want %d; body=%s", putA.Code, http.StatusOK, putA.Body.String())
	}

	getB := doRequestAsOrg(r, http.MethodGet, "/organizations/org_b/retention", "org_b", "")

	if getB.Code != http.StatusOK {
		t.Fatalf("GET org_b status = %d, want %d; body=%s", getB.Code, http.StatusOK, getB.Body.String())
	}
	var dtoB retentionDTO
	if err := json.Unmarshal(getB.Body.Bytes(), &dtoB); err != nil {
		t.Fatalf("decode org_b body: %v", err)
	}
	if dtoB.LogDays != nil {
		t.Errorf("org_b's logDays = %v, want nil — org_a's retention window must never leak across", dtoB.LogDays)
	}
}
