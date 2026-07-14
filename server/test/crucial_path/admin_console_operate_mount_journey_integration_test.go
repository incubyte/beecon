//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, connectionDTO,
// wireErrorEnvelope, and doJSONRequest already declared there;
// connectionsPageDTO from connection_lifecycle_journey_integration_test.go;
// triggerInstanceDTO, triggerInstancesPageDTO, createdTriggerInstanceDTO,
// createTriggerInstance, getTriggerInstance, listTriggerInstances,
// setConnectionStatus, outlookDefinitionWithTrigger, and
// outlookMessageReceivedSlug from trigger_instances_journey_integration_test.go
// — same package). This file tells the Admin UI's Slice 2 "operate the
// estate" story end to end against the real composition root: the
// AdminOrgScope bridge (FD3) lets an operator holding only the installation
// admin key reach an organization's connections and trigger instances under
// /api/v1/organizations/{orgId}/..., scoped strictly by the {orgId} that
// appears in the URL path — never by a header or query param, and never by
// an org's own API key — while the pre-existing org-key-authenticated
// /api/v1/connections and /api/v1/trigger-instances routes (Phase 1-3, the
// SDK's own surface) keep working exactly as before.
//
// NOTE ON A DISCOVERED REGRESSION: this Slice's own router.go change mounts a
// new r.Route("/{orgId}", ...) subrouter (for /connections and
// /trigger-instances) inside the SAME r.Route("/organizations", ...) block
// that already registers direct leaf handlers on that exact pattern
// (r.Get("/{orgId}", ...) and r.Patch("/{orgId}", ...) — Organizations.Get
// and UpdateAllowedRedirectURIs, both pre-existing Phase 1 endpoints).
// chi cannot serve a leaf handler and a mounted subrouter on the identical
// pattern node: once the subrouter mount is added, GET and PATCH
// /api/v1/organizations/{orgId} both start 404ing (proven by
// TestBuildRouter_SingleOrganizationGetAndPatchStillWorkAfterTheAdminConsoleMount
// in server/internal/app, and independently by
// TestChiRepro_LeafAndSubrouterOnSamePattern, a throwaway chi-only repro run
// during this review). Nested sub-paths (e.g. /{orgId}/api-keys,
// /{orgId}/connections) are unaffected — only the exact "/{orgId}" leaf
// breaks. See this file's final test-run notes for the flagged bug report.
// Because that PATCH endpoint is how every other journey in this package
// sets an organization's allow-list, this file's own fixture setup
// (setUpOrgWithConnection) writes allowed_redirect_uris directly to the
// database instead of going through the now-broken HTTP endpoint, so Slice
// 2's actual subject — the AdminOrgScope connections/trigger-instances mount
// — can still be exercised and pinned independently of that regression.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/app"
	organizationsbun "beecon/internal/organizations/driven/bun"
	"beecon/test/support"
)

// setOrgAllowedRedirectURIs writes an organization's allow-list directly to
// its database row. Every other journey in this package sets it through
// PATCH /api/v1/organizations/{orgId} — this file uses the direct-DB
// shortcut instead because that exact route is currently broken by this
// slice's own router change (see this file's header comment); the shortcut
// keeps this file's fixture setup working so the AdminOrgScope mount itself
// — the thing this file actually tests — can still be exercised.
func setOrgAllowedRedirectURIs(t *testing.T, wired *app.Wired, orgID string, uris []string) {
	t.Helper()
	encoded, err := json.Marshal(uris)
	if err != nil {
		t.Fatalf("marshal allowed redirect uris: %v", err)
	}
	_, err = wired.DB.NewUpdate().
		Model((*organizationsbun.OrganizationRow)(nil)).
		Set("allowed_redirect_uris = ?", string(encoded)).
		Where("id = ?", orgID).
		Exec(context.Background())
	if err != nil {
		t.Fatalf("set allowed redirect uris for org %q: %v", orgID, err)
	}
}

// setUpOrgWithConnection creates a fresh organization (adminAuth), allows
// redirectURI, issues that org its own API key, creates a user in it, and
// initiates one connection against integrationID — the minimum scaffolding
// every sub-test in this file needs before it can exercise the admin
// console's org-scoped mount against a real organization.
func setUpOrgWithConnection(t *testing.T, wired *app.Wired, adminAuth, orgName, integrationID, redirectURI string) (orgID, orgAuth string, conn initiatedConnectionDTO) {
	t.Helper()

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"`+orgName+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org %q status = %d, want %d; body=%s", orgName, w.Code, http.StatusCreated, w.Body.String())
	}
	var org organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org %q: %v", orgName, err)
	}

	setOrgAllowedRedirectURIs(t, wired, org.ID, []string{redirectURI})

	var key issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key for %q status = %d, want %d; body=%s", orgName, w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatalf("decode key for %q: %v", orgName, err)
	}
	orgAuth = "Bearer " + key.Key

	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Operator Test User"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user for %q status = %d, want %d; body=%s", orgName, w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user for %q: %v", orgName, err)
	}

	body := `{"userId":"` + user.ID + `","integrationId":"` + integrationID + `","redirectUri":"` + redirectURI + `"}`
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("initiate connection for %q status = %d, want %d; body=%s", orgName, w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conn); err != nil {
		t.Fatalf("decode initiated connection for %q: %v", orgName, err)
	}
	return org.ID, orgAuth, conn
}

// setUpOrgWithActiveConnectionAndTriggerInstance is setUpOrgWithConnection
// plus flipping the connection straight to ACTIVE at the database row (the
// same shortcut trigger_instances_journey_integration_test.go's own EXPIRED
// sub-test uses) and creating one trigger instance on it — this file only
// needs a real instance to exist and be reachable through the admin mount,
// not a full OAuth handshake.
func setUpOrgWithActiveConnectionAndTriggerInstance(t *testing.T, wired *app.Wired, adminAuth, orgName, integrationID, redirectURI string) (orgID, orgAuth string, instance createdTriggerInstanceDTO) {
	t.Helper()
	orgID, orgAuth, conn := setUpOrgWithConnection(t, wired, adminAuth, orgName, integrationID, redirectURI)
	setConnectionStatus(t, wired, conn.ID, "ACTIVE")

	status, created := createTriggerInstance(t, wired, orgAuth, conn.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	if status != http.StatusCreated {
		t.Fatalf("create trigger instance for %q status = %d, want %d", orgName, status, http.StatusCreated)
	}
	return orgID, orgAuth, created
}

func listConnectionsUnderAdminMount(t *testing.T, wired *app.Wired, orgID, authHeader string) (int, connectionsPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgID+"/connections", authHeader, "")
	var page connectionsPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode connections page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func getConnectionUnderAdminMount(t *testing.T, wired *app.Wired, orgID, connID, authHeader string) int {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgID+"/connections/"+connID, authHeader, "")
	return w.Code
}

func listTriggerInstancesUnderAdminMount(t *testing.T, wired *app.Wired, orgID, authHeader string) (int, triggerInstancesPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgID+"/trigger-instances", authHeader, "")
	var page triggerInstancesPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode trigger instances page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func getTriggerInstanceUnderAdminMount(t *testing.T, wired *app.Wired, orgID, instanceID, authHeader string) int {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgID+"/trigger-instances/"+instanceID, authHeader, "")
	return w.Code
}

func connectionIDsOf(page connectionsPageDTO) []string {
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func triggerInstanceIDsOf(page triggerInstancesPageDTO) []string {
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// TestAdminConsoleConnectionsMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact
// covers Slice 2's AC1 and AC7 at the wire level: an operator holding only
// the admin key lists exactly one organization's connections under
// /api/v1/organizations/{orgId}/connections, the {orgId} in the path (not any
// header) is what scopes the result, requesting under a different
// organization's id returns that organization's own data with no bleed
// either way, and the pre-existing org-key /api/v1/connections route is
// unaffected by the second mount.
func TestAdminConsoleConnectionsMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"client-id","clientSecret":"client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}
	const redirectURI = "https://consumer.example.com/callback"

	orgAID, orgAAuth, connA := setUpOrgWithConnection(t, wired, adminAuth, "Acme", integration.ID, redirectURI)
	orgBID, _, connB := setUpOrgWithConnection(t, wired, adminAuth, "Globex", integration.ID, redirectURI)

	t.Run("no admin key is unauthorized", func(t *testing.T) {
		status, _ := listConnectionsUnderAdminMount(t, wired, orgAID, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("a wrong admin key is unauthorized", func(t *testing.T) {
		status, _ := listConnectionsUnderAdminMount(t, wired, orgAID, "Bearer wrong-key")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("org A's own API key does not satisfy the admin console mount", func(t *testing.T) {
		status, _ := listConnectionsUnderAdminMount(t, wired, orgAID, orgAAuth)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the console mount is admin-key-only, not org-key", status, http.StatusUnauthorized)
		}
	})

	t.Run("the admin key against org A's path returns only org A's connection", func(t *testing.T) {
		status, page := listConnectionsUnderAdminMount(t, wired, orgAID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		ids := connectionIDsOf(page)
		if !containsID(ids, connA.ID) {
			t.Errorf("items = %v, want them to include org A's connection %q", ids, connA.ID)
		}
		if containsID(ids, connB.ID) {
			t.Errorf("items = %v leaked org B's connection %q into org A's page — the path did not scope the result", ids, connB.ID)
		}
	})

	t.Run("the same admin key against org B's path returns only org B's connection (path is authoritative)", func(t *testing.T) {
		status, page := listConnectionsUnderAdminMount(t, wired, orgBID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		ids := connectionIDsOf(page)
		if !containsID(ids, connB.ID) {
			t.Errorf("items = %v, want them to include org B's connection %q", ids, connB.ID)
		}
		if containsID(ids, connA.ID) {
			t.Errorf("items = %v leaked org A's connection %q into org B's page", ids, connA.ID)
		}
	})

	t.Run("fetching org A's connection id under org B's path is not-found, never a cross-org leak", func(t *testing.T) {
		status := getConnectionUnderAdminMount(t, wired, orgBID, connA.ID, adminAuth)
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("fetching org A's connection id under its own path succeeds", func(t *testing.T) {
		status := getConnectionUnderAdminMount(t, wired, orgAID, connA.ID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
	})

	t.Run("regression: the pre-existing SDK-facing org-key route still lists org A's connection", func(t *testing.T) {
		page := listConnections(t, wired.Router, orgAAuth, "")
		ids := connectionIDsOf(page)
		if !containsID(ids, connA.ID) {
			t.Fatalf("/api/v1/connections items = %v, want them to include %q — the second admin mount must not have disturbed the original org-key route", ids, connA.ID)
		}
	})

	t.Run("the admin mount's Disable is the same handler wired for the SDK route (full reuse, not read-only)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgAID+"/connections/"+connA.ID+"/disable", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var status connectionStatusDTO
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode disable response: %v", err)
		}
		if status.Status != "DISCONNECTED" {
			t.Errorf("status = %q, want %q", status.Status, "DISCONNECTED")
		}
	})
}

// TestAdminConsoleTriggerInstancesMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact
// covers Slice 2's AC4, AC5, AC6, and AC7 for trigger instances: listing and
// fetching under /api/v1/organizations/{orgId}/trigger-instances is scoped by
// the path org id with no cross-org bleed, disable/enable/delete are the same
// handlers reused verbatim, and the pre-existing org-key
// /api/v1/trigger-instances route is unaffected.
func TestAdminConsoleTriggerInstancesMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithTrigger(fakeMS))
	adminAuth := "Bearer " + support.AdminAPIKey

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"client-id","clientSecret":"client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}
	const redirectURI = "https://consumer.example.com/callback"

	orgAID, orgAAuth, instanceA := setUpOrgWithActiveConnectionAndTriggerInstance(t, wired, adminAuth, "Acme", integration.ID, redirectURI)
	orgBID, _, instanceB := setUpOrgWithActiveConnectionAndTriggerInstance(t, wired, adminAuth, "Globex", integration.ID, redirectURI)

	t.Run("no admin key is unauthorized", func(t *testing.T) {
		status, _ := listTriggerInstancesUnderAdminMount(t, wired, orgAID, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("a wrong admin key is unauthorized", func(t *testing.T) {
		status, _ := listTriggerInstancesUnderAdminMount(t, wired, orgAID, "Bearer wrong-key")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("org A's own API key does not satisfy the admin console mount", func(t *testing.T) {
		status, _ := listTriggerInstancesUnderAdminMount(t, wired, orgAID, orgAAuth)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the console mount is admin-key-only, not org-key", status, http.StatusUnauthorized)
		}
	})

	t.Run("the admin key against org A's path returns only org A's trigger instance", func(t *testing.T) {
		status, page := listTriggerInstancesUnderAdminMount(t, wired, orgAID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		ids := triggerInstanceIDsOf(page)
		if !containsID(ids, instanceA.ID) {
			t.Errorf("items = %v, want them to include org A's instance %q", ids, instanceA.ID)
		}
		if containsID(ids, instanceB.ID) {
			t.Errorf("items = %v leaked org B's instance %q into org A's page — the path did not scope the result", ids, instanceB.ID)
		}
	})

	t.Run("the same admin key against org B's path returns only org B's trigger instance (path is authoritative)", func(t *testing.T) {
		status, page := listTriggerInstancesUnderAdminMount(t, wired, orgBID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		ids := triggerInstanceIDsOf(page)
		if !containsID(ids, instanceB.ID) {
			t.Errorf("items = %v, want them to include org B's instance %q", ids, instanceB.ID)
		}
		if containsID(ids, instanceA.ID) {
			t.Errorf("items = %v leaked org A's instance %q into org B's page", ids, instanceA.ID)
		}
	})

	t.Run("fetching org A's instance id under org B's path is not-found, never a cross-org leak", func(t *testing.T) {
		status := getTriggerInstanceUnderAdminMount(t, wired, orgBID, instanceA.ID, adminAuth)
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("regression: the pre-existing SDK-facing org-key route still lists org A's instance", func(t *testing.T) {
		listStatus, page := listTriggerInstances(t, wired, orgAAuth, "")
		if listStatus != http.StatusOK {
			t.Fatalf("status = %d, want %d", listStatus, http.StatusOK)
		}
		ids := triggerInstanceIDsOf(page)
		if !containsID(ids, instanceA.ID) {
			t.Fatalf("/api/v1/trigger-instances items = %v, want them to include %q — the second admin mount must not have disturbed the original org-key route", ids, instanceA.ID)
		}
	})

	t.Run("the admin mount's disable/enable/delete are the same handlers reused verbatim, scoped to org A only", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgAID+"/trigger-instances/"+instanceA.ID+"/disable", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("disable status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var disabled triggerInstanceStatusDTO
		if err := json.Unmarshal(w.Body.Bytes(), &disabled); err != nil {
			t.Fatalf("decode disable response: %v", err)
		}
		if disabled.Status != "DISABLED" {
			t.Errorf("status = %q, want %q", disabled.Status, "DISABLED")
		}

		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgAID+"/trigger-instances/"+instanceA.ID+"/enable", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("enable status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}

		w = doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/organizations/"+orgAID+"/trigger-instances/"+instanceA.ID, adminAuth, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}

		afterDeleteStatus := getTriggerInstanceUnderAdminMount(t, wired, orgAID, instanceA.ID, adminAuth)
		if afterDeleteStatus != http.StatusNotFound {
			t.Fatalf("get-after-delete status = %d, want %d", afterDeleteStatus, http.StatusNotFound)
		}

		// Org B's instance must be entirely undisturbed by everything done to
		// org A's instance above.
		stillStatus := getTriggerInstanceUnderAdminMount(t, wired, orgBID, instanceB.ID, adminAuth)
		if stillStatus != http.StatusOK {
			t.Fatalf("org B's instance status = %d, want %d — it must be unaffected by org A's mutations", stillStatus, http.StatusOK)
		}
	})
}
