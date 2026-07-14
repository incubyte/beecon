package db

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// ClaimDue is the dual-dialect lease-claim primitive every background
// worker's "find due rows, lease them, return them" query shares (FD7,
// Phase 4 Slice 7). Phase 3's own evolution-triggers section named this
// exactly: "a fifth claiming worker -> extract a lease-query helper into
// internal/db" — the purge worker (Slice 7) is that fifth claiming query,
// arriving after delivery.Repository.ClaimDue, triggers.Repository.
// ClaimDuePolls, and connections.Repository.ClaimDueRefresh/ClaimDueReconcile,
// which this helper now backs (tidy-first, same slice).
//
// It builds and runs an atomic
//
//	UPDATE table SET leaseColumn = now+leaseTTL
//	WHERE id IN (
//	    SELECT id FROM table
//	    WHERE wherePredicate AND (leaseColumn IS NULL OR leaseColumn < now)
//	    ORDER BY created_at
//	    LIMIT limit
//	    [FOR UPDATE SKIP LOCKED]
//	)
//	RETURNING *
//
// — Postgres adds FOR UPDATE SKIP LOCKED so two running binary instances
// never claim the same row and never block each other; SQLite needs neither
// (its single-writer lock already makes the UPDATE-with-lease-predicate
// atomic) and doesn't support the clause. The claimed batch is scanned into
// dest (a pointer to a slice of the caller's own bun row struct — bun's own
// Scan does the reflection).
//
// wherePredicate is the caller's own due-row condition, already containing
// its own `?` placeholders (e.g. "status = ? AND next_attempt_at <= ?");
// whereArgs are wherePredicate's own placeholder values, in the same order
// they appear. now is used both inside wherePredicate's caller-supplied args
// (if the caller's own predicate needs "now") and, always, as the lease-
// expiry check's own comparison value and the leaseTTL's anchor.
func ClaimDue(ctx context.Context, database *bun.DB, dest any, table, leaseColumn, wherePredicate string, whereArgs []any, now time.Time, leaseTTL time.Duration, limit int) error {
	leaseUntil := now.Add(leaseTTL)

	forUpdateSkipLocked := ""
	if database.Dialect().Name() == dialect.PG {
		forUpdateSkipLocked = "\n\tFOR UPDATE SKIP LOCKED"
	}

	query := fmt.Sprintf(`
UPDATE %[1]s
SET %[2]s = ?
WHERE id IN (
	SELECT id FROM %[1]s
	WHERE %[3]s
		AND (%[2]s IS NULL OR %[2]s < ?)
	ORDER BY created_at
	LIMIT ?%[4]s
)
RETURNING *
`, table, leaseColumn, wherePredicate, forUpdateSkipLocked)

	args := make([]any, 0, len(whereArgs)+3)
	args = append(args, leaseUntil)
	args = append(args, whereArgs...)
	args = append(args, now, limit)

	return database.NewRaw(query, args...).Scan(ctx, dest)
}
