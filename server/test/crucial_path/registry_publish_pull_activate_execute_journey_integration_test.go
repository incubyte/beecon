//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, wireErrorEnvelope, doJSONRequest,
// doOperatorAuthRequest/csrfTokenFrom (operator_auth_bootstrap_and_login_/
// operator_auth_logout_expiry_and_revocation_journey_integration_test.go),
// executionResultDTO/executeTool (tool_execution_journey_integration_test.go),
// outlookDefinitionAgainst/oauthJourneyFixture/openConnectPageAndGetState —
// same package). This file tells the registry sub-phase's Slice 1 walking
// skeleton story end to end against two real composition roots: a real
// registryservice HTTP server (support.NewTestRegistryServer, standing in for
// the separately-deployed cmd/registry binary — PD59) and the installation's
// own real router, joined only over the registry's authenticated pull API
// (PD64) — never the reverse (H6). A catalog maintainer publishes a
// single-provider bundle -> the registry mints tool_ ids and assigns 1.0.0
// -> an operator, authenticated with the shipped operator session + CSRF
// (ConsoleAuth), activates that version -> the newly-activated tool executes
// by its tool_ id and by its slug with an identical result shape -> an
// unknown tool_ id is a 404 not_found, distinct from a tool-level failure.
package crucial_path

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"beecon/test/support"
)

type publishedToolIdentityDTO struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type publishBundleResultDTO struct {
	Version     string                     `json:"version"`
	ContentHash string                     `json:"contentHash"`
	Tools       []publishedToolIdentityDTO `json:"tools"`
}

// publishOutlookBundleWithListMessagesTool publishes a single-provider
// "outlook" bundle carrying one tool (outlook-list-messages, pointed at
// fakeGraph instead of the real internet, PD8) to registryServer, returning
// the registry's publish response decoded (assigned version, content hash,
// and every tool's registry-minted identity — Slice 1's publish API).
func publishOutlookBundleWithListMessagesTool(t *testing.T, registryServer *support.TestRegistryServer, fakeGraph *support.FakeGraph) publishBundleResultDTO {
	t.Helper()

	bundleJSON := `{
		"formatVersion": 1,
		"name": "Outlook",
		"logo": "https://static.beecon.dev/providers/outlook.png",
		"authScheme": "oauth2",
		"baseUrl": "",
		"oauth": {"authorizeUrl": "https://fake-microsoft.example.com/oauth2/v2.0/authorize", "tokenUrl": "https://fake-microsoft.example.com/token", "scopes": ["Mail.Read"]},
		"tools": [{
			"slug": "outlook-list-messages",
			"name": "List messages",
			"inputSchema": {"type": "object"},
			"outputSchema": {"type": "object"},
			"sample": {"value": []},
			"mapping": {"method": "GET", "path": "` + fakeGraph.MessagesURL + `"}
		}]
	}`

	req, err := http.NewRequest(http.MethodPost, registryServer.URL+"/registry/v1/providers/outlook/bundles", strings.NewReader(bundleJSON))
	if err != nil {
		t.Fatalf("build publish request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+registryServer.PublishToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := registryServer.Client().Do(req)
	if err != nil {
		t.Fatalf("publish request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("publish status = %d, want %d; body=%s", resp.StatusCode, http.StatusCreated, body)
	}

	var result publishBundleResultDTO
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode publish response: %v; body=%s", err, body)
	}
	return result
}

func TestRegistryWalkingSkeletonJourney_PublishPullActivateExecuteByToolID(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "raw-microsoft-access-token-for-registry-journey", RefreshToken: "raw-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	registryServer := support.NewTestRegistryServer(t, "test-publish-token", "test-registry-api-key")

	// The installation boots with the bare Outlook OAuth definition (no
	// tools yet, PD59: registry pull/activate is additive to the embedded
	// boot path) and is configured to pull from the real registry server
	// above over BEECON_REGISTRY_URL/BEECON_REGISTRY_API_KEY.
	wired := support.BootAppWithProviderDefinitionsAndRegistry(t, outlookDefinitionAgainst(fakeMS), registryServer.URL, registryServer.APIKey)
	adminAuth := "Bearer " + support.AdminAPIKey

	// An operator must exist and be logged in before any console mutation
	// (Slice 1's Activate route is ConsoleAuth-guarded).
	w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
		`{"email":"maintainer@example.com","password":"correct horse battery staple"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap operator: status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	w = doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
		`{"email":"maintainer@example.com","password":"correct horse battery staple"}`, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("login: status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	sessionCookies := w.Result().Cookies()
	csrfToken := csrfTokenFrom(sessionCookies)
	if csrfToken == "" {
		t.Fatal("test fixture bug: no beecon_csrf cookie value captured from login")
	}

	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	var published publishBundleResultDTO
	t.Run("a catalog maintainer publishes a single-provider bundle and the registry assigns version 1.0.0, minting a tool_ id for its tool", func(t *testing.T) {
		published = publishOutlookBundleWithListMessagesTool(t, registryServer, fakeGraph)

		if published.Version != "1.0.0" {
			t.Errorf("version = %q, want %q (a provider's first bundle)", published.Version, "1.0.0")
		}
		if !strings.HasPrefix(published.ContentHash, "sha256:") {
			t.Errorf("contentHash = %q, want a sha256: prefixed hash", published.ContentHash)
		}
		if len(published.Tools) != 1 {
			t.Fatalf("published %d tool identities, want 1", len(published.Tools))
		}
		if published.Tools[0].Slug != "outlook-list-messages" {
			t.Errorf("tools[0].slug = %q, want %q", published.Tools[0].Slug, "outlook-list-messages")
		}
		if !strings.HasPrefix(published.Tools[0].ID, "tool_") {
			t.Errorf("tools[0].id = %q, want a tool_-prefixed id", published.Tools[0].ID)
		}
	})
	toolID := published.Tools[0].ID

	t.Run("activating without any session cookie is rejected", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/registry/providers/outlook/activate", "", `{"version":"1.0.0"}`, nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("activating with the demoted admin key (an operator now exists) is rejected", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/registry/providers/outlook/activate", adminAuth, `{"version":"1.0.0"}`, nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the break-glass admin key must be demoted once an operator exists; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("activating with a session cookie but no X-CSRF-Token header is rejected", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/registry/providers/outlook/activate", "", `{"version":"1.0.0"}`, sessionCookies)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("an operator authenticated with session + CSRF activates the pulled version, which the catalog immediately starts serving", func(t *testing.T) {
		req := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/registry/providers/outlook/activate", csrfToken, `{"version":"1.0.0"}`, sessionCookies)
		if req.Code != http.StatusOK {
			t.Fatalf("activate status = %d, want %d; body=%s", req.Code, http.StatusOK, req.Body.String())
		}
		var dto struct {
			ActiveVersion string `json:"activeVersion"`
		}
		if err := json.Unmarshal(req.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode activate response: %v; body=%s", err, req.Body.String())
		}
		if dto.ActiveVersion != "1.0.0" {
			t.Errorf("activeVersion = %q, want %q", dto.ActiveVersion, "1.0.0")
		}
	})

	var resultBySlug, resultByID executionResultDTO
	t.Run("the newly-activated tool executes by its slug", func(t *testing.T) {
		status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d; dto=%+v", status, http.StatusOK, dto)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		resultBySlug = dto
	})

	t.Run("the same tool executes by its registry-minted tool_ id, producing an identical result shape", func(t *testing.T) {
		status, dto := executeTool(t, wired, fixture.orgAuth, toolID, fixture.userID, initiated.ID, `{}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d; dto=%+v", status, http.StatusOK, dto)
		}
		resultByID = dto

		if resultByID.Successful != resultBySlug.Successful {
			t.Errorf("successful (by id) = %v, want %v (by slug)", resultByID.Successful, resultBySlug.Successful)
		}
		bySlugJSON, _ := json.Marshal(resultBySlug)
		byIDJSON, _ := json.Marshal(resultByID)
		if string(bySlugJSON) != string(byIDJSON) {
			t.Errorf("execute-by-id result %s does not match execute-by-slug result %s", byIDJSON, bySlugJSON)
		}
	})

	t.Run("executing an unknown tool_ id returns a not-found error distinct from an execution failure", func(t *testing.T) {
		status, _ := executeTool(t, wired, fixture.orgAuth, "tool_doesnotexistatall00000000", fixture.userID, initiated.ID, `{}`)
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})
}
