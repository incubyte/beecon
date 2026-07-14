// Package bun_test exercises the bun-backed Repository/WorkQueue directly
// against a real SQLite database: ClaimDue's lease semantics (a claimed row
// must not be re-claimable until its lease expires, an expired lease must be
// re-claimable, due/status/ordering/limit must all be honored by the real
// SQL, not just the in-memory fake's map filtering) mirror
// triggers/driven/bun/repository_test.go's own reasoning for the same class
// of "only a real dialect can prove this" risk.
package bun_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/db"
	"beecon/internal/delivery"
	deliverybun "beecon/internal/delivery/driven/bun"
	"beecon/internal/organizations"
)

var testDSNCounter int64

func newTestRepository(t *testing.T) *deliverybun.Repository {
	t.Helper()
	n := atomic.AddInt64(&testDSNCounter, 1)
	dsn := fmt.Sprintf("file:outbox_events_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return deliverybun.NewRepository(database)
}

func testEvent(id delivery.EventID, org organizations.OrgID, createdAt time.Time, nextAttemptAt time.Time) delivery.Event {
	return delivery.Event{
		ID:            id,
		OrgID:         org,
		Type:          "webhook.test",
		Body:          []byte(`{"id":"` + string(id) + `"}`),
		Status:        delivery.StatusPending,
		Attempts:      0,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
	}
}

func mustSave(t *testing.T, repo *deliverybun.Repository, event delivery.Event) {
	t.Helper()
	if err := repo.SaveEvent(context.Background(), event); err != nil {
		t.Fatalf("SaveEvent(%s): %v", event.ID, err)
	}
}

func TestClaimDue_ClaimsAPendingDueEvent(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_1", "org_1", now, now))

	claimed, err := repo.ClaimDue(context.Background(), now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "evt_1" {
		t.Fatalf("claimed = %+v, want exactly evt_1", claimed)
	}
}

func TestClaimDue_DoesNotClaimAnEventNotYetDue(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_future", "org_1", now, now.Add(time.Hour)))

	claimed, err := repo.ClaimDue(context.Background(), now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 0 {
		t.Errorf("claimed = %+v, want none — the event's NextAttemptAt is an hour in the future", claimed)
	}
}

func TestClaimDue_DoesNotClaimANonPendingEvent(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []delivery.Status{delivery.StatusDelivered, delivery.StatusFailed, delivery.StatusNoEndpoint} {
		event := testEvent(delivery.EventID("evt_"+string(status)), "org_1", now, now)
		event.Status = status
		mustSave(t, repo, event)
	}

	claimed, err := repo.ClaimDue(context.Background(), now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 0 {
		t.Errorf("claimed = %+v, want none — only PENDING events are claimable", claimed)
	}
}

// TestClaimDue_DoesNotReClaimARowWhoseLeaseHasNotExpired is the core lease
// semantics test (section 3 of the architecture doc): the first ClaimDue
// call leases the row past now; a second call at the same instant must not
// see it again — otherwise two dispatcher ticks (or two binary instances)
// could both attempt the same delivery concurrently.
func TestClaimDue_DoesNotReClaimARowWhoseLeaseHasNotExpired(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_1", "org_1", now, now))

	first, err := repo.ClaimDue(context.Background(), now, 5*time.Minute, 10)
	if err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %+v, want exactly 1 row", first)
	}

	second, err := repo.ClaimDue(context.Background(), now, 5*time.Minute, 10)
	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second claim = %+v, want none — the row's lease has not expired", second)
	}
}

// TestClaimDue_ReClaimsARowOnceItsLeaseHasExpired is the crash-safety half:
// once now has moved past the first lease's expiry, the row must become
// claimable again (PD29: "graceful shutdown finishes or releases claimed
// work" — a crash mid-delivery relies on exactly this).
func TestClaimDue_ReClaimsARowOnceItsLeaseHasExpired(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_1", "org_1", now, now))
	leaseTTL := time.Minute

	if _, err := repo.ClaimDue(context.Background(), now, leaseTTL, 10); err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}

	afterLeaseExpiry := now.Add(leaseTTL).Add(time.Second)
	reClaimed, err := repo.ClaimDue(context.Background(), afterLeaseExpiry, leaseTTL, 10)

	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	if len(reClaimed) != 1 || reClaimed[0].ID != "evt_1" {
		t.Fatalf("reClaimed = %+v, want evt_1 to be claimable again once its lease expired", reClaimed)
	}
}

// TestClaimDue_SaveEventClearsTheLeaseSoAnEarlierRescheduledRetryIsImmediatelyReclaimable
// pins EventRow.LeaseUntil's own documented convention (repository.go):
// SaveEvent always means "this attempt is over, whatever the outcome," so it
// persists LeaseUntil nil unconditionally — completion clears the lease
// immediately rather than leaving it to expire on its own. This is proven the
// hard way: claim with a long lease, reschedule to PENDING with a
// NextAttemptAt soon (simulating a failed attempt's retry, PD30), then
// re-claim once that retry is due but still well BEFORE the original lease
// would have expired. If SaveEvent had left the stale lease_until in place,
// this re-claim would be wrongly blocked by it — the only way it succeeds is
// if SaveEvent actually clears the lease on every save, not just a terminal
// one.
func TestClaimDue_SaveEventClearsTheLeaseSoAnEarlierRescheduledRetryIsImmediatelyReclaimable(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_1", "org_1", now, now))
	originalLeaseTTL := 10 * time.Minute

	claimed, err := repo.ClaimDue(context.Background(), now, originalLeaseTTL, 10)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %+v, want exactly 1 row", claimed)
	}

	rescheduled := claimed[0]
	rescheduled.Status = delivery.StatusPending
	rescheduled.Attempts = 1
	rescheduled.NextAttemptAt = now.Add(2 * time.Minute)
	if err := repo.SaveEvent(context.Background(), rescheduled); err != nil {
		t.Fatalf("SaveEvent (reschedule): %v", err)
	}

	// retryDue is past the rescheduled NextAttemptAt but still well before
	// now+originalLeaseTTL — the window where a still-set stale lease would
	// wrongly block the claim if SaveEvent hadn't cleared it.
	retryDue := now.Add(3 * time.Minute)
	reClaimed, err := repo.ClaimDue(context.Background(), retryDue, originalLeaseTTL, 10)

	if err != nil {
		t.Fatalf("ClaimDue at the rescheduled retry's due time: %v", err)
	}
	if len(reClaimed) != 1 || reClaimed[0].ID != "evt_1" {
		t.Fatalf("reClaimed = %+v, want evt_1 immediately claimable — SaveEvent must clear the lease on every save, not just a terminal one", reClaimed)
	}
}

// TestClaimDue_OrdersOldestCreatedFirstAndRespectsLimit pins PD30's "no
// head-of-line blocking" ordering rule (oldest created_at first) and the
// caller-supplied limit, against the real dialect's ORDER BY/LIMIT.
func TestClaimDue_OrdersOldestCreatedFirstAndRespectsLimit(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_newest", "org_1", now.Add(2*time.Second), now))
	mustSave(t, repo, testEvent("evt_oldest", "org_1", now, now))
	mustSave(t, repo, testEvent("evt_middle", "org_1", now.Add(time.Second), now))

	claimed, err := repo.ClaimDue(context.Background(), now, time.Minute, 2)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d rows, want exactly 2 (the limit)", len(claimed))
	}
	if claimed[0].ID != "evt_oldest" || claimed[1].ID != "evt_middle" {
		t.Errorf("claim order = [%s, %s], want [evt_oldest, evt_middle] (oldest created_at first)", claimed[0].ID, claimed[1].ID)
	}
}

// TestClaimDue_ScansAcrossOrganizationsByDesign pins WorkQueue's own
// documented installation-level scope (port.go): the dispatcher is one
// shared background loop, not a per-org one, so a single ClaimDue call must
// be able to claim due events belonging to different organizations at once
// — while every claimed row still carries its own OrgID.
func TestClaimDue_ScansAcrossOrganizationsByDesign(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustSave(t, repo, testEvent("evt_org_a", "org_a", now, now))
	mustSave(t, repo, testEvent("evt_org_b", "org_b", now.Add(time.Second), now))

	claimed, err := repo.ClaimDue(context.Background(), now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d rows, want 2 (both organizations)", len(claimed))
	}
	byOrg := map[organizations.OrgID]delivery.EventID{}
	for _, event := range claimed {
		byOrg[event.OrgID] = event.ID
	}
	if byOrg["org_a"] != "evt_org_a" || byOrg["org_b"] != "evt_org_b" {
		t.Errorf("claimed events = %+v, want each org's own event with its OrgID intact", claimed)
	}
}
