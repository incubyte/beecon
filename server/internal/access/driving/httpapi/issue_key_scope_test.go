// Package httpapi (in-package test, see handler_test.go for the shared
// newTestRouter/doRequest/wireErrorEnvelope/decodeError helpers reused here).
// This file covers Issue's scope request/response wire shape (PD41, Slice
// 4): choosing a scope on create, an invalid scope's validation error, and
// that List surfaces every key's own scope.
package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/access"
)

// doRequestWithBody (a request that needs a JSON body, unlike doRequest's
// always-empty one) is already declared in rotate_handler_test.go — same
// package, reused here rather than redeclared.

func TestIssue_WithNoScopeInTheRequestBodyDefaultsToReadWrite(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodPost, "/org_1/api-keys")

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto issuedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Scope != string(access.ScopeReadWrite) {
		t.Errorf("scope = %q, want %q (every pre-existing caller that omits scope keeps full access)", dto.Scope, access.ScopeReadWrite)
	}
}

func TestIssue_WithAnExplicitReadOnlyScopeReturnsItInTheResponse(t *testing.T) {
	r := newTestRouter()

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys", `{"scope":"read-only"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto issuedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Scope != "read-only" {
		t.Errorf("scope = %q, want %q", dto.Scope, "read-only")
	}
}

func TestIssue_WithAnExplicitReadWriteScopeReturnsItInTheResponse(t *testing.T) {
	r := newTestRouter()

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys", `{"scope":"read-write"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto issuedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Scope != "read-write" {
		t.Errorf("scope = %q, want %q", dto.Scope, "read-write")
	}
}

func TestIssue_RejectsAnUnrecognizedScopeValueWithThePD5ValidationEnvelope(t *testing.T) {
	r := newTestRouter()

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys", `{"scope":"admin"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}
}

// TestList_SurfacesEachIssuedKeysOwnScopeAndNeverASecret pins List's wire
// shape once scope is in play: two keys issued with different scopes each
// show their own scope, and (mirroring TestList_Returns200WithNoSecretFieldAnywhereInTheJSON's
// own guard in handler_test.go) neither entry ever carries a "key" field.
func TestList_SurfacesEachIssuedKeysOwnScopeAndNeverASecret(t *testing.T) {
	r := newTestRouter()
	doRequestWithBody(r, http.MethodPost, "/org_1/api-keys", `{"scope":"read-only"}`)
	doRequestWithBody(r, http.MethodPost, "/org_1/api-keys", `{"scope":"read-write"}`)

	w := doRequest(r, http.MethodGet, "/org_1/api-keys")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	gotScopes := map[string]bool{}
	for _, entry := range entries {
		if _, present := entry["key"]; present {
			t.Fatalf("list entry %+v carries a %q field — the full secret must never appear in List", entry, "key")
		}
		scope, _ := entry["scope"].(string)
		gotScopes[scope] = true
	}
	if !gotScopes["read-only"] || !gotScopes["read-write"] {
		t.Errorf("scopes seen across the listing = %v, want both %q and %q present", gotScopes, "read-only", "read-write")
	}
}
