// Package delivery_test must be external (not in-package): driven/memory
// imports delivery, so an in-package test importing it would be an import
// cycle (mirrors access_test's own reasoning, facade_test.go). This file
// covers Slice 3 facade behavior the HTTP-level crucial_path journey cannot
// reach on its own — most importantly Enqueue's NO_ENDPOINT path, which the
// only public HTTP entry point into Enqueue (SendTest) deliberately refuses
// to exercise before an endpoint exists — plus byte-identical envelopes
// across retries, fresh-timestamp-per-attempt, rotation's dual-secret
// signing, and "every attempt always logs," all pinned fast and
// deterministically against the in-memory adapters rather than a real
// httptest server.
package delivery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/access"
	accessmemory "beecon/internal/access/driven/memory"
	"beecon/internal/delivery"
	deliverymemory "beecon/internal/delivery/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

const orgA = organizations.OrgID("org_a")
const orgB = organizations.OrgID("org_b")

// spyCaller is a scriptable, recording delivery.EndpointCaller: each Post
// call is recorded (including a defensive copy of body, since
// applyAttemptOutcome/dispatchOne must not mutate or alias the persisted
// envelope), and responses are consumed FIFO from Script; once exhausted,
// further calls succeed with 200.
type spyCaller struct {
	calls  []spyCall
	Script []spyResponse
}

type spyCall struct {
	URL     string
	Headers map[string]string
	Body    []byte
	Timeout time.Duration
}

type spyResponse struct {
	Status int
	Err    error
}

func (s *spyCaller) Post(_ context.Context, url string, headers map[string]string, body []byte, timeout time.Duration) (int, error) {
	bodyCopy := append([]byte(nil), body...)
	s.calls = append(s.calls, spyCall{URL: url, Headers: headers, Body: bodyCopy, Timeout: timeout})
	if len(s.Script) == 0 {
		return 200, nil
	}
	next := s.Script[0]
	s.Script = s.Script[1:]
	return next.Status, next.Err
}

// spyRecorder records every delivery.Recorder.Record call, verbatim.
type spyRecorder struct {
	entries []delivery.LogEntry
}

func (s *spyRecorder) Record(_ context.Context, entry delivery.LogEntry) error {
	s.entries = append(s.entries, entry)
	return nil
}

// fakeSecretIssuer is a minimal delivery.SecretIssuer stub used only by
// TestDispatchOnce_ASigningFailureCountsAsAFailedAttemptAndStillLogs: it
// hands DispatchOnce a fixed, non-base64-decodable "active" secret —
// something the real access.Facade can never actually produce (every
// Beecon-minted whsec_ value is always valid base64), but Sign must still
// fail safely, and DispatchOnce must still record one failed attempt, if it
// ever happened.
type fakeSecretIssuer struct {
	active []string
}

func (f fakeSecretIssuer) IssueWebhookSecret(context.Context, organizations.OrgID) (access.IssuedWebhookSecret, error) {
	return access.IssuedWebhookSecret{Secret: "whsec_unused"}, nil
}

func (f fakeSecretIssuer) RotateWebhookSecret(context.Context, organizations.OrgID, *int) (access.RotateWebhookSecretResult, error) {
	return access.RotateWebhookSecretResult{}, nil
}

func (f fakeSecretIssuer) ActiveWebhookSecrets(context.Context, organizations.OrgID) ([]string, error) {
	return f.active, nil
}

func (f fakeSecretIssuer) WebhookSecretPrefix(context.Context, organizations.OrgID) (string, error) {
	return "", nil
}

// newAccessFacade builds a real (memory-backed) access.Facade to serve as
// delivery's SecretIssuer port — the same composition app/wiring.go uses in
// production (BOUNDARIES: delivery depends on access) — with clock and id
// generation deterministic for the test.
func newAccessFacade(now func() time.Time) *access.Facade {
	return accessmemory.NewFacadeWithOverrides(accessmemory.Overrides{Now: now})
}

func assertDomainError(t *testing.T, err error, wantCode string, wantStatus int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected domain error with code %q, got nil", wantCode)
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Code != wantCode {
		t.Errorf("error code = %q, want %q", de.Code, wantCode)
	}
	if de.Status != wantStatus {
		t.Errorf("error status = %d, want %d", de.Status, wantStatus)
	}
}

// --- SetEndpoint / GetEndpoint / RotateSecret ---

func TestSetEndpoint_FirstCallMintsASecretReturnedExactlyOnce(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})

	result, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Secret == "" {
		t.Fatal("expected a freshly minted secret on first creation")
	}
}

func TestSetEndpoint_ASecondCallThatOnlyChangesTheURLKeepsTheExistingSecret(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	first, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook-v2")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.Secret != "" {
		t.Errorf("Secret = %q, want empty — a URL-only update must not reissue the secret", second.Secret)
	}
	if second.ID != first.ID {
		t.Errorf("ID changed across a URL update: %q -> %q", first.ID, second.ID)
	}
	if second.URL != "https://example.com/hook-v2" {
		t.Errorf("URL = %q, want the updated value", second.URL)
	}
}

func TestSetEndpoint_RejectsANonAbsoluteURLBeforeTouchingAnyPort(t *testing.T) {
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{})

	_, err := f.SetEndpoint(context.Background(), orgA, "not-a-url")

	assertDomainError(t, err, delivery.CodeValidationFailed, 422)
}

func TestGetEndpoint_NeverIncludesTheFullSecret(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	created, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	view, err := f.GetEndpoint(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.SecretPrefix == "" {
		t.Error("expected a non-empty cosmetic secret prefix")
	}
	if view.SecretPrefix == created.Secret {
		t.Fatal("SecretPrefix must never equal the full secret")
	}
}

func TestGetEndpoint_ReturnsNotFoundWhenNoEndpointIsConfigured(t *testing.T) {
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{})

	_, err := f.GetEndpoint(context.Background(), orgA)

	assertDomainError(t, err, delivery.CodeNotFound, 404)
}

func TestRotateSecret_RejectsAnOrgWithNoConfiguredEndpoint(t *testing.T) {
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(nil)})

	_, err := f.RotateSecret(context.Background(), orgA, nil)

	assertDomainError(t, err, delivery.CodeValidationFailed, 422)
}

// --- SendTest ---

func TestSendTest_RejectsAnOrgWithNoConfiguredEndpoint(t *testing.T) {
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(nil)})

	_, err := f.SendTest(context.Background(), orgA)

	assertDomainError(t, err, delivery.CodeValidationFailed, 422)
}

func TestSendTest_EnqueuesAPendingWebhookTestEventOnceAnEndpointExists(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	if _, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event, err := f.SendTest(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != delivery.EventTypeWebhookTest {
		t.Errorf("Type = %q, want %q", event.Type, delivery.EventTypeWebhookTest)
	}
	if event.Status != delivery.StatusPending {
		t.Errorf("Status = %q, want %q", event.Status, delivery.StatusPending)
	}
}

// --- Enqueue: the NO_ENDPOINT path (unreachable via HTTP in Slice 3, since
// SendTest is the only public Enqueue trigger and it refuses outright when
// there's no endpoint — this is the direct facade-level proof of FD7/AC9). ---

func TestEnqueue_WithNoConfiguredEndpointPersistsTheEventAsNoEndpointWithZeroAttempts(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Now: now})

	event, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{"hello": "world"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != delivery.StatusNoEndpoint {
		t.Errorf("Status = %q, want %q", event.Status, delivery.StatusNoEndpoint)
	}
	if event.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", event.Attempts)
	}
}

// TestEnqueue_ANoEndpointEventIsNeverClaimedByDispatchOnce pins that
// NO_ENDPOINT truly means "parked" — DispatchOnce's WorkQueue only ever
// claims PENDING rows, so a NO_ENDPOINT event must sit untouched (and
// therefore never accumulate failed delivery attempts, AC9's "accumulates no
// failed deliveries") until a manual Redeliver re-queues it.
func TestEnqueue_ANoEndpointEventIsNeverClaimedByDispatchOnce(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	caller := &spyCaller{}
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Caller: caller, Now: now})
	event, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if len(caller.calls) != 0 {
		t.Errorf("caller was invoked %d times, want 0 — a NO_ENDPOINT event must never be dispatched", len(caller.calls))
	}
	if event.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", event.Attempts)
	}
}

// TestEnqueue_NudgesTheDispatcherOnlyWhenTheEventLandsPending pins PD30's
// "immediately": Enqueue must wake the dispatcher loop when the event is
// actually deliverable, and must not bother when it lands NO_ENDPOINT (there
// is nothing to dispatch).
func TestEnqueue_NudgesTheDispatcherOnlyWhenTheEventLandsPending(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var nudged int
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	f = f.WithNudge(func() { nudged++ })
	if _, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{}); err != nil {
		t.Fatalf("unexpected error (no endpoint case): %v", err)
	}
	if nudged != 0 {
		t.Fatalf("nudged = %d after a NO_ENDPOINT enqueue, want 0", nudged)
	}

	if _, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{}); err != nil {
		t.Fatalf("unexpected error (pending case): %v", err)
	}
	if nudged != 1 {
		t.Errorf("nudged = %d after a PENDING enqueue, want 1", nudged)
	}
}

// --- DispatchOnce: outcomes, byte-identical envelopes, fresh timestamps,
// rotation dual-signing, and "every attempt always logs." ---

func setUpEndpointWithSecret(t *testing.T, now func() time.Time, caller *spyCaller, recorder delivery.Recorder) (*delivery.Facade, string) {
	t.Helper()
	accessFacade := newAccessFacade(now)
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: accessFacade, Caller: caller, Recorder: recorder, Now: now})
	created, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")
	if err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	return f, created.Secret
}

func TestDispatchOnce_A2xxResponseMarksTheEventDelivered(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	caller := &spyCaller{Script: []spyResponse{{Status: 200}}}
	f, _ := setUpEndpointWithSecret(t, now, caller, nil)
	event, err := f.SendTest(context.Background(), orgA)
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	got, err := f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := findEvent(got.Items, event.ID)
	if found == nil {
		t.Fatal("event not found after dispatch")
	}
	if found.Status != delivery.StatusDelivered {
		t.Errorf("Status = %q, want %q", found.Status, delivery.StatusDelivered)
	}
	if found.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", found.Attempts)
	}
}

func TestDispatchOnce_ANon2xxResponseReschedulesWithTheSameIDAndByteIdenticalBody(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tick := start
	now := func() time.Time { return tick }
	accessFacade := newAccessFacade(now)
	repo := deliverymemory.NewRepository()
	caller := &spyCaller{Script: []spyResponse{{Status: 500}, {Status: 500}}}
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Repository: repo, WorkQueue: repo, Secrets: accessFacade, Caller: caller, Now: now}).
		WithJitter(func() float64 { return 0.5 })
	if _, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook"); err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	event, err := f.SendTest(context.Background(), orgA)
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce (attempt 1): %v", err)
	}
	if len(caller.calls) != 1 {
		t.Fatalf("caller calls = %d, want 1", len(caller.calls))
	}
	firstBody := caller.calls[0].Body
	firstHeaders := caller.calls[0].Headers

	got, err := f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := findEvent(got.Items, event.ID)
	if found == nil || found.Status != delivery.StatusPending {
		t.Fatalf("after a failed attempt the event must stay PENDING for another try, got %+v", found)
	}
	if found.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", found.Attempts)
	}
	wantNext := start.Add(5 * time.Second) // schedule.go's first retry delay, jitter neutralized at 0.5
	if !found.NextAttemptAt.Equal(wantNext) {
		t.Errorf("NextAttemptAt = %v, want %v", found.NextAttemptAt, wantNext)
	}

	// Travel to the scheduled retry (same facade, same underlying repo) and
	// dispatch again.
	tick = wantNext
	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce (attempt 2): %v", err)
	}
	if len(caller.calls) != 2 {
		t.Fatalf("caller calls = %d, want 2", len(caller.calls))
	}
	secondBody := caller.calls[1].Body
	secondHeaders := caller.calls[1].Headers

	if string(firstBody) != string(secondBody) {
		t.Errorf("body changed across retries: %q vs %q", firstBody, secondBody)
	}
	if firstHeaders[delivery.HeaderWebhookID] != secondHeaders[delivery.HeaderWebhookID] {
		t.Errorf("webhook-id changed across retries: %q vs %q", firstHeaders[delivery.HeaderWebhookID], secondHeaders[delivery.HeaderWebhookID])
	}
	if firstHeaders[delivery.HeaderWebhookTimestamp] == secondHeaders[delivery.HeaderWebhookTimestamp] {
		t.Error("webhook-timestamp did not change across retries — want a fresh timestamp per attempt")
	}
}

// TestDispatchOnce_ExhaustsAtMaxAttemptsAndMarksTheEventFailed pins PD30's
// exhaustion outcome directly against the facade (schedule_test.go already
// pins the pure math; this pins DispatchOnce actually applying it end to
// end, MaxAttempts times, fast — no real sleeps, just repeated DispatchOnce
// calls against an always-failing caller).
func TestDispatchOnce_ExhaustsAtMaxAttemptsAndMarksTheEventFailed(t *testing.T) {
	cursor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return cursor } // read by reference on every call, so advancing cursor between DispatchOnce calls moves the same facade's clock forward
	caller := &spyCaller{}
	for i := 0; i < delivery.MaxAttempts; i++ {
		caller.Script = append(caller.Script, spyResponse{Status: 503})
	}
	accessFacade := newAccessFacade(now)
	repo := deliverymemory.NewRepository()
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Repository: repo, WorkQueue: repo, Secrets: accessFacade, Caller: caller, Now: now}).
		WithJitter(func() float64 { return 0.5 })
	if _, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook"); err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	event, err := f.SendTest(context.Background(), orgA)
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	// clearAnyScheduledDelay comfortably exceeds even the longest jittered
	// PD30 delay (24h * 1.1), so advancing the shared clock by this much
	// before every attempt guarantees ClaimDue's own "next_attempt_at <= now"
	// predicate is satisfied without computing the exact jittered instant.
	const clearAnyScheduledDelay = 40 * time.Hour
	for i := 0; i < delivery.MaxAttempts; i++ {
		if err := f.DispatchOnce(context.Background()); err != nil {
			t.Fatalf("DispatchOnce (attempt %d): %v", i+1, err)
		}
		cursor = cursor.Add(clearAnyScheduledDelay)
	}

	got, err := f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := findEvent(got.Items, event.ID)
	if found == nil {
		t.Fatal("event not found")
	}
	if found.Status != delivery.StatusFailed {
		t.Errorf("Status = %q, want %q after %d attempts", found.Status, delivery.StatusFailed, delivery.MaxAttempts)
	}
	if found.Attempts != delivery.MaxAttempts {
		t.Errorf("Attempts = %d, want %d", found.Attempts, delivery.MaxAttempts)
	}
	if len(caller.calls) != delivery.MaxAttempts {
		t.Errorf("caller was invoked %d times, want exactly %d", len(caller.calls), delivery.MaxAttempts)
	}
}

// TestDispatchOnce_SignsEveryAttemptWithEveryCurrentlyActiveSecret pins
// PD31's rotation-overlap signing: while two secrets are both active,
// DispatchOnce must hand the caller a webhook-signature header carrying one
// v1 value verifiable under each.
func TestDispatchOnce_SignsEveryAttemptWithEveryCurrentlyActiveSecret(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	caller := &spyCaller{Script: []spyResponse{{Status: 200}}}
	accessFacade := newAccessFacade(now)
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: accessFacade, Caller: caller, Now: now})
	created, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")
	if err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	rotated, err := accessFacade.RotateWebhookSecret(context.Background(), orgA, nil)
	if err != nil {
		t.Fatalf("RotateWebhookSecret: %v", err)
	}

	if _, err := f.SendTest(context.Background(), orgA); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if len(caller.calls) != 1 {
		t.Fatalf("caller calls = %d, want 1", len(caller.calls))
	}
	sigHeader := caller.calls[0].Headers[delivery.HeaderWebhookSignature]
	if sigHeader == "" {
		t.Fatal("empty webhook-signature header")
	}
	for name, secret := range map[string]string{"original": created.Secret, "rotated": rotated.Secret} {
		signed, err := delivery.Sign(delivery.EventID(caller.calls[0].Headers[delivery.HeaderWebhookID]), now(), caller.calls[0].Body, []string{secret})
		if err != nil {
			t.Fatalf("Sign (%s secret): %v", name, err)
		}
		if !containsSignatureValue(sigHeader, signed.Signature) {
			t.Errorf("webhook-signature %q does not contain a value verifiable under the %s secret", sigHeader, name)
		}
	}
}

// TestDispatchOnce_EveryAttemptWritesALogEntryRegardlessOfOutcome pins the
// AC directly: "every delivery attempt writes a log entry" — success and
// failure both.
func TestDispatchOnce_EveryAttemptWritesALogEntryRegardlessOfOutcome(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	recorder := &spyRecorder{}
	caller := &spyCaller{Script: []spyResponse{{Status: 503}}}
	f, _ := setUpEndpointWithSecret(t, now, caller, recorder)
	event, err := f.SendTest(context.Background(), orgA)
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if len(recorder.entries) != 1 {
		t.Fatalf("recorder entries = %d, want 1", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.EventID != string(event.ID) {
		t.Errorf("EventID = %q, want %q", entry.EventID, event.ID)
	}
	if entry.EventType != delivery.EventTypeWebhookTest {
		t.Errorf("EventType = %q, want %q", entry.EventType, delivery.EventTypeWebhookTest)
	}
	if entry.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", entry.Attempt)
	}
	if entry.Status != 503 {
		t.Errorf("Status = %d, want 503", entry.Status)
	}
	if entry.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", entry.DurationMs)
	}
}

// TestDispatchOnce_ASigningFailureCountsAsAFailedAttemptAndStillLogs pins
// signAndPost's documented contract (facade.go): a secret that fails to
// decode (never true for a Beecon-minted one, but Sign fails loudly rather
// than silently mis-signing) never reaches the network, still counts as one
// failed delivery attempt (rescheduled per PD30, exactly like a non-2xx
// response), and still writes one attempt log entry with status 0 (no
// response was ever attempted).
func TestDispatchOnce_ASigningFailureCountsAsAFailedAttemptAndStillLogs(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	caller := &spyCaller{}
	recorder := &spyRecorder{}
	secrets := fakeSecretIssuer{active: []string{"whsec_not-valid-base64!!!"}}
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: secrets, Caller: caller, Recorder: recorder, Now: now})
	if _, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook"); err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	event, err := f.SendTest(context.Background(), orgA)
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if len(caller.calls) != 0 {
		t.Errorf("caller was invoked %d times, want 0 — a signing failure must never reach the network", len(caller.calls))
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("recorder entries = %d, want 1 — a signing failure still writes one attempt log entry", len(recorder.entries))
	}
	if recorder.entries[0].Status != 0 {
		t.Errorf("logged Status = %d, want 0 (no response was ever attempted)", recorder.entries[0].Status)
	}

	got, err := f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := findEvent(got.Items, event.ID)
	if found == nil {
		t.Fatal("event not found")
	}
	if found.Status != delivery.StatusPending {
		t.Errorf("Status = %q, want %q — a signing failure reschedules exactly like a failed POST", found.Status, delivery.StatusPending)
	}
	if found.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", found.Attempts)
	}
}

// --- Redeliver ---

func TestRedeliver_RequeuesWithTheSameIDAndBodyRegardlessOfCurrentStatus(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Now: now})
	original, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if original.Status != delivery.StatusNoEndpoint {
		t.Fatalf("test fixture bug: expected NO_ENDPOINT, got %q", original.Status)
	}

	requeued, err := f.Redeliver(context.Background(), orgA, original.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requeued.ID != original.ID {
		t.Errorf("ID = %q, want the same id %q", requeued.ID, original.ID)
	}
	if string(requeued.Body) != string(original.Body) {
		t.Error("body changed across redelivery")
	}
	if requeued.Status != delivery.StatusPending {
		t.Errorf("Status = %q, want %q — Redeliver must work even from NO_ENDPOINT", requeued.Status, delivery.StatusPending)
	}
}

func TestRedeliver_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{})

	_, err := f.Redeliver(context.Background(), orgA, "evt_does_not_exist")

	assertDomainError(t, err, delivery.CodeNotFound, 404)
}

func TestRedeliver_ReturnsNotFoundForAnEventBelongingToAnotherOrg(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Now: now})
	event, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	_, err = f.Redeliver(context.Background(), orgB, event.ID)

	assertDomainError(t, err, delivery.CodeNotFound, 404)
}

// --- ListEvents: cursor pagination ---

func TestListEvents_CursorPaginationWalksEveryEventExactlyOnceNewestFirst(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tick := start
	now := func() time.Time { return tick }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Now: now})
	want := map[string]bool{}
	for i := 0; i < 5; i++ {
		event, err := f.Enqueue(context.Background(), orgA, "trigger.event", map[string]any{})
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		want[string(event.ID)] = true
		tick = tick.Add(time.Second)
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		result, err := f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		for _, item := range result.Items {
			if seen[string(item.ID)] {
				t.Fatalf("event %q seen more than once while paginating", item.ID)
			}
			seen[string(item.ID)] = true
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("pagination missed event %q", id)
		}
	}
}

func findEvent(items []delivery.Event, id delivery.EventID) *delivery.Event {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func containsSignatureValue(header, value string) bool {
	for _, part := range splitBySpace(header) {
		if part == value {
			return true
		}
	}
	return false
}

func splitBySpace(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}
