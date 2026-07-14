//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, wireErrorEnvelope,
// doJSONRequest, createOrgAndKey (key_rotation_journey_integration_test.go),
// rfc3339MillisLayout — same package). This file tells Slice 3's story end
// to end against the real composition root, the real delivery/access
// facades, and a real SQLite database — the phase's security surface: set
// endpoint (secret once, GET never shows it, a database dump holds no raw
// whsec_) -> a test event delivers and its signature verifies against the
// raw body via an INDEPENDENT HMAC recomputation (never by calling the
// production signer) -> non-2xx and timeout responses retry on the PD30
// schedule with an identical webhook-id and byte-identical body (clock
// travel, no real sleeps) -> exhaustion -> FAILED -> list -> redeliver ->
// an org with no endpoint accumulates no failed deliveries and rejects a
// test-event request -> a non-absolute URL is rejected -> secret rotation
// (both signatures verify during the overlap window, the old one is dead
// after clock travel past it) -> restart survival (enqueue, tear the app
// down, re-Wire against the same database, the dispatcher still delivers).
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/test/support"
)

type webhookEndpointCreatedDTO struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Secret    string `json:"secret"`
	CreatedAt string `json:"createdAt"`
}

type webhookEndpointViewDTO struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	SecretPrefix string `json:"secretPrefix"`
	CreatedAt    string `json:"createdAt"`
}

type rotatedWebhookSecretDTO struct {
	Secret string `json:"secret"`
}

type outboxEventDTO struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	CreatedAt      string `json:"createdAt"`
	DeliveryStatus string `json:"deliveryStatus"`
	Attempts       int    `json:"attempts"`
	LastAttemptAt  string `json:"lastAttemptAt"`
}

type outboxEventsPageDTO struct {
	Items      []outboxEventDTO `json:"items"`
	NextCursor string           `json:"nextCursor"`
}

// dispatcherLoopName mirrors app/workers.go's own unexported
// dispatcherLoopName constant — the name worker.Group registers the outbox
// dispatcher loop under. It is not exported (workers are an internal
// composition detail), so this journey pins the string literal directly,
// the same way it already pins route paths and JSON field names as the
// production contract.
const dispatcherLoopName = "dispatcher"

func setWebhookEndpoint(t *testing.T, router http.Handler, orgAuth, url string) (int, webhookEndpointCreatedDTO) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodPut, "/api/v1/webhook-endpoint/", orgAuth, `{"url":"`+url+`"}`)
	var dto webhookEndpointCreatedDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode set-endpoint response: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func getWebhookEndpoint(t *testing.T, router http.Handler, orgAuth string) (int, webhookEndpointViewDTO, string) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, "/api/v1/webhook-endpoint/", orgAuth, "")
	var dto webhookEndpointViewDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode get-endpoint response: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto, w.Body.String()
}

func rotateWebhookSecret(t *testing.T, router http.Handler, orgAuth string) (int, rotatedWebhookSecretDTO) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodPost, "/api/v1/webhook-endpoint/rotate-secret", orgAuth, "")
	var dto rotatedWebhookSecretDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode rotate-secret response: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func sendTestEvent(t *testing.T, router http.Handler, orgAuth string) int {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodPost, "/api/v1/webhook-endpoint/test", orgAuth, "")
	return w.Code
}

func listOutboxEvents(t *testing.T, router http.Handler, orgAuth, query string) (int, outboxEventsPageDTO) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, "/api/v1/events/"+query, orgAuth, "")
	var page outboxEventsPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode events page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func redeliverEvent(t *testing.T, router http.Handler, orgAuth, id string) int {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodPost, "/api/v1/events/"+id+"/redeliver", orgAuth, "")
	return w.Code
}

func findOutboxEvent(items []outboxEventDTO, id string) *outboxEventDTO {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

// TestWebhookChannelJourney_EndpointSecretTestEventAndDatabaseNeverHoldsTheRawSecret
// is AC1/AC2/AC3/AC4/AC8's story: setting the endpoint, the secret's
// once-only visibility, and a verifiable signed test delivery.
func TestWebhookChannelJourney_EndpointSecretTestEventAndDatabaseNeverHoldsTheRawSecret(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithDeliveryTimeoutAndClock(t, 50*time.Millisecond, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "Webhook Channel Co")
	orgAuth := "Bearer " + issued.Key
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})

	t.Run("setting the endpoint URL rejects a non-absolute-http(s) value", func(t *testing.T) {
		status, _ := setWebhookEndpoint(t, wired.Router, orgAuth, "not-a-url")
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
		}
	})

	var created webhookEndpointCreatedDTO
	t.Run("setting the endpoint mints a wep_/whsec_ pair, the secret returned exactly once", func(t *testing.T) {
		status, dto := setWebhookEndpoint(t, wired.Router, orgAuth, receiver.URL)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if !strings.HasPrefix(dto.ID, "wep_") {
			t.Errorf("id = %q, want a wep_-prefixed id", dto.ID)
		}
		if !strings.HasPrefix(dto.Secret, "whsec_") {
			t.Errorf("secret = %q, want a whsec_-prefixed secret", dto.Secret)
		}
		created = dto
	})
	secret := created.Secret

	t.Run("GET never shows the secret, only its cosmetic prefix", func(t *testing.T) {
		status, view, rawBody := getWebhookEndpoint(t, wired.Router, orgAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if strings.Contains(rawBody, secret) {
			t.Fatalf("GET response contains the raw secret: %s", rawBody)
		}
		if view.SecretPrefix == "" {
			t.Error("expected a non-empty secretPrefix")
		}
		if view.URL != receiver.URL {
			t.Errorf("url = %q, want %q", view.URL, receiver.URL)
		}
	})

	t.Run("a database dump of webhook_signing_secrets contains no raw whsec_ value", func(t *testing.T) {
		rows, err := wired.DB.QueryContext(context.Background(),
			"SELECT id, organization_id, display_prefix, encrypted_secret FROM webhook_signing_secrets WHERE organization_id = ?", org.ID)
		if err != nil {
			t.Fatalf("dump webhook_signing_secrets: %v", err)
		}
		defer rows.Close()
		rowCount := 0
		for rows.Next() {
			rowCount++
			var id, orgID, displayPrefix, encryptedSecret string
			if err := rows.Scan(&id, &orgID, &displayPrefix, &encryptedSecret); err != nil {
				t.Fatalf("scan row: %v", err)
			}
			for column, value := range map[string]string{
				"id": id, "organization_id": orgID, "display_prefix": displayPrefix, "encrypted_secret": encryptedSecret,
			} {
				if strings.Contains(value, secret) {
					t.Errorf("column %q of the database dump contains the raw secret %q", column, secret)
				}
			}
			if encryptedSecret == secret {
				t.Error("encrypted_secret column stores the secret in plaintext")
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate rows: %v", err)
		}
		if rowCount != 1 {
			t.Fatalf("dumped %d webhook_signing_secrets rows, want exactly 1", rowCount)
		}
	})

	t.Run("requesting a test event delivers a signed webhook.test verifiable via independent HMAC recomputation", func(t *testing.T) {
		status := sendTestEvent(t, wired.Router, orgAuth)
		if status != http.StatusAccepted {
			t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
		}
		if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
			t.Fatalf("DispatchOnce: %v", err)
		}

		if receiver.CallCount() != 1 {
			t.Fatalf("receiver call count = %d, want 1", receiver.CallCount())
		}
		last, ok := receiver.LastDelivery()
		if !ok {
			t.Fatal("expected a delivery")
		}
		var body map[string]any
		if err := json.Unmarshal(last.Body, &body); err != nil {
			t.Fatalf("decode delivered body: %v; body=%s", err, last.Body)
		}
		if body["type"] != "webhook.test" {
			t.Errorf("type = %v, want %q", body["type"], "webhook.test")
		}
		if last.Headers.Get("webhook-id") == "" || last.Headers.Get("webhook-timestamp") == "" || last.Headers.Get("webhook-signature") == "" {
			t.Fatalf("missing Standard Webhooks headers: %v", last.Headers)
		}
		if !support.VerifyFakeReceiverSignature(last, secret) {
			t.Fatal("signature does not verify against the endpoint secret and the exact delivered body")
		}

		listStatus, page := listOutboxEvents(t, wired.Router, orgAuth, "")
		if listStatus != http.StatusOK {
			t.Fatalf("list status = %d, want %d", listStatus, http.StatusOK)
		}
		found := findOutboxEvent(page.Items, last.Headers.Get("webhook-id"))
		if found == nil {
			t.Fatalf("delivered test event not found in list; items=%+v", page.Items)
		}
		if found.DeliveryStatus != "DELIVERED" {
			t.Errorf("deliveryStatus = %q, want %q", found.DeliveryStatus, "DELIVERED")
		}

		// AC11 ("every delivery attempt writes a log entry with event id, event
		// type, attempt number, response status, and duration") — proving the
		// recorder -> logging -> migration-0014-columns -> logs-API chain.
		// GET /api/v1/logs carries no server-side "kind" query filter (only
		// connectionId/userId/toolSlug/from/to — logging/facade.go's own
		// QueryParams), so this filters client-side on the decoded page instead
		// of claiming a filter query param that doesn't exist. fetchLogs/
		// logEntryDTO/logsPageDTO are shared with
		// tool_execution_journey_integration_test.go (same package).
		logsPage := fetchLogs(t, wired, orgAuth, "")
		var deliveryLog *logEntryDTO
		for i := range logsPage.Entries {
			if logsPage.Entries[i].Kind == "webhook_delivery" && logsPage.Entries[i].EventID == last.Headers.Get("webhook-id") {
				deliveryLog = &logsPage.Entries[i]
			}
		}
		if deliveryLog == nil {
			t.Fatalf("no webhook_delivery log entry found for event %q; entries=%+v", last.Headers.Get("webhook-id"), logsPage.Entries)
		}
		if deliveryLog.Attempt != 1 {
			t.Errorf("attempt = %d, want 1", deliveryLog.Attempt)
		}
		if deliveryLog.Status != http.StatusOK {
			t.Errorf("status = %d, want %d", deliveryLog.Status, http.StatusOK)
		}
		if deliveryLog.DurationMs < 0 {
			t.Errorf("durationMs = %d, want >= 0", deliveryLog.DurationMs)
		}
	})
}

// TestWebhookChannelJourney_RetriesExhaustionListRedeliver is AC6/AC7's
// story: non-2xx and timeout responses both retry with an identical
// webhook-id and byte-identical body across the full PD30 schedule (clock
// travel, no real sleeps), exhaustion marks the event FAILED, and a manual
// redeliver succeeds once the endpoint starts answering normally.
func TestWebhookChannelJourney_RetriesExhaustionListRedeliver(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithDeliveryTimeoutAndClock(t, 30*time.Millisecond, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issued := createOrgAndKey(t, wired.Router, adminAuth, "Retry Co")
	orgAuth := "Bearer " + issued.Key

	failing := make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		failing = append(failing, http.StatusInternalServerError)
	}
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{Responses: failing})
	if status, _ := setWebhookEndpoint(t, wired.Router, orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	dispatchOnce := func() {
		t.Helper()
		if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
			t.Fatalf("DispatchOnce: %v", err)
		}
	}

	if status := sendTestEvent(t, wired.Router, orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
	}
	dispatchOnce()
	if receiver.CallCount() != 1 {
		t.Fatalf("attempt 1: receiver call count = %d, want 1", receiver.CallCount())
	}
	firstAttempt, _ := receiver.LastDelivery()
	wantID := firstAttempt.Headers.Get("webhook-id")
	wantBody := append([]byte(nil), firstAttempt.Body...)

	// PD30's schedule, jittered up to ±10% — advancing by delay*1.2 clears
	// the jitter with margin so every subsequent attempt is reliably due.
	scheduleDelays := []time.Duration{
		5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour,
		5 * time.Hour, 10 * time.Hour, 14 * time.Hour, 20 * time.Hour, 24 * time.Hour,
	}
	var lastAttempt support.FakeReceiverDelivery
	for i, delay := range scheduleDelays {
		clock.Advance(delay + delay/5)
		dispatchOnce()
		wantCalls := i + 2
		if receiver.CallCount() != wantCalls {
			t.Fatalf("attempt %d: receiver call count = %d, want %d", wantCalls, receiver.CallCount(), wantCalls)
		}
		lastAttempt, _ = receiver.LastDelivery()
		if lastAttempt.Headers.Get("webhook-id") != wantID {
			t.Fatalf("attempt %d: webhook-id = %q, want %q (must stay stable across retries)", wantCalls, lastAttempt.Headers.Get("webhook-id"), wantID)
		}
		if string(lastAttempt.Body) != string(wantBody) {
			t.Fatalf("attempt %d: body changed across retries", wantCalls)
		}
	}
	if lastAttempt.Headers.Get("webhook-timestamp") == firstAttempt.Headers.Get("webhook-timestamp") {
		t.Error("webhook-timestamp never changed across 10 attempts — want a fresh timestamp per attempt")
	}
	if receiver.CallCount() != 10 {
		t.Fatalf("total receiver calls = %d, want exactly 10 (PD30 MaxAttempts)", receiver.CallCount())
	}

	var failedEventID string
	t.Run("after exhaustion the event shows delivery status FAILED and appears in the events list", func(t *testing.T) {
		status, page := listOutboxEvents(t, wired.Router, orgAuth, "?deliveryStatus=FAILED")
		if status != http.StatusOK {
			t.Fatalf("list status = %d, want %d", status, http.StatusOK)
		}
		found := findOutboxEvent(page.Items, wantID)
		if found == nil {
			t.Fatalf("FAILED event %q not found in list; items=%+v", wantID, page.Items)
		}
		if found.Attempts != 10 {
			t.Errorf("attempts = %d, want 10", found.Attempts)
		}
		failedEventID = found.ID
	})

	t.Run("redeliver re-queues the FAILED event with the same id, and it delivers once the endpoint answers 2xx", func(t *testing.T) {
		receiver.SetResponses([]int{http.StatusOK})
		status := redeliverEvent(t, wired.Router, orgAuth, failedEventID)
		if status != http.StatusAccepted {
			t.Fatalf("redeliver status = %d, want %d", status, http.StatusAccepted)
		}
		dispatchOnce()

		listStatus, page := listOutboxEvents(t, wired.Router, orgAuth, "?deliveryStatus=DELIVERED")
		if listStatus != http.StatusOK {
			t.Fatalf("list status = %d, want %d", listStatus, http.StatusOK)
		}
		found := findOutboxEvent(page.Items, failedEventID)
		if found == nil {
			t.Fatalf("redelivered event %q not found as DELIVERED; items=%+v", failedEventID, page.Items)
		}
	})
}

// TestWebhookChannelJourney_ATimeoutIsRetriedWithTheSameIdAndBody covers
// AC6's other half explicitly: a delivery that never answers within
// BEECON_DELIVERY_TIMEOUT is retried exactly like a non-2xx response.
func TestWebhookChannelJourney_ATimeoutIsRetriedWithTheSameIdAndBody(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	const deliveryTimeout = 20 * time.Millisecond
	wired := support.BootAppWithDeliveryTimeoutAndClock(t, deliveryTimeout, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issued := createOrgAndKey(t, wired.Router, adminAuth, "Timeout Co")
	orgAuth := "Bearer " + issued.Key

	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{
		Responses:    []int{support.TimeoutResponse},
		TimeoutDelay: deliveryTimeout * 10,
	})
	if status, _ := setWebhookEndpoint(t, wired.Router, orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	if status := sendTestEvent(t, wired.Router, orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
	}

	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce (attempt 1, times out): %v", err)
	}
	if receiver.CallCount() != 1 {
		t.Fatalf("attempt 1: receiver call count = %d, want 1", receiver.CallCount())
	}
	first, _ := receiver.LastDelivery()

	clock.Advance(6 * time.Second) // past the ±10%-jittered ~5s first retry delay
	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce (attempt 2, succeeds): %v", err)
	}
	if receiver.CallCount() != 2 {
		t.Fatalf("attempt 2: receiver call count = %d, want 2", receiver.CallCount())
	}
	second, _ := receiver.LastDelivery()

	if first.Headers.Get("webhook-id") != second.Headers.Get("webhook-id") {
		t.Errorf("webhook-id changed after a timeout: %q vs %q", first.Headers.Get("webhook-id"), second.Headers.Get("webhook-id"))
	}
	if string(first.Body) != string(second.Body) {
		t.Error("body changed after a timeout retry")
	}

	status, page := listOutboxEvents(t, wired.Router, orgAuth, "?deliveryStatus=DELIVERED")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want %d", status, http.StatusOK)
	}
	if findOutboxEvent(page.Items, first.Headers.Get("webhook-id")) == nil {
		t.Fatalf("expected the event to be DELIVERED after the retry; items=%+v", page.Items)
	}
}

// TestWebhookChannelJourney_NoEndpointOrgRejectsTestEvent is AC9's HTTP-
// reachable half (the NO_ENDPOINT-persisted-events half of AC9 is exercised
// at the facade level in internal/delivery/facade_test.go, since Slice 3's
// only public Enqueue trigger — SendTest — refuses outright before an
// endpoint exists, so an HTTP-only journey cannot produce a NO_ENDPOINT
// event to list).
func TestWebhookChannelJourney_NoEndpointOrgRejectsTestEvent(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issued := createOrgAndKey(t, wired.Router, adminAuth, "No Endpoint Co")
	orgAuth := "Bearer " + issued.Key

	status := sendTestEvent(t, wired.Router, orgAuth)

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d — requesting a test event with no endpoint configured must be rejected", status, http.StatusUnprocessableEntity)
	}

	getStatus, _, _ := getWebhookEndpoint(t, wired.Router, orgAuth)
	if getStatus != http.StatusNotFound {
		t.Errorf("get-endpoint status = %d, want %d", getStatus, http.StatusNotFound)
	}
}

// TestWebhookChannelJourney_SecretRotationOverlapWindow is AC10: rotating
// the secret returns a new one exactly once, deliveries during the overlap
// window carry signatures verifiable under either secret, and after the
// window only the new secret verifies.
func TestWebhookChannelJourney_SecretRotationOverlapWindow(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithDeliveryTimeoutAndClock(t, 50*time.Millisecond, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey
	_, issued := createOrgAndKey(t, wired.Router, adminAuth, "Rotation Co")
	orgAuth := "Bearer " + issued.Key
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	dispatchOnce := func() {
		t.Helper()
		if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
			t.Fatalf("DispatchOnce: %v", err)
		}
	}

	setStatus, created := setWebhookEndpoint(t, wired.Router, orgAuth, receiver.URL)
	if setStatus != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", setStatus, http.StatusOK)
	}
	originalSecret := created.Secret

	rotateStatus, rotated := rotateWebhookSecret(t, wired.Router, orgAuth)
	if rotateStatus != http.StatusOK {
		t.Fatalf("rotate status = %d, want %d", rotateStatus, http.StatusOK)
	}
	if rotated.Secret == "" || rotated.Secret == originalSecret {
		t.Fatalf("rotated secret = %q, want a fresh value distinct from the original %q", rotated.Secret, originalSecret)
	}
	newSecret := rotated.Secret

	t.Run("during the overlap window a delivery's signature verifies under both the old and the new secret", func(t *testing.T) {
		if status := sendTestEvent(t, wired.Router, orgAuth); status != http.StatusAccepted {
			t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
		}
		dispatchOnce()
		last, ok := receiver.LastDelivery()
		if !ok {
			t.Fatal("expected a delivery")
		}
		if !support.VerifyFakeReceiverSignature(last, originalSecret) {
			t.Error("signature does not verify under the original (still-overlapping) secret")
		}
		if !support.VerifyFakeReceiverSignature(last, newSecret) {
			t.Error("signature does not verify under the freshly rotated secret")
		}
	})

	t.Run("after the overlap window ends, only the new secret verifies", func(t *testing.T) {
		clock.Advance(24*time.Hour + time.Minute) // past the default 24h overlap window

		if status := sendTestEvent(t, wired.Router, orgAuth); status != http.StatusAccepted {
			t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
		}
		dispatchOnce()
		last, ok := receiver.LastDelivery()
		if !ok {
			t.Fatal("expected a delivery")
		}
		if support.VerifyFakeReceiverSignature(last, originalSecret) {
			t.Error("the original secret still verifies after its overlap window ended — it must be dead")
		}
		if !support.VerifyFakeReceiverSignature(last, newSecret) {
			t.Error("the new secret must still verify after the old one's window ended")
		}
	})
}

// TestWebhookChannelJourney_SurvivesARestart is the outbox's own durability
// guarantee (AC5): an event accepted just before a restart is still
// delivered after it — enqueue against the first boot, tear the app down
// (without dropping the shared in-memory SQLite database), re-Wire against
// the same DSN, and dispatch through the fresh instance.
func TestWebhookChannelJourney_SurvivesARestart(t *testing.T) {
	dsn := support.NewTestDSN(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	// Mirrors TestOrganizationsSurviveRestartAndMigrationsAreIdempotent's own
	// BootAppAt(t, dsn) pattern: no custom clock or delivery timeout is
	// needed here since the first boot deliberately never dispatches, and
	// the second boot's one DispatchOnce call succeeds immediately (the fake
	// receiver answers 200 with no artificial delay).
	first := support.BootAppAt(t, dsn)
	_, issued := createOrgAndKey(t, first.Router, adminAuth, "Restart Co")
	orgAuth := "Bearer " + issued.Key
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})

	if status, _ := setWebhookEndpoint(t, first.Router, orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	if status := sendTestEvent(t, first.Router, orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
	}
	// Deliberately never call DispatchOnce on the first boot — the event is
	// enqueued (persisted) but not yet attempted, exactly the "accepted just
	// before a restart" scenario.

	listStatus, page := listOutboxEvents(t, first.Router, orgAuth, "")
	if listStatus != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listStatus, http.StatusOK)
	}
	if len(page.Items) != 1 || page.Items[0].DeliveryStatus != "PENDING" {
		t.Fatalf("items = %+v, want exactly one PENDING event before the restart", page.Items)
	}
	pendingEventID := page.Items[0].ID

	// Re-wire against the same DSN — the "restart" — without closing the
	// first connection (app_factory.go's own documented cache=shared
	// contract), mirroring the organizations journey's restart-survival test.
	second := support.BootAppAt(t, dsn)
	secondOrgAuth := orgAuth // the org's api key row lives in the shared database too

	if err := second.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce after restart: %v", err)
	}

	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count after restart-dispatch = %d, want 1", receiver.CallCount())
	}

	afterStatus, afterPage := listOutboxEvents(t, second.Router, secondOrgAuth, "?deliveryStatus=DELIVERED")
	if afterStatus != http.StatusOK {
		t.Fatalf("list status = %d, want %d", afterStatus, http.StatusOK)
	}
	if findOutboxEvent(afterPage.Items, pendingEventID) == nil {
		t.Fatalf("event %q enqueued before the restart was not delivered after it; items=%+v", pendingEventID, afterPage.Items)
	}
}
