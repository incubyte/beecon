// claim_test.go is internal/db.ClaimDue's own focused unit test (FD7,
// architecture doc §7): the generic lease-claim primitive delivery.
// Repository.ClaimDue, triggers.Repository.ClaimDuePolls, and connections.
// Repository.ClaimDueRefresh/ClaimDueReconcile all now share is exercised
// here directly, against an ad-hoc table of its own — not one of those four
// callers' real schemas — to prove the helper is genuinely generic (any
// table, any lease column, any caller-supplied predicate), against a real
// SQLite database rather than a mock. The four callers' own existing lease
// tests (TestClaimDue_*, TestClaimDuePolls_*, TestClaimDueRefresh_*,
// TestClaimDueReconcile_*) remain the behavior-preserving regression pins
// for the refactor itself; this file covers the primitive's own contract in
// isolation: the caller-supplied WHERE predicate, the lease-null-or-expired
// check, ORDER BY created_at, LIMIT, and RETURNING scanning into the
// caller's own row type.
package db_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/db"
)

var claimTestDSNCounter int64

// claimDueTestRow is a table shape unrelated to any real Beecon module's
// schema — proving ClaimDue's own genericity rather than re-testing one of
// the four production callers' specific columns.
type claimDueTestRow struct {
	upstreambun.BaseModel `bun:"table:claim_due_test_rows,alias:cdt"`

	ID         string     `bun:"id,pk"`
	Status     string     `bun:"status,notnull"`
	DueAt      time.Time  `bun:"due_at,notnull"`
	LeaseUntil *time.Time `bun:"lease_until"`
	CreatedAt  time.Time  `bun:"created_at,notnull"`
}

func newClaimDueTestDB(t *testing.T) *upstreambun.DB {
	t.Helper()
	n := atomic.AddInt64(&claimTestDSNCounter, 1)
	dsn := fmt.Sprintf("file:claim_due_helper_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.NewCreateTable().Model((*claimDueTestRow)(nil)).Exec(context.Background()); err != nil {
		t.Fatalf("create ad-hoc claim_due_test_rows table: %v", err)
	}
	return database
}

func mustInsertClaimDueTestRow(t *testing.T, database *upstreambun.DB, row claimDueTestRow) {
	t.Helper()
	if _, err := database.NewInsert().Model(&row).Exec(context.Background()); err != nil {
		t.Fatalf("insert %s: %v", row.ID, err)
	}
}

// TestClaimDue_ClaimsARowMatchingTheCallerSuppliedPredicateAndDueByLease
// pins the two halves of eligibility ClaimDue combines: the caller's own
// wherePredicate/whereArgs (here, status = ? AND due_at <= ?) AND the
// lease-null-or-expired check it always adds on top.
func TestClaimDue_ClaimsARowMatchingTheCallerSuppliedPredicateAndDueByLease(t *testing.T) {
	database := newClaimDueTestDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_1", Status: "DUE", DueAt: now, CreatedAt: now})

	var claimed []claimDueTestRow
	err := db.ClaimDue(context.Background(), database, &claimed, "claim_due_test_rows", "lease_until",
		"status = ? AND due_at <= ?", []any{"DUE", now}, now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "row_1" {
		t.Fatalf("claimed = %+v, want exactly row_1", claimed)
	}
	if claimed[0].LeaseUntil == nil || !claimed[0].LeaseUntil.Equal(now.Add(time.Minute)) {
		t.Errorf("LeaseUntil = %v, want now+leaseTTL (%v)", claimed[0].LeaseUntil, now.Add(time.Minute))
	}
}

// TestClaimDue_DoesNotClaimARowFailingTheCallerSuppliedPredicate proves the
// wherePredicate is actually applied, not just the lease check: a row with a
// non-matching status is never claimed, however due it is.
func TestClaimDue_DoesNotClaimARowFailingTheCallerSuppliedPredicate(t *testing.T) {
	database := newClaimDueTestDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_done", Status: "DONE", DueAt: now, CreatedAt: now})

	var claimed []claimDueTestRow
	err := db.ClaimDue(context.Background(), database, &claimed, "claim_due_test_rows", "lease_until",
		"status = ? AND due_at <= ?", []any{"DUE", now}, now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %+v, want none — the row's status does not match the caller's own predicate", claimed)
	}
}

// TestClaimDue_DoesNotReClaimARowWhoseLeaseHasNotExpired is the core lease
// semantics pinned against the generic helper directly.
func TestClaimDue_DoesNotReClaimARowWhoseLeaseHasNotExpired(t *testing.T) {
	database := newClaimDueTestDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_1", Status: "DUE", DueAt: now, CreatedAt: now})
	predicate := "status = ? AND due_at <= ?"
	args := []any{"DUE", now}

	var first []claimDueTestRow
	if err := db.ClaimDue(context.Background(), database, &first, "claim_due_test_rows", "lease_until", predicate, args, now, 5*time.Minute, 10); err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %+v, want exactly 1 row", first)
	}

	var second []claimDueTestRow
	err := db.ClaimDue(context.Background(), database, &second, "claim_due_test_rows", "lease_until", predicate, args, now, 5*time.Minute, 10)

	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second claim = %+v, want none — the row's lease has not expired", second)
	}
}

// TestClaimDue_ReClaimsARowOnceItsLeaseHasExpired is the crash-safety half.
func TestClaimDue_ReClaimsARowOnceItsLeaseHasExpired(t *testing.T) {
	database := newClaimDueTestDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_1", Status: "DUE", DueAt: now, CreatedAt: now})
	predicate := "status = ? AND due_at <= ?"
	args := []any{"DUE", now}
	leaseTTL := time.Minute

	var first []claimDueTestRow
	if err := db.ClaimDue(context.Background(), database, &first, "claim_due_test_rows", "lease_until", predicate, args, now, leaseTTL, 10); err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}

	afterExpiry := now.Add(leaseTTL).Add(time.Second)
	var reClaimed []claimDueTestRow
	err := db.ClaimDue(context.Background(), database, &reClaimed, "claim_due_test_rows", "lease_until", predicate, args, afterExpiry, leaseTTL, 10)

	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	if len(reClaimed) != 1 || reClaimed[0].ID != "row_1" {
		t.Fatalf("reClaimed = %+v, want row_1 claimable again once its lease expired", reClaimed)
	}
}

// TestClaimDue_OrdersOldestCreatedFirstAndRespectsLimit pins the fixed
// ORDER BY created_at + caller-supplied LIMIT against the real dialect.
func TestClaimDue_OrdersOldestCreatedFirstAndRespectsLimit(t *testing.T) {
	database := newClaimDueTestDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_newest", Status: "DUE", DueAt: now, CreatedAt: now.Add(2 * time.Second)})
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_oldest", Status: "DUE", DueAt: now, CreatedAt: now})
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_middle", Status: "DUE", DueAt: now, CreatedAt: now.Add(time.Second)})

	var claimed []claimDueTestRow
	err := db.ClaimDue(context.Background(), database, &claimed, "claim_due_test_rows", "lease_until",
		"status = ? AND due_at <= ?", []any{"DUE", now}, now, time.Minute, 2)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d rows, want exactly 2 (the limit)", len(claimed))
	}
	if claimed[0].ID != "row_oldest" || claimed[1].ID != "row_middle" {
		t.Errorf("claim order = [%s, %s], want [row_oldest, row_middle] (oldest created_at first)", claimed[0].ID, claimed[1].ID)
	}
}

// TestClaimDue_DoesNotClaimARowNotYetDuePerTheCallersOwnPredicate proves
// ClaimDue applies exactly the predicate the caller supplied — a row whose
// due_at is in the future (per this caller's own "due_at <= now" clause)
// is not claimed, without ClaimDue itself knowing anything about "due" as a
// concept.
func TestClaimDue_DoesNotClaimARowNotYetDuePerTheCallersOwnPredicate(t *testing.T) {
	database := newClaimDueTestDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustInsertClaimDueTestRow(t, database, claimDueTestRow{ID: "row_future", Status: "DUE", DueAt: now.Add(time.Hour), CreatedAt: now})

	var claimed []claimDueTestRow
	err := db.ClaimDue(context.Background(), database, &claimed, "claim_due_test_rows", "lease_until",
		"status = ? AND due_at <= ?", []any{"DUE", now}, now, time.Minute, 10)

	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 0 {
		t.Errorf("claimed = %+v, want none — due_at is an hour in the future", claimed)
	}
}
