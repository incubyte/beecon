//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, oauthJourneyFixture,
// openConnectPageAndGetState, doJSONRequest, listTools,
// executionResultWithCursorDTO, and executeHubspotTool (a provider-agnostic
// tool-execute-with-cursor helper despite its name) —
// hubspot_journey_integration_test.go — same package). This file tells the
// Providers strand's Gmail slice's story end to end against the real
// composition root: Gmail arrives purely as a definition file (gmail.yaml,
// no provider-specific Go code); gmail-list-messages pages with the
// canonical pageSize/cursor convention mapped onto Gmail's own
// maxResults/pageToken; gmail-get-message's {input.messageId} path token is
// URL-escaped and round-trips a slash/question-mark-carrying id intact;
// gmail-send-message forwards the caller-supplied raw MIME value to Gmail
// unchanged; and an upstream Gmail rejection surfaces as a tool-level
// failure carrying the provider's own status and message, not a platform
// HTTP error.
package crucial_path

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

// gmailDefinitionAgainst is Gmail's real gmail.yaml shape, re-expressed as a
// catalog.ProviderDefinition pointed at fg instead of the real internet:
// the shared Google OAuth block's userInfo mapping (email/name), and the
// three tools' declared mappings (pagination, path-parameter templating,
// and JSON body mapping).
func gmailDefinitionAgainst(fg *support.FakeGoogle) []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "gmail",
			Name:         "Gmail",
			Logo:         "https://static.beecon.dev/providers/gmail.png",
			AuthScheme:   "oauth2",
			BaseURL:      fg.BaseURL,
			AuthorizeURL: "https://fake-google.example.com/o/oauth2/v2/auth",
			TokenURL:     fg.TokenURL,
			UserInfoURL:  fg.UserInfoURL,
			Scopes: []string{
				"openid", "email", "profile",
				"https://www.googleapis.com/auth/gmail.readonly",
				"https://www.googleapis.com/auth/gmail.send",
			},
			UserInfo: catalog.UserInfoMapping{EmailField: "email", DisplayNameField: "name"},
			Tools: []catalog.ProviderTool{
				{
					Slug:        "gmail-list-messages",
					Name:        "List messages",
					Description: "List messages in the authenticated user's mailbox, cursor-paginated.",
					Method:      "GET",
					Path:        "/users/me/messages",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"q":        map[string]any{"type": "string"},
							"pageSize": map[string]any{"type": "integer"},
							"cursor":   map[string]any{"type": "string"},
						},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Query:      map[string]string{"q": "{input.q}"},
						Pagination: &catalog.Pagination{PageSizeParam: "maxResults", CursorParam: "pageToken", NextCursorPath: "nextPageToken"},
					},
				},
				{
					Slug:        "gmail-get-message",
					Name:        "Get message",
					Description: "Retrieves a specific email message by its ID from the authenticated user's Gmail mailbox.",
					Method:      "GET",
					Path:        "/users/me/messages/{input.messageId}",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"messageId": map[string]any{"type": "string"},
							"format":    map[string]any{"type": "string"},
						},
						"required": []any{"messageId"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping:      catalog.Mapping{Query: map[string]string{"format": "{input.format}"}},
				},
				{
					Slug:        "gmail-send-message",
					Name:        "Send message",
					Description: "Sends an email message from the authenticated user's Gmail account.",
					Method:      "POST",
					Path:        "/users/me/messages/send",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"raw": map[string]any{"type": "string"},
						},
						"required": []any{"raw"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping:      catalog.Mapping{Body: map[string]string{"raw": "{input.raw}"}},
				},
			},
		},
	}
}

// newGmailJourneyFixture is newOAuthJourneyFixture
// (oauth_handshake_journey_integration_test.go), re-pointed at the "gmail"
// providerSlug — every other scaffolding step (org, allow-listed redirect
// URI, org key, user) is provider-agnostic, so it returns the same
// oauthJourneyFixture type and reuses its initiate/getConnection methods.
func newGmailJourneyFixture(t *testing.T, wired *app.Wired) oauthJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/gmail-callback"

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme Gmail"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"gmail","clientId":"gmail-client-id","clientSecret":"gmail-client-secret"}`)
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

// activateGmailConnection drives initiate -> connect page -> callback
// through live HTTP requests, mirroring activateHubspotConnection
// (hubspot_journey_integration_test.go) against the Gmail fixture/definition.
func activateGmailConnection(t *testing.T, wired *app.Wired, fixture oauthJourneyFixture) initiatedConnectionDTO {
	t.Helper()
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (handshake must complete); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	return initiated
}

// TestGmailJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode is the
// boot-load AC: booted against the real embedded providers/ directory (not
// a fake), the catalog API lists Gmail's three tools under provider slug
// "gmail", each with a non-empty input and output schema — proving Gmail
// arrived purely as a definition file (mirrors
// TestHubspotJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode).
func TestGmailJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode(t *testing.T) {
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

	status, page := listTools(t, wired, orgAuth, "?providerSlug=gmail")

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	wantSlugs := map[string]bool{"gmail-list-messages": false, "gmail-get-message": false, "gmail-send-message": false}
	for _, item := range page.Items {
		if _, declared := wantSlugs[item.Slug]; declared {
			wantSlugs[item.Slug] = true
		}
		if item.Provider.Slug != "gmail" {
			t.Errorf("item %q provider.slug = %q, want %q", item.Slug, item.Provider.Slug, "gmail")
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

// TestGmailJourney_ListMessagesPagesWithTheCanonicalPageSizeAndCursorConvention
// is the pagination AC: the first page returns messages and a nextCursor
// mapped from Gmail's own nextPageToken; feeding that cursor back in as the
// next call's canonical "cursor" argument reaches Gmail as its own
// pageToken query parameter and fetches the following page.
func TestGmailJourney_ListMessagesPagesWithTheCanonicalPageSizeAndCursorConvention(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada",
		Messages: []support.FakeGoogleMessage{
			{ID: "msg-1", ThreadID: "thread-1"},
			{ID: "msg-2", ThreadID: "thread-2"},
			{ID: "msg-3", ThreadID: "thread-3"},
		},
	})
	wired := support.BootAppWithProviderDefinitions(t, gmailDefinitionAgainst(fakeGoogle))
	fixture := newGmailJourneyFixture(t, wired)
	initiated := activateGmailConnection(t, wired, fixture)

	var firstPageIDs, secondPageIDs []string
	var nextCursor string
	t.Run("executing gmail-list-messages with pageSize returns messages and a nextCursor mapped from nextPageToken", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gmail-list-messages", fixture.userID, initiated.ID, `{"pageSize":2}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		if dto.NextCursor == "" {
			t.Fatal("nextCursor is empty, want a cursor since a further page remains")
		}
		nextCursor = dto.NextCursor
		firstPageIDs = gmailMessageIDsFromData(t, dto.Data)
		if len(firstPageIDs) != 2 {
			t.Fatalf("first page returned %d messages, want 2", len(firstPageIDs))
		}
		if got := fakeGoogle.LastMessagesQuery.Get("maxResults"); got != "2" {
			t.Errorf("Gmail received maxResults=%q, want %q (canonical pageSize mapped to Gmail's own param)", got, "2")
		}
	})

	t.Run("feeding the nextCursor back in as the canonical cursor argument reaches Gmail as pageToken and fetches the following page", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gmail-list-messages", fixture.userID, initiated.ID,
			fmt.Sprintf(`{"pageSize":2,"cursor":%q}`, nextCursor))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		secondPageIDs = gmailMessageIDsFromData(t, dto.Data)
		if len(secondPageIDs) != 1 {
			t.Fatalf("second page returned %d messages, want 1 (the third and last message)", len(secondPageIDs))
		}
		if dto.NextCursor != "" {
			t.Errorf("nextCursor = %q, want empty — the last page carries no further cursor", dto.NextCursor)
		}
		if got := fakeGoogle.LastMessagesQuery.Get("pageToken"); got != nextCursor {
			t.Errorf(`Gmail received pageToken=%q, want the previous page's nextCursor %q`, got, nextCursor)
		}
	})

	t.Run("the two pages together cover every message exactly once", func(t *testing.T) {
		seen := map[string]bool{}
		for _, id := range append(firstPageIDs, secondPageIDs...) {
			if seen[id] {
				t.Fatalf("message id %q seen more than once across the two pages", id)
			}
			seen[id] = true
		}
		if len(seen) != 3 {
			t.Fatalf("walked %d messages across both pages, want exactly 3", len(seen))
		}
	})
}

// gmailMessageIDsFromData extracts each message's "id" from a decoded
// gmail-list-messages Result.Data ({"messages":[{"id":...}, ...]}).
func gmailMessageIDsFromData(t *testing.T, data any) []string {
	t.Helper()
	object, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want a decoded JSON object", data)
	}
	messages, ok := object["messages"].([]any)
	if !ok {
		t.Fatalf(`data["messages"] = %T, want an array`, object["messages"])
	}
	ids := make([]string, 0, len(messages))
	for _, m := range messages {
		message, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("message entry = %T, want an object", m)
		}
		id, _ := message["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestGmailJourney_GetMessageMessageIdIsURLEscapedIntoThePath is the
// path-escaping AC: a messageId containing a slash and query-string
// characters must survive the round trip through RenderPath's
// URL-escaping, over the wire to a fake Gmail upstream, and back out as the
// exact same string (mirrors
// TestCatalogToolsJourney_ExecuteOutlookGetMessageEndToEnd's proof for
// Outlook's own {input.messageId} path token).
func TestGmailJourney_GetMessageMessageIdIsURLEscapedIntoThePath(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada"})
	wired := support.BootAppWithProviderDefinitions(t, gmailDefinitionAgainst(fakeGoogle))
	fixture := newGmailJourneyFixture(t, wired)
	initiated := activateGmailConnection(t, wired, fixture)

	const messageID = "message/id?needs escaping&more"

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gmail-get-message", fixture.userID, initiated.ID,
		fmt.Sprintf(`{"messageId":%q,"format":"full"}`, messageID))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	data, ok := dto.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want the fetched message object", dto.Data)
	}
	if got := data["id"]; got != messageID {
		t.Errorf("data.id = %v, want %q (the templated messageId round-tripped)", got, messageID)
	}

	t.Run("Gmail received the messageId as its own path segment, correctly URL-escaped over the wire", func(t *testing.T) {
		if fakeGoogle.LastGetMessageIDPath != messageID {
			t.Errorf("LastGetMessageIDPath = %q, want %q", fakeGoogle.LastGetMessageIDPath, messageID)
		}
	})

	t.Run("the format input reached Gmail as the format query parameter", func(t *testing.T) {
		if got := fakeGoogle.LastGetMessageQuery.Get("format"); got != "full" {
			t.Errorf("format = %q, want %q", got, "full")
		}
	})
}

// TestGmailJourney_SendMessageForwardsTheCallerSuppliedRawValueUnchanged is
// the raw-body-passthrough AC: gmail-send-message's body mapping
// (raw: "{input.raw}") forwards the caller-supplied base64url MIME value to
// Gmail's JSON body unchanged — the token grammar substitutes but never
// alters it (PD79).
func TestGmailJourney_SendMessageForwardsTheCallerSuppliedRawValueUnchanged(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada"})
	wired := support.BootAppWithProviderDefinitions(t, gmailDefinitionAgainst(fakeGoogle))
	fixture := newGmailJourneyFixture(t, wired)
	initiated := activateGmailConnection(t, wired, fixture)

	const rawMIME = "VG86IGdyYWNlQGV4YW1wbGUuY29tClN1YmplY3Q6IEhlbGxvCgpIaQ=="

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gmail-send-message", fixture.userID, initiated.ID,
		fmt.Sprintf(`{"raw":%q}`, rawMIME))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	data, ok := dto.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want the sent-message object", dto.Data)
	}
	if data["id"] != "sent-message-1" {
		t.Errorf("data.id = %v, want %q", data["id"], "sent-message-1")
	}

	if fakeGoogle.LastSendBody == nil {
		t.Fatal("Gmail received no send-message body")
	}
	if got := fakeGoogle.LastSendBody["raw"]; got != rawMIME {
		t.Errorf(`Gmail received body["raw"] = %v, want the caller-supplied value %q unchanged`, got, rawMIME)
	}
}

// TestGmailJourney_SendMessageUpstreamErrorSurfacesAsAToolLevelFailure is
// the upstream-error AC: an upstream Gmail rejection is a tool-level
// failure carrying the provider's own status and message, not a platform
// HTTP error (mirrors
// TestHubspotJourney_CreateContactUpstreamErrorSurfacesAsTheProvidersStatusAndMessage).
func TestGmailJourney_SendMessageUpstreamErrorSurfacesAsAToolLevelFailure(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada",
		SendStatus: http.StatusBadRequest, SendBody: `{"error":{"message":"Invalid raw message"}}`,
	})
	wired := support.BootAppWithProviderDefinitions(t, gmailDefinitionAgainst(fakeGoogle))
	fixture := newGmailJourneyFixture(t, wired)
	initiated := activateGmailConnection(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gmail-send-message", fixture.userID, initiated.ID,
		`{"raw":"bm90LXZhbGlkLW1pbWU="}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (an upstream error is a tool-level failure, not an HTTP error)", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for an upstream 400 response")
	}
	if dto.Data != nil {
		t.Errorf("data = %v, want nil for a failed execution", dto.Data)
	}
	if dto.Error == nil {
		t.Fatal("error is nil, want the provider's status and message")
	}
	if !strings.Contains(dto.Error.Message, "400") {
		t.Errorf("error.message = %q, want it to surface the provider's status code", dto.Error.Message)
	}
	if !strings.Contains(dto.Error.Message, "Invalid raw message") {
		t.Errorf("error.message = %q, want it to surface the provider's response body", dto.Error.Message)
	}
}
