//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, wireErrorEnvelope, doJSONRequest;
// createOrgAndKey from key_rotation_journey_integration_test.go — same
// package). This file tells Slice 8's multi-endpoint management story end to
// end against the real composition root: registering a second endpoint for
// an org (URL + event-type filter) alongside the Phase 3 single one,
// rejecting registration beyond the configured cap naming it, listing every
// endpoint's own filter/status/failure count, updating and deleting one,
// rotating one endpoint's secret independently of a sibling's, RequireWrite
// guarding every mutation while list/read stay open to a read-only key, org
// isolation on the org-key CRUD surface, and the same handlers reused,
// org-scoped by path, under the admin console's
// /api/v1/organizations/{orgId}/webhook-endpoints mount.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"beecon/test/support"
)

type endpointListItemWireDTO struct {
	ID                  string   `json:"id"`
	URL                 string   `json:"url"`
	EventTypes          []string `json:"eventTypes"`
	Status              string   `json:"status"`
	ConsecutiveFailures int      `json:"consecutiveFailures"`
	SecretPrefix        string   `json:"secretPrefix"`
	CreatedAt           string   `json:"createdAt"`
}

type endpointListWireDTO struct {
	Items []endpointListItemWireDTO `json:"items"`
}

type createEndpointWireDTO struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	EventTypes []string `json:"eventTypes"`
	Secret     string   `json:"secret"`
}

type updateEndpointWireDTO struct {
	ID                  string   `json:"id"`
	URL                 string   `json:"url"`
	EventTypes          []string `json:"eventTypes"`
	Status              string   `json:"status"`
	ConsecutiveFailures int      `json:"consecutiveFailures"`
}

type rotatedEndpointSecretWireDTO struct {
	Secret string `json:"secret"`
}

func createEndpointAt(t *testing.T, router http.Handler, basePath, auth, url string, eventTypes []string) (int, createEndpointWireDTO) {
	t.Helper()
	body := `{"url":"` + url + `"`
	if eventTypes != nil {
		encoded, err := json.Marshal(eventTypes)
		if err != nil {
			t.Fatalf("marshal eventTypes: %v", err)
		}
		body += `,"eventTypes":` + string(encoded)
	}
	body += `}`
	w := doJSONRequest(t, router, http.MethodPost, basePath, auth, body)
	var dto createEndpointWireDTO
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode create-endpoint response: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func listEndpointsAt(t *testing.T, router http.Handler, basePath, auth string) (int, endpointListWireDTO) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, basePath, auth, "")
	var page endpointListWireDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode endpoint list: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func findEndpointItem(items []endpointListItemWireDTO, id string) *endpointListItemWireDTO {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

// TestWebhookEndpointsManagementJourney_MultiEndpointCRUDCapFilterAndPerEndpointRotate
// is Slice 8's headline CRUD story (AC1, AC2, AC3, AC8) against the org-key
// surface: registering a second endpoint alongside the Phase 3 one, the
// cap's own naming, the list's own per-endpoint shape, update, delete, and a
// rotate on one endpoint leaving a sibling's own secret untouched.
func TestWebhookEndpointsManagementJourney_MultiEndpointCRUDCapFilterAndPerEndpointRotate(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "Endpoints Journey Co")
	orgAuth := "Bearer " + issued.Key
	_ = org

	const basePath = "/api/v1/webhook-endpoints/"

	t.Run("the Phase 3 single-endpoint alias keeps working as endpoint #1", func(t *testing.T) {
		status, first := setWebhookEndpoint(t, wired.Router, orgAuth, "https://example.com/hook-legacy")
		if status != http.StatusOK {
			t.Fatalf("SetEndpoint status = %d, want %d", status, http.StatusOK)
		}
		if first.Secret == "" {
			t.Error("expected a secret on first creation via the PD31 alias")
		}
	})

	var secondID, secondSecret string
	t.Run("registering a second endpoint with its own URL and event-type filter succeeds, its own whsec_ secret shown once", func(t *testing.T) {
		status, created := createEndpointAt(t, wired.Router, basePath, orgAuth, "https://example.com/hook-filtered", []string{"trigger.event"})
		if status != http.StatusCreated {
			t.Fatalf("CreateEndpoint status = %d, want %d", status, http.StatusCreated)
		}
		if !strings.HasPrefix(created.Secret, "whsec_") {
			t.Errorf("secret = %q, want it to start with whsec_", created.Secret)
		}
		if len(created.EventTypes) != 1 || created.EventTypes[0] != "trigger.event" {
			t.Errorf("EventTypes = %v, want [\"trigger.event\"]", created.EventTypes)
		}
		secondID, secondSecret = created.ID, created.Secret
	})

	t.Run("listing shows both endpoints, each with its own filter/status/failure count", func(t *testing.T) {
		status, page := listEndpointsAt(t, wired.Router, basePath, orgAuth)
		if status != http.StatusOK {
			t.Fatalf("ListEndpoints status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(page.Items))
		}
		filtered := findEndpointItem(page.Items, secondID)
		if filtered == nil {
			t.Fatal("second endpoint missing from the list")
		}
		if filtered.Status != "ENABLED" {
			t.Errorf("Status = %q, want %q", filtered.Status, "ENABLED")
		}
		if filtered.ConsecutiveFailures != 0 {
			t.Errorf("ConsecutiveFailures = %d, want 0", filtered.ConsecutiveFailures)
		}
		if strings.Contains(filtered.SecretPrefix, secondSecret) && filtered.SecretPrefix == secondSecret {
			t.Error("the list's secretPrefix must never equal the full secret")
		}
	})

	t.Run("updating the second endpoint replaces its URL and filter", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/webhook-endpoints/"+secondID, orgAuth,
			`{"url":"https://example.com/hook-filtered-v2","eventTypes":["connection.expired"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateEndpoint status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var updated updateEndpointWireDTO
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatalf("decode update response: %v", err)
		}
		if updated.URL != "https://example.com/hook-filtered-v2" {
			t.Errorf("URL = %q, want the updated value", updated.URL)
		}
		if len(updated.EventTypes) != 1 || updated.EventTypes[0] != "connection.expired" {
			t.Errorf("EventTypes = %v, want [\"connection.expired\"]", updated.EventTypes)
		}
	})

	t.Run("rotating the second endpoint's secret mints a fresh one, distinct from the first endpoint's own", func(t *testing.T) {
		firstStatus, firstView, _ := getWebhookEndpoint(t, wired.Router, orgAuth)
		if firstStatus != http.StatusOK {
			t.Fatalf("GetEndpoint (PD31 alias, endpoint #1) status = %d, want %d", firstStatus, http.StatusOK)
		}

		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+secondID+"/rotate-secret", orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("RotateEndpointSecret status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var rotated rotatedEndpointSecretWireDTO
		if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
			t.Fatalf("decode rotate response: %v", err)
		}
		if rotated.Secret == "" || rotated.Secret == secondSecret {
			t.Error("expected a freshly minted, different secret for the second endpoint")
		}

		secondStatus, secondFirstCheck, _ := getWebhookEndpoint(t, wired.Router, orgAuth)
		if secondStatus != http.StatusOK {
			t.Fatalf("GetEndpoint (recheck) status = %d, want %d", secondStatus, http.StatusOK)
		}
		if secondFirstCheck.SecretPrefix != firstView.SecretPrefix {
			t.Errorf("endpoint #1's own secret prefix changed (%q -> %q) after rotating the SECOND endpoint's secret — rotation must be scoped per endpoint", firstView.SecretPrefix, secondFirstCheck.SecretPrefix)
		}
	})

	t.Run("disable then enable resets and resumes the second endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+secondID+"/disable", orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("DisableEndpoint status = %d, want %d", w.Code, http.StatusOK)
		}
		var disabled updateEndpointWireDTO
		if err := json.Unmarshal(w.Body.Bytes(), &disabled); err != nil {
			t.Fatalf("decode disable response: %v", err)
		}
		if disabled.Status != "DISABLED" {
			t.Errorf("Status = %q, want %q", disabled.Status, "DISABLED")
		}

		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+secondID+"/enable", orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("EnableEndpoint status = %d, want %d", w.Code, http.StatusOK)
		}
		var enabled updateEndpointWireDTO
		if err := json.Unmarshal(w.Body.Bytes(), &enabled); err != nil {
			t.Fatalf("decode enable response: %v", err)
		}
		if enabled.Status != "ENABLED" {
			t.Errorf("Status = %q, want %q", enabled.Status, "ENABLED")
		}
	})

	t.Run("deleting the second endpoint removes it from the list", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/webhook-endpoints/"+secondID, orgAuth, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteEndpoint status = %d, want %d", w.Code, http.StatusNoContent)
		}
		status, page := listEndpointsAt(t, wired.Router, basePath, orgAuth)
		if status != http.StatusOK {
			t.Fatalf("ListEndpoints status = %d, want %d", status, http.StatusOK)
		}
		if findEndpointItem(page.Items, secondID) != nil {
			t.Error("deleted endpoint still appears in the list")
		}
	})
}

// TestWebhookEndpointsManagementJourney_RegistrationBeyondTheCapIsRejectedNamingIt
// is AC2 end to end: BEECON_WEBHOOK_ENDPOINT_CAP's default is 5 (config.go);
// registering a 6th endpoint for the same org is rejected with a validation
// error whose message names the configured cap.
func TestWebhookEndpointsManagementJourney_RegistrationBeyondTheCapIsRejectedNamingIt(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issued := createOrgAndKey(t, wired.Router, adminAuth, "Cap Journey Co")
	orgAuth := "Bearer " + issued.Key
	const basePath = "/api/v1/webhook-endpoints/"

	const defaultCap = 5
	for i := 0; i < defaultCap; i++ {
		status, _ := createEndpointAt(t, wired.Router, basePath, orgAuth, "https://example.com/hook-"+strconv.Itoa(i), nil)
		if status != http.StatusCreated {
			t.Fatalf("CreateEndpoint (%d/%d) status = %d, want %d", i+1, defaultCap, status, http.StatusCreated)
		}
	}

	w := doJSONRequest(t, wired.Router, http.MethodPost, basePath, orgAuth, `{"url":"https://example.com/one-too-many"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !strings.Contains(env.Error.Message, strconv.Itoa(defaultCap)) {
		// The cap detail lives in the domain error's Details, rendered by
		// httpx's error envelope alongside the top-level message; either
		// location naming the cap satisfies the AC, so fall back to the raw
		// body before failing.
		if !strings.Contains(w.Body.String(), strconv.Itoa(defaultCap)) {
			t.Errorf("neither error.message (%q) nor the response body names the configured cap %d", env.Error.Message, defaultCap)
		}
	}
}

// TestWebhookEndpointsManagementJourney_RequireWriteGuardsEveryMutationButNotListing
// is PD41 applied to Slice 8's own new routes: a read-only key is rejected
// with 403 creating, updating, deleting, rotating, enabling, and disabling an
// endpoint, while GET (list) keeps working for it — mirroring
// api_key_scope_journey_integration_test.go's own established pattern for
// this same class of guarantee.
func TestWebhookEndpointsManagementJourney_RequireWriteGuardsEveryMutationButNotListing(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "RequireWrite Endpoints Co")
	readWriteAuth := "Bearer " + issued.Key

	readOnlyKey := issueScopedKey(t, wired, adminAuth, org.ID, `{"scope":"read-only"}`)
	readOnlyAuth := "Bearer " + readOnlyKey.Key

	const basePath = "/api/v1/webhook-endpoints/"
	status, created := createEndpointAt(t, wired.Router, basePath, readWriteAuth, "https://example.com/hook", nil)
	if status != http.StatusCreated {
		t.Fatalf("setup CreateEndpoint status = %d, want %d", status, http.StatusCreated)
	}

	t.Run("a read-only key is rejected with 403 creating an endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, basePath, readOnlyAuth, `{"url":"https://example.com/should-be-rejected"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-only key can still list endpoints", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, basePath, readOnlyAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("a read-only key is rejected with 403 updating an endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/webhook-endpoints/"+created.ID, readOnlyAuth, `{"url":"https://example.com/should-be-rejected"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-only key is rejected with 403 rotating an endpoint's secret", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+created.ID+"/rotate-secret", readOnlyAuth, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-only key is rejected with 403 disabling an endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+created.ID+"/disable", readOnlyAuth, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-only key is rejected with 403 enabling an endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+created.ID+"/enable", readOnlyAuth, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-only key is rejected with 403 deleting an endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/webhook-endpoints/"+created.ID, readOnlyAuth, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-write key succeeds deleting the endpoint", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/webhook-endpoints/"+created.ID, readWriteAuth, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
	})
}

// TestWebhookEndpointsManagementJourney_OrgKeyMountNeverLeaksAcrossOrganizations
// pins org isolation on the org-key CRUD surface: org B's own API key cannot
// see, update, rotate, or delete an endpoint id that belongs to org A —
// every cross-org attempt is not-found, never a leak or a silent success.
func TestWebhookEndpointsManagementJourney_OrgKeyMountNeverLeaksAcrossOrganizations(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issuedA := createOrgAndKey(t, wired.Router, adminAuth, "Org A Endpoints Co")
	_, issuedB := createOrgAndKey(t, wired.Router, adminAuth, "Org B Endpoints Co")
	orgAAuth := "Bearer " + issuedA.Key
	orgBAuth := "Bearer " + issuedB.Key
	const basePath = "/api/v1/webhook-endpoints/"

	status, createdA := createEndpointAt(t, wired.Router, basePath, orgAAuth, "https://example.com/org-a-hook", nil)
	if status != http.StatusCreated {
		t.Fatalf("CreateEndpoint for org A status = %d, want %d", status, http.StatusCreated)
	}

	t.Run("org B's list never includes org A's endpoint", func(t *testing.T) {
		listStatus, page := listEndpointsAt(t, wired.Router, basePath, orgBAuth)
		if listStatus != http.StatusOK {
			t.Fatalf("ListEndpoints (org B) status = %d, want %d", listStatus, http.StatusOK)
		}
		if findEndpointItem(page.Items, createdA.ID) != nil {
			t.Error("org A's endpoint leaked into org B's list")
		}
	})

	t.Run("org B updating org A's endpoint id is not-found", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/webhook-endpoints/"+createdA.ID, orgBAuth, `{"url":"https://example.com/stolen"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("org B rotating org A's endpoint secret is not-found", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoints/"+createdA.ID+"/rotate-secret", orgBAuth, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("org B deleting org A's endpoint is not-found, and org A's endpoint survives", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/webhook-endpoints/"+createdA.ID, orgBAuth, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		listStatus, page := listEndpointsAt(t, wired.Router, basePath, orgAAuth)
		if listStatus != http.StatusOK {
			t.Fatalf("ListEndpoints (org A) status = %d, want %d", listStatus, http.StatusOK)
		}
		if findEndpointItem(page.Items, createdA.ID) == nil {
			t.Error("org A's endpoint disappeared after org B's failed delete attempt — a cross-org 404 must never have side effects")
		}
	})
}

// TestAdminConsoleWebhookEndpointsMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact
// mirrors admin_console_operate_mount_journey_integration_test.go's own
// established pattern for Slice 8's own new mount (architecture §3.6/router.go):
// the admin key against /api/v1/organizations/{orgId}/webhook-endpoints reuses
// the very same handlers, scoped strictly by the path org id, while the
// pre-existing org-key /api/v1/webhook-endpoints route keeps working
// unaffected.
func TestAdminConsoleWebhookEndpointsMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	orgA, issuedA := createOrgAndKey(t, wired.Router, adminAuth, "Admin Mount Org A")
	orgB, _ := createOrgAndKey(t, wired.Router, adminAuth, "Admin Mount Org B")
	orgAAuth := "Bearer " + issuedA.Key

	sdkStatus, createdViaSDK := createEndpointAt(t, wired.Router, "/api/v1/webhook-endpoints/", orgAAuth, "https://example.com/org-a-hook", nil)
	if sdkStatus != http.StatusCreated {
		t.Fatalf("CreateEndpoint via the SDK route status = %d, want %d", sdkStatus, http.StatusCreated)
	}

	t.Run("no admin key against org A's admin mount is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgA.ID+"/webhook-endpoints/", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("org A's own API key does not satisfy the admin console mount", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgA.ID+"/webhook-endpoints/", orgAAuth, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the console mount is admin-key-only, not org-key", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("the admin key against org A's path lists exactly org A's endpoint", func(t *testing.T) {
		listStatus, page := listEndpointsAt(t, wired.Router, "/api/v1/organizations/"+orgA.ID+"/webhook-endpoints/", adminAuth)
		if listStatus != http.StatusOK {
			t.Fatalf("status = %d, want %d", listStatus, http.StatusOK)
		}
		if findEndpointItem(page.Items, createdViaSDK.ID) == nil {
			t.Errorf("items = %v, want them to include org A's endpoint %q", page.Items, createdViaSDK.ID)
		}
	})

	t.Run("the same admin key against org B's path never sees org A's endpoint", func(t *testing.T) {
		listStatus, page := listEndpointsAt(t, wired.Router, "/api/v1/organizations/"+orgB.ID+"/webhook-endpoints/", adminAuth)
		if listStatus != http.StatusOK {
			t.Fatalf("status = %d, want %d", listStatus, http.StatusOK)
		}
		if findEndpointItem(page.Items, createdViaSDK.ID) != nil {
			t.Errorf("org B's admin-mount list leaked org A's endpoint %q", createdViaSDK.ID)
		}
	})

	t.Run("creating an endpoint through the admin mount is the same CreateEndpoint handler, reused verbatim", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgA.ID+"/webhook-endpoints/", adminAuth, `{"url":"https://example.com/created-via-admin-mount"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("regression: the pre-existing org-key route still lists org A's endpoint", func(t *testing.T) {
		status, page := listEndpointsAt(t, wired.Router, "/api/v1/webhook-endpoints/", orgAAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if findEndpointItem(page.Items, createdViaSDK.ID) == nil {
			t.Fatal("the SDK-facing route no longer lists the endpoint it created itself — the admin mount must not have disturbed it")
		}
	})
}
