//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, oauthJourneyFixture,
// openConnectPageAndGetState, doJSONRequest, listTools,
// listTriggerDefinitions/triggerDefinitionsPageDTO
// (trigger_definitions_journey_integration_test.go),
// executionResultWithCursorDTO, executeHubspotTool (hubspot_journey_
// integration_test.go — a provider-agnostic tool-execute-with-cursor helper
// despite its name) — same package). This file tells the Providers strand's
// Slice 3 story end to end against the real composition root: Slack arrives
// purely as a definition file (slack.yaml, no provider-specific Go code)
// declaring no userInfo block and no trigger; slack-post-message posts a
// bearer-authenticated JSON body and a genuine ok:true response is a
// successful tool call; slack-list-channels pages with the canonical
// pageSize/cursor convention mapped onto Slack's own limit/cursor query
// parameters and response_metadata.next_cursor; and — PD77's documented
// deviation — a Slack ok:false body arriving on HTTP 200 surfaces as a
// *successful* tool result carrying ok:false in Data, not a tool-level
// failure, because providerhttp treats any 2xx as success and the token
// grammar has no conditional to translate an ok:false body into one
// (deferred to CEL, ADR-0012).
//
// A note on activation: slack-post-message/slack-list-channels need an
// ACTIVE connection, produced via the real OAuth handshake
// (activateSlackConnectionViaCallback) — see
// TestSlackJourney_ActivatesWithNoCapturedIdentityWhenTheDefinitionHasNoUserInfoURL
// below for the dedicated activation-path test: exchangeTokensAndFetchAccount
// (connections/oauth.go) skips the account-profile fetch when the
// definition's UserInfoURL is empty (PD77's deviation), mirroring
// reconcileOne's existing guard (connections/reconcile.go), so a Slack
// connection reaches ACTIVE with no captured email/displayName.
package crucial_path

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

// slackDefinitionAgainst is Slack's real slack.yaml shape, re-expressed as a
// catalog.ProviderDefinition pointed at fs instead of the real internet: no
// userInfoUrl/userInfo block (PD77's deviation), the JSON-body-mapping
// slack-post-message, and the cursor-paginated slack-list-channels.
func slackDefinitionAgainst(fs *support.FakeSlack) []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "slack",
			Name:         "Slack",
			Logo:         "https://static.beecon.dev/providers/slack.png",
			AuthScheme:   "oauth2",
			BaseURL:      fs.BaseURL,
			AuthorizeURL: "https://fake-slack.example.com/oauth/v2/authorize",
			TokenURL:     fs.TokenURL,
			Scopes:       []string{"chat:write", "channels:read"},
			Tools: []catalog.ProviderTool{
				{
					Slug:        "slack-post-message",
					Name:        "Post message",
					Description: "Post a message to a Slack channel.",
					Method:      "POST",
					Path:        "/chat.postMessage",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"channel": map[string]any{"type": "string"},
							"text":    map[string]any{"type": "string"},
						},
						"required": []any{"channel", "text"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Body: map[string]string{
							"channel": "{input.channel}",
							"text":    "{input.text}",
						},
					},
				},
				{
					Slug:        "slack-list-channels",
					Name:        "List channels",
					Description: "List conversations (channels) visible to the bot, cursor-paginated.",
					Method:      "GET",
					Path:        "/conversations.list",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"types":    map[string]any{"type": "string"},
							"pageSize": map[string]any{"type": "integer"},
							"cursor":   map[string]any{"type": "string"},
						},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Query: map[string]string{"types": "{input.types}"},
						Pagination: &catalog.Pagination{
							PageSizeParam:  "limit",
							CursorParam:    "cursor",
							NextCursorPath: "response_metadata.next_cursor",
						},
					},
				},
			},
		},
	}
}

// slackJourneyFixture is oauthJourneyFixture with no further Slack-specific
// scaffolding needed — every test below drives the real OAuth handshake via
// activateSlackConnectionViaCallback.
type slackJourneyFixture struct {
	oauthJourneyFixture
}

// newSlackJourneyFixture is newOAuthJourneyFixture
// (oauth_handshake_journey_integration_test.go), re-pointed at the "slack"
// providerSlug — mirrors newGmailJourneyFixture/newHubspotJourneyFixture.
func newSlackJourneyFixture(t *testing.T, wired *app.Wired) slackJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/slack-callback"

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme Slack"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"slack","clientId":"slack-client-id","clientSecret":"slack-client-secret"}`)
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

	return slackJourneyFixture{
		oauthJourneyFixture: oauthJourneyFixture{
			orgAuth:            orgAuth,
			userID:             user.ID,
			integrationID:      integration.ID,
			allowedRedirectURI: allowedRedirectURI,
		},
	}
}

// activateSlackConnectionViaCallback drives the real OAuth handshake —
// initiate, open the connect page, and the callback with a fake
// authorization code — and returns the resulting connection's stable id.
// This now works for Slack's userInfo-less definition (PD77's deviation)
// because exchangeTokensAndFetchAccount (connections/oauth.go) skips the
// account-profile fetch when UserInfoURL is empty, mirroring reconcileOne's
// existing guard (connections/reconcile.go), instead of issuing an HTTP
// request against "".
func activateSlackConnectionViaCallback(t *testing.T, wired *app.Wired, fixture slackJourneyFixture) string {
	t.Helper()
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	parsed, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	return parsed.Query().Get("connectionId")
}

// TestSlackJourney_DefinitionLoadsAtBootWithNoUserInfoAndZeroTriggers is the
// boot-load AC: booted against the real embedded providers/ directory (not a
// fake), the catalog lists both Slack tools with non-empty schemas under
// provider slug "slack", and trigger-definitions surfaces exactly zero
// triggers for it (PD81: Slack ships no trigger in this strand) — proving
// Slack arrived purely as a definition file (mirrors
// TestGmailJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode/
// TestGoogleCalendarJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode).
func TestSlackJourney_DefinitionLoadsAtBootWithNoUserInfoAndZeroTriggers(t *testing.T) {
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

	t.Run("tools list surfaces both Slack tools with non-empty schemas", func(t *testing.T) {
		status, page := listTools(t, wired, orgAuth, "?providerSlug=slack")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		wantSlugs := map[string]bool{"slack-post-message": false, "slack-list-channels": false}
		for _, item := range page.Items {
			if _, declared := wantSlugs[item.Slug]; declared {
				wantSlugs[item.Slug] = true
			}
			if item.Provider.Slug != "slack" {
				t.Errorf("item %q provider.slug = %q, want %q", item.Slug, item.Provider.Slug, "slack")
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
	})

	t.Run("trigger-definitions surfaces zero Slack triggers", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, orgAuth, "?providerSlug=slack")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 0 {
			t.Errorf("items = %+v, want zero triggers (Slack declares none, PD81)", page.Items)
		}
	})
}

// TestSlackJourney_PostMessagePostsABearerAuthenticatedJSONBodyAndAGenuineOkTrueResponseSucceeds
// is AC2: slack-post-message's body mapping builds the JSON
// {"channel":...,"text":...} shape under the connection's own access token as
// a bearer credential, and a genuine {ok:true,...} response is a successful
// tool call whose Data carries Slack's own record.
func TestSlackJourney_PostMessagePostsABearerAuthenticatedJSONBodyAndAGenuineOkTrueResponseSucceeds(t *testing.T) {
	const accessToken = "xoxb-fake-bot-token"
	fakeSlack := support.NewFakeSlack(t, support.FakeSlackScript{AccessToken: accessToken})
	wired := support.BootAppWithProviderDefinitions(t, slackDefinitionAgainst(fakeSlack))
	fixture := newSlackJourneyFixture(t, wired)
	connectionID := activateSlackConnectionViaCallback(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "slack-post-message", fixture.userID, connectionID,
		`{"channel":"C123456","text":"Deploy finished"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	data, ok := dto.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want the decoded chat.postMessage response", dto.Data)
	}
	if data["ok"] != true {
		t.Errorf("data.ok = %v, want true", data["ok"])
	}
	if data["channel"] != "C123456" {
		t.Errorf("data.channel = %v, want %q", data["channel"], "C123456")
	}

	t.Run("Slack received the call as POST /chat.postMessage with a bearer-authenticated JSON body", func(t *testing.T) {
		if fakeSlack.PostMessageCallCount != 1 {
			t.Fatalf("PostMessageCallCount = %d, want 1", fakeSlack.PostMessageCallCount)
		}
		if fakeSlack.LastPostMessageAuthorizationHeader != "Bearer "+accessToken {
			t.Errorf("Authorization header = %q, want %q", fakeSlack.LastPostMessageAuthorizationHeader, "Bearer "+accessToken)
		}
		if fakeSlack.LastPostMessageBody == nil {
			t.Fatal("Slack received no chat.postMessage body")
		}
		if fakeSlack.LastPostMessageBody["channel"] != "C123456" {
			t.Errorf(`body["channel"] = %v, want %q`, fakeSlack.LastPostMessageBody["channel"], "C123456")
		}
		if fakeSlack.LastPostMessageBody["text"] != "Deploy finished" {
			t.Errorf(`body["text"] = %v, want %q`, fakeSlack.LastPostMessageBody["text"], "Deploy finished")
		}
	})
}

// slackChannelIDsFromData extracts each channel's "id" from a decoded
// slack-list-channels Result.Data ({"channels":[{"id":...}, ...]}).
func slackChannelIDsFromData(t *testing.T, data any) []string {
	t.Helper()
	object, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want a decoded JSON object", data)
	}
	channels, ok := object["channels"].([]any)
	if !ok {
		t.Fatalf(`data["channels"] = %T, want an array`, object["channels"])
	}
	ids := make([]string, 0, len(channels))
	for _, c := range channels {
		channel, ok := c.(map[string]any)
		if !ok {
			t.Fatalf("channel entry = %T, want an object", c)
		}
		id, _ := channel["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestSlackJourney_ListChannelsPagesWithTheCanonicalPageSizeAndCursorConvention
// is AC3: the first page returns channels and a nextCursor read from
// response_metadata.next_cursor; feeding that cursor back in as the next
// call's canonical "cursor" argument reaches Slack as its own cursor query
// parameter and fetches the following page.
func TestSlackJourney_ListChannelsPagesWithTheCanonicalPageSizeAndCursorConvention(t *testing.T) {
	const accessToken = "xoxb-fake-bot-token"
	fakeSlack := support.NewFakeSlack(t, support.FakeSlackScript{
		AccessToken: accessToken,
		Channels: []support.FakeSlackChannel{
			{ID: "C1", Name: "general", IsChannel: true},
			{ID: "C2", Name: "random", IsChannel: true},
			{ID: "C3", Name: "eng", IsChannel: true},
		},
	})
	wired := support.BootAppWithProviderDefinitions(t, slackDefinitionAgainst(fakeSlack))
	fixture := newSlackJourneyFixture(t, wired)
	connectionID := activateSlackConnectionViaCallback(t, wired, fixture)

	var firstPageIDs, secondPageIDs []string
	var nextCursor string
	t.Run("executing slack-list-channels with pageSize returns channels and a nextCursor read from response_metadata.next_cursor", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "slack-list-channels", fixture.userID, connectionID, `{"pageSize":2}`)
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
		firstPageIDs = slackChannelIDsFromData(t, dto.Data)
		if len(firstPageIDs) != 2 {
			t.Fatalf("first page returned %d channels, want 2", len(firstPageIDs))
		}
		if got := fakeSlack.LastChannelsQuery.Get("limit"); got != "2" {
			t.Errorf("Slack received limit=%q, want %q (canonical pageSize mapped to Slack's own param)", got, "2")
		}
	})

	t.Run("feeding the nextCursor back in as the canonical cursor argument reaches Slack as its own cursor query parameter and fetches the following page", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "slack-list-channels", fixture.userID, connectionID,
			fmt.Sprintf(`{"pageSize":2,"cursor":%q}`, nextCursor))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		secondPageIDs = slackChannelIDsFromData(t, dto.Data)
		if len(secondPageIDs) != 1 {
			t.Fatalf("second page returned %d channels, want 1 (the third and last channel)", len(secondPageIDs))
		}
		if dto.NextCursor != "" {
			t.Errorf("nextCursor = %q, want empty — the last page carries no further cursor", dto.NextCursor)
		}
		if got := fakeSlack.LastChannelsQuery.Get("cursor"); got != nextCursor {
			t.Errorf(`Slack received cursor=%q, want the previous page's nextCursor %q`, got, nextCursor)
		}
	})

	t.Run("the two pages together cover every channel exactly once", func(t *testing.T) {
		seen := map[string]bool{}
		for _, id := range append(firstPageIDs, secondPageIDs...) {
			if seen[id] {
				t.Fatalf("channel id %q seen more than once across the two pages", id)
			}
			seen[id] = true
		}
		if len(seen) != 3 {
			t.Fatalf("walked %d channels across both pages, want exactly 3", len(seen))
		}
	})
}

// TestSlackJourney_PostMessageReturnsAToolSuccessCarryingSlacksOkFalseBodyUntilConditionalMappingLands
// is AC4: PD77's documented, deliberately-not-fixed-here deviation. Slack's
// real chat.postMessage returns HTTP 200 even on failure, with
// {ok:false,error:"..."} in the body; providerhttp treats any 2xx status as
// a successful tool call, so the failure surfaces as a *successful* result
// carrying ok:false and the error string in Data, not a tool-level failure —
// this test pins that current behavior, it is not asserting the eventual
// conditional-mapping fix (deferred to CEL, ADR-0012) has landed.
func TestSlackJourney_PostMessageReturnsAToolSuccessCarryingSlacksOkFalseBodyUntilConditionalMappingLands(t *testing.T) {
	const accessToken = "xoxb-fake-bot-token"
	fakeSlack := support.NewFakeSlack(t, support.FakeSlackScript{AccessToken: accessToken, PostMessageError: "channel_not_found"})
	wired := support.BootAppWithProviderDefinitions(t, slackDefinitionAgainst(fakeSlack))
	fixture := newSlackJourneyFixture(t, wired)
	connectionID := activateSlackConnectionViaCallback(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "slack-post-message", fixture.userID, connectionID,
		`{"channel":"C_MISSING","text":"hello"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true — PD77's documented deviation: a Slack HTTP 200 is always a successful tool call regardless of the ok field; error = %+v", dto.Error)
	}
	if dto.Error != nil {
		t.Errorf("error = %+v, want nil for a 2xx response (the deviation: no conditional to translate ok:false into a tool-level failure)", dto.Error)
	}
	data, ok := dto.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want the decoded chat.postMessage response", dto.Data)
	}
	if data["ok"] != false {
		t.Errorf("data.ok = %v, want false — Slack's own failure body surfaced as Data, not translated into a tool-level failure", data["ok"])
	}
	if data["error"] != "channel_not_found" {
		t.Errorf(`data.error = %v, want %q`, data["error"], "channel_not_found")
	}
}

// TestSlackJourney_ActivatesWithNoCapturedIdentityWhenTheDefinitionHasNoUserInfoURL
// is the real end-to-end handshake for a userInfo-less provider: slack.yaml
// deliberately omits userInfoUrl (PD77's documented deviation), and
// exchangeTokensAndFetchAccount (connections/oauth.go) now guards that case
// exactly as reconcileOne already did (connections/reconcile.go) — skipping
// the account-profile fetch entirely rather than issuing an HTTP request
// against an empty URL — so the callback redirects with status=success and
// the connection reaches ACTIVE with no captured email/displayName.
func TestSlackJourney_ActivatesWithNoCapturedIdentityWhenTheDefinitionHasNoUserInfoURL(t *testing.T) {
	fakeSlack := support.NewFakeSlack(t, support.FakeSlackScript{AccessToken: "xoxb-fake-bot-token"})
	wired := support.BootAppWithProviderDefinitions(t, slackDefinitionAgainst(fakeSlack))
	fixture := newSlackJourneyFixture(t, wired)
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")

	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d; body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	location := w.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got := parsed.Query().Get("status"); got != "success" {
		t.Errorf("status = %q, want %q", got, "success")
	}
	got := fixture.getConnection(t, wired, initiated.ID)
	if got.Status != "ACTIVE" {
		t.Errorf("status = %q, want %q — a userInfo-less definition must still activate", got.Status, "ACTIVE")
	}
	if got.Account != nil {
		t.Errorf("account = %+v, want nil — no userInfoUrl means no identity is captured", got.Account)
	}
}
