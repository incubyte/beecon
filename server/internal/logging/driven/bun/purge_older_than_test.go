// purge_older_than_test.go proves PurgeOlderThan (Slice 7, PD44) directly
// against a real SQLite database: unconditional-by-age hard deletion of
// event_logs rows, org-scoped, idempotent on a second run — mirrors
// delivery/driven/bun/purge_terminal_older_than_test.go's own reasoning for
// "only a real dialect can prove this," one module over. Unlike delivery's
// own purge, a log entry carries no in-flight state to protect, so there is
// no "survives regardless of age" case here — every entry past cutoff is
// eligible regardless of Kind.
package bun_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/db"
	loggingbun "beecon/internal/logging/driven/bun"

	"beecon/internal/logging"
	"beecon/internal/organizations"
)

var purgeTestDSNCounter int64

func newPurgeTestRepository(t *testing.T) *loggingbun.Repository {
	t.Helper()
	n := atomic.AddInt64(&purgeTestDSNCounter, 1)
	dsn := fmt.Sprintf("file:event_logs_purge_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return loggingbun.NewRepository(database)
}

func purgeTestEntry(id logging.LogID, org organizations.OrgID, createdAt time.Time) logging.EventLog {
	return logging.EventLog{
		ID:           id,
		OrgID:        org,
		UserID:       "user_1",
		ConnectionID: "conn_1",
		ToolSlug:     "outlook-list-messages",
		Kind:         logging.KindToolExecution,
		Status:       200,
		DurationMs:   10,
		RequestBody:  `{}`,
		ResponseBody: `{}`,
		CreatedAt:    createdAt,
	}
}

func mustSaveEntry(t *testing.T, repo *loggingbun.Repository, entry logging.EventLog) {
	t.Helper()
	if err := repo.Save(context.Background(), entry); err != nil {
		t.Fatalf("Save(%s): %v", entry.ID, err)
	}
}

// TestPurgeOlderThan_DeletesEntriesOlderThanCutoff is the purge worker's
// positive case for logging: an entry created before cutoff is hard-deleted.
func TestPurgeOlderThan_DeletesEntriesOlderThanCutoff(t *testing.T) {
	repo := newPurgeTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	old := now.AddDate(0, 0, -31)

	mustSaveEntry(t, repo, purgeTestEntry("log_old", "org_1", old))

	purged, err := repo.PurgeOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}
	remaining, err := repo.Query(context.Background(), "org_1", logging.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining entries = %d, want 0 — the old entry must be gone", len(remaining))
	}
}

// TestPurgeOlderThan_KeepsEntriesNewerThanCutoff pins the age boundary: an
// entry created after cutoff must survive.
func TestPurgeOlderThan_KeepsEntriesNewerThanCutoff(t *testing.T) {
	repo := newPurgeTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	recent := now.AddDate(0, 0, -1)

	mustSaveEntry(t, repo, purgeTestEntry("log_recent", "org_1", recent))

	purged, err := repo.PurgeOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if purged != 0 {
		t.Fatalf("purged = %d, want 0 — an entry newer than cutoff must survive", purged)
	}
	remaining, err := repo.Query(context.Background(), "org_1", logging.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining entries = %d, want 1 (the recent entry kept)", len(remaining))
	}
}

// TestPurgeOlderThan_OnlyPurgesTheGivenOrganization pins org isolation:
// purging org_1 must never remove org_2's equally-old entries.
func TestPurgeOlderThan_OnlyPurgesTheGivenOrganization(t *testing.T) {
	repo := newPurgeTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	old := now.AddDate(0, 0, -31)

	mustSaveEntry(t, repo, purgeTestEntry("log_org_1", "org_1", old))
	mustSaveEntry(t, repo, purgeTestEntry("log_org_2", "org_2", old))

	purged, err := repo.PurgeOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want exactly 1 (org_1's own entry only)", purged)
	}
	remainingOrgTwo, err := repo.Query(context.Background(), "org_2", logging.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Query org_2: %v", err)
	}
	if len(remainingOrgTwo) != 1 {
		t.Fatalf("org_2 remaining entries = %d, want 1 — org_2's entry must survive a purge scoped to org_1", len(remainingOrgTwo))
	}
}

// TestPurgeOlderThan_ASecondRunAtTheSameCutoffDeletesNothingFurther is the
// same idempotency guarantee delivery's own purge relies on (no lease
// column needed): once a row is gone, a repeat call finds nothing left to
// remove.
func TestPurgeOlderThan_ASecondRunAtTheSameCutoffDeletesNothingFurther(t *testing.T) {
	repo := newPurgeTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	old := now.AddDate(0, 0, -31)

	mustSaveEntry(t, repo, purgeTestEntry("log_1", "org_1", old))

	first, err := repo.PurgeOlderThan(context.Background(), "org_1", cutoff)
	if err != nil {
		t.Fatalf("first PurgeOlderThan: %v", err)
	}
	if first != 1 {
		t.Fatalf("first run purged = %d, want 1", first)
	}

	second, err := repo.PurgeOlderThan(context.Background(), "org_1", cutoff)

	if err != nil {
		t.Fatalf("second PurgeOlderThan: %v", err)
	}
	if second != 0 {
		t.Fatalf("second run purged = %d, want 0 — repeated runs must be idempotent", second)
	}
}
