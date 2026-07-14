// Package access_test must be external (not in-package): driven/memory
// imports access, so an in-package test importing it would be an import
// cycle. Domain errors are *httpx.DomainError, so assertions use errors.As
// to check Code/Status rather than errors.Is against a singleton.
package access_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

const orgA = organizations.OrgID("org_a")
const orgB = organizations.OrgID("org_b")

func newFacade() *access.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{})
}

func assertDomainError(t *testing.T, err error, wantCode string, wantStatus int) *httpx.DomainError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected domain error with code %q, got nil", wantCode)
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Code != wantCode {
		t.Fatalf("error code = %q, want %q", de.Code, wantCode)
	}
	if de.Status != wantStatus {
		t.Fatalf("error status = %d, want %d", de.Status, wantStatus)
	}
	return de
}

func TestIssue_ReturnsAKeyPrefixedIDAndTheFullSecretWithTheBeeconSkPrefix(t *testing.T) {
	f := newFacade()

	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(issued.ID), "key_") {
		t.Errorf("id = %q, want it to start with %q", issued.ID, "key_")
	}
	if !strings.HasPrefix(issued.Secret, access.SecretPrefix) {
		t.Errorf("secret = %q, want it to start with %q", issued.Secret, access.SecretPrefix)
	}
	if issued.Prefix != issued.Secret[:access.LookupPrefixLength] {
		t.Errorf("prefix = %q, want the first %d chars of the secret (%q)", issued.Prefix, access.LookupPrefixLength, issued.Secret[:access.LookupPrefixLength])
	}
}

func TestIssue_ProducesADifferentSecretForEachKey(t *testing.T) {
	f := newFacade()

	first, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.Secret == second.Secret {
		t.Fatal("two calls to Issue produced the same secret")
	}
}

func TestList_ReturnsIDPrefixAndCreatedAtForEveryKeyIssuedToTheOrg(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, err := f.List(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].ID != issued.ID {
		t.Errorf("ID = %q, want %q", keys[0].ID, issued.ID)
	}
	if keys[0].Prefix != issued.Prefix {
		t.Errorf("Prefix = %q, want %q", keys[0].Prefix, issued.Prefix)
	}
	if !keys[0].CreatedAt.Equal(issued.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", keys[0].CreatedAt, issued.CreatedAt)
	}
}

func TestList_OnlyReturnsKeysBelongingToTheGivenOrg(t *testing.T) {
	f := newFacade()
	issuedToA, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.Issue(context.Background(), orgB, access.ScopeReadWrite); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, err := f.List(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1 (org B's key must not leak into org A's list)", len(keys))
	}
	if keys[0].ID != issuedToA.ID {
		t.Errorf("ID = %q, want %q", keys[0].ID, issuedToA.ID)
	}
}

func TestRevoke_MarksTheKeyRevokedSoAFutureVerifyOfItsSecretIsRejected(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.Verify(context.Background(), issued.Secret); err != nil {
		t.Fatalf("expected the freshly issued secret to verify before revocation, got: %v", err)
	}

	if err := f.Revoke(context.Background(), orgA, issued.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = f.Verify(context.Background(), issued.Secret)
	assertDomainError(t, err, "unauthorized", 401)
}

func TestRevoke_ReturnsNotFoundForAKeyBelongingToAnotherOrg(t *testing.T) {
	f := newFacade()
	issuedToA, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = f.Revoke(context.Background(), orgB, issuedToA.ID)

	assertDomainError(t, err, access.CodeNotFound, 404)
}

func TestRevoke_ReturnsNotFoundForAnUnknownKeyID(t *testing.T) {
	f := newFacade()

	err := f.Revoke(context.Background(), orgA, access.KeyID("key_missing"))

	assertDomainError(t, err, access.CodeNotFound, 404)
}

func TestVerify_ReturnsTheIssuingOrgForAFreshlyIssuedSecret(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.Verify(context.Background(), issued.Secret)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OrgID != orgA {
		t.Errorf("Verify() org = %q, want %q", got.OrgID, orgA)
	}
}

func TestVerify_RejectsASecretWithAnUnknownLookupPrefix(t *testing.T) {
	f := newFacade()
	if _, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := f.Verify(context.Background(), access.SecretPrefix+"totally-unknown-secret-value")

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerify_RejectsAWrongSecretThatSharesAnIssuedKeysLookupPrefix(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Same 12-char lookup prefix as the real key (so FindByPrefix returns a
	// candidate), but a different remainder — the hash comparison must still
	// reject it.
	wrongSecret := issued.Prefix + "-not-the-real-remainder"

	_, err = f.Verify(context.Background(), wrongSecret)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerify_RejectsASecretMissingTheBeeconSkPrefixEntirely(t *testing.T) {
	f := newFacade()

	_, err := f.Verify(context.Background(), "not-shaped-like-a-key-at-all")

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerify_RejectsAnEmptySecret(t *testing.T) {
	f := newFacade()

	_, err := f.Verify(context.Background(), "")

	assertDomainError(t, err, "unauthorized", 401)
}
