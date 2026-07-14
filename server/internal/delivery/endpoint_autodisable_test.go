// endpoint_autodisable_test.go pins Slice 8's auto-disable bookkeeping
// (PD45) directly against Facade.DispatchOnce's own applyAutoDisableBookkeeping
// step: the exact threshold boundary (N-1 consecutive terminal FAILED events
// leave an endpoint ENABLED, the Nth flips it DISABLED_AUTO and drops it from
// subsequent fan-out), a DELIVERED event resetting the counter before the
// threshold is ever reached, and EnableEndpoint resetting the counter and
// resuming fan-out for an endpoint auto-disable quarantined. Reuses
// facade_test.go's own spyCaller/spyResponse/newAccessFacade/findEvent (same
// external test package) rather than restating them.
package delivery_test

import (
	"context"
	"testing"
	"time"

	deliverymemory "beecon/internal/delivery/driven/memory"

	"beecon/internal/delivery"
)

// clearAnyScheduledDelay comfortably exceeds even the longest jittered PD30
// delay (24h * 1.1) — advancing the shared clock by this much before every
// attempt guarantees ClaimDue's "next_attempt_at <= now" predicate is
// satisfied without computing the exact jittered instant (mirrors
// facade_test.go's own TestDispatchOnce_ExhaustsAtMaxAttemptsAndMarksTheEventFailed).
const clearAnyScheduledDelay = 40 * time.Hour

// autoDisableTestFixture bundles the facade, its single endpoint's id, and
// the shared mutable clock every test in this file drives.
type autoDisableTestFixture struct {
	f          *delivery.Facade
	endpointID delivery.EndpointID
	caller     *spyCaller
	cursor     *time.Time
}

func newAutoDisableTestFixture(t *testing.T) *autoDisableTestFixture {
	t.Helper()
	cursor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return cursor }
	caller := &spyCaller{}
	accessFacade := newAccessFacade(now)
	repo := deliverymemory.NewRepository()
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{
		Repository: repo, WorkQueue: repo, Secrets: accessFacade, Caller: caller, Now: now,
	}).WithJitter(func() float64 { return 0.5 })
	created, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")
	if err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	return &autoDisableTestFixture{f: f, endpointID: created.ID, caller: caller, cursor: &cursor}
}

// exhaustOneEventToTerminalFailed enqueues a fresh event and scripts+drives
// exactly delivery.MaxAttempts always-failing DispatchOnce calls so it ends
// terminal FAILED — one full "consecutive failure" unit for the endpoint's
// own bookkeeping.
func (fx *autoDisableTestFixture) exhaustOneEventToTerminalFailed(t *testing.T) {
	t.Helper()
	for i := 0; i < delivery.MaxAttempts; i++ {
		fx.caller.Script = append(fx.caller.Script, spyResponse{Status: 503})
	}
	events, err := fx.f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	event := events[0]

	for i := 0; i < delivery.MaxAttempts; i++ {
		if err := fx.f.DispatchOnce(context.Background()); err != nil {
			t.Fatalf("DispatchOnce (attempt %d): %v", i+1, err)
		}
		*fx.cursor = fx.cursor.Add(clearAnyScheduledDelay)
	}

	got, err := fx.f.ListEvents(context.Background(), orgA, delivery.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := findEvent(got.Items, event.ID)
	if found == nil || found.Status != delivery.StatusFailed {
		t.Fatalf("test fixture bug: event did not reach terminal FAILED, got %+v", found)
	}
}

// deliverOneEventSuccessfully enqueues a fresh event and drives exactly one
// 2xx-scripted DispatchOnce call so it ends DELIVERED.
func (fx *autoDisableTestFixture) deliverOneEventSuccessfully(t *testing.T) {
	t.Helper()
	fx.caller.Script = append(fx.caller.Script, spyResponse{Status: 200})
	if _, err := fx.f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := fx.f.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
}

func (fx *autoDisableTestFixture) endpoint(t *testing.T) delivery.EndpointListItem {
	t.Helper()
	endpoints, err := fx.f.ListEndpoints(context.Background(), orgA)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	for _, e := range endpoints {
		if e.ID == fx.endpointID {
			return e
		}
	}
	t.Fatalf("endpoint %q not found in ListEndpoints", fx.endpointID)
	return delivery.EndpointListItem{}
}

// TestAutoDisable_FourConsecutiveTerminalFailuresLeaveTheEndpointStillEnabled
// pins the threshold boundary's lower side: one short of
// BEECON_ENDPOINT_AUTODISABLE_FAILURES (the memory facade's test default is
// 5, matching config's own default), the endpoint must still be ENABLED.
func TestAutoDisable_FourConsecutiveTerminalFailuresLeaveTheEndpointStillEnabled(t *testing.T) {
	fx := newAutoDisableTestFixture(t)

	for i := 0; i < 4; i++ {
		fx.exhaustOneEventToTerminalFailed(t)
	}

	endpoint := fx.endpoint(t)
	if endpoint.ConsecutiveFailures != 4 {
		t.Errorf("ConsecutiveFailures = %d, want 4", endpoint.ConsecutiveFailures)
	}
	if endpoint.Status != delivery.EndpointStatusEnabled {
		t.Errorf("Status = %q, want %q — 4 consecutive failures must not yet auto-disable a threshold-5 endpoint", endpoint.Status, delivery.EndpointStatusEnabled)
	}
}

// TestAutoDisable_TheFifthConsecutiveTerminalFailureFlipsToDisabledAutoAndDropsFromFanOut
// pins the threshold boundary's other side, plus the AC's "stops receiving
// fan-out": once the 5th consecutive event reaches terminal FAILED, the
// endpoint must be DISABLED_AUTO, and a subsequent Enqueue for this org (its
// only endpoint) must land NO_ENDPOINT rather than route to it.
func TestAutoDisable_TheFifthConsecutiveTerminalFailureFlipsToDisabledAutoAndDropsFromFanOut(t *testing.T) {
	fx := newAutoDisableTestFixture(t)

	for i := 0; i < 5; i++ {
		fx.exhaustOneEventToTerminalFailed(t)
	}

	endpoint := fx.endpoint(t)
	if endpoint.ConsecutiveFailures != 5 {
		t.Errorf("ConsecutiveFailures = %d, want 5", endpoint.ConsecutiveFailures)
	}
	if endpoint.Status != delivery.EndpointStatusDisabledAuto {
		t.Fatalf("Status = %q, want %q after the 5th consecutive terminal failure", endpoint.Status, delivery.EndpointStatusDisabledAuto)
	}

	events, err := fx.f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})
	if err != nil {
		t.Fatalf("Enqueue after auto-disable: %v", err)
	}
	if len(events) != 1 || events[0].Status != delivery.StatusNoEndpoint {
		t.Errorf("post-auto-disable Enqueue = %+v, want a single NO_ENDPOINT placeholder — the DISABLED_AUTO endpoint must never receive fan-out again", events)
	}
	if events[0].EndpointID != "" {
		t.Errorf("EndpointID = %q, want empty on a NO_ENDPOINT placeholder", events[0].EndpointID)
	}
}

// TestAutoDisable_ADeliveredEventResetsTheConsecutiveFailureCounterToZeroBeforeTheThresholdIsReached
// pins the AC's "a successful delivery resets an endpoint's consecutive-
// failure counter": two consecutive terminal failures bring the counter to
// 2, well short of the threshold; a single DELIVERED event then resets it to
// 0 rather than merely pausing it, so a subsequent failure starts counting
// from 1, not 3.
func TestAutoDisable_ADeliveredEventResetsTheConsecutiveFailureCounterToZeroBeforeTheThresholdIsReached(t *testing.T) {
	fx := newAutoDisableTestFixture(t)
	fx.exhaustOneEventToTerminalFailed(t)
	fx.exhaustOneEventToTerminalFailed(t)
	if got := fx.endpoint(t).ConsecutiveFailures; got != 2 {
		t.Fatalf("test fixture bug: ConsecutiveFailures = %d after 2 failures, want 2", got)
	}

	fx.deliverOneEventSuccessfully(t)

	if got := fx.endpoint(t).ConsecutiveFailures; got != 0 {
		t.Fatalf("ConsecutiveFailures = %d after a DELIVERED event, want 0 (reset, not merely paused)", got)
	}
	if got := fx.endpoint(t).Status; got != delivery.EndpointStatusEnabled {
		t.Fatalf("Status = %q, want %q", got, delivery.EndpointStatusEnabled)
	}

	fx.exhaustOneEventToTerminalFailed(t)
	if got := fx.endpoint(t).ConsecutiveFailures; got != 1 {
		t.Errorf("ConsecutiveFailures = %d after one failure post-reset, want 1 (not accumulated from before the reset)", got)
	}
}

// TestEnableEndpoint_ResetsTheCounterAndResumesFanOutForAnAutoDisabledEndpoint
// pins the AC's "an operator re-enabling an auto-disabled endpoint resets it
// and resumes fan-out": after auto-disable, EnableEndpoint must both zero the
// counter and make the endpoint eligible for fan-out again.
func TestEnableEndpoint_ResetsTheCounterAndResumesFanOutForAnAutoDisabledEndpoint(t *testing.T) {
	fx := newAutoDisableTestFixture(t)
	for i := 0; i < 5; i++ {
		fx.exhaustOneEventToTerminalFailed(t)
	}
	if got := fx.endpoint(t).Status; got != delivery.EndpointStatusDisabledAuto {
		t.Fatalf("test fixture bug: endpoint is %q, want %q before re-enabling", got, delivery.EndpointStatusDisabledAuto)
	}

	if _, err := fx.f.EnableEndpoint(context.Background(), orgA, fx.endpointID); err != nil {
		t.Fatalf("EnableEndpoint: %v", err)
	}

	reEnabled := fx.endpoint(t)
	if reEnabled.Status != delivery.EndpointStatusEnabled {
		t.Errorf("Status = %q, want %q after EnableEndpoint", reEnabled.Status, delivery.EndpointStatusEnabled)
	}
	if reEnabled.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 after EnableEndpoint", reEnabled.ConsecutiveFailures)
	}

	events, err := fx.f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})
	if err != nil {
		t.Fatalf("Enqueue after re-enable: %v", err)
	}
	if len(events) != 1 || events[0].EndpointID != fx.endpointID {
		t.Errorf("post-re-enable Enqueue = %+v, want exactly one event routed back to endpoint %q", events, fx.endpointID)
	}
}
