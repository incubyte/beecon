// purge_terminal_older_than_test.go proves PurgeTerminalOlderThan's own
// critical guarantee (Slice 7, PD44) directly against a real SQLite
// database, reusing repository_test.go's own helpers (same package
// bun_test): a PENDING or otherwise non-terminal outbox event must never be
// removed, at any age, because the WHERE clause's status predicate is driven
// by delivery.TerminalStatuses (StatusPending is never a member of it) —
// this is the single highest-value test in the purge worker's surface,
// since a bug here would silently delete in-flight webhook deliveries.
package bun_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/delivery"
)

// TestPurgeTerminalOlderThan_APendingEventSurvivesRegardlessOfAge is the
// critical guarantee test named by the architecture doc (§3.5/§7): a
// PENDING event created far in the past (well past any plausible retention
// cutoff) must never be deleted by a purge run, because delivery.
// TerminalStatuses deliberately excludes StatusPending — the purge worker's
// WHERE clause can never match it, at any age.
func TestPurgeTerminalOlderThan_APendingEventSurvivesRegardlessOfAge(t *testing.T) {
	repo := newTestRepository(t)
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -1) // a one-day retention window: longAgo is years past it

	pending := testEvent("evt_pending_ancient", "org_1", longAgo, longAgo)
	pending.Status = delivery.StatusPending
	mustSave(t, repo, pending)

	purged, err := repo.PurgeTerminalOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeTerminalOlderThan: %v", err)
	}
	if purged != 0 {
		t.Fatalf("purged = %d, want 0 — a PENDING event must never be purged regardless of age", purged)
	}
	found, err := repo.FindEvent(context.Background(), "org_1", "evt_pending_ancient")
	if err != nil {
		t.Fatalf("FindEvent: %v", err)
	}
	if found == nil {
		t.Fatal("the ancient PENDING event was deleted — it must survive purge at any age")
	}
}

// TestPurgeTerminalOlderThan_DeletesEachTerminalStatusOlderThanCutoff pins
// the purge worker's positive case for every member of
// delivery.TerminalStatuses (DELIVERED/FAILED/NO_ENDPOINT): each, once past
// cutoff, is eligible for hard-deletion.
func TestPurgeTerminalOlderThan_DeletesEachTerminalStatusOlderThanCutoff(t *testing.T) {
	repo := newTestRepository(t)
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)

	for _, status := range delivery.TerminalStatuses {
		event := testEvent(delivery.EventID("evt_terminal_"+string(status)), "org_1", longAgo, longAgo)
		event.Status = status
		mustSave(t, repo, event)
	}

	purged, err := repo.PurgeTerminalOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeTerminalOlderThan: %v", err)
	}
	if purged != len(delivery.TerminalStatuses) {
		t.Fatalf("purged = %d, want %d (one per terminal status)", purged, len(delivery.TerminalStatuses))
	}
	for _, status := range delivery.TerminalStatuses {
		found, err := repo.FindEvent(context.Background(), "org_1", delivery.EventID("evt_terminal_"+string(status)))
		if err != nil {
			t.Fatalf("FindEvent(%s): %v", status, err)
		}
		if found != nil {
			t.Errorf("terminal event with status %s survived purge, want it deleted", status)
		}
	}
}

// TestPurgeTerminalOlderThan_KeepsATerminalEventNewerThanCutoff pins the
// purge predicate's other half: age matters, not just status — a terminal
// event created after cutoff must survive.
func TestPurgeTerminalOlderThan_KeepsATerminalEventNewerThanCutoff(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	recent := now.AddDate(0, 0, -1) // 1 day old, well inside a 30-day window

	event := testEvent("evt_recent_delivered", "org_1", recent, recent)
	event.Status = delivery.StatusDelivered
	mustSave(t, repo, event)

	purged, err := repo.PurgeTerminalOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeTerminalOlderThan: %v", err)
	}
	if purged != 0 {
		t.Fatalf("purged = %d, want 0 — a terminal event newer than cutoff must survive", purged)
	}
	found, err := repo.FindEvent(context.Background(), "org_1", "evt_recent_delivered")
	if err != nil {
		t.Fatalf("FindEvent: %v", err)
	}
	if found == nil {
		t.Fatal("the recent DELIVERED event was deleted, want it kept")
	}
}

// TestPurgeTerminalOlderThan_OnlyPurgesTheGivenOrganization pins org
// isolation: purging org_1 must never touch org_2's own terminal events,
// even ones equally eligible by age and status.
func TestPurgeTerminalOlderThan_OnlyPurgesTheGivenOrganization(t *testing.T) {
	repo := newTestRepository(t)
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)

	orgOneEvent := testEvent("evt_org_1", "org_1", longAgo, longAgo)
	orgOneEvent.Status = delivery.StatusDelivered
	mustSave(t, repo, orgOneEvent)

	orgTwoEvent := testEvent("evt_org_2", "org_2", longAgo, longAgo)
	orgTwoEvent.Status = delivery.StatusDelivered
	mustSave(t, repo, orgTwoEvent)

	purged, err := repo.PurgeTerminalOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeTerminalOlderThan: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want exactly 1 (org_1's own event only)", purged)
	}
	orgTwoFound, err := repo.FindEvent(context.Background(), "org_2", "evt_org_2")
	if err != nil {
		t.Fatalf("FindEvent(org_2): %v", err)
	}
	if orgTwoFound == nil {
		t.Fatal("org_2's terminal event was deleted by a purge scoped to org_1 — org isolation violated")
	}
}

// TestPurgeTerminalOlderThan_ASecondRunAtTheSameCutoffDeletesNothingFurther
// is the idempotency guarantee the architecture doc names as the reason
// purge needs no lease column of its own (§3.5/§7): once a row is gone, a
// second identical call (simulating a concurrent second binary instance, or
// the next scheduled tick before anything new has aged past cutoff) finds
// nothing left to remove — the DELETE itself is safely idempotent.
func TestPurgeTerminalOlderThan_ASecondRunAtTheSameCutoffDeletesNothingFurther(t *testing.T) {
	repo := newTestRepository(t)
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)

	event := testEvent("evt_1", "org_1", longAgo, longAgo)
	event.Status = delivery.StatusFailed
	mustSave(t, repo, event)

	first, err := repo.PurgeTerminalOlderThan(context.Background(), "org_1", cutoff)
	if err != nil {
		t.Fatalf("first PurgeTerminalOlderThan: %v", err)
	}
	if first != 1 {
		t.Fatalf("first run purged = %d, want 1", first)
	}

	second, err := repo.PurgeTerminalOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("second PurgeTerminalOlderThan: %v", err)
	}
	if second != 0 {
		t.Fatalf("second run purged = %d, want 0 — two runs against the same state must be idempotent (no lease needed)", second)
	}
}
