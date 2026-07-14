//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, wireErrorEnvelope, doJSONRequest,
// and userDTO from files already declared there). This file tells Slice 4's
// list-users-per-org story end to end against the real composition root
// (PD40): the new GET /api/v1/organizations/{orgId}/users read is
// admin-guarded (never an org API key — it's the console's own mount, not
// the SDK's), cursor-paginated, and strictly scoped by the {orgId} in the
// path, matching every other admin-console mount's own isolation guarantee
// (Slice 2's connections/trigger-instances precedent).
package crucial_path

import (
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/app"
	"beecon/test/support"
)

type usersPageDTO struct {
	Items      []userDTO `json:"items"`
	NextCursor string    `json:"nextCursor"`
}

func listUsersUnderAdminMount(t *testing.T, wired *app.Wired, orgID, authHeader, query string) (int, usersPageDTO) {
	t.Helper()
	path := "/api/v1/organizations/" + orgID + "/users"
	if query != "" {
		path += "?" + query
	}
	w := doJSONRequest(t, wired.Router, http.MethodGet, path, authHeader, "")
	var page usersPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode users page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func createOrgWithKey(t *testing.T, wired *app.Wired, adminAuth, name string) (orgID, orgAuth string) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"`+name+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org %q status = %d, want %d; body=%s", name, w.Code, http.StatusCreated, w.Body.String())
	}
	var org organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org %q: %v", name, err)
	}

	var key issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key for %q status = %d, want %d; body=%s", name, w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatalf("decode key for %q: %v", name, err)
	}
	return org.ID, "Bearer " + key.Key
}

func userNamesOf(page usersPageDTO) []string {
	names := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		names = append(names, item.Name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// TestListUsersPerOrgJourney_TheAdminConsoleListsExactlyOneOrgsUsersScopedByThePathId
// covers Slice 4's AC1 at the wire level: an operator holding only the
// admin key lists an organization's end-users under
// /api/v1/organizations/{orgId}/users, scoped strictly by the {orgId} in the
// path — no admin key, a wrong admin key, and the org's own API key are all
// rejected (the console mount is admin-key-only, matching Slice 2's own
// AdminOrgScope precedent), and one org's users never leak into another
// org's page.
func TestListUsersPerOrgJourney_TheAdminConsoleListsExactlyOneOrgsUsersScopedByThePathId(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	orgAID, orgAAuth := createOrgWithKey(t, wired, adminAuth, "Acme")
	orgBID, orgBAuth := createOrgWithKey(t, wired, adminAuth, "Globex")

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org A user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAAuth, `{"name":"Grace Hopper"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org A second user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgBAuth, `{"name":"Bob Noxious"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org B user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	t.Run("no admin key is unauthorized", func(t *testing.T) {
		status, _ := listUsersUnderAdminMount(t, wired, orgAID, "", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("a wrong admin key is unauthorized", func(t *testing.T) {
		status, _ := listUsersUnderAdminMount(t, wired, orgAID, "Bearer wrong-key", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("org A's own API key does not satisfy the admin console mount", func(t *testing.T) {
		status, _ := listUsersUnderAdminMount(t, wired, orgAID, orgAAuth, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the console mount is admin-key-only, not org-key", status, http.StatusUnauthorized)
		}
	})

	t.Run("the admin key against org A's path returns only org A's two users", func(t *testing.T) {
		status, page := listUsersUnderAdminMount(t, wired, orgAID, adminAuth, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		names := userNamesOf(page)
		if len(names) != 2 {
			t.Fatalf("items = %v, want exactly 2 (org A's own users)", names)
		}
		if !containsName(names, "Ada Lovelace") || !containsName(names, "Grace Hopper") {
			t.Errorf("items = %v, want them to include both of org A's users", names)
		}
		if containsName(names, "Bob Noxious") {
			t.Errorf("items = %v leaked org B's user into org A's page — the path did not scope the result", names)
		}
	})

	t.Run("the same admin key against org B's path returns only org B's user (path is authoritative)", func(t *testing.T) {
		status, page := listUsersUnderAdminMount(t, wired, orgBID, adminAuth, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		names := userNamesOf(page)
		if len(names) != 1 || names[0] != "Bob Noxious" {
			t.Fatalf("items = %v, want exactly [\"Bob Noxious\"]", names)
		}
	})

	t.Run("cursor pagination: a limit of 1 returns one row and a usable nextCursor that reaches the second", func(t *testing.T) {
		status, firstPage := listUsersUnderAdminMount(t, wired, orgAID, adminAuth, "limit=1")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(firstPage.Items) != 1 {
			t.Fatalf("first page items = %v, want exactly 1", firstPage.Items)
		}
		if firstPage.NextCursor == "" {
			t.Fatal("nextCursor is empty, want a cursor for org A's second user")
		}

		status, secondPage := listUsersUnderAdminMount(t, wired, orgAID, adminAuth, "cursor="+firstPage.NextCursor)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(secondPage.Items) != 1 {
			t.Fatalf("second page items = %v, want exactly 1", secondPage.Items)
		}
		if secondPage.Items[0].ID == firstPage.Items[0].ID {
			t.Fatal("second page repeated the first page's row instead of advancing past it")
		}
	})

	t.Run("creating a user through the console's own POST under the admin mount reuses CreateUser verbatim", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgAID+"/users", adminAuth, `{"name":"Created From Console"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}

		status, page := listUsersUnderAdminMount(t, wired, orgAID, adminAuth, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !containsName(userNamesOf(page), "Created From Console") {
			t.Errorf("items = %v, want them to include the user just created through the console mount", userNamesOf(page))
		}
	})
}
