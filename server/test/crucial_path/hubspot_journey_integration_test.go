//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, connectionWithAccountDTO,
// wireErrorEnvelope, doJSONRequest, oauthJourneyFixture,
// openConnectPageAndGetState, executionErrorDTO, toolsPageDTO, listTools —
// same package). This file tells Slice 2's story end to end against the real
// composition root: Hubspot, the second provider, arrives purely as a
// definition file (no provider-specific Go code); the installation admin
// creates a Hubspot Integration whose client secret is stored encrypted; an
// end user completes Hubspot OAuth through the same middle-man pages into an
// ACTIVE connection carrying account email/hub-domain metadata; a denied
// consent leaves the connection INITIATED; hubspot-list-contacts pages with
// the canonical pageSize/cursor convention; hubspot-create-contact proves
// JSON body mapping and surfaces upstream errors as tool-level failures; and
// a database dump — including a simulated Phase 1 plaintext row — carries no
// plaintext client secret anywhere.
package crucial_path

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/internal/catalog"
	catalogbun "beecon/internal/catalog/driven/bun"
	"beecon/test/support"
)

// executionResultWithCursorDTO is executionResultDTO
// (tool_execution_journey_integration_test.go) plus nextCursor (PD15b) —
// declared separately here since this file is the only one that needs to
// observe pagination's cursor field.
type executionResultWithCursorDTO struct {
	Successful bool               `json:"successful"`
	Error      *executionErrorDTO `json:"error"`
	Data       any                `json:"data"`
	NextCursor string             `json:"nextCursor"`
}

func executeHubspotTool(t *testing.T, wired *app.Wired, orgAuth, slug, userID, connectionID, argumentsJSON string) (int, executionResultWithCursorDTO) {
	t.Helper()
	body := fmt.Sprintf(`{"userId":%q,"connectionId":%q,"arguments":%s}`, userID, connectionID, argumentsJSON)
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/tools/"+slug+"/execute", orgAuth, body)
	var dto executionResultWithCursorDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode execution result: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

// hubspotDefinitionAgainst is Hubspot's real hubspot.yaml shape, re-expressed
// as a catalog.ProviderDefinition pointed at fh instead of the real internet:
// credentialStyle/userInfo (PD13/PD16), the pagination-declaring
// hubspot-list-contacts, and the JSON-body-mapping hubspot-create-contact.
func hubspotDefinitionAgainst(fh *support.FakeHubspot) []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:            "hubspot",
			Name:            "Hubspot",
			Logo:            "https://static.beecon.dev/providers/hubspot.png",
			AuthScheme:      "oauth2",
			BaseURL:         fh.BaseURL,
			AuthorizeURL:    "https://fake-hubspot.example.com/oauth/authorize",
			TokenURL:        fh.TokenURL,
			UserInfoURL:     fh.UserInfoURLTemplate,
			Scopes:          []string{"crm.objects.contacts.read", "crm.objects.contacts.write", "files"},
			CredentialStyle: catalog.CredentialStyleFormBody,
			UserInfo:        catalog.UserInfoMapping{EmailField: "user", DisplayNameField: "hub_domain"},
			Tools: []catalog.ProviderTool{
				{
					Slug:        "hubspot-list-contacts",
					Name:        "List contacts",
					Description: "List CRM contacts, cursor-paginated.",
					Method:      "GET",
					Path:        "/crm/v3/objects/contacts",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"pageSize": map[string]any{"type": "integer"},
							"cursor":   map[string]any{"type": "string"},
						},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Pagination: &catalog.Pagination{PageSizeParam: "limit", CursorParam: "after", NextCursorPath: "paging.next.after"},
					},
				},
				{
					Slug:        "hubspot-create-contact",
					Name:        "Create contact",
					Description: "Create a CRM contact.",
					Method:      "POST",
					Path:        "/crm/v3/objects/contacts",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"email":     map[string]any{"type": "string"},
							"firstname": map[string]any{"type": "string"},
							"lastname":  map[string]any{"type": "string"},
						},
						"required": []any{"email"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Body: map[string]string{
							"properties.email":     "{input.email}",
							"properties.firstname": "{input.firstname}",
							"properties.lastname":  "{input.lastname}",
						},
					},
				},
				{
					// hubspot-upload-file (Slice 7, PD22, AC4): the file input
					// resolves org-scoped to previously uploaded bytes and streams
					// to fh as multipart form data.
					Slug:        "hubspot-upload-file",
					Name:        "Upload file",
					Description: "Upload a file to Hubspot's file manager.",
					Method:      "POST",
					Path:        "/files/v3/files",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file": map[string]any{"type": "string"},
						},
						"required": []any{"file"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						FileInputs: []string{"file"},
					},
				},
			},
		},
	}
}

// newHubspotJourneyFixture is newOAuthJourneyFixture
// (oauth_handshake_journey_integration_test.go), re-pointed at the "hubspot"
// providerSlug — every other scaffolding step (org, allow-listed redirect
// URI, org key, user) is provider-agnostic, so it returns the same
// oauthJourneyFixture type and reuses its initiate/getConnection methods.
func newHubspotJourneyFixture(t *testing.T, wired *app.Wired) oauthJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/hubspot-callback"

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme Hubspot"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"hubspot","clientId":"hubspot-client-id","clientSecret":"hubspot-client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
		`{"allowedRedirectUris":["`+allowedRedirectURI+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set allow-list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	orgAuth := "Bearer " + orgKey.Key

	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	return oauthJourneyFixture{
		orgAuth:            orgAuth,
		userID:             user.ID,
		integrationID:      integration.ID,
		allowedRedirectURI: allowedRedirectURI,
	}
}

// activateHubspotConnection drives initiate -> connect page -> callback
// through live HTTP requests, mirroring
// activateConnectionThroughRealHandshake (tool_execution_journey_...) but
// against the Hubspot fixture/definition.
func activateHubspotConnection(t *testing.T, wired *app.Wired, fixture oauthJourneyFixture) initiatedConnectionDTO {
	t.Helper()
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (handshake must complete); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	return initiated
}

// TestHubspotJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode is AC1:
// booted against the real embedded providers/ directory (not a fake), the
// catalog API lists Hubspot's three tools with their schemas intact — proving
// the second provider arrived purely as a definition file.
func TestHubspotJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}
	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	orgAuth := "Bearer " + orgKey.Key

	status, page := listTools(t, wired, orgAuth, "?providerSlug=hubspot&includeDeprecated=true")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	wantSlugs := map[string]bool{"hubspot-list-contacts": false, "hubspot-create-contact": false, "hubspot-upload-file": false}
	for _, item := range page.Items {
		if _, declared := wantSlugs[item.Slug]; declared {
			wantSlugs[item.Slug] = true
		}
		if item.Provider.Slug != "hubspot" {
			t.Errorf("item %q provider.slug = %q, want %q", item.Slug, item.Provider.Slug, "hubspot")
		}
		if len(item.InputSchema) == 0 || len(item.OutputSchema) == 0 {
			t.Errorf("item %q has an empty input/output schema", item.Slug)
		}
	}
	for slug, found := range wantSlugs {
		if !found {
			t.Errorf("tools list %+v is missing %q", page.Items, slug)
		}
	}
}

// TestHubspotJourney_ConnectAndActivate covers AC2 (intg_-prefixed id), AC4
// (stable conn_ id), AC5 (scopes + single-use CSRF state), AC6 (account
// email/hub-domain metadata), and AC3's write-path half (the Integration's
// client secret is never echoed in the create response).
func TestHubspotJourney_ConnectAndActivate(t *testing.T) {
	const rawAccessToken = "raw-hubspot-access-token-value"
	const rawRefreshToken = "raw-hubspot-refresh-token-value"
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{
		AccessToken:  rawAccessToken,
		RefreshToken: rawRefreshToken,
		AccountEmail: "ada@acmehubspot.example.com",
		HubDomain:    "acmehubspot.hubspot.com",
	})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &org)

	const clientSecret = "hubspot-client-secret-must-never-leak"
	var integration integrationSummaryDTO
	t.Run("installation admin creates a Hubspot Integration with an intg_-prefixed id and the secret is never echoed", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
			`{"providerSlug":"hubspot","clientId":"hubspot-client-id","clientSecret":"`+clientSecret+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if strings.Contains(w.Body.String(), clientSecret) {
			t.Fatalf("create-integration response %s contains the client secret", w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
			t.Fatalf("decode integration: %v", err)
		}
		if !strings.HasPrefix(integration.ID, "intg_") {
			t.Errorf("id = %q, want it to start with %q", integration.ID, "intg_")
		}
		if integration.ProviderSlug != "hubspot" {
			t.Errorf("providerSlug = %q, want %q", integration.ProviderSlug, "hubspot")
		}
	})

	w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
		`{"allowedRedirectUris":["https://consumer.example.com/hubspot-callback"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set allow-list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &orgKey)
	orgAuth := "Bearer " + orgKey.Key
	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &user)

	fixture := oauthJourneyFixture{
		orgAuth:            orgAuth,
		userID:             user.ID,
		integrationID:      integration.ID,
		allowedRedirectURI: "https://consumer.example.com/hubspot-callback",
	}
	initiated := fixture.initiate(t, wired)

	var state string
	t.Run("the consent redirect carries the definition's scopes and a single-use CSRF state parameter", func(t *testing.T) {
		state = openConnectPageAndGetState(t, wired, initiated)
		if state == "" {
			t.Fatal("connect page carried no CSRF state")
		}
	})

	t.Run("completing the handshake activates the connection under its stable conn_ id", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
		if w.Code != http.StatusFound {
			t.Fatalf("callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
		}
		location := w.Header().Get("Location")
		if strings.Contains(location, rawAccessToken) || strings.Contains(location, rawRefreshToken) {
			t.Fatalf("callback redirect Location %q must never contain a raw token", location)
		}
		if !strings.HasPrefix(initiated.ID, "conn_") {
			t.Errorf("connection id = %q, want it to start with %q", initiated.ID, "conn_")
		}
	})

	t.Run("get-connection shows ACTIVE status and the account email/hub-domain metadata", func(t *testing.T) {
		got := fixture.getConnection(t, wired, initiated.ID)
		if got.ID != initiated.ID {
			t.Errorf("id = %q, want the stable id %q", got.ID, initiated.ID)
		}
		if got.Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", got.Status, "ACTIVE")
		}
		if got.Account == nil {
			t.Fatal("account is nil, want the captured profile")
		}
		if got.Account.Email != "ada@acmehubspot.example.com" {
			t.Errorf("account.email = %q, want %q", got.Account.Email, "ada@acmehubspot.example.com")
		}
		if got.Account.DisplayName != "acmehubspot.hubspot.com" {
			t.Errorf("account.displayName (hub domain) = %q, want %q", got.Account.DisplayName, "acmehubspot.hubspot.com")
		}
	})
}

// TestHubspotJourney_ConsentDeniedLeavesConnectionInitiated is AC7: the
// browser returns to the consumer's own redirectUri with an error status, and
// the connection never activates.
func TestHubspotJourney_ConsentDeniedLeavesConnectionInitiated(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?state="+state+"&error=access_denied", "", "")

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect back to consumer even on denial); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "status=error") {
		t.Errorf("redirect Location %q does not carry status=error", location)
	}
	if !strings.Contains(location, "connectionId="+initiated.ID) {
		t.Errorf("redirect Location %q does not carry connectionId=%s", location, initiated.ID)
	}
	got := fixture.getConnection(t, wired, initiated.ID)
	if got.Status != "INITIATED" {
		t.Errorf("status after consent denial = %q, want it to remain %q", got.Status, "INITIATED")
	}
}

// TestHubspotJourney_ListContactsPagesWithTheCanonicalPageSizeAndCursorConvention
// is AC8/AC9: the first page returns contacts and a nextCursor; feeding that
// cursor back in as the next call's canonical "cursor" argument fetches the
// following page without repeating any contact.
func TestHubspotJourney_ListContactsPagesWithTheCanonicalPageSizeAndCursorConvention(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com",
		Contacts: []support.FakeHubspotContact{
			{ID: "contact-1", Properties: map[string]string{"email": "one@example.com"}},
			{ID: "contact-2", Properties: map[string]string{"email": "two@example.com"}},
			{ID: "contact-3", Properties: map[string]string{"email": "three@example.com"}},
		},
	})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	var firstPageIDs, secondPageIDs []string
	var nextCursor string
	t.Run("executing hubspot-list-contacts with pageSize returns contacts and a nextCursor", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-list-contacts", fixture.userID, initiated.ID, `{"pageSize":2}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		if dto.Error != nil {
			t.Errorf("error = %+v, want nil", dto.Error)
		}
		if dto.NextCursor == "" {
			t.Fatal("nextCursor is empty, want a cursor since a further page remains")
		}
		nextCursor = dto.NextCursor
		firstPageIDs = contactIDsFromData(t, dto.Data)
		if len(firstPageIDs) != 2 {
			t.Fatalf("first page returned %d contacts, want 2", len(firstPageIDs))
		}
		if got := fakeHubspot.LastContactsQuery.Get("limit"); got != "2" {
			t.Errorf("Hubspot received limit=%q, want %q (canonical pageSize mapped to Hubspot's own param)", got, "2")
		}
	})

	t.Run("feeding the nextCursor back in as the canonical cursor argument fetches the following page", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-list-contacts", fixture.userID, initiated.ID,
			fmt.Sprintf(`{"pageSize":2,"cursor":%q}`, nextCursor))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		secondPageIDs = contactIDsFromData(t, dto.Data)
		if len(secondPageIDs) != 1 {
			t.Fatalf("second page returned %d contacts, want 1 (the third and last contact)", len(secondPageIDs))
		}
		if dto.NextCursor != "" {
			t.Errorf("nextCursor = %q, want empty — the last page carries no further cursor", dto.NextCursor)
		}
		if got := fakeHubspot.LastContactsQuery.Get("after"); got != nextCursor {
			t.Errorf(`Hubspot received after=%q, want the previous page's nextCursor %q`, got, nextCursor)
		}
	})

	t.Run("the two pages together cover every contact exactly once", func(t *testing.T) {
		seen := map[string]bool{}
		for _, id := range append(firstPageIDs, secondPageIDs...) {
			if seen[id] {
				t.Fatalf("contact id %q seen more than once across the two pages", id)
			}
			seen[id] = true
		}
		if len(seen) != 3 {
			t.Fatalf("walked %d contacts across both pages, want exactly 3", len(seen))
		}
	})
}

// contactIDsFromData extracts each contact's "id" from a decoded
// hubspot-list-contacts Result.Data ({"results":[{"id":...}, ...]}).
func contactIDsFromData(t *testing.T, data any) []string {
	t.Helper()
	object, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want a decoded JSON object", data)
	}
	results, ok := object["results"].([]any)
	if !ok {
		t.Fatalf(`data["results"] = %T, want an array`, object["results"])
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		contact, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("result entry = %T, want an object", r)
		}
		id, _ := contact["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestHubspotJourney_CreateContactBuildsTheJSONBodyFromTheFlatInputSchema is
// AC10's happy path: hubspot-create-contact's dotted body mapping
// ("properties.email": "{input.email}", ...) builds the nested
// {"properties": {...}} JSON shape Hubspot's API requires, and the created
// contact comes back as Result.Data.
func TestHubspotJourney_CreateContactBuildsTheJSONBodyFromTheFlatInputSchema(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-create-contact", fixture.userID, initiated.ID,
		`{"email":"grace@example.com","firstname":"Grace","lastname":"Hopper"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	data, ok := dto.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want the created contact object", dto.Data)
	}
	if data["id"] != "contact-created-1" {
		t.Errorf("data.id = %v, want %q", data["id"], "contact-created-1")
	}

	if fakeHubspot.LastCreateContactBody == nil {
		t.Fatal("Hubspot received no create-contact body")
	}
	properties, ok := fakeHubspot.LastCreateContactBody["properties"].(map[string]any)
	if !ok {
		t.Fatalf(`Hubspot's received body["properties"] = %T, want the nested object PD16's body mapping builds`, fakeHubspot.LastCreateContactBody["properties"])
	}
	if properties["email"] != "grace@example.com" {
		t.Errorf(`properties.email = %v, want %q`, properties["email"], "grace@example.com")
	}
	if properties["firstname"] != "Grace" {
		t.Errorf(`properties.firstname = %v, want %q`, properties["firstname"], "Grace")
	}
	if properties["lastname"] != "Hopper" {
		t.Errorf(`properties.lastname = %v, want %q`, properties["lastname"], "Hopper")
	}
}

// TestHubspotJourney_CreateContactUpstreamErrorSurfacesAsTheProvidersStatusAndMessage
// is AC10's failure half: an upstream Hubspot rejection is a tool-level
// failure carrying the provider's own status and message, not a platform HTTP
// error.
func TestHubspotJourney_CreateContactUpstreamErrorSurfacesAsTheProvidersStatusAndMessage(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com",
		CreateStatus: http.StatusConflict, CreateBody: `{"message":"Contact already exists with email grace@example.com"}`,
	})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-create-contact", fixture.userID, initiated.ID,
		`{"email":"grace@example.com"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (an upstream error is a tool-level failure, not an HTTP error)", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for an upstream 409 response")
	}
	if dto.Data != nil {
		t.Errorf("data = %v, want nil for a failed execution", dto.Data)
	}
	if dto.Error == nil {
		t.Fatal("error is nil, want the provider's status and message")
	}
	if !strings.Contains(dto.Error.Message, "409") {
		t.Errorf("error.message = %q, want it to surface the provider's status code", dto.Error.Message)
	}
	if !strings.Contains(dto.Error.Message, "already exists") {
		t.Errorf("error.message = %q, want it to surface the provider's response body", dto.Error.Message)
	}
}

// TestHubspotJourney_DatabaseDumpContainsNoPlaintextClientSecretIncludingMigratedPhase1Rows
// is AC3: every Integration client secret persisted in the real SQLite
// database is ciphertext — the Hubspot Integration this journey creates
// (encrypted at CreateIntegration) and a simulated Phase 1 Outlook row
// (created by writing directly to the integrations table with
// client_secret_encrypted=false, exactly as a pre-Slice-2 row would exist) —
// after a reboot runs the boot backfill (EncryptPlaintextClientSecrets).
func TestHubspotJourney_DatabaseDumpContainsNoPlaintextClientSecretIncludingMigratedPhase1Rows(t *testing.T) {
	dsn := support.NewTestDSN(t)
	wired := support.BootAppAt(t, dsn) // first boot: real embedded definitions (outlook + hubspot), runs migration 0006.
	adminAuth := "Bearer " + support.AdminAPIKey

	const hubspotSecret = "hubspot-oauth-client-secret-value"
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"hubspot","clientId":"hubspot-client-id","clientSecret":"`+hubspotSecret+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Simulate a Phase 1 row: written directly to the table, bypassing the
	// facade's own encryption, exactly as a row created before the vault
	// existed would look.
	const legacySecret = "phase1-plaintext-outlook-client-secret"
	legacyRow := catalogbun.IntegrationRow{
		ID: "intg_legacy_phase1", ProviderSlug: "outlook", ClientID: "legacy-client-id",
		ClientSecret: legacySecret, ClientSecretEncrypted: false, CreatedAt: time.Now().UTC(),
	}
	if _, err := wired.DB.NewInsert().Model(&legacyRow).Exec(context.Background()); err != nil {
		t.Fatalf("seed legacy plaintext row: %v", err)
	}

	// Reboot against the same database: the boot backfill must encrypt the
	// legacy row (idempotently — it must not disturb the already-encrypted
	// Hubspot row).
	wired2 := support.BootAppAt(t, dsn)

	rows, err := wired2.DB.QueryContext(context.Background(), "SELECT id, client_secret, client_secret_encrypted FROM integrations")
	if err != nil {
		t.Fatalf("query integrations: %v", err)
	}
	defer rows.Close()
	rowCount := 0
	for rows.Next() {
		rowCount++
		var id, clientSecret string
		var encrypted bool
		if err := rows.Scan(&id, &clientSecret, &encrypted); err != nil {
			t.Fatalf("scan integrations row: %v", err)
		}
		if !encrypted {
			t.Errorf("integration %q has client_secret_encrypted = false after boot, want true", id)
		}
		if clientSecret == hubspotSecret || strings.Contains(clientSecret, hubspotSecret) {
			t.Errorf("integration %q's client_secret column contains the raw Hubspot secret", id)
		}
		if clientSecret == legacySecret || strings.Contains(clientSecret, legacySecret) {
			t.Errorf("integration %q's client_secret column contains the raw legacy Phase 1 secret", id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate integrations rows: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("dumped %d integrations rows, want 2 (the Hubspot integration and the migrated legacy row)", rowCount)
	}
}
