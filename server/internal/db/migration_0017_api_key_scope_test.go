// migration_0017_api_key_scope_test.go proves migration 0017's own DDL
// directly against a fully-migrated real SQLite database (every embedded
// migration through db.Migrate's normal boot, not a hand-seeded pre-migration
// fixture like migration_0012_backfill_test.go needs — 0017 only adds a
// column with a DEFAULT, there is no pre-existing data shape to reconstruct):
// a row inserted without ever mentioning the "scope" column must still read
// back as "read-write" (PD41, Slice 4) — the exact backward-compatibility
// guarantee this slice's AC promises every key issued before this phase.
package db_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"beecon/internal/config"
	"beecon/internal/db"
)

var migration0017TestDSNCounter int64

// newFullyMigratedSQLiteDB boots a fresh in-memory SQLite database and runs
// every real embedded migration (through 0017) against it, mirroring how the
// application itself boots — unlike migration_0012_backfill_test.go's
// freshSQLiteDB, which deliberately stops short so a pre-migration fixture
// can be hand-seeded; 0017 adds a column with a DEFAULT onto a table shape
// that already exists by then, so there's nothing to reconstruct by hand.
func newFullyMigratedSQLiteDB(t *testing.T) *bun.DB {
	t.Helper()
	n := atomic.AddInt64(&migration0017TestDSNCounter, 1)
	dsn := fmt.Sprintf("file:migration_0017_scope_%d?mode=memory&cache=shared", n)
	sqldb, err := db.New(config.DriverSQLite, dsn)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	if err := db.Migrate(context.Background(), sqldb); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return sqldb
}

func TestMigration0017_ARowInsertedWithoutMentioningScopeDefaultsToReadWrite(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO server_api_keys (id, org_id, created_at) VALUES (?, ?, ?)",
		"key_no_scope_mentioned", "org_x", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("insert row without a scope column: %v", err)
	}

	var scope string
	if err := sqldb.QueryRowContext(ctx, "SELECT scope FROM server_api_keys WHERE id = ?", "key_no_scope_mentioned").Scan(&scope); err != nil {
		t.Fatalf("query scope column: %v", err)
	}
	if scope != "read-write" {
		t.Errorf("scope = %q, want %q (migration 0017's column default)", scope, "read-write")
	}
}

// TestMigration0017_AnExplicitlyInsertedReadOnlyScopeIsPreservedUnchanged
// pins the column's other side: 0017 only supplies a default, it must never
// coerce or clamp an explicitly-provided value.
func TestMigration0017_AnExplicitlyInsertedReadOnlyScopeIsPreservedUnchanged(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO server_api_keys (id, org_id, created_at, scope) VALUES (?, ?, ?, ?)",
		"key_explicit_read_only", "org_x", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "read-only",
	); err != nil {
		t.Fatalf("insert row with an explicit scope: %v", err)
	}

	var scope string
	if err := sqldb.QueryRowContext(ctx, "SELECT scope FROM server_api_keys WHERE id = ?", "key_explicit_read_only").Scan(&scope); err != nil {
		t.Fatalf("query scope column: %v", err)
	}
	if scope != "read-only" {
		t.Errorf("scope = %q, want %q (an explicit value must not be overridden by the column default)", scope, "read-only")
	}
}
