// facade_purge_test.go exercises logging.Facade.PurgeOnce (Slice 7, PD44):
// per-org effective-window resolution through RetentionReader, the
// 0/unlimited skip rule, and cutoff freshness across two runs — the
// orchestration half; PurgeOlderThan's own real-SQLite age predicate is
// proven directly in driven/bun/purge_older_than_test.go.
package logging_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/logging"
	memory "beecon/internal/logging/driven/memory"
	"beecon/internal/organizations"
)

// fakeRetentionReader is a minimal, scriptable logging.RetentionReader,
// mirroring delivery's own facade_purge_test.go fake: Days is a plain map
// read fresh on every call, so a test can mutate it between two PurgeOnce
// runs.
type fakeRetentionReader struct {
	orgs []organizations.OrgID
	days map[organizations.OrgID]int
}

func (f *fakeRetentionReader) ListOrgIDs(context.Context) ([]organizations.OrgID, error) {
	return f.orgs, nil
}

func (f *fakeRetentionReader) EffectiveLogRetentionDays(_ context.Context, org organizations.OrgID) (int, error) {
	return f.days[org], nil
}

func purgeTestFacade(now *time.Time, retention logging.RetentionReader) *logging.Facade {
	f := memory.NewFacadeWithOverrides(memory.Overrides{Now: func() time.Time { return *now }})
	return f.WithRetention(retention)
}

func recordEntryAt(t *testing.T, f *logging.Facade, org organizations.OrgID) {
	t.Helper()
	if err := f.Record(context.Background(), recordInput(org, nil)); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func survivingCount(t *testing.T, f *logging.Facade, org organizations.OrgID) int {
	t.Helper()
	result, err := f.Query(context.Background(), org, logging.QueryParams{Limit: 200})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return len(result.Entries)
}

// TestPurgeOnce_SkipsAnOrgWhoseEffectiveWindowIsZeroUnlimited is AC4 at
// logging's PurgeOnce orchestration level: an org whose effective
// log-retention window resolves to 0 is skipped entirely, regardless of how
// old its entries are.
func TestPurgeOnce_SkipsAnOrgWhoseEffectiveWindowIsZeroUnlimited(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	f := purgeTestFacade(&now, &fakeRetentionReader{
		orgs: []organizations.OrgID{"org_unlimited"},
		days: map[organizations.OrgID]int{"org_unlimited": 0},
	})
	recordEntryAt(t, f, "org_unlimited")

	now = now.AddDate(50, 0, 0)

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if got := survivingCount(t, f, "org_unlimited"); got != 1 {
		t.Fatalf("surviving entries = %d, want 1 — a 0/unlimited window must disable purging for this org", got)
	}
}

// TestPurgeOnce_PurgesAnEntryPastTheEffectiveWindowForANonZeroOrg is
// PurgeOnce's positive case: an org with a finite window loses its entries
// once they age past it.
func TestPurgeOnce_PurgesAnEntryPastTheEffectiveWindowForANonZeroOrg(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	f := purgeTestFacade(&now, &fakeRetentionReader{
		orgs: []organizations.OrgID{"org_30"},
		days: map[organizations.OrgID]int{"org_30": 30},
	})
	recordEntryAt(t, f, "org_30")

	now = now.AddDate(0, 0, 31)

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if got := survivingCount(t, f, "org_30"); got != 0 {
		t.Fatalf("surviving entries = %d, want 0 — the entry is 31 days old against a 30-day window", got)
	}
}

// TestPurgeOnce_CutoffIsResolvedFreshOnEveryRunNotCachedFromAnEarlierOne
// mirrors delivery's own equivalent test: the same entry, at the same age,
// survives a first run under a wide window and is purged by a second run
// once the org's window has since narrowed.
func TestPurgeOnce_CutoffIsResolvedFreshOnEveryRunNotCachedFromAnEarlierOne(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	retention := &fakeRetentionReader{
		orgs: []organizations.OrgID{"org_1"},
		days: map[organizations.OrgID]int{"org_1": 30},
	}
	f := purgeTestFacade(&now, retention)
	recordEntryAt(t, f, "org_1")

	now = now.AddDate(0, 0, 10)
	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("first PurgeOnce: %v", err)
	}
	if got := survivingCount(t, f, "org_1"); got != 1 {
		t.Fatalf("after first run, surviving entries = %d, want 1 (10 days old, inside the 30-day window)", got)
	}

	retention.days["org_1"] = 5

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("second PurgeOnce: %v", err)
	}
	if got := survivingCount(t, f, "org_1"); got != 0 {
		t.Fatalf("after second run, surviving entries = %d, want 0 — the same 10-day-old entry must now be past the narrowed 5-day window", got)
	}
}

// TestPurgeOnce_IsANoOpWhenNoRetentionReaderIsWired pins WithRetention's
// nil-safe convention for logging's own PurgeOnce.
func TestPurgeOnce_IsANoOpWhenNoRetentionReaderIsWired(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})

	if err := f.PurgeOnce(context.Background()); err != nil {
		t.Fatalf("PurgeOnce with no RetentionReader wired: %v, want nil (silent no-op)", err)
	}
}
