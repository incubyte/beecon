// migration_0024_event_log_tool_id_test.go proves migration 0024's own DDL
// directly against a fully-migrated real SQLite database (mirrors
// migration_0022_operator_login_throttle_test.go's own use of
// newFullyMigratedSQLiteDB): tool_id is a plain nullable column added onto a
// table shape (event_logs) that already exists by then — an existing/
// pre-registry row inserted without ever mentioning it reads back SQL NULL,
// a new row round-trips an explicit tool_ id, and the down migration
// actually drops the column.
package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestMigration0024_ARowInsertedWithoutMentioningToolIDReadsBackNull(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO event_logs (id, org_id, tool_slug, kind, status, duration_ms, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"log_pre_registry_no_tool_id", "org_x", "outlook-list-messages", "tool_execution", 200, 42,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("insert row naming no tool_id column: %v", err)
	}

	var toolID sql.NullString
	if err := sqldb.QueryRowContext(ctx, "SELECT tool_id FROM event_logs WHERE id = ?", "log_pre_registry_no_tool_id").Scan(&toolID); err != nil {
		t.Fatalf("query tool_id column: %v", err)
	}
	if toolID.Valid {
		t.Errorf("tool_id = %q, want SQL NULL (an existing/pre-registry row must default to no tool_ id)", toolID.String)
	}
}

// TestMigration0024_ANewRowRoundTripsAnExplicitToolID pins the column's other
// side: an explicitly written tool_ id (what execution's log attribution,
// via app/recorders.go, actually persists) is preserved unchanged.
func TestMigration0024_ANewRowRoundTripsAnExplicitToolID(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO event_logs (id, org_id, tool_id, tool_slug, kind, status, duration_ms, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"log_with_tool_id", "org_x", "tool_round_trip_check", "outlook-list-messages", "tool_execution", 200, 42,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("insert row with an explicit tool_id: %v", err)
	}

	var toolID sql.NullString
	if err := sqldb.QueryRowContext(ctx, "SELECT tool_id FROM event_logs WHERE id = ?", "log_with_tool_id").Scan(&toolID); err != nil {
		t.Fatalf("query tool_id column: %v", err)
	}
	if !toolID.Valid || toolID.String != "tool_round_trip_check" {
		t.Errorf("tool_id = %q (valid=%v), want %q", toolID.String, toolID.Valid, "tool_round_trip_check")
	}
}

// TestMigration0024_DownMigrationDropsTheToolIDColumn reads the real
// down.sql file content and runs it against a fully-migrated database: the
// column must be gone afterward (selecting it errors), proving 0024 ships a
// working down migration, not only an up.
func TestMigration0024_DownMigrationDropsTheToolIDColumn(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	downSQL, err := os.ReadFile("migrations/0024_event_log_tool_id.down.sql")
	if err != nil {
		t.Fatalf("read down migration file: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("run down migration: %v", err)
	}

	_, err = sqldb.ExecContext(ctx, "SELECT tool_id FROM event_logs LIMIT 1")
	if err == nil {
		t.Error("expected querying tool_id after the down migration to fail (column dropped), got nil error")
	}
}
