// governance_handler_test.go (in-package test, same package as
// handler_test.go — reuses its fakeDefinitions, doRequest, doRequestAsOrg,
// and wireErrorEnvelope/decodeError helpers). Exercises ListWithVisibility
// (Slice 5, AC1) and List's ?featured=true filter (AC7) through an actual
// chi router, wired against a REAL organizations.Facade as the
// GovernanceReader (not the package's default unrestricted fake) so
// SetGovernance's effect is observable through the HTTP layer exactly as
// production wires it.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
	orgsmemory "beecon/internal/organizations/driven/memory"
)

// newGovernedTestRouter mirrors newTestRouter, but wires a real
// organizations.Facade as the catalog facade's GovernanceReader, and returns
// it alongside the router so a test can call SetGovernance directly (there
// is no HTTP route for it in this package's own router — the governance
// admin routes live in organizations/driving/httpapi, mounted separately in
// production).
func newGovernedTestRouter(t *testing.T) (chi.Router, *organizations.Facade) {
	t.Helper()
	orgs := orgsmemory.NewFacadeWithOverrides(orgsmemory.Overrides{})
	facade, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions(), Governance: orgs})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/governance/catalog", h.ListWithVisibility)
	return r, orgs
}

func TestListWithVisibility_Returns401WhenNoOrgContext(t *testing.T) {
	r, _ := newGovernedTestRouter(t)

	w := doRequestAsOrg(r, http.MethodGet, "/governance/catalog", "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestListWithVisibility_Returns200WithEveryIntegrationAnnotatedByEffectiveVisibility
// is AC1: the operator's unfiltered governance view, distinct from List's
// own already-filtered result.
func TestListWithVisibility_Returns200WithEveryIntegrationAnnotatedByEffectiveVisibility(t *testing.T) {
	r, orgs := newGovernedTestRouter(t)
	created := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)
	var integration integrationSummaryDTO
	if err := json.Unmarshal(created.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode created integration: %v", err)
	}
	if _, err := orgs.SetGovernance(t.Context(), "org_1", organizations.GovernanceUpdate{Hidden: []string{integration.ID}}); err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/governance/catalog", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dtos []integrationVisibilityDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dtos); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dtos) != 1 {
		t.Fatalf("len(dtos) = %d, want 1", len(dtos))
	}
	if dtos[0].ID != integration.ID {
		t.Errorf("id = %q, want %q", dtos[0].ID, integration.ID)
	}
	if dtos[0].Visibility != catalog.VisibilityHidden {
		t.Errorf("visibility = %q, want %q", dtos[0].Visibility, catalog.VisibilityHidden)
	}
}

// TestListWithVisibility_IsScopedToTheRequestingOrgsGovernance proves the
// same installation catalog is annotated differently for two organizations
// with different governance — no cross-org bleed through this endpoint.
func TestListWithVisibility_IsScopedToTheRequestingOrgsGovernance(t *testing.T) {
	r, orgs := newGovernedTestRouter(t)
	created := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)
	var integration integrationSummaryDTO
	if err := json.Unmarshal(created.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode created integration: %v", err)
	}
	if _, err := orgs.SetGovernance(t.Context(), "org_a", organizations.GovernanceUpdate{Hidden: []string{integration.ID}}); err != nil {
		t.Fatalf("SetGovernance org_a: %v", err)
	}
	// org_b never configured: continuity default (visible).

	wA := doRequestAsOrg(r, http.MethodGet, "/governance/catalog", "org_a", "")
	wB := doRequestAsOrg(r, http.MethodGet, "/governance/catalog", "org_b", "")

	var dtosA, dtosB []integrationVisibilityDTO
	if err := json.Unmarshal(wA.Body.Bytes(), &dtosA); err != nil {
		t.Fatalf("decode org_a body: %v", err)
	}
	if err := json.Unmarshal(wB.Body.Bytes(), &dtosB); err != nil {
		t.Fatalf("decode org_b body: %v", err)
	}
	if dtosA[0].Visibility != catalog.VisibilityHidden {
		t.Errorf("org_a's visibility = %q, want %q", dtosA[0].Visibility, catalog.VisibilityHidden)
	}
	if dtosB[0].Visibility != catalog.VisibilityVisible {
		t.Errorf("org_b's visibility = %q, want %q — org_a's hidden rule must never leak to org_b", dtosB[0].Visibility, catalog.VisibilityVisible)
	}
}

// TestList_FeaturedTrueReturnsTheConfiguredOrderedOnboardingSubset is AC7 at
// the HTTP layer: ?featured=true returns the operator-ordered subset instead
// of the full filtered catalog.
func TestList_FeaturedTrueReturnsTheConfiguredOrderedOnboardingSubset(t *testing.T) {
	r, orgs := newGovernedTestRouter(t)
	first := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"c1","clientSecret":"s1"}`)
	second := doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"c2","clientSecret":"s2"}`)
	var firstDTO, secondDTO integrationSummaryDTO
	if err := json.Unmarshal(first.Body.Bytes(), &firstDTO); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondDTO); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if _, err := orgs.SetGovernance(t.Context(), "org_1", organizations.GovernanceUpdate{
		Featured: []string{secondDTO.ID, firstDTO.ID},
	}); err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}

	w := doRequestAsOrg(r, http.MethodGet, "/?featured=true", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dtos []integrationSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dtos); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dtos) != 2 || dtos[0].ID != secondDTO.ID || dtos[1].ID != firstDTO.ID {
		t.Fatalf("dtos = %+v, want [%q %q] in the configured order", dtos, secondDTO.ID, firstDTO.ID)
	}
}

// TestList_FeaturedTrueFallsBackToTheFullVisibleListWhenNoneAreFeatured pins
// the fallback half of AC7 at the HTTP layer, using the unconfigured default
// (no SetGovernance call at all).
func TestList_FeaturedTrueFallsBackToTheFullVisibleListWhenNoneAreFeatured(t *testing.T) {
	r, _ := newGovernedTestRouter(t)
	doRequest(r, http.MethodPost, "/", `{"providerSlug":"outlook","clientId":"cid","clientSecret":"csecret"}`)

	w := doRequestAsOrg(r, http.MethodGet, "/?featured=true", "org_1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dtos []integrationSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dtos); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dtos) != 1 {
		t.Fatalf("len(dtos) = %d, want 1 (the fallback: first-N visible integrations)", len(dtos))
	}
}
