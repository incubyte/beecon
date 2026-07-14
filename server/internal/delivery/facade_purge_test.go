// facade_purge_test.go exercises delivery.Facade.PurgeOnce (Slice 7, PD44):
// the per-org effective-window resolution through the RetentionReader port,
// the 0/unlimited "skip this org entirely" rule, and that the cutoff is
// resolved fresh on every call rather than cached from an earlier run —
// against the in-memory Repository (facade_test.go's own convention), since
// PurgeTerminalOlderThan's own real-SQLite predicate is already proven
// directly in driven/bun/purge_terminal_older_than_test.go. This file's
// job is PurgeOnce's own orchestration: which orgs it visits, which window
// it resolves for each, and when it skips one outright.
package delivery_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/delivery"
	deliverymemory "beecon/internal/delivery/driven/memory"
	"beecon/internal/organizations"
)

// fakeRetentionReader is a minimal, scriptable delivery.RetentionReader:
// Days is read fresh on every EffectiveEventRetentionDays call (a plain map
// lookup, not snapshotted), so a test can mutate it between two PurgeOnce
// calls to prove the cutoff is never cached.
type fakeRetentionReader struct {
	orgs []organizations.OrgID
	days map[organizations.OrgID]int
}

func (f *fakeRetentionReader) ListOrgIDs(context.Context) ([]organizations.OrgID, error) {
	return f.orgs, nil
}

func (f *fakeRetentionReader) EffectiveEventRetentionDays(_ context.Context, org organizations.OrgID) (int, error) {
	return f.days[org], nil
}

// purgeTestFacade builds a delivery.Facade over the in-memory Repository
// with a fixed, mutable clock (via a *time.Time the test can advance) and
// the given RetentionReader wired.
func purgeTestFacade(t *testing.T, now *time.Time, retention delivery.RetentionReader) *delivery.Facade {
	t.Helper()
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{
		Now: func() time.Time { return *now },
	})
	return f.WithRetention(retention)
}

func seedTerminalEvent(t *testing.T, f *delivery.Facade, org organizations.OrgID, createdAt time.Time, id delivery.EventID) {
	t.Helper()
	// Enqueue always sets NO_ENDPOINT for an org with no configured
	// endpoint (this facade never gets one) — NO_ENDPOINT is itself one of
	// delivery.TerminalStatuses, so seeding through the facade's own public
	// Enqueue reaches a real terminal, purge-eligible event without reaching
	// into the repository directly. CreatedAt is fixed by the facade's
	// clock at Enqueue time, so the caller controls it via the shared *now.
	_, err := f.Enqueue(context.Background(), org, delivery.EventTypeWebhookTest, map[string]any{"id": string(id)})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

func survivingEventCount(t *testing.T, f *delivery.Facade, org organizations.OrgID) int {
	t.Helper()
	result, err := f.ListEvents(context.Background(), org, delivery.ListEventsParams{Limit: 200})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return len(result.Items)
}

// TestPurgeOnce_SkipsAnOrgWhoseEffectiveWindowIsZeroUnlimited is AC4 at the
// PurgeOnce orchestration level: an org whose effective event-retention
// window resolves to 0 is skipped entirely — nothing is purged for it,
// regardless of how old its terminal events are.
func TestPurgeOnce_SkipsAnOrgWhoseEffectiveWindowIsZeroUnlimited(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	f := purgeTestFacade(t, &now, &fakeRetentionReader{
		orgs: []organizations.OrgID{"org_unlimited"},
		days: map[organizations.OrgID]int{"org_unlimited": 0},
	})
	seedTerminalEvent(t, f, "org_unlimited", now, "evt_1")

	farFuture := now.AddDate(50, 0, 0) // 50 years later — any finite window would purge this
	now = farFuture

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}

	if got := survivingEventCount(t, f, "org_unlimited"); got != 1 {
		t.Fatalf("surviving events = %d, want 1 — a 0/unlimited retention window must disable purging for this org entirely", got)
	}
}

// TestPurgeOnce_PurgesATerminalEventPastTheEffectiveWindowForANonZeroOrg is
// PurgeOnce's positive case: an org with a finite window loses its terminal
// events once they age past it.
func TestPurgeOnce_PurgesATerminalEventPastTheEffectiveWindowForANonZeroOrg(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	f := purgeTestFacade(t, &now, &fakeRetentionReader{
		orgs: []organizations.OrgID{"org_30"},
		days: map[organizations.OrgID]int{"org_30": 30},
	})
	seedTerminalEvent(t, f, "org_30", now, "evt_1")

	now = now.AddDate(0, 0, 31)

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}

	if got := survivingEventCount(t, f, "org_30"); got != 0 {
		t.Fatalf("surviving events = %d, want 0 — the event is 31 days old against a 30-day window", got)
	}
}

// TestPurgeOnce_APendingEventSurvivesEvenWithAFiniteRetentionWindow re-pins
// the critical guarantee at the PurgeOnce orchestration layer (the row-level
// proof lives in driven/bun/purge_terminal_older_than_test.go): a PENDING
// event is never purged, even for an org with a small, finite window and an
// event old enough to otherwise qualify.
func TestPurgeOnce_APendingEventSurvivesEvenWithAFiniteRetentionWindow(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	// SendTest requires a configured endpoint to reach StatusPending
	// (Enqueue only lands PENDING when an endpoint exists) — SetEndpoint
	// needs a SecretIssuer, so this test wires the real access facade the
	// same way facade_test.go's own SendTest/DispatchOnce tests do.
	accessFacade := newAccessFacade(func() time.Time { return now })
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{
		Secrets: accessFacade,
		Now:     func() time.Time { return now },
	}).WithRetention(&fakeRetentionReader{
		orgs: []organizations.OrgID{"org_with_endpoint"},
		days: map[organizations.OrgID]int{"org_with_endpoint": 1},
	})
	if _, err := f.SetEndpoint(context.Background(), "org_with_endpoint", "https://example.com/hook"); err != nil {
		t.Fatalf("SetEndpoint: %v", err)
	}
	if _, err := f.SendTest(context.Background(), "org_with_endpoint"); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	now = now.AddDate(1, 0, 0) // a full year past — nothing about status changed, it's still PENDING

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}

	if got := survivingEventCount(t, f, "org_with_endpoint"); got != 1 {
		t.Fatalf("surviving events = %d, want 1 — a PENDING event must survive purge regardless of the configured window", got)
	}
}

// TestPurgeOnce_CutoffIsResolvedFreshOnEveryRunNotCachedFromAnEarlierOne is
// the "shortening a window purges on the next run, not retroactively" AC:
// the same event, at the same age, survives a first PurgeOnce run under a
// wide window and is purged by a second run once the org's effective window
// has since been narrowed — proving PurgeOnce re-resolves the window (and
// the cutoff derived from it) on every call rather than memoizing either.
func TestPurgeOnce_CutoffIsResolvedFreshOnEveryRunNotCachedFromAnEarlierOne(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	retention := &fakeRetentionReader{
		orgs: []organizations.OrgID{"org_1"},
		days: map[organizations.OrgID]int{"org_1": 30},
	}
	f := purgeTestFacade(t, &now, retention)
	seedTerminalEvent(t, f, "org_1", now, "evt_1")

	now = now.AddDate(0, 0, 10) // 10 days old: inside the 30-day window
	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("first PurgeOnce: %v", err)
	}
	if got := survivingEventCount(t, f, "org_1"); got != 1 {
		t.Fatalf("after first run, surviving events = %d, want 1 (10 days old, inside the 30-day window)", got)
	}

	// The operator shortens the window (Facade.SetRetention in
	// organizations, exercised separately) — this test only needs
	// PurgeOnce's own next call to see the new, narrower value.
	retention.days["org_1"] = 5

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("second PurgeOnce: %v", err)
	}
	if got := survivingEventCount(t, f, "org_1"); got != 0 {
		t.Fatalf("after second run, surviving events = %d, want 0 — the same 10-day-old event must now be past the narrowed 5-day window", got)
	}
}

// TestPurgeOnce_IsANoOpWhenNoRetentionReaderIsWired pins WithRetention's own
// documented nil-safe convention: a Facade built without one never panics
// and never errors when PurgeOnce is called.
func TestPurgeOnce_IsANoOpWhenNoRetentionReaderIsWired(t *testing.T) {
	f := deliverymemory.NewFacadeWithOverrides(deliverymemory.Overrides{})

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce with no RetentionReader wired: %v, want nil (silent no-op)", err)
	}
}
