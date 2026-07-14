// endpoint_crud_test.go pins Slice 8's multi-endpoint CRUD surface (PD45)
// directly against the facade: the cap (AC2), the event-type filter's own
// validation, list/update/delete/rotate, and the PD31 single-endpoint alias
// continuing to operate over the org's first endpoint once more than one
// exists. Reuses facade_test.go's own newAccessFacade/assertDomainError (same
// external test package) rather than restating them.
package delivery_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	deliverymemory "beecon/internal/delivery/driven/memory"

	"beecon/internal/delivery"
	"beecon/internal/httpx"
)

func newFacadeWithEndpointCap(cap int) *delivery.Facade {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{
		Secrets:     newAccessFacade(now),
		EndpointCap: cap,
		Now:         now,
	})
}

// TestCreateEndpoint_MintsItsOwnURLAndSecretShownExactlyOnce is AC1's
// positive case at the facade level: each endpoint gets its own secret,
// returned only at creation.
func TestCreateEndpoint_MintsItsOwnURLAndSecretShownExactlyOnce(t *testing.T) {
	f := newFacadeWithEndpointCap(5)

	result, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Secret == "" {
		t.Error("expected a freshly minted secret on creation")
	}
	if result.URL != "https://example.com/hook" {
		t.Errorf("URL = %q, want the requested value", result.URL)
	}
	if result.Status != delivery.EndpointStatusEnabled {
		t.Errorf("Status = %q, want %q", result.Status, delivery.EndpointStatusEnabled)
	}
}

// TestCreateEndpoint_RejectsRegistrationBeyondTheConfiguredCapNamingIt is
// AC2: the (cap+1)th endpoint for an org is rejected, and the error names
// the configured cap.
func TestCreateEndpoint_RejectsRegistrationBeyondTheConfiguredCapNamingIt(t *testing.T) {
	const cap = 3
	f := newFacadeWithEndpointCap(cap)
	for i := 0; i < cap; i++ {
		if _, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", nil); err != nil {
			t.Fatalf("CreateEndpoint (%d/%d): %v", i+1, cap, err)
		}
	}

	_, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/one-too-many", nil)
	assertDomainError(t, err, delivery.CodeValidationFailed, 422)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T", err)
	}
	issue, _ := de.Details["issue"].(string)
	if issue == "" {
		t.Fatal("expected the validation error's details to carry an issue message")
	}
	if !strings.Contains(issue, fmt.Sprintf("%d", cap)) {
		t.Errorf("issue = %q, want it to name the configured cap %d", issue, cap)
	}
}

// TestCreateEndpoint_ACapOfOneOrgsOwnCapIsIndependentOfAnotherOrgs proves the
// cap is per-org: org B can still register its first endpoint even after org
// A has exhausted its own cap of one.
func TestCreateEndpoint_ACapOfOneOrgsOwnCapIsIndependentOfAnotherOrgs(t *testing.T) {
	f := newFacadeWithEndpointCap(1)
	if _, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-a", nil); err != nil {
		t.Fatalf("CreateEndpoint for org A: %v", err)
	}

	if _, err := f.CreateEndpoint(context.Background(), orgB, "https://example.com/hook-b", nil); err != nil {
		t.Fatalf("org B's first endpoint was rejected: %v — the cap must be per-org, not installation-wide", err)
	}
}

// TestCreateEndpoint_RejectsAnEventTypeFilterNamingAnUnknownType pins
// ValidateEventTypeFilter's own contract at the CreateEndpoint entry point.
func TestCreateEndpoint_RejectsAnEventTypeFilterNamingAnUnknownType(t *testing.T) {
	f := newFacadeWithEndpointCap(5)

	_, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", []string{"not.a.real.type"})

	assertDomainError(t, err, delivery.CodeValidationFailed, 422)
}

// TestCreateEndpoint_RejectsAnExplicitlyEmptyEventTypeFilter pins the "would
// silently receive nothing, forever" guard: an empty (but non-nil) filter is
// invalid, distinct from an omitted (nil) one.
func TestCreateEndpoint_RejectsAnExplicitlyEmptyEventTypeFilter(t *testing.T) {
	f := newFacadeWithEndpointCap(5)

	_, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", []string{})

	assertDomainError(t, err, delivery.CodeValidationFailed, 422)
}

// TestListEndpoints_ReturnsEveryEndpointWithItsOwnFilterStatusAndFailureCount
// pins the read shape Slice 8's console/SDK consumes.
func TestListEndpoints_ReturnsEveryEndpointWithItsOwnFilterStatusAndFailureCount(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	created, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", []string{delivery.EventTypeTriggerEvent})
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	items, err := f.ListEndpoints(context.Background(), orgA)

	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.ID != created.ID {
		t.Errorf("ID = %q, want %q", item.ID, created.ID)
	}
	if len(item.EventTypes) != 1 || item.EventTypes[0] != delivery.EventTypeTriggerEvent {
		t.Errorf("EventTypes = %v, want [%q]", item.EventTypes, delivery.EventTypeTriggerEvent)
	}
	if item.Status != delivery.EndpointStatusEnabled {
		t.Errorf("Status = %q, want %q", item.Status, delivery.EndpointStatusEnabled)
	}
	if item.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", item.ConsecutiveFailures)
	}
	if item.SecretPrefix == "" {
		t.Error("expected a non-empty cosmetic secret prefix")
	}
}

// TestUpdateEndpoint_ReplacesTheURLAndFilterButNeverReturnsASecret pins the
// whole-object update contract.
func TestUpdateEndpoint_ReplacesTheURLAndFilterButNeverReturnsASecret(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	created, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	updated, err := f.UpdateEndpoint(context.Background(), orgA, created.ID, "https://example.com/hook-v2", []string{delivery.EventTypeConnectionExpired})

	if err != nil {
		t.Fatalf("UpdateEndpoint: %v", err)
	}
	if updated.URL != "https://example.com/hook-v2" {
		t.Errorf("URL = %q, want the updated value", updated.URL)
	}
	if len(updated.EventTypes) != 1 || updated.EventTypes[0] != delivery.EventTypeConnectionExpired {
		t.Errorf("EventTypes = %v, want [%q]", updated.EventTypes, delivery.EventTypeConnectionExpired)
	}
}

// TestUpdateEndpoint_ReturnsNotFoundForAnIDBelongingToAnotherOrganization
// pins org isolation on the CRUD surface.
func TestUpdateEndpoint_ReturnsNotFoundForAnIDBelongingToAnotherOrganization(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	created, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	_, err = f.UpdateEndpoint(context.Background(), orgB, created.ID, "https://example.com/stolen", nil)

	assertDomainError(t, err, delivery.CodeNotFound, 404)
}

// TestDeleteEndpoint_RemovesItSoItNoLongerReceivesFanOut pins AC8's delete.
func TestDeleteEndpoint_RemovesItSoItNoLongerReceivesFanOut(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	created, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	if err := f.DeleteEndpoint(context.Background(), orgA, created.ID); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}

	items, err := f.ListEndpoints(context.Background(), orgA)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d after DeleteEndpoint, want 0", len(items))
	}
	events, err := f.Enqueue(context.Background(), orgA, delivery.EventTypeTriggerEvent, map[string]any{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(events) != 1 || events[0].Status != delivery.StatusNoEndpoint {
		t.Errorf("post-delete Enqueue = %+v, want a single NO_ENDPOINT placeholder", events)
	}
}

// TestDeleteEndpoint_ReturnsNotFoundForAnUnknownID pins the not-found path.
func TestDeleteEndpoint_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f := newFacadeWithEndpointCap(5)

	err := f.DeleteEndpoint(context.Background(), orgA, delivery.EndpointID("wep_does_not_exist"))

	assertDomainError(t, err, delivery.CodeNotFound, 404)
}

// TestRotateEndpointSecret_MintsAFreshSecretForThatSpecificEndpointOnly pins
// AC8's per-endpoint rotate: rotating one endpoint's secret must not disturb
// a sibling endpoint's own, unrelated secret.
func TestRotateEndpointSecret_MintsAFreshSecretForThatSpecificEndpointOnly(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	endpointA, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-a", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint A: %v", err)
	}
	endpointB, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook-b", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint B: %v", err)
	}

	rotated, err := f.RotateEndpointSecret(context.Background(), orgA, endpointA.ID, nil)

	if err != nil {
		t.Fatalf("RotateEndpointSecret: %v", err)
	}
	if rotated.Secret == "" || rotated.Secret == endpointA.Secret {
		t.Error("expected a freshly minted, different secret for endpoint A")
	}

	itemsAfter, err := f.ListEndpoints(context.Background(), orgA)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	for _, item := range itemsAfter {
		if item.ID == endpointB.ID && item.SecretPrefix != endpointB.Secret[:len(item.SecretPrefix)] {
			t.Errorf("endpoint B's secret prefix changed to %q after rotating endpoint A's secret — rotation must be scoped per endpoint", item.SecretPrefix)
		}
	}
}

// TestRotateEndpointSecret_ReturnsNotFoundForAnIDBelongingToAnotherOrganization
// pins org isolation on rotate.
func TestRotateEndpointSecret_ReturnsNotFoundForAnIDBelongingToAnotherOrganization(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	created, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/hook", nil)
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	_, err = f.RotateEndpointSecret(context.Background(), orgB, created.ID, nil)

	assertDomainError(t, err, delivery.CodeNotFound, 404)
}

// --- PD31 alias continuity once more than one endpoint exists (Slice 8) ---

// TestSetEndpoint_PD31AliasOperatesOverTheOrgsFirstEndpointOnly pins the
// architecture's own promise: SetEndpoint/GetEndpoint/RotateSecret/SendTest
// keep working over exactly the org's first (oldest) endpoint, unaffected by
// a second endpoint created afterward through the multi-endpoint CRUD
// surface.
func TestSetEndpoint_PD31AliasOperatesOverTheOrgsFirstEndpointOnly(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	first, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/first-hook")
	if err != nil {
		t.Fatalf("SetEndpoint (first): %v", err)
	}
	if _, err := f.CreateEndpoint(context.Background(), orgA, "https://example.com/second-hook", nil); err != nil {
		t.Fatalf("CreateEndpoint (second): %v", err)
	}

	view, err := f.GetEndpoint(context.Background(), orgA)
	if err != nil {
		t.Fatalf("GetEndpoint: %v", err)
	}
	if view.ID != first.ID {
		t.Errorf("GetEndpoint's ID = %q, want the org's first endpoint %q, not the more recently created one", view.ID, first.ID)
	}

	second, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/first-hook-updated")
	if err != nil {
		t.Fatalf("SetEndpoint (URL-only update): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("a later SetEndpoint call updated endpoint %q, want it to keep updating the first endpoint %q", second.ID, first.ID)
	}
}

// TestSendTest_BypassesAnEndpointsOwnFilterAndDisabledStatus pins the
// documented exception: a requested test delivery targets org's first
// endpoint directly, regardless of that endpoint's own event-type filter or
// enabled/disabled status — useful for diagnosing exactly the endpoint an
// operator is trying to re-enable.
func TestSendTest_BypassesAnEndpointsOwnFilterAndDisabledStatus(t *testing.T) {
	f := newFacadeWithEndpointCap(5)
	created, err := f.SetEndpoint(context.Background(), orgA, "https://example.com/hook")
	if err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	// SetEndpoint's own PD31 endpoint carries no filter by construction; give
	// it one that would exclude webhook.test, then disable it outright — a
	// direct SendTest must still reach it.
	if _, err := f.UpdateEndpoint(context.Background(), orgA, created.ID, "https://example.com/hook", []string{delivery.EventTypeConnectionExpired}); err != nil {
		t.Fatalf("UpdateEndpoint (add an excluding filter): %v", err)
	}
	if _, err := f.DisableEndpoint(context.Background(), orgA, created.ID); err != nil {
		t.Fatalf("DisableEndpoint: %v", err)
	}

	event, err := f.SendTest(context.Background(), orgA)

	if err != nil {
		t.Fatalf("SendTest on a filtered, disabled endpoint: %v — SendTest must bypass both", err)
	}
	if event.EndpointID != created.ID {
		t.Errorf("EndpointID = %q, want %q", event.EndpointID, created.ID)
	}
	if event.Status != delivery.StatusPending {
		t.Errorf("Status = %q, want %q", event.Status, delivery.StatusPending)
	}
}
