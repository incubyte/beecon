//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header). This file covers the Slice 2 AC "The full key secret
// is not recoverable from the database (stored hashed; a database dump does
// not contain it)" literally: it issues a key through the real API, then
// dumps every column of every row of server_api_keys straight from the
// booted database and asserts the secret's post-prefix remainder appears
// nowhere. The 12-char lookup prefix is stored in plaintext by design (PD3),
// so the assertion targets the remainder — the part an attacker with a DB
// dump would need to authenticate.
package crucial_path

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"beecon/test/support"
)

func TestIssuedKeySecret_IsNotRecoverableFromADatabaseDump(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Dump Test Org"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var issued issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issued key: %v", err)
	}
	remainder := strings.TrimPrefix(issued.Key, issued.Prefix)
	if remainder == "" || remainder == issued.Key {
		t.Fatalf("test fixture bug: issued key %q does not start with its prefix %q", issued.Key, issued.Prefix)
	}

	rows, err := wired.DB.QueryContext(context.Background(),
		"SELECT id, org_id, lookup_prefix, secret_hash FROM server_api_keys")
	if err != nil {
		t.Fatalf("dump server_api_keys: %v", err)
	}
	defer rows.Close()

	rowCount := 0
	for rows.Next() {
		rowCount++
		var id, orgID, lookupPrefix, secretHash string
		if err := rows.Scan(&id, &orgID, &lookupPrefix, &secretHash); err != nil {
			t.Fatalf("scan dumped row: %v", err)
		}
		for column, value := range map[string]string{
			"id":            id,
			"org_id":        orgID,
			"lookup_prefix": lookupPrefix,
			"secret_hash":   secretHash,
		} {
			if strings.Contains(value, remainder) {
				t.Errorf("column %q of the database dump contains the secret's remainder — the full key %q is recoverable from the database", column, issued.Key)
			}
			if strings.Contains(value, issued.Key) {
				t.Errorf("column %q of the database dump contains the full secret %q", column, issued.Key)
			}
		}
		// The stored value must be the hash of the remainder, not the
		// remainder (or any other reversible encoding of it).
		wantHash := sha256.Sum256([]byte(remainder))
		if secretHash != hex.EncodeToString(wantHash[:]) {
			t.Errorf("secret_hash = %q, want the hex-encoded SHA-256 of the secret's remainder — storage scheme drifted from PD3", secretHash)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate dumped rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("dumped %d server_api_keys rows, want exactly 1", rowCount)
	}
}
