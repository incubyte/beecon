//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, wireErrorEnvelope, and
// doJSONRequest already declared there — same package). This file tells
// Slice 5's story end to end against the real composition root: the
// catalog's previously-ignored org param becomes real governance. It proves,
// over real HTTP against the full router:
//
//  1. PD42 continuity — an org with no governance configured sees the full
//     installation catalog and can initiate to anything, identically to
//     every phase before this one.
//  2. Allow-list enforcement — once set, the consumer catalog returns only
//     the allow-listed integrations, and initiating to a non-allowed
//     integration is not-found (AC5).
//  3. Hidden enforcement — a hidden integration disappears from the catalog
//     even when allow-listed, and initiating to it is not-found.
//  4. Governance is strictly org-scoped — two organizations independently
//     curate the same shared installation catalog, and neither can see or
//     initiate to an integration only visible under the other's rules. This
//     is the AC5 cross-org isolation journey.
//  5. The operator's unfiltered governance view (GET .../governance/catalog)
//     annotates each integration's effective visibility for the org asked
//     about, distinct from the consumer's already-filtered list.
//  6. Onboarding's featured subset (?featured=true) returns the configured
//     ordered subset, and SetGovernance rejects a featured list exceeding
//     the cap.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"testing"

	"beecon/test/support"
)

type governanceDTO struct {
	AllowList  *[]string `json:"allowList"`
	Hidden     []string  `json:"hidden"`
	Onboarding struct {
		Featured []string `json:"featured"`
		Cap      int      `json:"cap"`
	} `json:"onboarding"`
}

type integrationVisibilityDTO struct {
	ID           string `json:"id"`
	ProviderSlug string `json:"providerSlug"`
	Name         string `json:"name"`
	Visibility   string `json:"visibility"`
}

func TestGovernanceJourney(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	// --- Setup: two real provider-backed integrations (outlook, hubspot),
	// two organizations each with their own org key, both allowing the same
	// consumer redirect URI. ---
	const redirectURI = "https://consumer.example.com/callback"

	createIntegrationAs := func(providerSlug string) integrationSummaryDTO {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
			`{"providerSlug":"`+providerSlug+`","clientId":"cid-`+providerSlug+`","clientSecret":"secret-`+providerSlug+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s integration status = %d, want %d; body=%s", providerSlug, w.Code, http.StatusCreated, w.Body.String())
		}
		var dto integrationSummaryDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode %s integration: %v", providerSlug, err)
		}
		return dto
	}
	outlookIntg := createIntegrationAs("outlook")
	hubspotIntg := createIntegrationAs("hubspot")

	createOrgWithKey := func(name string) (organizationDTO, string) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"`+name+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create org %s status = %d, want %d; body=%s", name, w.Code, http.StatusCreated, w.Body.String())
		}
		var org organizationDTO
		if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
			t.Fatalf("decode org %s: %v", name, err)
		}
		w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
			`{"allowedRedirectUris":["`+redirectURI+`"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("set allowed redirect uris for %s status = %d, want %d; body=%s", name, w.Code, http.StatusOK, w.Body.String())
		}
		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("issue key for %s status = %d, want %d; body=%s", name, w.Code, http.StatusCreated, w.Body.String())
		}
		var key issuedKeyDTO
		if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
			t.Fatalf("decode key for %s: %v", name, err)
		}
		return org, "Bearer " + key.Key
	}
	orgA, orgAAuth := createOrgWithKey("Org A")
	orgB, orgBAuth := createOrgWithKey("Org B")

	createUser := func(orgAuth, name string) userDTO {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"`+name+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create user %s status = %d, want %d; body=%s", name, w.Code, http.StatusCreated, w.Body.String())
		}
		var user userDTO
		if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
			t.Fatalf("decode user %s: %v", name, err)
		}
		return user
	}
	userA := createUser(orgAAuth, "Ada (org A)")
	userB := createUser(orgBAuth, "Bea (org B)")

	initiate := func(orgAuth, userID, integrationID string) int {
		body := `{"userId":"` + userID + `","integrationId":"` + integrationID + `","redirectUri":"` + redirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
		return w.Code
	}

	listIntegrations := func(orgAuth string) []integrationSummaryDTO {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/", orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("list integrations status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var list []integrationSummaryDTO
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v; body=%s", err, w.Body.String())
		}
		return list
	}

	// --- 1. PD42 continuity: neither org has governance configured yet. ---
	t.Run("PD42 continuity: an unconfigured org sees the full installation catalog", func(t *testing.T) {
		list := listIntegrations(orgAAuth)
		if len(list) != 2 {
			t.Fatalf("org A's unconfigured catalog has %d integrations, want 2 (outlook + hubspot, unrestricted)", len(list))
		}
	})

	t.Run("PD42 continuity: an unconfigured org can initiate to any integration", func(t *testing.T) {
		status := initiate(orgAAuth, userA.ID, outlookIntg.ID)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want %d", status, http.StatusCreated)
		}
		status = initiate(orgAAuth, userA.ID, hubspotIntg.ID)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want %d", status, http.StatusCreated)
		}
	})

	t.Run("GET governance for an unconfigured org returns the continuity-preserving default", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgA.ID+"/governance", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var got governanceDTO
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.AllowList != nil {
			t.Errorf("allowList = %v, want null", got.AllowList)
		}
		if got.Onboarding.Cap != 8 {
			t.Errorf("onboarding.cap = %d, want the platform default 8", got.Onboarding.Cap)
		}
	})

	// --- 2/3/4: operator curates org A and org B differently; assert no
	// cross-org bleed anywhere (AC5's headline isolation journey). ---
	t.Run("operator allow-lists org A to outlook only", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/organizations/"+orgA.ID+"/governance", adminAuth,
			`{"allowList":["`+outlookIntg.ID+`"],"hidden":[],"onboarding":{"featured":[],"cap":8}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("operator hides outlook for org B (org B keeps seeing hubspot)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/organizations/"+orgB.ID+"/governance", adminAuth,
			`{"allowList":null,"hidden":["`+outlookIntg.ID+`"],"onboarding":{"featured":[],"cap":8}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("org A's consumer catalog now shows only outlook", func(t *testing.T) {
		list := listIntegrations(orgAAuth)
		if len(list) != 1 || list[0].ID != outlookIntg.ID {
			t.Fatalf("org A's catalog = %+v, want only outlook", list)
		}
	})

	t.Run("org B's consumer catalog now shows only hubspot, unaffected by org A's allow-list", func(t *testing.T) {
		list := listIntegrations(orgBAuth)
		if len(list) != 1 || list[0].ID != hubspotIntg.ID {
			t.Fatalf("org B's catalog = %+v, want only hubspot", list)
		}
	})

	t.Run("AC5: org A cannot initiate to hubspot (never allow-listed for A) — not-found", func(t *testing.T) {
		status := initiate(orgAAuth, userA.ID, hubspotIntg.ID)
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("AC5: org A can still initiate to its own visible outlook", func(t *testing.T) {
		status := initiate(orgAAuth, userA.ID, outlookIntg.ID)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want %d", status, http.StatusCreated)
		}
	})

	t.Run("AC5: org B cannot initiate to outlook (hidden for B) — not-found", func(t *testing.T) {
		status := initiate(orgBAuth, userB.ID, outlookIntg.ID)
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("AC5: org B can still initiate to its own visible hubspot", func(t *testing.T) {
		status := initiate(orgBAuth, userB.ID, hubspotIntg.ID)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want %d", status, http.StatusCreated)
		}
	})

	t.Run("ListTools honors org A's visibility: outlook's tools list, hubspot's do not", func(t *testing.T) {
		wOutlook := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools?integrationId="+outlookIntg.ID, orgAAuth, "")
		if wOutlook.Code != http.StatusOK {
			t.Fatalf("outlook tools status = %d, want %d; body=%s", wOutlook.Code, http.StatusOK, wOutlook.Body.String())
		}
		wHubspot := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools?integrationId="+hubspotIntg.ID, orgAAuth, "")
		if wHubspot.Code != http.StatusOK {
			t.Fatalf("hubspot tools status = %d, want %d; body=%s", wHubspot.Code, http.StatusOK, wHubspot.Body.String())
		}
		var hubspotPage toolsPageDTO
		if err := json.Unmarshal(wHubspot.Body.Bytes(), &hubspotPage); err != nil {
			t.Fatalf("decode hubspot tools page: %v", err)
		}
		if len(hubspotPage.Items) != 0 {
			t.Errorf("org A's hubspot-filtered tools = %+v, want empty (hubspot not allow-listed for org A)", hubspotPage.Items)
		}
	})

	// --- 5. The operator's unfiltered governance view (AC1). ---
	t.Run("operator's governance-catalog view shows outlook VISIBLE and hubspot NOT_ALLOWED for org A", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgA.ID+"/governance/catalog", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var items []integrationVisibilityDTO
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v; body=%s", err, w.Body.String())
		}
		if len(items) != 2 {
			t.Fatalf("len(items) = %d, want 2 (every installation integration, unfiltered)", len(items))
		}
		byID := map[string]string{}
		for _, item := range items {
			byID[item.ID] = item.Visibility
		}
		if byID[outlookIntg.ID] != "VISIBLE" {
			t.Errorf("outlook visibility for org A = %q, want VISIBLE", byID[outlookIntg.ID])
		}
		if byID[hubspotIntg.ID] != "NOT_ALLOWED" {
			t.Errorf("hubspot visibility for org A = %q, want NOT_ALLOWED", byID[hubspotIntg.ID])
		}
	})

	t.Run("operator's governance-catalog view shows outlook HIDDEN for org B", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgB.ID+"/governance/catalog", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var items []integrationVisibilityDTO
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		byID := map[string]string{}
		for _, item := range items {
			byID[item.ID] = item.Visibility
		}
		if byID[outlookIntg.ID] != "HIDDEN" {
			t.Errorf("outlook visibility for org B = %q, want HIDDEN", byID[outlookIntg.ID])
		}
		if byID[hubspotIntg.ID] != "VISIBLE" {
			t.Errorf("hubspot visibility for org B = %q, want VISIBLE", byID[hubspotIntg.ID])
		}
	})

	// --- 6. Onboarding featured subset + cap validation (AC7). ---
	t.Run("operator features outlook for org B's onboarding", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/organizations/"+orgB.ID+"/governance", adminAuth,
			`{"allowList":null,"hidden":["`+outlookIntg.ID+`"],"onboarding":{"featured":["`+hubspotIntg.ID+`"],"cap":8}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("?featured=true returns the configured featured subset for org B", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/?featured=true", orgBAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var list []integrationSummaryDTO
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode: %v; body=%s", err, w.Body.String())
		}
		if len(list) != 1 || list[0].ID != hubspotIntg.ID {
			t.Fatalf("featured list = %+v, want only hubspot", list)
		}
	})

	t.Run("setting a featured list longer than the cap is a validation error", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/organizations/"+orgB.ID+"/governance", adminAuth,
			`{"allowList":null,"hidden":[],"onboarding":{"featured":["a","b","c"],"cap":2}}`)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "validation_failed" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
		}
	})
}
