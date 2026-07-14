// webhook_fanout_test.go pins Slice 8's fan-out behavior (PD45) directly
// against Facade.Enqueue/DispatchOnce: one Event per enabled, filter-matching
// endpoint; an excluded or disabled endpoint gets none; zero matches still
// produce exactly one NO_ENDPOINT placeholder; and — the AC's own words —
// "a failure at one endpoint never blocks delivery to another." Reuses
// facade_test.go's own spyCall/spyResponse/newAccessFacade/findEvent (same
// external test package) rather than restating them.
package delivery_test

import (
	"context"
	"testing"
	"time"

	deliverymemory "beecon/internal/delivery/driven/memory"

	"beecon/internal/delivery"
)

// perURLCaller is a delivery.EndpointCaller whose scripted responses are
// looked up by URL (FIFO per URL) rather than facade_test.go's spyCaller's
// single shared FIFO — required whenever a test needs two endpoints to see
// different outcomes within the same DispatchOnce batch, since ClaimDue may
// claim both due events in one call.
type perURLCaller struct {
	calls   []spyCall
	scripts map[string][]spyResponse
}

func (c *perURLCaller) Post(_ context.Context, url string, headers map[string]string, body []byte, timeout time.Duration) (int, error) {
	c.calls = append(c.calls, spyCall{URL: url, Headers: headers, Body: append([]byte(nil), body...), Timeout: timeout})
	queue := c.scripts[url]
	if len(queue) == 0 {
		return 200, nil
	}
	next := queue[0]
	c.scripts[url] = queue[1:]
	return next.Status, next.Err
}

func newPerURLCaller() *perURLCaller {
	return &perURLCaller{scripts: map[string][]spyResponse{}}
}

// TestEnqueue_FansOutOneEventPerEnabledMatchingEndpoint is AC3/AC4's positive
// case: two endpoints, both ENABLED and both matching the event type, each
// get their own Event row with their own EndpointID.
func TestEnqueue_FansOutOneEventPerEnabledMatchingEndpoint(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	endpointA, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-a", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint A: %v", err)
	}
	endpointB, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-b", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint B: %v", err)
	}

	events, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})

	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 (one per matching enabled endpoint)", len(events))
	}
	gotEndpoints := map[delivery.EndpointID]bool{events[0].EndpointID: true, events[1].EndpointID: true}
	if !gotEndpoints[endpointA.ID] || !gotEndpoints[endpointB.ID] {
		t.Errorf("event endpoint ids = %v, want exactly {%q, %q}", gotEndpoints, endpointA.ID, endpointB.ID)
	}
	if events[0].ID == events[1].ID {
		t.Error("both fanned-out events share the same id — each endpoint must get its own Event")
	}
}

// TestEnqueue_AnEndpointWhoseFilterExcludesTheEventTypeGetsNoEvent is AC3's
// negative case: an endpoint whose filter names other types entirely must
// not receive an event of a type it never listed, even while a sibling
// endpoint with no filter does.
func TestEnqueue_AnEndpointWhoseFilterExcludesTheEventTypeGetsNoEvent(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	matchesEverything, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-all", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint (no filter): %v", err)
	}
	excluded, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-filtered", []string{delivery.EventTypeConnectionExpired})
	if err != nil {
		t.Fatalf("CreateEndpoint (filtered): %v", err)
	}

	events, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})

	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1 (only the unfiltered endpoint matches trigger.event)", len(events))
	}
	if events[0].EndpointID != matchesEverything.ID {
		t.Errorf("EndpointID = %q, want %q", events[0].EndpointID, matchesEverything.ID)
	}
	if events[0].EndpointID == excluded.ID {
		t.Error("the filtered endpoint (connection.expired only) must never receive a trigger.event delivery")
	}
}

// TestEnqueue_ADisabledEndpointGetsNoEvent is AC3/AC5's "stops receiving
// fan-out" from the caller side: an operator-DISABLED endpoint (not
// DISABLED_AUTO — the same fan-out exclusion applies to both) must not be
// selected even though its filter would otherwise match.
func TestEnqueue_ADisabledEndpointGetsNoEvent(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	enabled, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-enabled", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint (enabled): %v", err)
	}
	disabled, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-disabled", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint (to be disabled): %v", err)
	}
	if _, err := f.DisableEndpoint(context.Background(), orgA, disabled.ID); err != nil {
		t.Fatalf("DisableEndpoint: %v", err)
	}

	events, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})

	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 1 || events[0].EndpointID != enabled.ID {
		t.Errorf("events = %+v, want exactly one event addressed to the still-enabled endpoint %q", events, enabled.ID)
	}
}

// TestEnqueue_NoEnabledMatchingEndpointProducesASingleNoEndpointPlaceholder
// is FD7's fallback, now reachable through the multi-endpoint filter/status
// path too (not just "zero endpoints configured"): every endpoint disabled
// or filtered out still yields exactly one NO_ENDPOINT placeholder, never
// zero events and never one per excluded endpoint.
func TestEnqueue_NoEnabledMatchingEndpointProducesASingleNoEndpointPlaceholder(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{Secrets: newAccessFacade(now), Now: now})
	disabled, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-disabled", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if _, err := f.DisableEndpoint(context.Background(), orgA, disabled.ID); err != nil {
		t.Fatalf("DisableEndpoint: %v", err)
	}
	if _, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-filtered", []string{delivery.EventTypeConnectionExpired}); err != nil {
		t.Fatalf("CreateEndpoint (filtered): %v", err)
	}

	events, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})

	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want exactly 1 NO_ENDPOINT placeholder", len(events))
	}
	if events[0].Status != delivery.StatusNoEndpoint {
		t.Errorf("Status = %q, want %q", events[0].Status, delivery.StatusNoEndpoint)
	}
	if events[0].EndpointID != "" {
		t.Errorf("EndpointID = %q, want empty on a NO_ENDPOINT placeholder", events[0].EndpointID)
	}
}

// TestDispatchOnce_OneEndpointsFailureNeverBlocksDeliveryToAnother is the
// AC's headline independence guarantee: two endpoints fan out from the same
// Enqueue call; one's delivery attempt fails, the other's succeeds, in the
// very same DispatchOnce batch — the failure must not prevent, delay, or
// mark failed the sibling's own, independent delivery.
func TestDispatchOnce_OneEndpointsFailureNeverBlocksDeliveryToAnother(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	accessFacade := newAccessFacade(now)
	repo := deliverymemory.NewRepository()
	caller := newPerURLCaller()
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{
		Repository: repo, WorkQueue: repo, Secrets: accessFacade, Caller: caller, Now: now,
	}).WithJitter(func() float64 { return 0.5 })

	failingEndpoint, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-failing", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint (failing): %v", err)
	}
	succeedingEndpoint, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-succeeding", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint (succeeding): %v", err)
	}
	caller.scripts[failingEndpoint.URL] = []spyResponse{{Status: 503}}
	caller.scripts[succeedingEndpoint.URL] = []spyResponse{{Status: 200}}

	events, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	got, err := f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var failingEvent, succeedingEvent *delivery.Event
	for _, e := range events {
		if e.EndpointID == failingEndpoint.ID {
			failingEvent = findEvent(got.Items, e.ID)
		}
		if e.EndpointID == succeedingEndpoint.ID {
			succeedingEvent = findEvent(got.Items, e.ID)
		}
	}
	if failingEvent == nil || succeedingEvent == nil {
		t.Fatalf("could not locate both fanned-out events after dispatch: failing=%v succeeding=%v", failingEvent, succeedingEvent)
	}
	if succeedingEvent.Status != delivery.StatusDelivered {
		t.Errorf("succeeding endpoint's event Status = %q, want %q — the failing sibling must not have blocked it", succeedingEvent.Status, delivery.StatusDelivered)
	}
	if failingEvent.Status != delivery.StatusPending {
		t.Errorf("failing endpoint's event Status = %q, want %q (rescheduled for its own independent retry)", failingEvent.Status, delivery.StatusPending)
	}
	if failingEvent.Attempts != 1 {
		t.Errorf("failing endpoint's event Attempts = %d, want 1", failingEvent.Attempts)
	}

	failingEndpointsAfter, err := f.ListEndpoints(context.Background(), orgA)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	for _, e := range failingEndpointsAfter {
		if e.ID == succeedingEndpoint.ID && e.ConsecutiveFailures != 0 {
			t.Errorf("the succeeding endpoint's ConsecutiveFailures = %d, want 0 — its sibling's failure must never bleed into its own bookkeeping", e.ConsecutiveFailures)
		}
	}
}

// TestDispatchOnce_SignsEachEndpointsAttemptWithOnlyThatEndpointsOwnActiveSecrets
// pins per-endpoint signing (Slice 8): endpoint A's delivery must verify
// under endpoint A's own secret and must NOT verify under endpoint B's
// entirely separate secret, proving secrets are scoped per endpoint, not
// per org.
func TestDispatchOnce_SignsEachEndpointsAttemptWithOnlyThatEndpointsOwnActiveSecrets(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	accessFacade := newAccessFacade(now)
	repo := deliverymemory.NewRepository()
	caller := newPerURLCaller()
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{
		Repository: repo, WorkQueue: repo, Secrets: accessFacade, Caller: caller, Now: now,
	})

	endpointA, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-a", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint A: %v", err)
	}
	endpointB, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-b", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint B: %v", err)
	}

	if _, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if len(caller.calls) != 2 {
		t.Fatalf("caller calls = %d, want 2", len(caller.calls))
	}
	var callToA, callToB *spyCall
	for i := range caller.calls {
		switch caller.calls[i].URL {
		case endpointA.URL:
			callToA = &caller.calls[i]
		case endpointB.URL:
			callToB = &caller.calls[i]
		}
	}
	if callToA == nil || callToB == nil {
		t.Fatalf("did not observe a call to both endpoint URLs: A=%v B=%v", callToA, callToB)
	}

	secretsForA, err := accessFacade.ActiveWebhookSecrets(context.Background(), orgA, string(endpointA.ID))
	if err != nil {
		t.Fatalf("ActiveWebhookSecrets A: %v", err)
	}
	secretsForB, err := accessFacade.ActiveWebhookSecrets(context.Background(), orgA, string(endpointB.ID))
	if err != nil {
		t.Fatalf("ActiveWebhookSecrets B: %v", err)
	}

	sigForA := callToA.Headers[delivery.HeaderWebhookSignature]
	signedWithA, err := delivery.Sign(delivery.EventID(callToA.Headers[delivery.HeaderWebhookID]), now(), callToA.Body, secretsForA)
	if err != nil {
		t.Fatalf("Sign with A's secret: %v", err)
	}
	if !containsSignatureValue(sigForA, signedWithA.Signature) {
		t.Error("endpoint A's delivery does not verify under endpoint A's own secret")
	}
	signedWithB, err := delivery.Sign(delivery.EventID(callToA.Headers[delivery.HeaderWebhookID]), now(), callToA.Body, secretsForB)
	if err == nil && containsSignatureValue(sigForA, signedWithB.Signature) {
		t.Error("endpoint A's delivery verifies under endpoint B's secret — secrets must be scoped per endpoint, not shared across the org")
	}
}
