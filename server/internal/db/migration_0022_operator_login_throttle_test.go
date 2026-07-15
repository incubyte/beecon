// migration_0022_operator_login_throttle_test.go proves migration 0022's own
// DDL directly against a fully-migrated real SQLite database (mirrors
// migration_0017_api_key_scope_test.go's own newFullyMigratedSQLiteDB
// helper): failed_attempts and locked_until are a plain DEFAULT-0/NULL
// column pair added onto a table shape (operator_accounts) that already
// exists by then, so — like 0017 — there is nothing to reconstruct by hand;
// an existing row inserted without mentioning either column gets
// failed_attempts=0 and locked_until IS NULL, and the down migration
// actually drops both columns.
package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestMigration0022_ARowInsertedWithoutMentioningEitherColumnDefaultsToZeroFailedAttemptsAndNullLockedUntil(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO operator_accounts (id, email, password_hash, status, created_at) VALUES (?, ?, ?, ?, ?)",
		"op_no_throttle_columns_mentioned", "existing-operator@example.com", "$argon2id$placeholder", "ACTIVE",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("insert row naming neither throttle column: %v", err)
	}

	var failedAttempts int
	var lockedUntil sql.NullTime
	err := sqldb.QueryRowContext(ctx,
		"SELECT failed_attempts, locked_until FROM operator_accounts WHERE id = ?",
		"op_no_throttle_columns_mentioned",
	).Scan(&failedAttempts, &lockedUntil)
	if err != nil {
		t.Fatalf("query defaulted row: %v", err)
	}
	if failedAttempts != 0 {
		t.Errorf("failed_attempts = %d, want 0 (an existing/pre-migration row must default to un-throttled)", failedAttempts)
	}
	if lockedUntil.Valid {
		t.Errorf("locked_until = %v, want SQL NULL", lockedUntil.Time)
	}
}

// TestMigration0022_TheColumnsRoundTripAnExplicitValue pins the columns'
// other side: an explicitly written failed_attempts/locked_until pair (what
// OperatorRepository.RecordFailedAttempt itself writes) is preserved
// unchanged, not coerced back to the column defaults.
func TestMigration0022_TheColumnsRoundTripAnExplicitValue(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()
	lockedUntil := time.Date(2026, 7, 15, 9, 15, 0, 0, time.UTC)

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO operator_accounts (id, email, password_hash, status, failed_attempts, locked_until, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"op_with_explicit_throttle", "throttled-operator@example.com", "$argon2id$placeholder", "ACTIVE",
		5, lockedUntil, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("insert row with explicit throttle values: %v", err)
	}

	var failedAttempts int
	var gotLockedUntil sql.NullTime
	err := sqldb.QueryRowContext(ctx,
		"SELECT failed_attempts, locked_until FROM operator_accounts WHERE id = ?",
		"op_with_explicit_throttle",
	).Scan(&failedAttempts, &gotLockedUntil)
	if err != nil {
		t.Fatalf("query explicit row: %v", err)
	}
	if failedAttempts != 5 {
		t.Errorf("failed_attempts = %d, want 5", failedAttempts)
	}
	if !gotLockedUntil.Valid || !gotLockedUntil.Time.Equal(lockedUntil) {
		t.Errorf("locked_until = %v (valid=%v), want %v", gotLockedUntil.Time, gotLockedUntil.Valid, lockedUntil)
	}
}

// TestMigration0022_DownMigrationDropsBothThrottleColumns reads the real
// down.sql file content and runs it against a fully-migrated database: both
// columns must be gone afterward (selecting either errors), proving 0022
// ships a working down migration, not only an up.
func TestMigration0022_DownMigrationDropsBothThrottleColumns(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	downSQL, err := os.ReadFile("migrations/0022_operator_login_throttle.down.sql")
	if err != nil {
		t.Fatalf("read down migration file: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("run down migration: %v", err)
	}

	_, err = sqldb.ExecContext(ctx, "SELECT failed_attempts FROM operator_accounts LIMIT 1")
	if err == nil {
		t.Error("expected querying failed_attempts after the down migration to fail (column dropped), got nil error")
	}

	_, err = sqldb.ExecContext(ctx, "SELECT locked_until FROM operator_accounts LIMIT 1")
	if err == nil {
		t.Error("expected querying locked_until after the down migration to fail (column dropped), got nil error")
	}
}
