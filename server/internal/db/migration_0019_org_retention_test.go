// migration_0019_org_retention_test.go proves migration 0019's own DDL
// directly against a fully-migrated real SQLite database (mirrors
// migration_0018_org_governance_test.go's own newFullyMigratedSQLiteDB
// helper): the two new columns default to NULL on an existing org_governance
// row (PD44's "unset inherits the installation default"), an explicit value
// is preserved unchanged, and the down migration actually drops both
// columns.
package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

func TestMigration0019_ARowInsertedNamingOnlyThePrimaryKeyGetsBothRetentionColumnsNull(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO org_governance (organization_id) VALUES (?)", "org_defaults_only",
	); err != nil {
		t.Fatalf("insert row naming only the primary key: %v", err)
	}

	var logDays, eventDays sql.NullInt64
	err := sqldb.QueryRowContext(ctx,
		"SELECT log_retention_days, event_retention_days FROM org_governance WHERE organization_id = ?",
		"org_defaults_only",
	).Scan(&logDays, &eventDays)
	if err != nil {
		t.Fatalf("query defaulted row: %v", err)
	}
	if logDays.Valid {
		t.Errorf("log_retention_days = %v, want SQL NULL (inherit the installation default)", logDays.Int64)
	}
	if eventDays.Valid {
		t.Errorf("event_retention_days = %v, want SQL NULL (inherit the installation default)", eventDays.Int64)
	}
}

// TestMigration0019_AnExplicitRetentionValueIncludingZeroIsPreservedUnchanged
// pins the columns' other side: 0019 only supplies NULL as the default, it
// must never coerce an explicitly-provided value — including 0
// (unlimited/disabled), which must stay distinct from NULL, not be treated
// as "no value."
func TestMigration0019_AnExplicitRetentionValueIncludingZeroIsPreservedUnchanged(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO org_governance (organization_id, log_retention_days, event_retention_days) VALUES (?, ?, ?)",
		"org_with_explicit_retention", 14, 0,
	); err != nil {
		t.Fatalf("insert row with explicit retention values: %v", err)
	}

	var logDays, eventDays sql.NullInt64
	err := sqldb.QueryRowContext(ctx,
		"SELECT log_retention_days, event_retention_days FROM org_governance WHERE organization_id = ?",
		"org_with_explicit_retention",
	).Scan(&logDays, &eventDays)
	if err != nil {
		t.Fatalf("query explicit row: %v", err)
	}
	if !logDays.Valid || logDays.Int64 != 14 {
		t.Errorf("log_retention_days = %v (valid=%v), want 14", logDays.Int64, logDays.Valid)
	}
	if !eventDays.Valid || eventDays.Int64 != 0 {
		t.Errorf("event_retention_days = %v (valid=%v), want 0 (present, not NULL)", eventDays.Int64, eventDays.Valid)
	}
}

// TestMigration0019_DownMigrationDropsBothRetentionColumns reads the real
// down.sql file content and runs it against a fully-migrated database: both
// columns must be gone afterward (selecting either errors), proving 0019
// ships a working down migration, not only an up.
func TestMigration0019_DownMigrationDropsBothRetentionColumns(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	downSQL, err := os.ReadFile("migrations/0019_org_retention.down.sql")
	if err != nil {
		t.Fatalf("read down migration file: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("run down migration: %v", err)
	}

	_, err = sqldb.ExecContext(ctx, "SELECT log_retention_days FROM org_governance LIMIT 1")
	if err == nil {
		t.Error("expected querying log_retention_days after the down migration to fail (column dropped), got nil error")
	}

	_, err = sqldb.ExecContext(ctx, "SELECT event_retention_days FROM org_governance LIMIT 1")
	if err == nil {
		t.Error("expected querying event_retention_days after the down migration to fail (column dropped), got nil error")
	}
}
