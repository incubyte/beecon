//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header). This file reuses organizationDTO, wireErrorEnvelope,
// and doJSONRequest already declared there.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"beecon/test/support"
)

type issuedKeyDTO struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

type userDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ExternalID string `json:"externalId"`
	CreatedAt  string `json:"createdAt"`
}

// TestAccessAndUsersJourney tells the Slice 2 story end to end against the
// real composition root: boot -> create two orgs -> issue a key per org
// (secret present only at issue; list stays clean) -> create a user with
// org A's key -> fetch it back with A's key -> the same user is not-found
// through org B's key -> requests with no/malformed/unknown keys are
// unauthorized -> revoking org A's key locks org A out -> org B is
// unaffected.
func TestAccessAndUsersJourney(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	var orgA, orgB organizationDTO
	t.Run("creating two organizations", func(t *testing.T) {
		wA := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Org A"}`)
		if wA.Code != http.StatusCreated {
			t.Fatalf("create org A status = %d, want %d; body=%s", wA.Code, http.StatusCreated, wA.Body.String())
		}
		if err := json.Unmarshal(wA.Body.Bytes(), &orgA); err != nil {
			t.Fatalf("decode org A: %v", err)
		}

		wB := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Org B"}`)
		if wB.Code != http.StatusCreated {
			t.Fatalf("create org B status = %d, want %d; body=%s", wB.Code, http.StatusCreated, wB.Body.String())
		}
		if err := json.Unmarshal(wB.Body.Bytes(), &orgB); err != nil {
			t.Fatalf("decode org B: %v", err)
		}
	})

	var keyA, keyB issuedKeyDTO
	t.Run("issuing an api key for each org returns the full secret exactly once", func(t *testing.T) {
		wA := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgA.ID+"/api-keys", adminAuth, "")
		if wA.Code != http.StatusCreated {
			t.Fatalf("issue key A status = %d, want %d; body=%s", wA.Code, http.StatusCreated, wA.Body.String())
		}
		if err := json.Unmarshal(wA.Body.Bytes(), &keyA); err != nil {
			t.Fatalf("decode key A: %v", err)
		}
		if !strings.HasPrefix(keyA.Key, "beecon_sk_") {
			t.Errorf("key A secret = %q, want it to start with %q", keyA.Key, "beecon_sk_")
		}

		wB := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgB.ID+"/api-keys", adminAuth, "")
		if wB.Code != http.StatusCreated {
			t.Fatalf("issue key B status = %d, want %d; body=%s", wB.Code, http.StatusCreated, wB.Body.String())
		}
		if err := json.Unmarshal(wB.Body.Bytes(), &keyB); err != nil {
			t.Fatalf("decode key B: %v", err)
		}
	})

	t.Run("listing org A's keys never shows the secret", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgA.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("list keys status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var entries []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode list: %v; body=%s", err, w.Body.String())
		}
		if len(entries) != 1 {
			t.Fatalf("len(entries) = %d, want 1", len(entries))
		}
		if _, present := entries[0]["key"]; present {
			t.Fatalf("list entry %+v carries the full secret — it must never appear outside of Issue's response", entries[0])
		}
		if entries[0]["prefix"] != keyA.Prefix {
			t.Errorf("listed prefix = %v, want %q", entries[0]["prefix"], keyA.Prefix)
		}
	})

	orgAAuth := "Bearer " + keyA.Key
	orgBAuth := "Bearer " + keyB.Key
	var createdUser userDTO

	t.Run("creating a user with org A's key scopes it to org A", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAAuth, `{"name":"Ada Lovelace","externalId":"ext-1"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &createdUser); err != nil {
			t.Fatalf("decode created user: %v", err)
		}
		if !strings.HasPrefix(createdUser.ID, "user_") {
			t.Errorf("id = %q, want it to start with %q", createdUser.ID, "user_")
		}
	})

	t.Run("fetching the user with its own org's key succeeds", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+createdUser.ID, orgAAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("fetching org A's user with org B's key is not-found, not forbidden", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+createdUser.ID, orgBAuth, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "not_found" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
		}
	})

	t.Run("no key is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+createdUser.ID, "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("a malformed authorization header is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+createdUser.ID, keyA.Key, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("an unknown key is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+createdUser.ID, "Bearer beecon_sk_never-issued-anywhere", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("revoking org A's key locks org A out", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/organizations/"+orgA.ID+"/api-keys/"+keyA.ID, adminAuth, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("revoke status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}

		afterRevoke := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+createdUser.ID, orgAAuth, "")
		if afterRevoke.Code != http.StatusUnauthorized {
			t.Fatalf("status after revoke = %d, want %d; body=%s", afterRevoke.Code, http.StatusUnauthorized, afterRevoke.Body.String())
		}
	})

	t.Run("org B's key still works after org A's key was revoked", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgBAuth, `{"name":"Grace Hopper"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}
