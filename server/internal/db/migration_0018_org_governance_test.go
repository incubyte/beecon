// migration_0018_org_governance_test.go proves migration 0018's own DDL
// directly against a fully-migrated real SQLite database (mirrors
// migration_0017_api_key_scope_test.go's newFullyMigratedSQLiteDB helper — a
// brand-new table with column DEFAULTs, no pre-existing data shape to
// reconstruct like migration_0012_backfill_test.go needs): a row inserted
// naming only the primary key must read every other column back at its
// declared default (allow_list NULL, hidden/featured '[]', featured_cap 8) —
// exactly the shape organizations.NewDefaultGovernance synthesizes for an
// organization that never explicitly configured its governance (PD42). A
// second test proves the down migration actually drops the table, reading
// both .sql files' real content rather than a restated copy so a future edit
// to either file is exercised here instead of silently drifting out of sync.
package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

func TestMigration0018_ARowInsertedNamingOnlyThePrimaryKeyGetsEveryColumnsDeclaredDefault(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO org_governance (organization_id) VALUES (?)", "org_defaults_only",
	); err != nil {
		t.Fatalf("insert row naming only the primary key: %v", err)
	}

	var allowList sql.NullString
	var hidden, featured string
	var featuredCap int
	err := sqldb.QueryRowContext(ctx,
		"SELECT allow_list, hidden, featured, featured_cap FROM org_governance WHERE organization_id = ?",
		"org_defaults_only",
	).Scan(&allowList, &hidden, &featured, &featuredCap)
	if err != nil {
		t.Fatalf("query defaulted row: %v", err)
	}
	if allowList.Valid {
		t.Errorf("allow_list = %q, want SQL NULL (PD42's inherit-the-full-catalog default)", allowList.String)
	}
	if hidden != "[]" {
		t.Errorf("hidden = %q, want the column default %q", hidden, "[]")
	}
	if featured != "[]" {
		t.Errorf("featured = %q, want the column default %q", featured, "[]")
	}
	if featuredCap != 8 {
		t.Errorf("featured_cap = %d, want the column default 8", featuredCap)
	}
}

// TestMigration0018_AnExplicitAllowListIsPreservedUnchanged pins the
// column's other side: 0018 only supplies a default for allow_list (NULL),
// it must never coerce or clamp an explicitly-provided value.
func TestMigration0018_AnExplicitAllowListIsPreservedUnchanged(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO org_governance (organization_id, allow_list) VALUES (?, ?)",
		"org_with_allow_list", `["intg_1"]`,
	); err != nil {
		t.Fatalf("insert row with an explicit allow_list: %v", err)
	}

	var allowList string
	if err := sqldb.QueryRowContext(ctx, "SELECT allow_list FROM org_governance WHERE organization_id = ?", "org_with_allow_list").Scan(&allowList); err != nil {
		t.Fatalf("query allow_list: %v", err)
	}
	if allowList != `["intg_1"]` {
		t.Errorf("allow_list = %q, want the explicit value preserved unchanged", allowList)
	}
}

// TestMigration0018_OrganizationIDIsThePrimaryKeyRejectingADuplicateInsert
// pins the schema's PK: a second row naming an already-used organization_id
// must fail, guaranteeing SaveGovernance's own find-then-insert-or-update
// upsert logic (bun/repository.go) is the only way to replace a row, never a
// second silent insert.
func TestMigration0018_OrganizationIDIsThePrimaryKeyRejectingADuplicateInsert(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx, "INSERT INTO org_governance (organization_id) VALUES (?)", "org_dup"); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err := sqldb.ExecContext(ctx, "INSERT INTO org_governance (organization_id) VALUES (?)", "org_dup")

	if err == nil {
		t.Fatal("expected a primary-key violation inserting a second row with the same organization_id, got nil error")
	}
}

// TestMigration0018_DownMigrationDropsTheTable reads the real down.sql file
// content and runs it against a fully-migrated database: the table must be
// gone afterward (querying it errors), proving 0018 ships a working down
// migration rather than only an up.
func TestMigration0018_DownMigrationDropsTheTable(t *testing.T) {
	sqldb := newFullyMigratedSQLiteDB(t)
	ctx := context.Background()

	downSQL, err := os.ReadFile("migrations/0018_org_governance.down.sql")
	if err != nil {
		t.Fatalf("read down migration file: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("run down migration: %v", err)
	}

	_, err = sqldb.ExecContext(ctx, "SELECT 1 FROM org_governance LIMIT 1")

	if err == nil {
		t.Fatal("expected querying org_governance after the down migration to fail (table dropped), got nil error")
	}
}
