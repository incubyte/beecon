//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, wireErrorEnvelope,
// doJSONRequest, createOrgAndKey (key_rotation_journey_integration_test.go);
// newBrowserTokenFixture/mintBrowserUserToken/browserTokenFixture.createUser
// (browser_token_journey_integration_test.go); newOAuthJourneyFixture/
// activateConnectionThroughRealHandshake, outlookMessageReceivedSlug/
// createTriggerInstance/setConnectionStatus (trigger_instances_journey_integration_test.go);
// outlookDefinitionWithPollingTrigger/pollOnce/pollThenDispatch/pollerLoopName/
// pollTestIntervalSeconds (trigger_polling_journey_integration_test.go);
// outlookDefinitionForTokenSelfHeal/runRefresher/refresherLoopName
// (token_selfheal_journey_integration_test.go); setWebhookEndpoint/
// sendTestEvent/dispatcherLoopName (webhook_channel_journey_integration_test.go);
// scrapeMetrics (rate_limit_metrics_journey_integration_test.go) — same
// package). This file tells Slice 7's story end to end against the real
// composition root: (a) a user token minted with a 25-hour lifetime is
// rejected as unauthorized even though its own exp has not yet passed; (b) a
// database failure during org-or-user (and org-only) auth surfaces as 500,
// never 401; (d) after driving one real delivered event, one real poll run
// with an emitted event, one real scheduled refresh, and one still-PENDING
// event, the admin-guarded /metrics scrape carries every PD38d series with
// the labels that traffic actually produced; and the shutdown-drain story:
// real background workers started for real, an event enqueued while they are
// running, Stop draining the in-flight dispatch for real rather than
// abandoning it, then a re-Wire against the same database proving the event
// was delivered exactly once — never lost, never delivered twice.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"beecon/test/support"
)

// TestOperabilityJourney_A25HourUserTokenIsRejectedAsUnauthorized is PD38a's
// own AC, verbatim: a token whose exp-iat spans more than 24 hours is
// rejected as unauthorized even though, unlike every other rejection case in
// browser_token_journey_integration_test.go's own matrix, its exp has not
// actually passed yet — the lifetime cap is a distinct rule from plain
// expiry.
func TestOperabilityJourney_A25HourUserTokenIsRejectedAsUnauthorized(t *testing.T) {
	wired := support.BootApp(t)
	fixture := newBrowserTokenFixture(t, wired)
	adaID := fixture.createUser(t, wired, "Ada Lovelace")

	now := time.Now()
	oneSecondOver25Hours := mintBrowserUserToken(t, fixture.signingSecret, fixture.signingSecretKid, "HS256", adaID,
		now.Unix(), now.Add(25*time.Hour).Unix())

	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/", "Bearer "+oneSecondOver25Hours, "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}

	t.Run("a token at exactly the 24h cap is still accepted", func(t *testing.T) {
		exactly24h := mintBrowserUserToken(t, fixture.signingSecret, fixture.signingSecretKid, "HS256", adaID,
			now.Unix(), now.Add(24*time.Hour).Unix())
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/integrations/", "Bearer "+exactly24h, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

// TestOperabilityJourney_ADatabaseFailureDuringAuthSurfaces500NeverUnauthorized
// is PD38b's own AC, verbatim: closing the database out from under a
// perfectly valid, still-live org API key must surface as 500 — an
// infrastructure failure, not a verdict on the credential — for both the
// org-or-user-guarded surface and the org-only-guarded surface, rather than
// being misreported as 401.
func TestOperabilityJourney_ADatabaseFailureDuringAuthSurfaces500NeverUnauthorized(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issued := createOrgAndKey(t, wired.Router, adminAuth, "DB Down Co")
	orgAuth := "Bearer " + issued.Key

	t.Run("the key authenticates normally while the database is up", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools/", orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	if err := wired.DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	t.Run("an org-or-user-guarded route surfaces 500, not 401, once the database is down", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools/", orgAuth, "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code == "unauthorized" {
			t.Error("error.code = \"unauthorized\", want anything but that — an infrastructure failure must never look like a rejected credential")
		}
	})

	t.Run("an org-only-guarded route also surfaces 500, not 401, once the database is down", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/logs/", orgAuth, "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
		}
	})
}

// metricLineValue extracts the numeric value of a single Prometheus
// exposition-format line matching exactly metricAndLabels (either a bare
// metric name, or "name{label=\"value\"}") — used for the unlabeled outbox
// gauges and the single-labeled connections-by-status gauge, where label
// ordering ambiguity (client_golang always sorts labels alphabetically, but
// asserting the exact rendered line is still simplest with one label) is not
// a concern. Multi-label counters (delivery-attempts, etc.) are asserted by
// individual label substring instead, the same lenient style
// rate_limit_metrics_journey_integration_test.go's own scrapeMetrics callers
// already use.
func metricLineValue(t *testing.T, body, metricAndLabels string) (float64, bool) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(metricAndLabels) + ` ([0-9eE.+-]+)$`)
	match := re.FindStringSubmatch(body)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		t.Fatalf("parse metric value %q for %q: %v", match[1], metricAndLabels, err)
	}
	return value, true
}

// TestOperabilityJourney_MetricsScrapeExposesAllNewSeriesAfterRealTraffic is
// PD38d's own AC: a delivered trigger.event (delivery-attempt), a real poll
// run that emitted it (trigger-poll-run + trigger-events-emitted), a real
// scheduled refresh (scheduled-refresh-outcome), a still-undelivered event
// (outbox depth + oldest-pending-age both > 0), and a connection put into
// EXPIRED (the connections-by-status gauge) together exercise every new
// series PD38d added, and the admin-guarded scrape must expose them all with
// the labels that traffic actually produced.
func TestOperabilityJourney_MetricsScrapeExposesAllNewSeriesAfterRealTraffic(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)

	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}

	// --- a real poll run that emits a real delivered trigger.event ---
	_, created := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	if created.ID == "" {
		t.Fatal("expected a created trigger instance id")
	}
	pollOnce(t, wired) // baseline poll run: RecordTriggerPollRun(true), nothing emitted

	clock.Advance((pollTestIntervalSeconds + 5) * time.Second)
	fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
		ID: "msg-metrics-journey", Subject: "hello", From: "sender@example.com",
		ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "preview",
	})
	pollThenDispatch(t, wired) // second poll run + one event emitted + one delivered delivery-attempt
	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count after poll+dispatch = %d, want 1", receiver.CallCount())
	}

	// --- a real scheduled refresh (still inside the connection's own lead
	// window, mirroring token_selfheal_journey_integration_test.go's own
	// timing) ---
	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{AccessToken: "refreshed-token", RefreshToken: "initial-refresh-token", ExpiresIn: 3600})
	clock.Advance(55 * time.Minute) // 5 minutes left on the original 60-minute expiry — inside the default 10-minute lead
	runRefresher(t, wired)
	if fakeMS.RefreshCallCount != 1 {
		t.Fatalf("RefreshCallCount after the scheduled scan = %d, want exactly 1", fakeMS.RefreshCallCount)
	}

	// --- a still-PENDING event for the outbox depth/oldest-age gauges ---
	if status := sendTestEvent(t, wired.Router, fixture.orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
	}
	clock.Advance(2 * time.Second) // so the pending event's own age is unambiguously > 0

	// --- an EXPIRED connection for the connections-by-status gauge (direct
	// row flip, mirroring trigger_instances_journey_integration_test.go's own
	// setConnectionStatus precedent — Slice 5's real EXPIRED transitions are
	// already covered end to end by token_selfheal_journey_integration_test.go;
	// this journey only needs one EXPIRED row to exist at scrape time) ---
	setConnectionStatus(t, wired, active.ID, "EXPIRED")

	status, body := scrapeMetrics(t, wired)
	if status != http.StatusOK {
		t.Fatalf("metrics scrape status = %d, want %d", status, http.StatusOK)
	}

	t.Run("delivery-attempt counts the delivered trigger.event by type and result", func(t *testing.T) {
		for _, want := range []string{`type="trigger.event"`, `result="success"`} {
			if !containsMetricLabelPair(body, "beecon_delivery_attempts_total", want) {
				t.Errorf("no beecon_delivery_attempts_total series carries %s:\n%s", want, body)
			}
		}
	})

	t.Run("trigger-poll-run counts real PollOnce runs by result", func(t *testing.T) {
		value, ok := metricLineValue(t, body, `beecon_trigger_poll_runs_total{result="success"}`)
		if !ok {
			t.Fatalf("beecon_trigger_poll_runs_total{result=\"success\"} not found:\n%s", body)
		}
		if value < 2 {
			t.Errorf("beecon_trigger_poll_runs_total{result=\"success\"} = %v, want >= 2 (baseline + the message poll)", value)
		}
	})

	t.Run("trigger-events-emitted counts the fired record by trigger slug", func(t *testing.T) {
		value, ok := metricLineValue(t, body, `beecon_trigger_events_emitted_total{triggerSlug="`+outlookMessageReceivedSlug+`"}`)
		if !ok {
			t.Fatalf("beecon_trigger_events_emitted_total for %q not found:\n%s", outlookMessageReceivedSlug, body)
		}
		if value != 1 {
			t.Errorf("beecon_trigger_events_emitted_total{triggerSlug=%q} = %v, want 1", outlookMessageReceivedSlug, value)
		}
	})

	t.Run("scheduled-refresh-outcome counts the real background refresh", func(t *testing.T) {
		value, ok := metricLineValue(t, body, `beecon_scheduled_refresh_outcomes_total{outcome="refreshed"}`)
		if !ok {
			t.Fatalf("beecon_scheduled_refresh_outcomes_total{outcome=\"refreshed\"} not found:\n%s", body)
		}
		if value < 1 {
			t.Errorf("beecon_scheduled_refresh_outcomes_total{outcome=\"refreshed\"} = %v, want >= 1", value)
		}
	})

	t.Run("outbox depth reflects the one still-PENDING event", func(t *testing.T) {
		value, ok := metricLineValue(t, body, "beecon_outbox_pending_depth")
		if !ok {
			t.Fatalf("beecon_outbox_pending_depth not found:\n%s", body)
		}
		if value != 1 {
			t.Errorf("beecon_outbox_pending_depth = %v, want exactly 1 (the trigger.event was already delivered; only the unsent test event is pending)", value)
		}
	})

	t.Run("outbox oldest-pending-age is greater than zero", func(t *testing.T) {
		value, ok := metricLineValue(t, body, "beecon_outbox_oldest_pending_age_seconds")
		if !ok {
			t.Fatalf("beecon_outbox_oldest_pending_age_seconds not found:\n%s", body)
		}
		if value <= 0 {
			t.Errorf("beecon_outbox_oldest_pending_age_seconds = %v, want > 0", value)
		}
	})

	t.Run("connections-by-status shows the EXPIRED connection", func(t *testing.T) {
		value, ok := metricLineValue(t, body, `beecon_connections_by_status{status="EXPIRED"}`)
		if !ok {
			t.Fatalf(`beecon_connections_by_status{status="EXPIRED"} not found:`+"\n%s", body)
		}
		if value < 1 {
			t.Errorf(`beecon_connections_by_status{status="EXPIRED"} = %v, want >= 1`, value)
		}
	})
}

// containsMetricLabelPair reports whether body has a line starting with
// metricName whose label set also contains labelSubstring — the same lenient
// per-label assertion style rate_limit_metrics_journey_integration_test.go's
// own metrics test already uses for multi-label series, since client_golang's
// label ordering is an implementation detail this journey should not pin.
func containsMetricLabelPair(body, metricName, labelSubstring string) bool {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(metricName) + `\{[^}]*` + regexp.QuoteMeta(labelSubstring) + `[^}]*\} `)
	return re.MatchString(body)
}

// TestOperabilityJourney_ShutdownDrainsThenRestartDeliversEventExactlyOnce is
// the shutdown-drain half of PD38d/the worker hardening: real background
// workers (not RunOnce) are started, an event is enqueued while they are
// running (auto-nudging the dispatcher promptly), Stop drains the real
// in-flight/nudged dispatch instead of abandoning it, and a re-Wire against
// the same database proves the event was delivered exactly once — the second
// boot's own dispatch must find nothing left to do.
func TestOperabilityJourney_ShutdownDrainsThenRestartDeliversEventExactlyOnce(t *testing.T) {
	dsn := support.NewTestDSN(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	first := support.BootAppAt(t, dsn)
	_, issued := createOrgAndKey(t, first.Router, adminAuth, "Shutdown Drain Co")
	orgAuth := "Bearer " + issued.Key
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})

	if status, _ := setWebhookEndpoint(t, first.Router, orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}

	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	first.Workers.Start(workersCtx) // real loops, real goroutines — not RunOnce

	// Event A: enqueued once the real dispatcher is already running and
	// waited out (not RunOnce) — proving the real background loop, driven by
	// Enqueue's own Nudge (app/wiring.go), is what delivers it.
	if status := sendTestEvent(t, first.Router, orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test (event A) status = %d, want %d", status, http.StatusAccepted)
	}
	waitUntilCallCount(t, receiver, 1)

	// Event B: "enqueue mid-flight" — it lands immediately before Stop, while
	// the real workers are still running, with no wait for its own delivery.
	// Whichever way the race with Stop's own cancellation goes, event B must
	// survive it one way or the other: delivered already by the real loop, or
	// still safely PENDING for the next boot to pick up — never lost.
	if status := sendTestEvent(t, first.Router, orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test (event B) status = %d, want %d", status, http.StatusAccepted)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelStop()
	first.Workers.Stop(stopCtx)

	// Re-Wire against the same database (the "restart") and dispatch
	// repeatedly until nothing is left PENDING — whatever Stop's own drain
	// did not finish must still be delivered here, and an already-DELIVERED
	// event must never be attempted again.
	second := support.BootAppAt(t, dsn)
	for i := 0; i < 5; i++ {
		if err := second.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
			t.Fatalf("DispatchOnce after restart (pass %d): %v", i, err)
		}
	}

	listStatus, page := listOutboxEvents(t, second.Router, orgAuth, "")
	if listStatus != http.StatusOK {
		t.Fatalf("list events status = %d, want %d", listStatus, http.StatusOK)
	}
	if len(page.Items) != 2 {
		t.Fatalf("events = %+v, want exactly 2 (event A and event B)", page.Items)
	}
	for _, item := range page.Items {
		if item.DeliveryStatus != "DELIVERED" {
			t.Errorf("event %q deliveryStatus = %q, want %q — nothing may be lost after the restart", item.ID, item.DeliveryStatus, "DELIVERED")
		}
		if item.Attempts != 1 {
			t.Errorf("event %q attempts = %d, want exactly 1 — nothing may be double-processed", item.ID, item.Attempts)
		}
	}
	if receiver.CallCount() != 2 {
		t.Errorf("receiver call count = %d, want exactly 2 (one per event, no duplicates)", receiver.CallCount())
	}
}

// waitUntilCallCount polls receiver's own CallCount every 2ms until it
// reaches at least want or a generous 1s deadline elapses, failing the test
// if it never does — the real-loop (not RunOnce) delivery in this file needs
// to wait for a background goroutine's own schedule rather than assert
// immediately, mirroring internal/worker/worker_test.go's own waitUntil.
func waitUntilCallCount(t *testing.T, receiver *support.FakeReceiver, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if receiver.CallCount() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if receiver.CallCount() < want {
		t.Fatalf("receiver call count = %d, want >= %d within 1s", receiver.CallCount(), want)
	}
}
