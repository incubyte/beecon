//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, wireErrorEnvelope,
// doJSONRequest, oauthJourneyFixture/newOAuthJourneyFixture/
// outlookDefinitionAgainst/openConnectPageAndGetState/connectionRowFromDB
// (oauth_handshake_journey_integration_test.go), activateConnectionThroughRealHandshake/
// executeTool/executionResultDTO (tool_execution_journey_integration_test.go),
// outlookMessageReceivedSlug/createTriggerInstance/setConnectionStatus
// (trigger_instances_journey_integration_test.go), pollOnce/pollerLoopName/
// outlookDefinitionWithPollingTrigger/decodeDelivery/deliveredEnvelope
// (trigger_polling_journey_integration_test.go), setWebhookEndpoint/
// dispatcherLoopName (webhook_channel_journey_integration_test.go) — same
// package). This file tells Slice 5's story end to end against the real
// composition root, real FakeMicrosoft/FakeGraph httptest servers, and a
// real SQLite database: a scheduled refresh runs before a token's own
// expiry so a request right after that instant never triggers the
// request-path refresh -> a rotated refresh token replaces the stored one
// -> a permanent refusal expires the connection and delivers a signed
// connection.expired carrying PD32's exact data shape -> a transient
// failure (5xx, network drop) leaves the connection ACTIVE and is retried
// -> a request-path refresh failure delivers its own connection.expired too
// -> concurrent execution and a scheduled refresh perform only one grant ->
// reconciliation catches a behind-our-back revocation -> reconnecting
// restores ACTIVE under the same id and both the scheduler and a paused
// trigger instance resume.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/internal/catalog"
	connectionsbun "beecon/internal/connections/driven/bun"
	"beecon/test/support"
)

// refresherLoopName and reconcilerLoopName mirror app/workers.go's own
// unexported loop names (mirrors dispatcherLoopName's/pollerLoopName's own
// precedent in this package).
const (
	refresherLoopName  = "refresher"
	reconcilerLoopName = "reconciler"
)

func runRefresher(t *testing.T, wired *app.Wired) {
	t.Helper()
	if err := wired.Workers.RunOnce(context.Background(), refresherLoopName); err != nil {
		t.Fatalf("RefreshDueOnce: %v", err)
	}
}

func runReconciler(t *testing.T, wired *app.Wired) {
	t.Helper()
	if err := wired.Workers.RunOnce(context.Background(), reconcilerLoopName); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
}

// outlookDefinitionForTokenSelfHeal is outlookDefinitionWithPollingTrigger
// (trigger_polling_journey_integration_test.go — BaseURL against fakeGraph
// plus the real outlook-message-received trigger) with the
// outlook-list-messages tool also declared against the same fakeGraph, so a
// single journey can exercise scheduled refresh, request-path execution, and
// trigger pause/resume together.
func outlookDefinitionForTokenSelfHeal(fakeMS *support.FakeMicrosoft, fakeGraph *support.FakeGraph) []catalog.ProviderDefinition {
	definitions := outlookDefinitionWithPollingTrigger(fakeMS, fakeGraph)
	definitions[0].Tools = []catalog.ProviderTool{
		{
			Slug:        "outlook-list-messages",
			Name:        "List messages",
			Method:      "GET",
			Path:        "/me/messages",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	return definitions
}

// setConnectionTokenExpiresAt flips a connection's token_expires_at directly
// at the database row (mirrors setConnectionStatus's own "direct row write"
// precedent, trigger_instances_journey_integration_test.go): the only way
// this file forces a token to already be expired without a real hour-long
// wait, distinct from the clock travel used to prove the "still has minutes
// left" proactive-refresh window.
func setConnectionTokenExpiresAt(t *testing.T, wired *app.Wired, connID string, at time.Time) {
	t.Helper()
	_, err := wired.DB.NewUpdate().
		Model((*connectionsbun.ConnectionRow)(nil)).
		Set("token_expires_at = ?", at).
		Where("id = ?", connID).
		Exec(context.Background())
	if err != nil {
		t.Fatalf("set connection token_expires_at: %v", err)
	}
}

// TestTokenSelfHealJourney_ScheduledRefreshBeforeExpirySoARequestRightAfterExpiryNeverTriggersTheRequestPathRefresh
// is Slice 5's own success criterion (AC1), verbatim: a background scan
// refreshes a token nearing expiry (inside BEECON_REFRESH_LEAD) before it
// actually expires, so a consumer executing right after the original expiry
// instant gets a normal success without the request path ever calling
// RefreshGrant again.
func TestTokenSelfHealJourney_ScheduledRefreshBeforeExpirySoARequestRightAfterExpiryNeverTriggersTheRequestPathRefresh(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)

	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{AccessToken: "scheduled-refresh-token", RefreshToken: "initial-refresh-token", ExpiresIn: 3600})
	clock.Advance(55 * time.Minute) // 5 minutes left — inside the default 10-minute BEECON_REFRESH_LEAD, NOT yet expired

	runRefresher(t, wired)

	if fakeMS.RefreshCallCount != 1 {
		t.Fatalf("RefreshCallCount after the scheduled scan = %d, want exactly 1 — the scheduler must refresh before the token actually expires", fakeMS.RefreshCallCount)
	}

	clock.Advance(6 * time.Minute) // now past the ORIGINAL 60-minute expiry instant
	status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, active.ID, "{}")

	if status != http.StatusOK || !result.Successful {
		t.Fatalf("execute right after the original expiry instant: status=%d successful=%v error=%+v", status, result.Successful, result.Error)
	}
	if fakeGraph.LastAuthorizationHeader != "Bearer scheduled-refresh-token" {
		t.Errorf("Authorization = %q, want the scheduled refresh's own token", fakeGraph.LastAuthorizationHeader)
	}
	if fakeMS.RefreshCallCount != 1 {
		t.Errorf("RefreshCallCount after executing = %d, want still exactly 1 — the request path must never refresh again", fakeMS.RefreshCallCount)
	}
}

// TestTokenSelfHealJourney_ScheduledRotatedRefreshTokenReplacesTheStoredOne
// is the slice's own "during a scheduled refresh" wording: a rotated refresh
// token the provider returns must replace the one stored, provably at the
// database row (not merely accepted in memory).
func TestTokenSelfHealJourney_ScheduledRotatedRefreshTokenReplacesTheStoredOne(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	before := connectionRowFromDB(t, wired.DB, active.ID)

	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{AccessToken: "scheduled-access-token", RefreshToken: "rotated-refresh-token", ExpiresIn: 3600})
	setConnectionTokenExpiresAt(t, wired, active.ID, clock.Now().Add(-time.Minute)) // already expired — isolates rotation from AC1's own timing bug

	runRefresher(t, wired)

	after := connectionRowFromDB(t, wired.DB, active.ID)
	if after.EncryptedRefreshToken == before.EncryptedRefreshToken {
		t.Error("encrypted_refresh_token did not change — a rotated refresh token must replace the stored one")
	}
}

// TestTokenSelfHealJourney_APermanentRefusalExpiresTheConnectionAndDeliversASignedConnectionExpiredEvent
// is AC3 and PD32: invalid_grant marks EXPIRED and delivers a signed
// connection.expired carrying exactly connectionId/userId/integrationId/
// providerSlug/reason, verified against the endpoint's own secret.
func TestTokenSelfHealJourney_APermanentRefusalExpiresTheConnectionAndDeliversASignedConnectionExpiredEvent(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	endpointStatus, endpoint := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL)
	if endpointStatus != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", endpointStatus, http.StatusOK)
	}

	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{InvalidGrant: true})
	setConnectionTokenExpiresAt(t, wired, active.ID, clock.Now().Add(-time.Minute))

	runRefresher(t, wired)

	got := fixture.getConnection(t, wired, active.ID)
	if got.Status != "EXPIRED" {
		t.Fatalf("status = %q, want %q after a permanent refusal", got.Status, "EXPIRED")
	}

	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1", receiver.CallCount())
	}
	last, _ := receiver.LastDelivery()
	if !support.VerifyFakeReceiverSignature(last, endpoint.Secret) {
		t.Fatal("signature does not verify against the endpoint's own secret")
	}
	envelope := decodeDelivery(t, last)
	if envelope.Type != "connection.expired" {
		t.Errorf("type = %q, want %q", envelope.Type, "connection.expired")
	}
	if envelope.Data["connectionId"] != active.ID {
		t.Errorf("data.connectionId = %v, want %q", envelope.Data["connectionId"], active.ID)
	}
	if envelope.Data["userId"] != fixture.userID {
		t.Errorf("data.userId = %v, want %q", envelope.Data["userId"], fixture.userID)
	}
	if envelope.Data["integrationId"] != fixture.integrationID {
		t.Errorf("data.integrationId = %v, want %q", envelope.Data["integrationId"], fixture.integrationID)
	}
	if envelope.Data["providerSlug"] != "outlook" {
		t.Errorf("data.providerSlug = %v, want %q", envelope.Data["providerSlug"], "outlook")
	}
	if reason, _ := envelope.Data["reason"].(string); reason == "" {
		t.Error("data.reason is empty, want a non-empty reason")
	}
}

// TestTokenSelfHealJourney_ATransientRefreshFailureLeavesTheConnectionActiveAndIsRetriedOnALaterScan
// is AC4: neither a network-level drop nor a bare provider 5xx must expire
// the connection — both are transient, and a later scan (past the scan's
// own claim lease) must retry successfully.
func TestTokenSelfHealJourney_ATransientRefreshFailureLeavesTheConnectionActiveAndIsRetriedOnALaterScan(t *testing.T) {
	for _, outcome := range []struct {
		name    string
		outcome support.RefreshOutcome
	}{
		{"a bare provider 5xx", support.RefreshOutcome{ServerError: true}},
		{"a network-level drop", support.RefreshOutcome{NetworkDrop: true}},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
				AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
				AccountEmail: "ada@example.com", AccountDisplayName: "Ada", ExpiresIn: 3600,
			})
			fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
			clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
			fixture := newOAuthJourneyFixture(t, wired)
			active := activateConnectionThroughRealHandshake(t, wired, fixture)

			fakeMS.QueueRefreshOutcomes(outcome.outcome)
			setConnectionTokenExpiresAt(t, wired, active.ID, clock.Now().Add(-time.Minute))

			runRefresher(t, wired)

			got := fixture.getConnection(t, wired, active.ID)
			if got.Status != "ACTIVE" {
				t.Fatalf("status = %q, want %q — a transient failure must not expire the connection", got.Status, "ACTIVE")
			}

			clock.Advance(time.Minute) // past this scan's own claim lease
			fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{AccessToken: "recovered-access-token", RefreshToken: "initial-refresh-token", ExpiresIn: 3600})
			runRefresher(t, wired)

			if fakeMS.RefreshCallCount != 2 {
				t.Fatalf("RefreshCallCount = %d, want exactly 2 (the failed attempt plus the retry)", fakeMS.RefreshCallCount)
			}
			got = fixture.getConnection(t, wired, active.ID)
			if got.Status != "ACTIVE" {
				t.Fatalf("status after the retry = %q, want %q", got.Status, "ACTIVE")
			}
		})
	}
}

// TestTokenSelfHealJourney_ARequestPathRefreshFailureAlsoDeliversASignedConnectionExpiredEvent
// is AC5's own half: a request-path 401 that forces a refresh which the
// provider denies must ALSO deliver connection.expired — the same one
// emission funnel the scheduler uses. This test and the permanent-refusal
// test above each drive a DIFFERENT connection through a DIFFERENT detection
// path (scheduler vs. request path); the assertion that only ONE event
// exists per connection — never zero, never doubled — is the count check
// FD1 promises no matter which path detects the transition.
func TestTokenSelfHealJourney_ARequestPathRefreshFailureAlsoDeliversASignedConnectionExpiredEvent(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{StatusCode: http.StatusUnauthorized, Body: `{"error":"InvalidAuthenticationToken"}`})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{InvalidGrant: true})

	status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, active.ID, "{}")

	if status != http.StatusOK {
		t.Fatalf("execute status = %d, want %d", status, http.StatusOK)
	}
	if result.Successful {
		t.Fatal("successful = true, want false — a denied request-path refresh must surface as a tool-level failure")
	}
	if result.Error == nil || result.Error.Code != "connection_not_active" {
		t.Fatalf("error = %+v, want code %q", result.Error, "connection_not_active")
	}
	got := fixture.getConnection(t, wired, active.ID)
	if got.Status != "EXPIRED" {
		t.Fatalf("status = %q, want %q", got.Status, "EXPIRED")
	}

	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1 — the request path's own detection must emit exactly one event, same as the scheduler's", receiver.CallCount())
	}
	last, _ := receiver.LastDelivery()
	envelope := decodeDelivery(t, last)
	if envelope.Type != "connection.expired" {
		t.Errorf("type = %q, want %q", envelope.Type, "connection.expired")
	}
	if envelope.Data["connectionId"] != active.ID {
		t.Errorf("data.connectionId = %v, want %q", envelope.Data["connectionId"], active.ID)
	}
}

// TestTokenSelfHealJourney_ConcurrentExecutionAndAScheduledRefreshPerformOnlyOneRefreshAndTheExecutionSucceeds
// is AC6: a concurrent tool execution (request path) and a scheduled refresh
// racing on the very same connection must perform only one refresh_token
// grant between them, and the execution must still succeed. The connection's
// token is forced already-expired (rather than merely "nearing" expiry) so
// this test isolates the concurrency guarantee from AC1's own known bug
// (documented above) — both entry points reach the identical refreshOnce
// funnel (refreshlock.go) regardless.
func TestTokenSelfHealJourney_ConcurrentExecutionAndAScheduledRefreshPerformOnlyOneRefreshAndTheExecutionSucceeds(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)

	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{AccessToken: "concurrent-refresh-token", RefreshToken: "initial-refresh-token", ExpiresIn: 3600})
	setConnectionTokenExpiresAt(t, wired, active.ID, clock.Now().Add(-time.Minute))

	var wg sync.WaitGroup
	var execStatus int
	var execResult executionResultDTO
	wg.Add(2)
	go func() {
		defer wg.Done()
		execStatus, execResult = executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, active.ID, "{}")
	}()
	go func() {
		defer wg.Done()
		_ = wired.Workers.RunOnce(context.Background(), refresherLoopName)
	}()
	wg.Wait()

	if execStatus != http.StatusOK || !execResult.Successful {
		t.Fatalf("concurrent execute: status=%d successful=%v error=%+v", execStatus, execResult.Successful, execResult.Error)
	}
	if fakeMS.RefreshCallCount != 1 {
		t.Fatalf("RefreshCallCount = %d, want exactly 1 between the concurrent execute and the scheduled refresh", fakeMS.RefreshCallCount)
	}
	got := fixture.getConnection(t, wired, active.ID)
	if got.Status != "ACTIVE" {
		t.Errorf("status = %q, want %q", got.Status, "ACTIVE")
	}
}

// TestTokenSelfHealJourney_ReconciliationDetectsABehindOurBackRevocationAndDeliversAnEvent
// is AC7/PD37: an authenticated probe rejected as unauthorized, followed by a
// refused confirmation refresh, is the only combination reconciliation
// treats as a provider-side revocation — it must mark EXPIRED and deliver
// connection.expired, even though the connection's own token has not
// actually expired yet.
func TestTokenSelfHealJourney_ReconciliationDetectsABehindOurBackRevocationAndDeliversAnEvent(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "initial-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}

	fakeMS.QueueUserInfoProbeStatuses(http.StatusUnauthorized)
	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{InvalidGrant: true})

	runReconciler(t, wired)

	got := fixture.getConnection(t, wired, active.ID)
	if got.Status != "EXPIRED" {
		t.Fatalf("status = %q, want %q after reconciliation detects a behind-our-back revocation", got.Status, "EXPIRED")
	}

	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if receiver.CallCount() != 1 {
		t.Fatalf("receiver call count = %d, want exactly 1", receiver.CallCount())
	}
	envelope := decodeDelivery(t, mustLastDelivery(t, receiver))
	if envelope.Type != "connection.expired" {
		t.Errorf("type = %q, want %q", envelope.Type, "connection.expired")
	}
}

func mustLastDelivery(t *testing.T, receiver *support.FakeReceiver) support.FakeReceiverDelivery {
	t.Helper()
	last, ok := receiver.LastDelivery()
	if !ok {
		t.Fatal("expected a delivery")
	}
	return last
}

// TestTokenSelfHealJourney_ReconnectingAnExpiredConnectionRestoresActiveAndResumesTheSchedulerAndItsPausedTriggerInstance
// is AC8: reconnecting restores ACTIVE under the same id, a trigger instance
// paused while the connection was EXPIRED resumes polling (skipping the gap,
// FD6 — unchanged Slice 4 semantics), and the refresh scheduler treats the
// reconnected connection normally going forward.
func TestTokenSelfHealJourney_ReconnectingAnExpiredConnectionRestoresActiveAndResumesTheSchedulerAndItsPausedTriggerInstance(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "initial-access-token", RefreshToken: "revoked-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada", ExpiresIn: 3600,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionForTokenSelfHeal(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	_, instance := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	pollOnce(t, wired) // baseline poll while still ACTIVE

	fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{InvalidGrant: true})
	setConnectionTokenExpiresAt(t, wired, active.ID, clock.Now().Add(-time.Minute))
	runRefresher(t, wired)

	got := fixture.getConnection(t, wired, active.ID)
	if got.Status != "EXPIRED" {
		t.Fatalf("status = %q, want %q", got.Status, "EXPIRED")
	}

	clock.Advance(31 * time.Second) // clears pollTestIntervalSeconds' own claim lease window
	pollOnce(t, wired)              // the poller notices the connection is no longer ACTIVE and pauses the instance
	instanceStatus, instanceView := getTriggerInstance(t, wired, fixture.orgAuth, instance.ID)
	if instanceStatus != http.StatusOK {
		t.Fatalf("get trigger instance status = %d, want %d", instanceStatus, http.StatusOK)
	}
	if instanceView.Status != "ACTIVE" {
		t.Fatalf("trigger instance Status = %q, want %q — pausing keeps the instance ACTIVE, it just stops polling until the connection resumes", instanceView.Status, "ACTIVE")
	}

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+active.ID+"/reconnect", fixture.orgAuth,
		`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("reconnect status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var reconnected initiatedConnectionDTO
	if err := json.Unmarshal(w.Body.Bytes(), &reconnected); err != nil {
		t.Fatalf("decode reconnect response: %v", err)
	}
	if reconnected.ID != active.ID {
		t.Fatalf("reconnect id = %q, want the same stable id %q", reconnected.ID, active.ID)
	}
	state := openConnectPageAndGetState(t, wired, reconnected)
	callback := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if callback.Code != http.StatusFound {
		t.Fatalf("reconnect callback status = %d, want %d; body=%s", callback.Code, http.StatusFound, callback.Body.String())
	}

	got = fixture.getConnection(t, wired, active.ID)
	if got.Status != "ACTIVE" {
		t.Fatalf("status after reconnect = %q, want %q", got.Status, "ACTIVE")
	}

	t.Run("the refresh scheduler resumes normal operation for the reconnected connection", func(t *testing.T) {
		fakeMS.QueueRefreshOutcomes(support.RefreshOutcome{AccessToken: "post-reconnect-refresh-token", RefreshToken: "initial-refresh-token", ExpiresIn: 3600})
		setConnectionTokenExpiresAt(t, wired, active.ID, clock.Now().Add(-time.Minute))

		runRefresher(t, wired)

		got := fixture.getConnection(t, wired, active.ID)
		if got.Status != "ACTIVE" {
			t.Fatalf("status after the post-reconnect scheduled refresh = %q, want %q", got.Status, "ACTIVE")
		}
	})

	t.Run("the paused trigger instance resumes polling, skipping records that arrived during the outage", func(t *testing.T) {
		fakeGraph.AddMessage("Inbox", support.FakeGraphMessage{
			ID: "msg-during-outage", Subject: "Missed", From: "sender@example.com",
			ReceivedDateTime: clock.Now().UTC().Format(time.RFC3339), BodyPreview: "arrived during the outage",
		})
		clock.Advance(time.Minute) // strictly after msg-during-outage's own timestamp
		receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
		if status, _ := setWebhookEndpoint(t, wired.Router, fixture.orgAuth, receiver.URL); status != http.StatusOK {
			t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
		}
		pollThenDispatch(t, wired) // the resume tick only resets the watermark, it does not also fetch
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
	})
}
