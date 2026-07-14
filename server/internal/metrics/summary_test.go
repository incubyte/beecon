package metrics

import (
	"context"
	"testing"
	"time"
)

// TestSummary_MapsRegisteredGaugesAndCountersIntoTheTypedShape pins
// Summary()'s Gather -> JSON mapping (Slice 3): once the
// connections-by-status gauge, the outbox gauges, and the delivery-attempts
// counter carry known values, Summary() must reflect those exact numbers —
// the same registry /metrics scrapes in text format, read here as a typed
// Go struct.
func TestSummary_MapsRegisteredGaugesAndCountersIntoTheTypedShape(t *testing.T) {
	registry := New()
	registry.RegisterConnectionsByStatusGauge(func(ctx context.Context) (map[string]int, error) {
		return map[string]int{"ACTIVE": 3, "INITIATED": 1, "EXPIRED": 0, "DISCONNECTED": 2}, nil
	})
	registry.RegisterOutboxGauges(
		func(ctx context.Context) (int, error) { return 5, nil },
		func(ctx context.Context) (time.Duration, error) { return 42 * time.Second, nil },
	)
	registry.RecordDeliveryAttempt("trigger.event", true)
	registry.RecordDeliveryAttempt("trigger.event", true)
	registry.RecordDeliveryAttempt("trigger.event", false)
	registry.RecordDeliveryAttempt("webhook.test", true)

	summary, err := registry.Summary()
	if err != nil {
		t.Fatalf("Summary(): unexpected error: %v", err)
	}

	wantByStatus := map[string]int{"ACTIVE": 3, "INITIATED": 1, "EXPIRED": 0, "DISCONNECTED": 2}
	for status, want := range wantByStatus {
		if got := summary.ConnectionsByStatus[status]; got != want {
			t.Errorf("ConnectionsByStatus[%q] = %d, want %d", status, got, want)
		}
	}

	if summary.Outbox.PendingDepth != 5 {
		t.Errorf("Outbox.PendingDepth = %d, want 5", summary.Outbox.PendingDepth)
	}
	if summary.Outbox.OldestPendingAgeSeconds != 42 {
		t.Errorf("Outbox.OldestPendingAgeSeconds = %v, want 42", summary.Outbox.OldestPendingAgeSeconds)
	}

	outcomeCount := func(outcomeType, result string) (int, bool) {
		for _, outcome := range summary.DeliveryOutcomes {
			if outcome.Type == outcomeType && outcome.Result == result {
				return outcome.Count, true
			}
		}
		return 0, false
	}
	if count, ok := outcomeCount("trigger.event", outcomeSuccess); !ok || count != 2 {
		t.Errorf("trigger.event/success count = %d (found=%v), want 2", count, ok)
	}
	if count, ok := outcomeCount("trigger.event", outcomeFailure); !ok || count != 1 {
		t.Errorf("trigger.event/failure count = %d (found=%v), want 1", count, ok)
	}
	if count, ok := outcomeCount("webhook.test", outcomeSuccess); !ok || count != 1 {
		t.Errorf("webhook.test/success count = %d (found=%v), want 1", count, ok)
	}
}

// TestSummary_ReturnsEmptyDeliveryOutcomesAndZeroedFieldsWhenNothingWasEverRecordedOrRegistered
// is the division-by-zero edge case: a fresh Registry whose
// connections-by-status/outbox gauges were never registered and which never
// recorded a single delivery attempt must still Summary() cleanly — an
// empty map, zeroed outbox figures, and an empty (never nil-panicking)
// DeliveryOutcomes slice — so a downstream success-rate calculation (the
// Admin UI's DashboardPage.formatSuccessRate, which divides by
// success+failure) receives "nothing recorded" rather than data it could
// divide by zero on.
func TestSummary_ReturnsEmptyDeliveryOutcomesAndZeroedFieldsWhenNothingWasEverRecordedOrRegistered(t *testing.T) {
	registry := New()

	summary, err := registry.Summary()
	if err != nil {
		t.Fatalf("Summary(): unexpected error: %v", err)
	}

	if len(summary.ConnectionsByStatus) != 0 {
		t.Errorf("ConnectionsByStatus = %v, want empty (no gauge was ever registered)", summary.ConnectionsByStatus)
	}
	if summary.Outbox.PendingDepth != 0 {
		t.Errorf("Outbox.PendingDepth = %d, want 0", summary.Outbox.PendingDepth)
	}
	if summary.Outbox.OldestPendingAgeSeconds != 0 {
		t.Errorf("Outbox.OldestPendingAgeSeconds = %v, want 0", summary.Outbox.OldestPendingAgeSeconds)
	}
	if len(summary.DeliveryOutcomes) != 0 {
		t.Errorf("DeliveryOutcomes = %v, want empty", summary.DeliveryOutcomes)
	}
}
