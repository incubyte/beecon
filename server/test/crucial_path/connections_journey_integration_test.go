//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// wireErrorEnvelope, and doJSONRequest already declared there). This file
// tells the Slice 3 story end to end against the real composition root: the
// installation admin creates an Outlook Integration and sets an
// organization's redirect-uri allow-list; the consumer lists integrations,
// creates a user, and initiates a connection whose redirectUrl is bound to a
// single-use token; cross-org access to users/integrations/connections is
// indistinguishable from not-found.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"beecon/test/support"
)

type integrationSummaryDTO struct {
	ID           string `json:"id"`
	ProviderSlug string `json:"providerSlug"`
	Name         string `json:"name"`
	Logo         string `json:"logo"`
	AuthScheme   string `json:"authScheme"`
}

type initiatedConnectionDTO struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	RedirectURL string `json:"redirectUrl"`
}

type connectionDTO struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ProviderSlug string `json:"providerSlug"`
	UserID       string `json:"userId"`
	CreatedAt    string `json:"createdAt"`
}

func TestOutlookCatalogAndConnectionInitiateJourney(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	const clientSecret = "super-secret-outlook-client-secret"

	var org organizationDTO
	t.Run("creating an organization", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
			t.Fatalf("decode org: %v", err)
		}
	})

	var integration integrationSummaryDTO
	t.Run("installation admin creates an Outlook Integration with OAuth client credentials", func(t *testing.T) {
		body := `{"providerSlug":"outlook","clientId":"client-id","clientSecret":"` + clientSecret + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if strings.Contains(w.Body.String(), clientSecret) {
			t.Fatalf("create-integration response %s contains the client secret — AC4 requires it never appear in any API response after creation", w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
			t.Fatalf("decode integration: %v", err)
		}
		if !strings.HasPrefix(integration.ID, "intg_") {
			t.Errorf("id = %q, want it to start with %q", integration.ID, "intg_")
		}
		if integration.ProviderSlug != "outlook" {
			t.Errorf("providerSlug = %q, want %q", integration.ProviderSlug, "outlook")
		}
		if integration.Name != "Outlook" {
			t.Errorf("name = %q, want %q", integration.Name, "Outlook")
		}
	})

	const allowedRedirectURI = "https://consumer.example.com/callback"
	t.Run("installation admin sets the organization's allowed redirect URIs", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
			`{"allowedRedirectUris":["`+allowedRedirectURI+`"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	var orgKey issuedKeyDTO
	t.Run("issuing an API key for the organization", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
			t.Fatalf("decode key: %v", err)
		}
	})
	orgAuth := "Bearer " + orgKey.Key

	t.Run("consumer lists integrations available to its organization, secret never present", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/", orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		if strings.Contains(w.Body.String(), clientSecret) {
			t.Fatalf("list-integrations response %s contains the client secret", w.Body.String())
		}
		var list []integrationSummaryDTO
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v; body=%s", err, w.Body.String())
		}
		found := false
		for _, i := range list {
			if i.ID == integration.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("list %+v does not include the created integration %q", list, integration.ID)
		}
	})

	var user userDTO
	t.Run("consumer creates a user in its organization", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
			t.Fatalf("decode user: %v", err)
		}
	})

	var initiated initiatedConnectionDTO
	t.Run("consumer initiates a connection with an allowed redirectUri", func(t *testing.T) {
		body := `{"userId":"` + user.ID + `","integrationId":"` + integration.ID + `","redirectUri":"` + allowedRedirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &initiated); err != nil {
			t.Fatalf("decode initiated connection: %v", err)
		}
		if !strings.HasPrefix(initiated.ID, "conn_") {
			t.Errorf("id = %q, want it to start with %q", initiated.ID, "conn_")
		}
		if initiated.Status != "INITIATED" {
			t.Errorf("status = %q, want %q", initiated.Status, "INITIATED")
		}
		if !strings.Contains(initiated.RedirectURL, "/connect/") {
			t.Errorf("redirectUrl = %q, want it to point at Beecon's own connect page", initiated.RedirectURL)
		}
	})

	t.Run("a second initiate for the same user+integration gets its own distinct token and redirectUrl", func(t *testing.T) {
		body := `{"userId":"` + user.ID + `","integrationId":"` + integration.ID + `","redirectUri":"` + allowedRedirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var second initiatedConnectionDTO
		if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
			t.Fatalf("decode second initiated connection: %v", err)
		}
		if second.ID == initiated.ID {
			t.Fatalf("second initiate reused the first connection's id %q — each initiate must mint a fresh connection", initiated.ID)
		}
		if second.RedirectURL == initiated.RedirectURL {
			t.Errorf("second initiate's redirectUrl %q is identical to the first's — each must be bound to its own single-use token", second.RedirectURL)
		}
	})

	t.Run("initiating with a redirectUri not on the allow-list is a validation error", func(t *testing.T) {
		body := `{"userId":"` + user.ID + `","integrationId":"` + integration.ID + `","redirectUri":"https://evil.example.com/callback"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
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

	t.Run("initiating with an unknown userId is not-found", func(t *testing.T) {
		body := `{"userId":"user_does_not_exist","integrationId":"` + integration.ID + `","redirectUri":"` + allowedRedirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("initiating with an unknown integrationId is not-found", func(t *testing.T) {
		body := `{"userId":"` + user.ID + `","integrationId":"intg_does_not_exist","redirectUri":"` + allowedRedirectURI + `"}`
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("consumer fetches the connection by id, seeing status/provider/user, no account yet", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var got connectionDTO
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode connection: %v", err)
		}
		if got.ID != initiated.ID {
			t.Errorf("id = %q, want %q", got.ID, initiated.ID)
		}
		if got.Status != "INITIATED" {
			t.Errorf("status = %q, want %q", got.Status, "INITIATED")
		}
		if got.ProviderSlug != "outlook" {
			t.Errorf("providerSlug = %q, want %q", got.ProviderSlug, "outlook")
		}
		if got.UserID != user.ID {
			t.Errorf("userId = %q, want %q", got.UserID, user.ID)
		}
		var raw map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode raw body: %v", err)
		}
		if _, present := raw["account"]; present {
			t.Errorf("connection response %s carries an account field before the OAuth handshake (Slice 4) has run", w.Body.String())
		}
	})

	t.Run("a second organization cannot see the first organization's user, integration list, or connection", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Second Org"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var otherOrg organizationDTO
		if err := json.Unmarshal(w.Body.Bytes(), &otherOrg); err != nil {
			t.Fatalf("decode other org: %v", err)
		}
		// Give org B the same allow-listed redirectUri as org A, so the
		// cross-org initiate below is rejected because the user belongs to
		// org A (not because org B's own allow-list is empty).
		w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+otherOrg.ID, adminAuth,
			`{"allowedRedirectUris":["`+allowedRedirectURI+`"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("patch other org status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+otherOrg.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var otherKey issuedKeyDTO
		if err := json.Unmarshal(w.Body.Bytes(), &otherKey); err != nil {
			t.Fatalf("decode other key: %v", err)
		}
		otherAuth := "Bearer " + otherKey.Key

		t.Run("fetching org A's user with org B's key is not-found", func(t *testing.T) {
			w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/users/"+user.ID, otherAuth, "")
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
			}
		})

		t.Run("initiating a connection under org B for org A's user is not-found", func(t *testing.T) {
			body := `{"userId":"` + user.ID + `","integrationId":"` + integration.ID + `","redirectUri":"` + allowedRedirectURI + `"}`
			w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", otherAuth, body)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
			}
		})

		t.Run("fetching org A's connection with org B's key is not-found", func(t *testing.T) {
			w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, otherAuth, "")
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
	})
}
