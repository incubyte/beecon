// migration_0020_webhook_endpoints_multi_test.go proves migration 0020's own
// DDL and backfill directly against a database shaped exactly as it stood
// right before 0020 ran (migrations 0001 through 0019 applied for real,
// mirroring migration_connection_lifecycle_backfill_test.go's "raw-SQL
// seeded test" pattern): a pre-Slice-8 org holding exactly one
// webhook_endpoints row and one webhook_signing_secrets row (0014's shape,
// no event_types/status/consecutive_failures/endpoint_id columns yet) stands
// in for what a real installation already has on disk. This file reads
// 0020_webhook_endpoints_multi.up.sql/.down.sql's actual file content rather
// than a restated copy, so a future edit to either is exercised here rather
// than silently drifting out of sync.
package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"beecon/internal/config"
	"beecon/internal/db"
)

var migration0020TestDSNCounter int64

// preSlice8DatabaseThroughMigration0019 opens a fresh in-memory SQLite
// database and applies every real embedded migration up to and including
// 0019 (every migration strictly before 0020) by reading each migration's
// own .up.sql file and executing it in ascending numeric order — the same
// "apply the real files, not a restated copy" reasoning
// migration_0012_backfill_test.go and migration_connection_lifecycle_backfill_test.go
// already use, just walking the whole prefix instead of stopping after one
// hand-restated table. It deliberately does NOT go through db.Migrate (which
// always applies every embedded migration, including 0020) — there would
// otherwise be no way to observe the pre-Slice-8 shape at all.
func preSlice8DatabaseThroughMigration0019(t *testing.T) *bun.DB {
	t.Helper()
	n := atomic.AddInt64(&migration0020TestDSNCounter, 1)
	dsn := fmt.Sprintf("file:migration_0020_pre_%d?mode=memory&cache=shared", n)
	sqldb, err := db.New(config.DriverSQLite, dsn)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })

	files, err := filepath.Glob(filepath.Join("migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migration files: %v", err)
	}
	sort.Strings(files)

	ctx := context.Background()
	applied := 0
	for _, file := range files {
		base := filepath.Base(file)
		// Every migration file starts "NNNN_" — stop at 0020 itself so this
		// fixture stands exactly where a real pre-Slice-8 database would.
		if base >= "0020_" {
			break
		}
		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", base, err)
		}
		if _, err := sqldb.ExecContext(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", base, err)
		}
		applied++
	}
	if applied != 19 {
		t.Fatalf("applied %d migrations before 0020, want exactly 19 — the glob/prefix walk didn't find the expected pre-0020 set", applied)
	}
	return sqldb
}

func applyMigration0020Up(t *testing.T, sqldb *bun.DB) {
	t.Helper()
	upSQL, err := os.ReadFile("migrations/0020_webhook_endpoints_multi.up.sql")
	if err != nil {
		t.Fatalf("read migration 0020 up: %v", err)
	}
	if _, err := sqldb.ExecContext(context.Background(), string(upSQL)); err != nil {
		t.Fatalf("apply migration 0020 up: %v", err)
	}
}

// seedPreSlice8WebhookEndpointAndSecret inserts exactly the shape a real
// pre-Slice-8 org would have on disk: one webhook_endpoints row (0014's
// columns only) and one webhook_signing_secrets row whose organization_id
// matches it but which has never carried an endpoint_id (the column doesn't
// exist yet at this point in the fixture).
func seedPreSlice8WebhookEndpointAndSecret(t *testing.T, sqldb *bun.DB, org, endpointID, secretID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO webhook_endpoints (id, organization_id, url, created_at) VALUES (?, ?, ?, ?)",
		endpointID, org, "https://example.com/hook", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed pre-Slice-8 webhook_endpoints row: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx,
		"INSERT INTO webhook_signing_secrets (id, organization_id, display_prefix, encrypted_secret, created_at) VALUES (?, ?, ?, ?, ?)",
		secretID, org, "whsec_AAAAAAAA", "encrypted-ciphertext", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed pre-Slice-8 webhook_signing_secrets row: %v", err)
	}
}

// TestMigration0020_BackfillsThePreExistingSecretsEndpointIDOntoThatOrgsSingleEndpoint
// is the headline AC: a pre-Slice-8 org's single endpoint becomes endpoint #1
// and its existing secret's endpoint_id is backfilled to point at it — the
// exact continuity guarantee the PD31 single-endpoint alias depends on
// (RotateSecret/GetEndpoint resolve through org's first endpoint and then its
// endpoint-scoped secret; a NULL endpoint_id would silently orphan every
// pre-existing secret).
func TestMigration0020_BackfillsThePreExistingSecretsEndpointIDOntoThatOrgsSingleEndpoint(t *testing.T) {
	sqldb := preSlice8DatabaseThroughMigration0019(t)
	ctx := context.Background()
	seedPreSlice8WebhookEndpointAndSecret(t, sqldb, "org_legacy", "wep_legacy_1", "whs_legacy_1")

	applyMigration0020Up(t, sqldb)

	var endpointID sql.NullString
	err := sqldb.QueryRowContext(ctx,
		"SELECT endpoint_id FROM webhook_signing_secrets WHERE id = ?", "whs_legacy_1",
	).Scan(&endpointID)
	if err != nil {
		t.Fatalf("query backfilled endpoint_id: %v", err)
	}
	if !endpointID.Valid {
		t.Fatal("endpoint_id is NULL after migration 0020, want it backfilled to the org's single endpoint id")
	}
	if endpointID.String != "wep_legacy_1" {
		t.Errorf("endpoint_id = %q, want %q (the org's own pre-existing endpoint)", endpointID.String, "wep_legacy_1")
	}
}

// TestMigration0020_TheMigratedEndpointDefaultsToEnabledWithNoFilterAndZeroFailures
// pins the other half of "becomes endpoint #1... with... no filter": the
// three new webhook_endpoints columns must default exactly the way a
// migrated Phase 3 endpoint needs to keep fanning out identically —
// event_types NULL (matches every type), status ENABLED, consecutive_failures
// 0.
func TestMigration0020_TheMigratedEndpointDefaultsToEnabledWithNoFilterAndZeroFailures(t *testing.T) {
	sqldb := preSlice8DatabaseThroughMigration0019(t)
	ctx := context.Background()
	seedPreSlice8WebhookEndpointAndSecret(t, sqldb, "org_legacy", "wep_legacy_1", "whs_legacy_1")

	applyMigration0020Up(t, sqldb)

	var eventTypes sql.NullString
	var status string
	var consecutiveFailures int
	err := sqldb.QueryRowContext(ctx,
		"SELECT event_types, status, consecutive_failures FROM webhook_endpoints WHERE id = ?", "wep_legacy_1",
	).Scan(&eventTypes, &status, &consecutiveFailures)
	if err != nil {
		t.Fatalf("query migrated endpoint row: %v", err)
	}
	if eventTypes.Valid {
		t.Errorf("event_types = %q, want SQL NULL (no filter — matches every event type)", eventTypes.String)
	}
	if status != "ENABLED" {
		t.Errorf("status = %q, want %q", status, "ENABLED")
	}
	if consecutiveFailures != 0 {
		t.Errorf("consecutive_failures = %d, want 0", consecutiveFailures)
	}
}

// TestMigration0020_ASecondEndpointCanNowBeInsertedForTheSameOrganization is
// AC1's persistence-layer proof: 0020 drops webhook_endpoints' old per-org
// UNIQUE index, so a second row for an organization_id that already has one
// must no longer violate a uniqueness constraint.
func TestMigration0020_ASecondEndpointCanNowBeInsertedForTheSameOrganization(t *testing.T) {
	sqldb := preSlice8DatabaseThroughMigration0019(t)
	ctx := context.Background()
	seedPreSlice8WebhookEndpointAndSecret(t, sqldb, "org_legacy", "wep_legacy_1", "whs_legacy_1")
	applyMigration0020Up(t, sqldb)

	_, err := sqldb.ExecContext(ctx,
		"INSERT INTO webhook_endpoints (id, organization_id, url, status, consecutive_failures, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"wep_legacy_2", "org_legacy", "https://example.com/second-hook", "ENABLED", 0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("insert a second endpoint for an org that already has one: %v — the pre-Slice-8 org-unique index must be gone", err)
	}

	var count int
	if err := sqldb.QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_endpoints WHERE organization_id = ?", "org_legacy").Scan(&count); err != nil {
		t.Fatalf("count org_legacy's endpoints: %v", err)
	}
	if count != 2 {
		t.Errorf("org_legacy has %d endpoints, want 2", count)
	}
}

// TestMigration0020_DownMigrationReversesCleanlyOnASingleEndpointOrg proves
// the down migration actually works, not only the up: dropping every column
// 0020 added and restoring the org-unique index must succeed against a
// database that only ever had one endpoint per org (the common case every
// pre-Slice-8 installation is in) — and the restored unique index must
// reject a second endpoint for the same org again, proving it's the real
// constraint back, not a no-op.
func TestMigration0020_DownMigrationReversesCleanlyOnASingleEndpointOrg(t *testing.T) {
	sqldb := preSlice8DatabaseThroughMigration0019(t)
	ctx := context.Background()
	seedPreSlice8WebhookEndpointAndSecret(t, sqldb, "org_legacy", "wep_legacy_1", "whs_legacy_1")
	applyMigration0020Up(t, sqldb)

	downSQL, err := os.ReadFile("migrations/0020_webhook_endpoints_multi.down.sql")
	if err != nil {
		t.Fatalf("read migration 0020 down: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(downSQL)); err != nil {
		t.Fatalf("run down migration on a single-endpoint org: %v", err)
	}

	for _, q := range []string{
		"SELECT event_types FROM webhook_endpoints LIMIT 1",
		"SELECT status FROM webhook_endpoints LIMIT 1",
		"SELECT consecutive_failures FROM webhook_endpoints LIMIT 1",
	} {
		if _, err := sqldb.ExecContext(ctx, q); err == nil {
			t.Errorf("query %q succeeded after the down migration, want an error (column dropped)", q)
		}
	}
	if _, err := sqldb.ExecContext(ctx, "SELECT endpoint_id FROM webhook_signing_secrets LIMIT 1"); err == nil {
		t.Error("querying webhook_signing_secrets.endpoint_id succeeded after the down migration, want an error (column dropped)")
	}
	if _, err := sqldb.ExecContext(ctx, "SELECT endpoint_id FROM outbox_events LIMIT 1"); err == nil {
		t.Error("querying outbox_events.endpoint_id succeeded after the down migration, want an error (column dropped)")
	}

	_, err = sqldb.ExecContext(ctx,
		"INSERT INTO webhook_endpoints (id, organization_id, url, created_at) VALUES (?, ?, ?, ?)",
		"wep_legacy_2", "org_legacy", "https://example.com/second-hook", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Error("a second endpoint for the same org was accepted after the down migration, want the restored org-unique index to reject it")
	}
}
