// migration_connection_lifecycle_backfill_test.go proves migration 0008's
// own backfill statement (UPDATE connections SET connect_token_used = TRUE
// WHERE status <> 'INITIATED') directly against a database shaped exactly as
// it stood right before 0008 ran (the connections table as migrations
// 0003+0004+0007 left it — no token_expires_at/connect_token_expires_at/
// connect_token_used columns yet). This is the "raw-SQL seeded test" the
// architecture calls for: db.Migrate's own bun-migrator machinery always
// applies every migration to a fresh database in one boot, so there is no
// way to observe pre-existing data through a normal app boot — the seeded
// rows below stand in for what a real pre-Slice-4 database would already
// contain. It reads 0008_connection_lifecycle.up.sql's actual file content
// rather than a restated copy, so a future edit to that file is exercised by
// this test rather than silently drifting out of sync with it.
package db_test

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"beecon/internal/config"
	"beecon/internal/db"
)

var backfillTestDSNCounter int64

func freshSQLiteDB(t *testing.T) *bun.DB {
	t.Helper()
	n := atomic.AddInt64(&backfillTestDSNCounter, 1)
	dsn := fmt.Sprintf("file:migration_0008_backfill_%d?mode=memory&cache=shared", n)
	sqldb, err := db.New(config.DriverSQLite, dsn)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return sqldb
}

// preSlice4ConnectionsTableDDL mirrors the connections table exactly as
// migrations 0003 (create table), 0004 (token/account columns), and 0007
// (encrypted_params) left it — before 0008 ever runs.
const preSlice4ConnectionsTableDDL = `
CREATE TABLE connections (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    integration_id VARCHAR(64) NOT NULL,
    provider_slug VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    redirect_uri VARCHAR(2048) NOT NULL,
    connect_token VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    encrypted_access_token TEXT NOT NULL DEFAULT '',
    encrypted_refresh_token TEXT NOT NULL DEFAULT '',
    account_email VARCHAR(255) NOT NULL DEFAULT '',
    account_display_name VARCHAR(255) NOT NULL DEFAULT '',
    encrypted_params TEXT NOT NULL DEFAULT ''
);
`

func TestMigration0008Backfill_MarksEveryNonInitiatedRowsConnectTokenAsAlreadyUsed(t *testing.T) {
	sqldb := freshSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx, preSlice4ConnectionsTableDDL); err != nil {
		t.Fatalf("create pre-Slice-4 connections table: %v", err)
	}

	seedConnectionRow(t, sqldb, "conn_active_phase1", "ACTIVE")
	seedConnectionRow(t, sqldb, "conn_disconnected_phase1", "DISCONNECTED")
	seedConnectionRow(t, sqldb, "conn_still_initiated", "INITIATED")

	migrationSQL, err := os.ReadFile("migrations/0008_connection_lifecycle.up.sql")
	if err != nil {
		t.Fatalf("read migration 0008: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("apply migration 0008: %v", err)
	}

	rows, err := sqldb.QueryContext(ctx, "SELECT id, connect_token_used FROM connections ORDER BY id")
	if err != nil {
		t.Fatalf("query connections: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var id string
		var connectTokenUsed bool
		if err := rows.Scan(&id, &connectTokenUsed); err != nil {
			t.Fatalf("scan connections row: %v", err)
		}
		got[id] = connectTokenUsed
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate connections rows: %v", err)
	}

	if !got["conn_active_phase1"] {
		t.Error(`a pre-existing ACTIVE row must have connect_token_used = TRUE after the backfill — it must not carry a reusable connect link`)
	}
	if !got["conn_disconnected_phase1"] {
		t.Error(`a pre-existing DISCONNECTED row must have connect_token_used = TRUE after the backfill`)
	}
	if got["conn_still_initiated"] {
		t.Error(`a pre-existing INITIATED row's connect link is still open — the backfill must leave connect_token_used = FALSE for it`)
	}
}

func seedConnectionRow(t *testing.T, sqldb *bun.DB, id, status string) {
	t.Helper()
	_, err := sqldb.NewInsert().Model(&preSlice4ConnectionRow{
		ID:            id,
		OrgID:         "org_1",
		UserID:        "user_1",
		IntegrationID: "intg_1",
		ProviderSlug:  "outlook",
		Status:        status,
		RedirectURI:   "https://consumer.example.com/callback",
		ConnectToken:  "connect_token_" + id,
		CreatedAt:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Exec(context.Background())
	if err != nil {
		t.Fatalf("seed connections row %q: %v", id, err)
	}
}

// preSlice4ConnectionRow models only the pre-Slice-4 columns
// preSlice4ConnectionsTableDDL creates — bun infers the table name
// ("connections") from the struct name's default pluralization, matched
// explicitly here for clarity.
type preSlice4ConnectionRow struct {
	bun.BaseModel `bun:"table:connections"`

	ID            string    `bun:"id,pk"`
	OrgID         string    `bun:"org_id"`
	UserID        string    `bun:"user_id"`
	IntegrationID string    `bun:"integration_id"`
	ProviderSlug  string    `bun:"provider_slug"`
	Status        string    `bun:"status"`
	RedirectURI   string    `bun:"redirect_uri"`
	ConnectToken  string    `bun:"connect_token"`
	CreatedAt     time.Time `bun:"created_at"`
}
