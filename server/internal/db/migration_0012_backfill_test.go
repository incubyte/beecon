// migration_0012_backfill_test.go proves migration 0012's own backfill
// statement (INSERT INTO server_api_key_secrets ... SELECT id, id,
// lookup_prefix, secret_hash, created_at, NULL FROM server_api_keys) directly
// against a database shaped exactly as it stood right before 0012 ran (the
// server_api_keys table as migration 0002 left it, carrying lookup_prefix
// and secret_hash directly — no server_api_key_secrets table yet). This is
// the same "raw-SQL seeded test" pattern
// migration_connection_lifecycle_backfill_test.go uses for 0008: db.Migrate's
// own bun-migrator machinery always applies every migration to a fresh
// database in one boot, so there is no way to observe pre-existing data
// through a normal app boot — the seeded rows below stand in for what a real
// pre-Slice-8 database would already contain. It reads
// 0012_api_key_secrets.up.sql's actual file content rather than a restated
// copy, so a future edit to that file is exercised by this test rather than
// silently drifting out of sync with it.
package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"beecon/internal/access"
	accessbun "beecon/internal/access/driven/bun"
	"beecon/internal/organizations"
)

// preSlice8ServerApiKeysTableDDL mirrors the server_api_keys table exactly as
// migration 0002 created it — lookup_prefix and secret_hash still live
// directly on the key row — before 0012 ever runs.
const preSlice8ServerApiKeysTableDDL = `
CREATE TABLE server_api_keys (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    lookup_prefix VARCHAR(20) NOT NULL,
    secret_hash VARCHAR(128) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL
);

CREATE INDEX idx_server_api_keys_org_id ON server_api_keys (org_id);
CREATE INDEX idx_server_api_keys_lookup_prefix ON server_api_keys (lookup_prefix);
`

// legacySecret is the plaintext of a pre-Slice-8 key's one and only secret —
// exactly the shape Phase 1/early Phase 2's single-secret-per-key model
// stored: a SecretPrefix-prefixed value whose first LookupPrefixLength
// characters are the plaintext lookup_prefix and whose remainder is hashed
// into secret_hash.
const legacySecret = "beecon_sk_AAlegacy-secret-issued-before-migration-0012"

// seedLegacyServerApiKeyRow inserts a row shaped exactly as a pre-0012
// server_api_keys table would hold it: lookup_prefix and the hex-encoded
// SHA-256 hash of the remainder, computed here with crypto/sha256 directly
// (mirroring access/secret.go's own hashSecretRemainder scheme) since this is
// an external, pre-migration fixture rather than a call through the access
// package.
func seedLegacyServerApiKeyRow(t *testing.T, sqldb *bun.DB, id, orgID, secret string, revokedAt *time.Time) {
	t.Helper()
	hash := sha256.Sum256([]byte(secret[access.LookupPrefixLength:]))
	_, err := sqldb.ExecContext(context.Background(),
		"INSERT INTO server_api_keys (id, org_id, lookup_prefix, secret_hash, created_at, revoked_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, orgID, secret[:access.LookupPrefixLength], hex.EncodeToString(hash[:]), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), revokedAt,
	)
	if err != nil {
		t.Fatalf("seed legacy server_api_keys row %q: %v", id, err)
	}
}

func applyMigration0012(t *testing.T, sqldb *bun.DB) {
	t.Helper()
	migrationSQL, err := os.ReadFile("migrations/0012_api_key_secrets.up.sql")
	if err != nil {
		t.Fatalf("read migration 0012: %v", err)
	}
	if _, err := sqldb.ExecContext(context.Background(), string(migrationSQL)); err != nil {
		t.Fatalf("apply migration 0012: %v", err)
	}
}

func TestMigration0012Backfill_MovesEachExistingKeysLookupPrefixAndHashIntoOneSecretRow(t *testing.T) {
	sqldb := freshSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx, preSlice8ServerApiKeysTableDDL); err != nil {
		t.Fatalf("create pre-Slice-8 server_api_keys table: %v", err)
	}
	seedLegacyServerApiKeyRow(t, sqldb, "key_active_legacy", "org_legacy_a", legacySecret, nil)
	revokedAt := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	seedLegacyServerApiKeyRow(t, sqldb, "key_revoked_legacy", "org_legacy_b", "beecon_sk_BBanother-legacy-secret-entirely", &revokedAt)

	applyMigration0012(t, sqldb)

	rows, err := sqldb.QueryContext(ctx, "SELECT id, key_id, lookup_prefix, secret_hash, expires_at FROM server_api_key_secrets ORDER BY key_id")
	if err != nil {
		t.Fatalf("query server_api_key_secrets: %v", err)
	}
	defer rows.Close()

	type backfilledSecret struct {
		id, keyID, lookupPrefix, secretHash string
		expiresAt                           *time.Time
	}
	var got []backfilledSecret
	for rows.Next() {
		var s backfilledSecret
		if err := rows.Scan(&s.id, &s.keyID, &s.lookupPrefix, &s.secretHash, &s.expiresAt); err != nil {
			t.Fatalf("scan server_api_key_secrets row: %v", err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate server_api_key_secrets rows: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("backfilled %d server_api_key_secrets rows, want exactly 2 (one per pre-existing key)", len(got))
	}
	wantHash := sha256.Sum256([]byte(legacySecret[access.LookupPrefixLength:]))
	first := got[0]
	if first.keyID != "key_active_legacy" {
		t.Fatalf("key_id = %q, want %q", first.keyID, "key_active_legacy")
	}
	if first.id != first.keyID {
		t.Errorf("backfilled secret id = %q, want it seeded from the key's own id (%q), matching 0012's SELECT id, id, ...", first.id, first.keyID)
	}
	if first.lookupPrefix != legacySecret[:access.LookupPrefixLength] {
		t.Errorf("lookup_prefix = %q, want %q", first.lookupPrefix, legacySecret[:access.LookupPrefixLength])
	}
	if first.secretHash != hex.EncodeToString(wantHash[:]) {
		t.Errorf("secret_hash = %q, want the hex-encoded SHA-256 of the legacy secret's remainder", first.secretHash)
	}
	if first.expiresAt != nil {
		t.Errorf("expires_at = %v, want NULL — a backfilled secret is the currently active one, not an outgoing overlap secret", first.expiresAt)
	}

	// The revoked key's own row's org_id/revoked_at stay on server_api_keys
	// untouched by 0012 (only the secret material moves) — confirmed here so
	// the backfill isn't mistaken for having dropped revocation state.
	var revokedAtBack *time.Time
	if err := sqldb.QueryRowContext(ctx, "SELECT revoked_at FROM server_api_keys WHERE id = ?", "key_revoked_legacy").Scan(&revokedAtBack); err != nil {
		t.Fatalf("query server_api_keys.revoked_at: %v", err)
	}
	if revokedAtBack == nil {
		t.Error("revoked_at on server_api_keys was lost across the migration")
	}
}

// TestMigration0012Backfill_PreExistingKeysSecretStillAuthenticatesThroughTheAccessFacadeAfterTheMigration
// is the coordinator's explicit ask: not just that the raw row shape looks
// right, but that a pre-existing org's server api key keeps authenticating
// end to end through the real access.Facade/driven/bun adapter once 0012 has
// run — the backfill is invisible to a consumer who was already using the
// key before the migration.
func TestMigration0012Backfill_PreExistingKeysSecretStillAuthenticatesThroughTheAccessFacadeAfterTheMigration(t *testing.T) {
	sqldb := freshSQLiteDB(t)
	ctx := context.Background()

	if _, err := sqldb.ExecContext(ctx, preSlice8ServerApiKeysTableDDL); err != nil {
		t.Fatalf("create pre-Slice-8 server_api_keys table: %v", err)
	}
	seedLegacyServerApiKeyRow(t, sqldb, "key_active_legacy", "org_legacy_a", legacySecret, nil)

	applyMigration0012(t, sqldb)

	repo := accessbun.NewRepository(sqldb)
	facade := access.NewFacade(repo, repo, repo, nil, nil, nil, nil,
		func() string { return "unused" }, func() string { return "unused" }, func() string { return "unused" }, func() string { return "unused" },
		func() time.Time { return time.Now() })

	gotOrg, err := facade.Verify(ctx, legacySecret)

	if err != nil {
		t.Fatalf("a pre-existing key's secret failed to verify after the 0012 migration: %v", err)
	}
	if gotOrg != organizations.OrgID("org_legacy_a") {
		t.Errorf("Verify() org = %q, want %q", gotOrg, "org_legacy_a")
	}
}
