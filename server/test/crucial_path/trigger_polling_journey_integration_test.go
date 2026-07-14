//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, wireErrorEnvelope,
// doJSONRequest, createOrgAndKey, newOAuthJourneyFixture/
// activateConnectionThroughRealHandshake/outlookDefinitionAgainst,
// newHubspotJourneyFixture/activateHubspotConnection/hubspotDefinitionAgainst,
// createTriggerInstance/disableTriggerInstance/enableTriggerInstance/
// outlookMessageReceivedSlug, setConnectionStatus, setWebhookEndpoint,
// listOutboxEvents/findOutboxEvent, dispatcherLoopName, fetchLogs/logEntryDTO
// — same package). This file tells Slice 4's story end to end against the
// real composition root, the real triggers/execution/delivery facades, and a
// real SQLite database: a new provider record fires a signed trigger.event
// with a schema-conforming payload -> baseline delivers nothing historical
// -> the same record never fires twice -> hubspot-contact-created fires,
// proving the poll engine is definition-driven -> disable/re-enable skips
// records that arrived while disabled -> a connection leaving ACTIVE pauses
// silently and reconnecting resumes -> a failing poll logs without killing
// the schedule -> two instances on different folders poll independently ->
// (mandatory carry-forward from the Slice 3 verifier) a no-endpoint org's
// fired event lands NO_ENDPOINT with zero attempts.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/internal/schema"
	"beecon/test/support"
)

// pollerLoopName mirrors app/workers.go's own unexported pollerLoopName
// constant (mirrors dispatcherLoopName's own precedent in
// webhook_channel_journey_integration_test.go, same package).
const pollerLoopName = "poller"

// hubspotContactCreatedSlug is the real hubspot.yaml trigger slug (PD35).
const hubspotContactCreatedSlug = "hubspot-contact-created"

// pollTestInterval is every fixture trigger definition's own
// PollIntervalSeconds below — at the platform floor (BEECON_POLL_MIN_INTERVAL's
// own default, config.go), so a plain 35s clock advance reliably clears one
// poll tick's reschedule with margin, no BEECON_POLL_MIN_INTERVAL override
// needed.
const pollTestIntervalSeconds = 30

func outlookMessagePayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":               map[string]any{"type": "string"},
			"subject":          map[string]any{"type": "string"},
			"from":             map[string]any{"type": "string"},
			"receivedDateTime": map[string]any{"type": "string"},
			"bodyPreview":      map[string]any{"type": "string"},
			"folderId":         map[string]any{"type": "string"},
		},
		"required": []any{"id", "receivedDateTime"},
	}
}

// outlookDefinitionWithPollingTrigger is outlookDefinitionAgainst
// (oauth_handshake_journey_integration_test.go) plus the real
// outlook-message-received trigger's full poll mapping (catalog/providers/
// outlook.yaml, PD35), pointed at fakeGraph instead of the real internet —
// unlike trigger_instances_journey_integration_test.go's own
// outlookDefinitionWithTrigger, which declares no Poll mapping at all (Slice
// 2 never executes it).
func outlookDefinitionWithPollingTrigger(fakeMS *support.FakeMicrosoft, fakeGraph *support.FakeGraph) []catalog.ProviderDefinition {
	definitions := outlookDefinitionAgainst(fakeMS)
	definitions[0].BaseURL = fakeGraph.BaseURL
	definitions[0].Triggers = []catalog.TriggerDefinition{
		{
			Slug:        outlookMessageReceivedSlug,
			Name:        "New message received",
			Description: "Triggered when a new message arrives in the configured mailbox folder.",
			ConfigSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"folderId": map[string]any{"type": "string", "default": "Inbox"}},
			},
			PayloadSchema:       outlookMessagePayloadSchema(),
			Ingestion:           "poll",
			PollIntervalSeconds: pollTestIntervalSeconds,
			Poll: catalog.TriggerPollMapping{
				Method:              "GET",
				Path:                "/me/mailFolders/{config.folderId}/messages",
				Query:               map[string]string{"$filter": "receivedDateTime gt {watermark}", "$orderby": "receivedDateTime"},
				RecordsPath:         "value",
				RecordIDPath:        "id",
				RecordTimestampPath: "receivedDateTime",
				Payload: map[string]string{
					"id": "id", "subject": "subject", "from": "from.emailAddress.address",
					"receivedDateTime": "receivedDateTime", "bodyPreview": "bodyPreview", "folderId": "parentFolderId",
				},
			},
		},
	}
	return definitions
}

// hubspotDefinitionWithPollingTrigger is hubspotDefinitionAgainst
// (hubspot_journey_integration_test.go) plus the real hubspot-contact-created
// trigger's full poll mapping (catalog/providers/hubspot.yaml, PD35) —
// structurally different from Outlook's (POST, a dotted JSON body instead of
// a query filter, a different recordTimestampPath), the "definition-driven,
// not Outlook-specific" proof (AC4).
func hubspotDefinitionWithPollingTrigger(fh *support.FakeHubspot) []catalog.ProviderDefinition {
	definitions := hubspotDefinitionAgainst(fh)
	definitions[0].Triggers = []catalog.TriggerDefinition{
		{
			Slug:         hubspotContactCreatedSlug,
			Name:         "New contact created",
			Description:  "Triggered when a new CRM contact is created, polled by createdate.",
			ConfigSchema: map[string]any{"type": "object"},
			PayloadSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":         map[string]any{"type": "string"},
					"properties": map[string]any{"type": "object"},
				},
				"required": []any{"id"},
			},
			Ingestion:           "poll",
			PollIntervalSeconds: pollTestIntervalSeconds,
			Poll: catalog.TriggerPollMapping{
				Method: "POST",
				Path:   "/crm/v3/objects/contacts/search",
				Body: map[string]string{
					"filterGroups.0.filters.0.propertyName": "createdate",
					"filterGroups.0.filters.0.operator":     "GT",
					"filterGroups.0.filters.0.value":        "{watermark}",
					"sorts.0":                               "createdate",
				},
				RecordsPath:         "results",
				RecordIDPath:        "id",
				RecordTimestampPath: "createdAt",
				Payload:             map[string]string{"id": "id", "properties": "properties"},
			},
		},
	}
	return definitions
}

// pollThenDispatch runs one poller tick immediately followed by one
// dispatcher tick — the two RunOnce calls a real crucial_path journey needs
// to observe a fired trigger.event actually arrive at a receiver (mirrors
// the architecture doc's own "PollOnce + DispatchOnce" phrasing).
func pollThenDispatch(t *testing.T, wired *app.Wired) {
	t.Helper()
	if err := wired.Workers.RunOnce(context.Background(), pollerLoopName); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
}

func pollOnce(t *testing.T, wired *app.Wired) {
	t.Helper()
	if err := wired.Workers.RunOnce(context.Background(), pollerLoopName); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
}

// deliveredEnvelope decodes one FakeReceiver delivery's body as PD32's
// {id, type, createdAt, data} outbox envelope.
type deliveredEnvelope struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

func decodeDelivery(t *testing.T, delivery support.FakeReceiverDelivery) deliveredEnvelope {
	t.Helper()
	var envelope deliveredEnvelope
	if err := json.Unmarshal(delivery.Body, &envelope); err != nil {
		t.Fatalf("decode delivered envelope: %v; body=%s", err, delivery.Body)
	}
	return envelope
}

// TestTriggerPollingJourney_BaselineThenANewMessageFiresASignedEventOnceAndNotAgain
// is AC1/AC2/AC3: baseline delivers nothing historical, a new record fires a
// signed trigger.event whose payload conforms to the definition's own
// payloadSchema, and the same record never fires a second time.
func TestTriggerPollingJourney_BaselineThenANewMessageFiresASignedEventOnceAndNotAgain(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	endpointStatus, endpoint := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL)
	if endpointStatus != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", endpointStatus, http.StatusOK)
	}
	secret := endpoint.Secret
	// A message that already existed before the instance was ever created —
	// AC2's own "records that existed before instance creation produce no
	// events" (baseline poll delivers nothing historical).
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-preexisting", Subject: "Old news", From: "old@example.com",
		ReceivedDateTime: clock.Now().Add(-time.Hour).UTC().Format(time.RFC3339), BodyPreview: "already here",
	})

	createStatus, created := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	if createStatus != http.StatusCreated {
		t.Fatalf("create trigger instance status = %d, want %d", createStatus, http.StatusCreated)
	}

	t.Run("baseline poll delivers nothing historical", func(t *testing.T) {
		pollOnce(t, wired)
		if receiver.CallCount() != 0 {
			t.Fatalf("receiver call count = %d, want 0 — the baseline poll must not fire the pre-existing message", receiver.CallCount())
		}
	})

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-new", Subject: "Hello", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "preview text",
	})

	t.Run("the new message fires a signed trigger.event whose payload conforms to the payload schema", func(t *testing.T) {
		pollThenDispatch(t, wired)

		if receiver.CallCount() != 1 {
			t.Fatalf("receiver call count = %d, want exactly 1", receiver.CallCount())
		}
		last, ok := receiver.LastDelivery()
		if !ok {
			t.Fatal("expected a delivery")
		}
		if !support.VerifyFakeReceiverSignature(last, secret) {
			t.Fatal("signature does not verify against the endpoint's own secret")
		}

		envelope := decodeDelivery(t, last)
		if envelope.Type != "trigger.event" {
			t.Errorf("type = %q, want %q", envelope.Type, "trigger.event")
		}
		if envelope.Data["triggerInstanceId"] != created.ID {
			t.Errorf("data.triggerInstanceId = %v, want %q", envelope.Data["triggerInstanceId"], created.ID)
		}
		if envelope.Data["connectionId"] != active.ID {
			t.Errorf("data.connectionId = %v, want %q", envelope.Data["connectionId"], active.ID)
		}
		if envelope.Data["userId"] != fixture.userID {
			t.Errorf("data.userId = %v, want %q", envelope.Data["userId"], fixture.userID)
		}
		if envelope.Data["triggerSlug"] != outlookMessageReceivedSlug {
			t.Errorf("data.triggerSlug = %v, want %q", envelope.Data["triggerSlug"], outlookMessageReceivedSlug)
		}
		payload, ok := envelope.Data["payload"].(map[string]any)
		if !ok {
			t.Fatalf("data.payload = %T, want an object", envelope.Data["payload"])
		}
		if payload["id"] != "msg-new" {
			t.Errorf("payload.id = %v, want %q", payload["id"], "msg-new")
		}
		if err := schema.Validate(outlookMessagePayloadSchema(), payload); err != nil {
			t.Errorf("payload %+v does not conform to the definition's payloadSchema: %v", payload, err)
		}
	})

	t.Run("a second poll never fires the same record again", func(t *testing.T) {
		clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
		pollThenDispatch(t, wired)
		if receiver.CallCount() != 1 {
			t.Fatalf("receiver call count = %d, want still exactly 1 — the same record must never fire twice", receiver.CallCount())
		}
	})
}

// TestTriggerPollingJourney_HubspotContactCreatedFiresProvingTheEngineIsDefinitionDriven
// is AC4: a hubspot-contact-created instance fires through the identical
// poll engine, proving it is definition-driven rather than Outlook-specific.
func TestTriggerPollingJourney_HubspotContactCreatedFiresProvingTheEngineIsDefinitionDriven(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com",
	})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, hubspotDefinitionWithPollingTrigger(fakeHubspot), clock.Now)
	fixture := newHubspotJourneyFixture(t, wired)
	active := activateHubspotConnection(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}

	createStatus, _ := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, hubspotContactCreatedSlug, `{}`)
	if createStatus != http.StatusCreated {
		t.Fatalf("create trigger instance status = %d, want %d", createStatus, http.StatusCreated)
	}
	pollOnce(t, wired) // baseline
	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 after the baseline poll", receiver.CallCount())
	}

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeHubspot.AddSearchContact(support.FakeHubspotSearchContact{
		ID: "contact-1", Properties: map[string]string{"email": "new@example.com"}, CreatedAt: clock.Now().UTC().Format(time.RFC3339),
	})

	pollThenDispatch(t, wired)

	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1", receiver.CallCount())
	}
	last, _ := receiver.LastDelivery()
	envelope := decodeDelivery(t, last)
	if envelope.Data["triggerSlug"] != hubspotContactCreatedSlug {
		t.Errorf("data.triggerSlug = %v, want %q", envelope.Data["triggerSlug"], hubspotContactCreatedSlug)
	}
	payload, ok := envelope.Data["payload"].(map[string]any)
	if !ok {
		t.Fatalf("data.payload = %T, want an object", envelope.Data["payload"])
	}
	if payload["id"] != "contact-1" {
		t.Errorf("payload.id = %v, want %q", payload["id"], "contact-1")
	}
	properties, ok := payload["properties"].(map[string]any)
	if !ok || properties["email"] != "new@example.com" {
		t.Errorf("payload.properties = %v, want the new contact's own properties carried through", payload["properties"])
	}
}

// TestTriggerPollingJourney_DisableThenReEnableSkipsRecordsThatArrivedWhileDisabled
// is AC5/PD34/FD6: a disabled instance stops firing; records that arrive
// while disabled are skipped on re-enable (never buffered), and new records
// after re-enable fire normally.
func TestTriggerPollingJourney_DisableThenReEnableSkipsRecordsThatArrivedWhileDisabled(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token", AccountEmail: "ada@example.com", AccountDisplayName: "Ada",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	_, created := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	pollOnce(t, wired) // baseline

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	if status, _ := disableTriggerInstance(t, wired, fixture.orgAuth, created.ID); status != http.StatusOK {
		t.Fatalf("disable status = %d, want %d", status, http.StatusOK)
	}
	// A record "arrives" while the instance is disabled.
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-while-disabled", Subject: "Missed", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "arrived while disabled",
	})
	pollOnce(t, wired) // a DISABLED instance is never claimed at all
	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 — a disabled instance must not fire", receiver.CallCount())
	}

	clock.Advance(time.Minute) // strictly after msg-while-disabled's own timestamp
	if status, _ := enableTriggerInstance(t, wired, fixture.orgAuth, created.ID); status != http.StatusOK {
		t.Fatalf("enable status = %d, want %d", status, http.StatusOK)
	}
	pollThenDispatch(t, wired) // re-enabled instance's own next tick: watermark reset skips the stale record
	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 — a record that arrived while disabled must never fire after re-enable (skip the gap)", receiver.CallCount())
	}

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-after-reenable", Subject: "Fresh", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "arrived after re-enable",
	})
	pollThenDispatch(t, wired)

	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1 — a record arriving after re-enable must fire", receiver.CallCount())
	}
	last, _ := receiver.LastDelivery()
	envelope := decodeDelivery(t, last)
	payload, _ := envelope.Data["payload"].(map[string]any)
	if payload["id"] != "msg-after-reenable" {
		t.Errorf("payload.id = %v, want %q (the record skipped while disabled must never fire)", payload["id"], "msg-after-reenable")
	}
}

// TestTriggerPollingJourney_ConnectionLeavingActivePausesSilentlyAndReconnectResumes
// is AC6: a connection leaving ACTIVE pauses polling automatically with no
// failed-poll log noise, and completing a reconnect resumes it (skipping
// records that arrived during the outage, the same "skip the gap" semantics
// as disable/enable, FD6).
func TestTriggerPollingJourney_ConnectionLeavingActivePausesSilentlyAndReconnectResumes(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token", AccountEmail: "ada@example.com", AccountDisplayName: "Ada",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	pollOnce(t, wired) // baseline

	logsBeforeExpiry := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+active.ID)
	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	setConnectionStatus(t, wired, active.ID, "EXPIRED")

	pollOnce(t, wired)
	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 — a non-ACTIVE connection must pause polling", receiver.CallCount())
	}
	logsAfterExpiry := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+active.ID)
	pollFailureLogs := func(entries []logEntryDTO) int {
		count := 0
		for _, e := range entries {
			if e.Kind == "trigger_poll" {
				count++
			}
		}
		return count
	}
	if pollFailureLogs(logsAfterExpiry.Entries) != pollFailureLogs(logsBeforeExpiry.Entries) {
		t.Fatalf("trigger_poll log entries changed while paused (%d -> %d), want no failed-poll noise",
			pollFailureLogs(logsBeforeExpiry.Entries), pollFailureLogs(logsAfterExpiry.Entries))
	}

	// A record "arrives" while the connection is disconnected.
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-during-outage", Subject: "Missed", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "arrived during the outage",
	})

	clock.Advance(time.Minute)                         // strictly after msg-during-outage's own timestamp
	setConnectionStatus(t, wired, active.ID, "ACTIVE") // "reconnect"
	pollThenDispatch(t, wired)                         // the resume tick only resets watermark, it does not also fetch
	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 — a record that arrived during the outage must never fire after reconnect (skip the gap)", receiver.CallCount())
	}

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-after-reconnect", Subject: "Fresh", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "arrived after reconnect",
	})
	pollThenDispatch(t, wired)

	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1 after reconnect resumes polling", receiver.CallCount())
	}
	last, _ := receiver.LastDelivery()
	envelope := decodeDelivery(t, last)
	payload, _ := envelope.Data["payload"].(map[string]any)
	if payload["id"] != "msg-after-reconnect" {
		t.Errorf("payload.id = %v, want %q", payload["id"], "msg-after-reconnect")
	}
}

// TestTriggerPollingJourney_AFailingPollLogsAndTheNextTickRecovers is AC7:
// a poll failure (a provider error) writes a log entry and reschedules
// without stopping the schedule — the next tick runs normally and fires the
// record the failed tick never got to observe.
func TestTriggerPollingJourney_AFailingPollLogsAndTheNextTickRecovers(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token", AccountEmail: "ada@example.com", AccountDisplayName: "Ada",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	pollOnce(t, wired) // baseline

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-during-outage-500", Subject: "Hello", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "preview",
	})
	fakeGraph.FailNextMailFolderPoll()

	pollOnce(t, wired) // this tick's own fetch returns 500

	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 — a failed poll must fire nothing", receiver.CallCount())
	}
	logsPage := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+active.ID)
	var pollFailureEntry *logEntryDTO
	for i := range logsPage.Entries {
		if logsPage.Entries[i].Kind == "trigger_poll" {
			pollFailureEntry = &logsPage.Entries[i]
		}
	}
	if pollFailureEntry == nil {
		t.Fatalf("no trigger_poll log entry found; entries=%+v", logsPage.Entries)
	}
	if pollFailureEntry.ToolSlug != outlookMessageReceivedSlug {
		t.Errorf("toolSlug (carries the trigger slug) = %q, want %q", pollFailureEntry.ToolSlug, outlookMessageReceivedSlug)
	}

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second) // the schedule was not killed by the failure
	pollThenDispatch(t, wired)                                 // this tick's own fetch succeeds

	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1 — the next tick must recover and fire the record the failed tick never got to observe", receiver.CallCount())
	}
	last, _ := receiver.LastDelivery()
	envelope := decodeDelivery(t, last)
	payload, _ := envelope.Data["payload"].(map[string]any)
	if payload["id"] != "msg-during-outage-500" {
		t.Errorf("payload.id = %v, want %q", payload["id"], "msg-during-outage-500")
	}
}

// TestTriggerPollingJourney_TwoInstancesOnDifferentFoldersPollIndependently
// is AC8: two instances sharing one connection but configured against
// different folders fire independently, each tracking its own watermark.
func TestTriggerPollingJourney_TwoInstancesOnDifferentFoldersPollIndependently(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token", AccountEmail: "ada@example.com", AccountDisplayName: "Ada",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	// Pre-existing messages in both folders, before either instance exists —
	// both instances' own baseline polls must ignore these.
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "inbox-preexisting", Subject: "Old", From: "a@example.com",
		ReceivedDateTime: clock.Now().Add(-time.Hour).UTC().Format(time.RFC3339), BodyPreview: "old",
	})
	fakeGraph.AddMessage("Archive", support.FakeGraphMessage{
		ID: "archive-preexisting", Subject: "Old", From: "a@example.com",
		ReceivedDateTime: clock.Now().Add(-time.Hour).UTC().Format(time.RFC3339), BodyPreview: "old",
	})

	_, instanceInbox := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	_, instanceArchive := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Archive"}`)

	pollOnce(t, wired) // baseline for both instances, in the same claimed batch
	if receiver.CallCount() != 0 {
		t.Fatalf("receiver call count = %d, want 0 after both instances' baseline polls", receiver.CallCount())
	}

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "inbox-new", Subject: "New in Inbox", From: "b@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "new",
	})

	t.Run("a new Inbox message fires only the Inbox instance", func(t *testing.T) {
		pollThenDispatch(t, wired)
		if receiver.CallCount() != 1 {
			t.Fatalf("receiver call count = %d, want exactly 1", receiver.CallCount())
		}
		last, _ := receiver.LastDelivery()
		envelope := decodeDelivery(t, last)
		if envelope.Data["triggerInstanceId"] != instanceInbox.ID {
			t.Errorf("data.triggerInstanceId = %v, want the Inbox instance %q", envelope.Data["triggerInstanceId"], instanceInbox.ID)
		}
	})

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Archive", support.FakeGraphMessage{
		ID: "archive-new", Subject: "New in Archive", From: "c@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "new",
	})

	t.Run("a new Archive message fires only the Archive instance, independent of the Inbox instance's own watermark", func(t *testing.T) {
		pollThenDispatch(t, wired)
		if receiver.CallCount() != 2 {
			t.Fatalf("receiver call count = %d, want exactly 2 (the earlier Inbox delivery plus this one)", receiver.CallCount())
		}
		last, _ := receiver.LastDelivery()
		envelope := decodeDelivery(t, last)
		if envelope.Data["triggerInstanceId"] != instanceArchive.ID {
			t.Errorf("data.triggerInstanceId = %v, want the Archive instance %q", envelope.Data["triggerInstanceId"], instanceArchive.ID)
		}
		payload, _ := envelope.Data["payload"].(map[string]any)
		if payload["id"] != "archive-new" {
			t.Errorf("payload.id = %v, want %q", payload["id"], "archive-new")
		}
	})
}

// TestTriggerPollingJourney_ANoEndpointOrgsFiredEventLandsNoEndpointWithZeroAttempts
// is the mandatory carry-forward from the Slice 3 verifier: an org with no
// webhook endpoint configured that nonetheless has a trigger fire must land
// the event NO_ENDPOINT with zero delivery attempts, asserted over HTTP via
// GET /api/v1/events — not auto-drained, not silently dropped.
func TestTriggerPollingJourney_ANoEndpointOrgsFiredEventLandsNoEndpointWithZeroAttempts(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token", AccountEmail: "ada@example.com", AccountDisplayName: "Ada",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	// Deliberately never PUT /api/v1/webhook-endpoint for this organization.

	_, created := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	pollOnce(t, wired) // baseline

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-no-endpoint", Subject: "Hello", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "preview",
	})
	pollOnce(t, wired)

	listStatus, page := listOutboxEvents(t, wired.Router, fixture.orgAuth, "?type=trigger.event")
	if listStatus != http.StatusOK {
		t.Fatalf("list events status = %d, want %d", listStatus, http.StatusOK)
	}
	if len(page.Items) != 1 {
		t.Fatalf("events = %+v, want exactly 1 fired trigger.event", page.Items)
	}
	event := page.Items[0]
	if event.DeliveryStatus != "NO_ENDPOINT" {
		t.Errorf("deliveryStatus = %q, want %q", event.DeliveryStatus, "NO_ENDPOINT")
	}
	if event.Attempts != 0 {
		t.Errorf("attempts = %d, want 0 — a NO_ENDPOINT event must never be attempted", event.Attempts)
	}
	_ = created
}
