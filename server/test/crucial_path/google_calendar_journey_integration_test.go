//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, oauthJourneyFixture,
// openConnectPageAndGetState, doJSONRequest, listTools,
// executionResultWithCursorDTO, executeHubspotTool (hubspot_journey_
// integration_test.go — a provider-agnostic tool-execute-with-cursor helper
// despite its name), listTriggerDefinitions/triggerDefinitionsPageDTO
// (trigger_definitions_journey_integration_test.go), createTriggerInstance
// (trigger_instances_journey_integration_test.go), pollOnce/pollThenDispatch/
// decodeDelivery/pollTestIntervalSeconds/setWebhookEndpoint
// (trigger_polling_journey_integration_test.go) — same package). This file
// tells the Providers strand's Google Calendar slice's story end to end
// against the real composition root: Calendar arrives purely as a
// definition file (google-calendar.yaml, no provider-specific Go code)
// reusing gmail.yaml's shared Google OAuth block (PD78); gcal-list-events
// defaults calendarId to primary via its inputSchema default and pages with
// the canonical pageSize/cursor convention; gcal-create-event builds the
// nested {start:{dateTime},end:{dateTime}} body from a flat input schema; an
// upstream Calendar rejection surfaces as a tool-level failure; and
// gcal-event-updated's poll mapping fetches newly updated events, sending
// updatedMin/orderBy/singleEvents literally on every tick and advancing its
// own watermark across ticks (mirrors hubspot-contact-created's own poll
// journey, PD80).
package crucial_path

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

// gcalEventUpdatedSlug is the real google-calendar.yaml trigger slug (PD80).
const gcalEventUpdatedSlug = "gcal-event-updated"

// gcalDefinitionAgainst is google-calendar.yaml's real shape, re-expressed as
// a catalog.ProviderDefinition pointed at fg instead of the real internet:
// the shared Google OAuth block (identical to gmail.yaml's, PD78), the two
// tools' declared mappings (default calendarId, pagination, dotted-key nested
// body), and the gcal-event-updated poll trigger's full mapping.
func gcalDefinitionAgainst(fg *support.FakeGoogle) []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "google-calendar",
			Name:         "Google Calendar",
			Logo:         "https://static.beecon.dev/providers/google-calendar.png",
			AuthScheme:   "oauth2",
			BaseURL:      fg.BaseURL,
			AuthorizeURL: "https://fake-google.example.com/o/oauth2/v2/auth",
			TokenURL:     fg.TokenURL,
			UserInfoURL:  fg.UserInfoURL,
			Scopes: []string{
				"openid", "email", "profile",
				"https://www.googleapis.com/auth/calendar.events",
			},
			UserInfo: catalog.UserInfoMapping{EmailField: "email", DisplayNameField: "name"},
			Tools: []catalog.ProviderTool{
				{
					Slug:        "gcal-list-events",
					Name:        "List events",
					Description: "List events on a Google Calendar, cursor-paginated.",
					Method:      "GET",
					Path:        "/calendars/{input.calendarId}/events",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"calendarId":   map[string]any{"type": "string", "default": "primary"},
							"timeMin":      map[string]any{"type": "string"},
							"singleEvents": map[string]any{"type": "boolean"},
							"orderBy":      map[string]any{"type": "string"},
							"pageSize":     map[string]any{"type": "integer"},
							"cursor":       map[string]any{"type": "string"},
						},
						"required": []any{"calendarId"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Query: map[string]string{
							"timeMin":      "{input.timeMin}",
							"singleEvents": "{input.singleEvents}",
							"orderBy":      "{input.orderBy}",
						},
						Pagination: &catalog.Pagination{PageSizeParam: "maxResults", CursorParam: "pageToken", NextCursorPath: "nextPageToken"},
					},
				},
				{
					Slug:        "gcal-create-event",
					Name:        "Create event",
					Description: "Create an event on a Google Calendar.",
					Method:      "POST",
					Path:        "/calendars/{input.calendarId}/events",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"calendarId":    map[string]any{"type": "string", "default": "primary"},
							"summary":       map[string]any{"type": "string"},
							"startDateTime": map[string]any{"type": "string"},
							"endDateTime":   map[string]any{"type": "string"},
						},
						"required": []any{"calendarId", "summary", "startDateTime", "endDateTime"},
					},
					OutputSchema: map[string]any{"type": "object"},
					Mapping: catalog.Mapping{
						Body: map[string]string{
							"summary":        "{input.summary}",
							"start.dateTime": "{input.startDateTime}",
							"end.dateTime":   "{input.endDateTime}",
						},
					},
				},
			},
			Triggers: []catalog.TriggerDefinition{
				{
					Slug:        gcalEventUpdatedSlug,
					Name:        "Event created or updated",
					Description: "Triggered when an event on the configured calendar is created or updated.",
					ConfigSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"calendarId": map[string]any{"type": "string", "default": "primary"}},
					},
					PayloadSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "string"},
							"summary": map[string]any{"type": "string"},
							"status":  map[string]any{"type": "string"},
							"start":   map[string]any{"type": "string"},
							"end":     map[string]any{"type": "string"},
							"updated": map[string]any{"type": "string"},
						},
						"required": []any{"id", "updated"},
					},
					Ingestion:           "poll",
					PollIntervalSeconds: pollTestIntervalSeconds,
					Poll: catalog.TriggerPollMapping{
						Method: "GET",
						Path:   "/calendars/{config.calendarId}/events",
						Query: map[string]string{
							"updatedMin":   "{watermark}",
							"orderBy":      "updated",
							"singleEvents": "true",
						},
						RecordsPath:         "items",
						RecordIDPath:        "id",
						RecordTimestampPath: "updated",
						Payload: map[string]string{
							"id": "id", "summary": "summary", "status": "status",
							"start": "start.dateTime", "end": "end.dateTime", "updated": "updated",
						},
					},
				},
			},
		},
	}
}

// newGCalJourneyFixture is newOAuthJourneyFixture
// (oauth_handshake_journey_integration_test.go), re-pointed at the
// "google-calendar" providerSlug — mirrors newGmailJourneyFixture
// (gmail_journey_integration_test.go).
func newGCalJourneyFixture(t *testing.T, wired *app.Wired) oauthJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	const allowedRedirectURI = "https://consumer.example.com/gcal-callback"

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme Calendar"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"google-calendar","clientId":"gcal-client-id","clientSecret":"gcal-client-secret"}`)
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

// activateGCalConnection drives initiate -> connect page -> callback through
// live HTTP requests, mirroring activateGmailConnection
// (gmail_journey_integration_test.go) against the Calendar fixture/definition.
func activateGCalConnection(t *testing.T, wired *app.Wired, fixture oauthJourneyFixture) initiatedConnectionDTO {
	t.Helper()
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (handshake must complete); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	return initiated
}

// TestGoogleCalendarJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode
// is the boot-load AC: booted against the real embedded providers/ directory
// (not a fake), the catalog lists gcal-list-events/gcal-create-event under
// provider slug "google-calendar" each with a non-empty input/output schema,
// and gcal-event-updated is present with ingestion "poll" (mirrors
// TestGmailJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode).
func TestGoogleCalendarJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode(t *testing.T) {
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

	t.Run("tools list surfaces both Calendar tools with non-empty schemas", func(t *testing.T) {
		status, page := listTools(t, wired, orgAuth, "?providerSlug=google-calendar")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		wantSlugs := map[string]bool{"gcal-list-events": false, "gcal-create-event": false}
		for _, item := range page.Items {
			if _, declared := wantSlugs[item.Slug]; declared {
				wantSlugs[item.Slug] = true
			}
			if item.Provider.Slug != "google-calendar" {
				t.Errorf("item %q provider.slug = %q, want %q", item.Slug, item.Provider.Slug, "google-calendar")
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

	t.Run("trigger-definitions surfaces gcal-event-updated with ingestion poll", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, orgAuth, "?providerSlug=google-calendar")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 1 || page.Items[0].Slug != gcalEventUpdatedSlug {
			t.Fatalf("items = %+v, want exactly %q", page.Items, gcalEventUpdatedSlug)
		}
		if page.Items[0].Ingestion != "poll" {
			t.Errorf("ingestion = %q, want %q", page.Items[0].Ingestion, "poll")
		}
	})
}

// gcalEventIDsFromData extracts each event's "id" from a decoded
// gcal-list-events Result.Data ({"items":[{"id":...}, ...]}).
func gcalEventIDsFromData(t *testing.T, data any) []string {
	t.Helper()
	object, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want a decoded JSON object", data)
	}
	items, ok := object["items"].([]any)
	if !ok {
		t.Fatalf(`data["items"] = %T, want an array`, object["items"])
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		event, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("event entry = %T, want an object", item)
		}
		id, _ := event["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestGoogleCalendarJourney_ListEventsDefaultsCalendarIdAndPaginates is the
// default-calendarId + pagination AC: gcal-list-events called with
// calendarId omitted reaches Calendar as /calendars/primary/events (the
// inputSchema default applied), and paginates with the canonical
// pageSize/cursor convention mapped onto maxResults/pageToken.
func TestGoogleCalendarJourney_ListEventsDefaultsCalendarIdAndPaginates(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada",
	})
	fakeGoogle.AddEvent(support.FakeGoogleEvent{
		ID: "evt-1", Summary: "Standup", Status: "confirmed",
		StartDateTime: "2026-01-01T09:00:00Z", EndDateTime: "2026-01-01T09:15:00Z", Updated: "2026-01-01T08:00:00Z",
	})
	fakeGoogle.AddEvent(support.FakeGoogleEvent{
		ID: "evt-2", Summary: "Planning", Status: "confirmed",
		StartDateTime: "2026-01-01T10:00:00Z", EndDateTime: "2026-01-01T10:30:00Z", Updated: "2026-01-01T08:05:00Z",
	})
	fakeGoogle.AddEvent(support.FakeGoogleEvent{
		ID: "evt-3", Summary: "Retro", Status: "confirmed",
		StartDateTime: "2026-01-01T11:00:00Z", EndDateTime: "2026-01-01T11:30:00Z", Updated: "2026-01-01T08:10:00Z",
	})

	wired := support.BootAppWithProviderDefinitions(t, gcalDefinitionAgainst(fakeGoogle))
	fixture := newGCalJourneyFixture(t, wired)
	initiated := activateGCalConnection(t, wired, fixture)

	var nextCursor string
	t.Run("calendarId omitted defaults to primary and the first page returns a nextCursor", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gcal-list-events", fixture.userID, initiated.ID, `{"pageSize":2}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		if fakeGoogle.LastEventsCalendarID != "primary" {
			t.Errorf("Calendar received calendarId path segment %q, want %q (the inputSchema default applied)", fakeGoogle.LastEventsCalendarID, "primary")
		}
		ids := gcalEventIDsFromData(t, dto.Data)
		if len(ids) != 2 {
			t.Fatalf("first page returned %d events, want 2", len(ids))
		}
		if dto.NextCursor == "" {
			t.Fatal("nextCursor is empty, want a cursor since a further page remains")
		}
		nextCursor = dto.NextCursor
		if got := fakeGoogle.LastEventsQuery.Get("maxResults"); got != "2" {
			t.Errorf("Calendar received maxResults=%q, want %q (canonical pageSize mapped to Calendar's own param)", got, "2")
		}
	})

	t.Run("feeding the nextCursor back in reaches Calendar as pageToken and fetches the remaining event", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gcal-list-events", fixture.userID, initiated.ID,
			fmt.Sprintf(`{"pageSize":2,"cursor":%q}`, nextCursor))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		ids := gcalEventIDsFromData(t, dto.Data)
		if len(ids) != 1 {
			t.Fatalf("second page returned %d events, want 1 (the third and last event)", len(ids))
		}
		if dto.NextCursor != "" {
			t.Errorf("nextCursor = %q, want empty — the last page carries no further cursor", dto.NextCursor)
		}
		if got := fakeGoogle.LastEventsQuery.Get("pageToken"); got != nextCursor {
			t.Errorf(`Calendar received pageToken=%q, want the previous page's nextCursor %q`, got, nextCursor)
		}
	})
}

// TestGoogleCalendarJourney_CreateEventBuildsTheNestedJSONBodyFromTheFlatInputSchema
// is the nested-body AC: gcal-create-event's dotted body mapping
// (start.dateTime/end.dateTime) builds the {"start":{"dateTime":...},
// "end":{"dateTime":...}} JSON shape Calendar's events.insert requires from
// the tool's flat inputSchema (mirrors Hubspot's properties.* body mapping).
func TestGoogleCalendarJourney_CreateEventBuildsTheNestedJSONBodyFromTheFlatInputSchema(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada"})
	wired := support.BootAppWithProviderDefinitions(t, gcalDefinitionAgainst(fakeGoogle))
	fixture := newGCalJourneyFixture(t, wired)
	initiated := activateGCalConnection(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gcal-create-event", fixture.userID, initiated.ID,
		`{"summary":"Design review","startDateTime":"2026-01-05T15:00:00Z","endDateTime":"2026-01-05T15:30:00Z"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	if fakeGoogle.LastCreateEventCalendarID != "primary" {
		t.Errorf("Calendar received calendarId path segment %q, want %q (the inputSchema default applied)", fakeGoogle.LastCreateEventCalendarID, "primary")
	}
	if fakeGoogle.LastCreateEventBody == nil {
		t.Fatal("Calendar received no create-event body")
	}
	if fakeGoogle.LastCreateEventBody["summary"] != "Design review" {
		t.Errorf(`body["summary"] = %v, want %q`, fakeGoogle.LastCreateEventBody["summary"], "Design review")
	}
	start, ok := fakeGoogle.LastCreateEventBody["start"].(map[string]any)
	if !ok {
		t.Fatalf(`Calendar's received body["start"] = %T, want the nested object the dotted body mapping builds`, fakeGoogle.LastCreateEventBody["start"])
	}
	if start["dateTime"] != "2026-01-05T15:00:00Z" {
		t.Errorf(`start.dateTime = %v, want %q`, start["dateTime"], "2026-01-05T15:00:00Z")
	}
	end, ok := fakeGoogle.LastCreateEventBody["end"].(map[string]any)
	if !ok {
		t.Fatalf(`Calendar's received body["end"] = %T, want the nested object the dotted body mapping builds`, fakeGoogle.LastCreateEventBody["end"])
	}
	if end["dateTime"] != "2026-01-05T15:30:00Z" {
		t.Errorf(`end.dateTime = %v, want %q`, end["dateTime"], "2026-01-05T15:30:00Z")
	}
}

// TestGoogleCalendarJourney_CreateEventUpstreamErrorSurfacesAsAToolLevelFailure
// is the upstream-error AC: an upstream Calendar rejection is a tool-level
// failure carrying the provider's own status and message, not a platform
// HTTP error (mirrors TestGmailJourney_SendMessageUpstreamErrorSurfacesAsAToolLevelFailure).
func TestGoogleCalendarJourney_CreateEventUpstreamErrorSurfacesAsAToolLevelFailure(t *testing.T) {
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada",
		CreateEventStatus: http.StatusBadRequest, CreateEventBody: `{"error":{"message":"Invalid time range"}}`,
	})
	wired := support.BootAppWithProviderDefinitions(t, gcalDefinitionAgainst(fakeGoogle))
	fixture := newGCalJourneyFixture(t, wired)
	initiated := activateGCalConnection(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "gcal-create-event", fixture.userID, initiated.ID,
		`{"summary":"Bad event","startDateTime":"2026-01-05T15:30:00Z","endDateTime":"2026-01-05T15:00:00Z"}`)

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
	if !strings.Contains(dto.Error.Message, "Invalid time range") {
		t.Errorf("error.message = %q, want it to surface the provider's response body", dto.Error.Message)
	}
}

// TestGoogleCalendarJourney_PollTriggerFetchesNewlyUpdatedEventsAdvancingTheWatermark
// is the poll AC: a baseline poll delivers nothing historical, a newly
// updated event fires with its id, RFC3339 updated timestamp, and mapped
// payload, Calendar receives updatedMin as the rendered watermark and
// orderBy=updated/singleEvents=true literally on every tick, and two
// successive polls with different watermarks demonstrate the watermark
// advancing (mirrors TestTriggerPollingJourney_HubspotContactCreatedFires
// ProvingTheEngineIsDefinitionDriven).
func TestGoogleCalendarJourney_PollTriggerFetchesNewlyUpdatedEventsAdvancingTheWatermark(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeGoogle := support.NewFakeGoogle(t, support.FakeGoogleScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountName: "Ada"})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, gcalDefinitionAgainst(fakeGoogle), clock.Now)
	fixture := newGCalJourneyFixture(t, wired)
	active := activateGCalConnection(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	// An event that already existed before the instance was ever created —
	// the baseline poll must not fire it.
	fakeGoogle.AddEvent(support.FakeGoogleEvent{
		ID: "evt-preexisting", Summary: "Old news", Status: "confirmed",
		StartDateTime: "2025-12-31T09:00:00Z", EndDateTime: "2025-12-31T09:30:00Z",
		Updated: clock.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	})

	createStatus, created := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, gcalEventUpdatedSlug, `{}`)
	if createStatus != http.StatusCreated {
		t.Fatalf("create trigger instance status = %d, want %d", createStatus, http.StatusCreated)
	}

	var firstUpdatedMin string
	t.Run("baseline poll delivers nothing historical, sending orderBy/singleEvents literally and a rendered updatedMin watermark", func(t *testing.T) {
		pollOnce(t, wired)
		if receiver.CallCount() != 0 {
			t.Fatalf("receiver call count = %d, want 0 — the baseline poll must not fire the pre-existing event", receiver.CallCount())
		}
		if got := fakeGoogle.LastEventsQuery.Get("orderBy"); got != "updated" {
			t.Errorf("orderBy = %q, want the literal %q on every poll tick", got, "updated")
		}
		if got := fakeGoogle.LastEventsQuery.Get("singleEvents"); got != "true" {
			t.Errorf("singleEvents = %q, want the literal %q on every poll tick", got, "true")
		}
		firstUpdatedMin = fakeGoogle.LastEventsQuery.Get("updatedMin")
		if firstUpdatedMin == "" || strings.Contains(firstUpdatedMin, "{watermark}") {
			t.Errorf("updatedMin = %q, want the {watermark} token substituted with a real timestamp", firstUpdatedMin)
		}
	})

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGoogle.AddEvent(support.FakeGoogleEvent{
		ID: "evt-new", Summary: "New sync", Status: "confirmed",
		StartDateTime: "2026-01-01T10:00:00Z", EndDateTime: "2026-01-01T10:30:00Z",
		Updated: clock.Now().UTC().Format(time.RFC3339),
	})

	t.Run("the newly updated event fires with its id, RFC3339 updated timestamp, and mapped payload, and the watermark has advanced", func(t *testing.T) {
		pollThenDispatch(t, wired)
		if receiver.CallCount() != 1 {
			t.Fatalf("receiver call count = %d, want exactly 1", receiver.CallCount())
		}
		last, ok := receiver.LastDelivery()
		if !ok {
			t.Fatal("expected a delivery")
		}
		envelope := decodeDelivery(t, last)
		if envelope.Data["triggerInstanceId"] != created.ID {
			t.Errorf("data.triggerInstanceId = %v, want %q", envelope.Data["triggerInstanceId"], created.ID)
		}
		payload, ok := envelope.Data["payload"].(map[string]any)
		if !ok {
			t.Fatalf("data.payload = %T, want an object", envelope.Data["payload"])
		}
		if payload["id"] != "evt-new" {
			t.Errorf("payload.id = %v, want %q", payload["id"], "evt-new")
		}
		if payload["summary"] != "New sync" {
			t.Errorf("payload.summary = %v, want %q", payload["summary"], "New sync")
		}
		wantUpdated := clock.Now().UTC().Format(time.RFC3339)
		if payload["updated"] != wantUpdated {
			t.Errorf("payload.updated = %v, want the event's own RFC3339 updated timestamp %q", payload["updated"], wantUpdated)
		}

		secondUpdatedMin := fakeGoogle.LastEventsQuery.Get("updatedMin")
		if secondUpdatedMin == "" || secondUpdatedMin == firstUpdatedMin {
			t.Errorf("updatedMin = %q, want it to have advanced past the baseline poll's own watermark %q", secondUpdatedMin, firstUpdatedMin)
		}
	})

	t.Run("a second poll never fires the same event again", func(t *testing.T) {
		clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
		pollThenDispatch(t, wired)
		if receiver.CallCount() != 1 {
			t.Fatalf("receiver call count = %d, want still exactly 1 — the same record must never fire twice", receiver.CallCount())
		}
	})
}
