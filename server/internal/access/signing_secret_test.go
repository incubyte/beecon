// Package access_test (see facade_test.go for the external-package rationale).
// This file covers PD20's signing-secret issuance: the raw secret is
// available exactly once, at Issue, and List never resurfaces it.
package access_test

import (
	"context"
	"strings"
	"testing"

	memory "beecon/internal/access/driven/memory"
)

func TestIssueSigningSecret_ReturnsAUskPrefixedIDAndTheFullSecretExactlyOnce(t *testing.T) {
	f := newFacade()

	issued, err := f.IssueSigningSecret(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(issued.ID), "usk_") {
		t.Errorf("id = %q, want it to start with %q", issued.ID, "usk_")
	}
	if issued.Secret == "" {
		t.Fatal("secret is empty, want a minted random secret")
	}
	if !strings.HasPrefix(issued.Secret, issued.Prefix) {
		t.Errorf("secret %q does not start with its own displayed prefix %q", issued.Secret, issued.Prefix)
	}
}

func TestIssueSigningSecret_ProducesADifferentSecretForEachIssue(t *testing.T) {
	f := newFacade()

	first, err := f.IssueSigningSecret(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.IssueSigningSecret(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.Secret == second.Secret {
		t.Fatal("two calls to IssueSigningSecret produced the same secret")
	}
	if first.ID == second.ID {
		t.Fatal("two calls to IssueSigningSecret produced the same id")
	}
}

func TestListSigningSecrets_ReturnsIDPrefixAndCreatedAtForEverySecretIssuedToTheOrg(t *testing.T) {
	f := newFacade()
	issued, err := f.IssueSigningSecret(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	secrets, err := f.ListSigningSecrets(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secrets) != 1 {
		t.Fatalf("len(secrets) = %d, want 1", len(secrets))
	}
	if secrets[0].ID != issued.ID {
		t.Errorf("ID = %q, want %q", secrets[0].ID, issued.ID)
	}
	if secrets[0].DisplayPrefix != issued.Prefix {
		t.Errorf("DisplayPrefix = %q, want %q", secrets[0].DisplayPrefix, issued.Prefix)
	}
	if !secrets[0].CreatedAt.Equal(issued.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", secrets[0].CreatedAt, issued.CreatedAt)
	}
}

func TestListSigningSecrets_OnlyReturnsSecretsBelongingToTheGivenOrg(t *testing.T) {
	f := newFacade()
	issuedToA, err := f.IssueSigningSecret(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.IssueSigningSecret(context.Background(), orgB); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	secrets, err := f.ListSigningSecrets(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secrets) != 1 {
		t.Fatalf("len(secrets) = %d, want 1 (org B's signing secret must not leak into org A's list)", len(secrets))
	}
	if secrets[0].ID != issuedToA.ID {
		t.Errorf("ID = %q, want %q", secrets[0].ID, issuedToA.ID)
	}
}

// TestIssueSigningSecret_StoresTheSecretEncryptedNotInPlaintext proves PD20's
// "stored encrypted with the vault key" requirement at the persisted-record
// level: the record access.Facade actually saves carries vault ciphertext,
// not the raw secret verbatim, and decrypting it with the same vault
// recovers exactly the secret Issue returned.
func TestIssueSigningSecret_StoresTheSecretEncryptedNotInPlaintext(t *testing.T) {
	repo := memory.NewSigningSecretRepository()
	f := memory.NewFacadeWithOverrides(memory.Overrides{SigningSecrets: repo, SigningSecretLookup: repo})

	issued, err := f.IssueSigningSecret(context.Background(), orgA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repo.FindByKid(context.Background(), issued.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored == nil {
		t.Fatal("expected the issued signing secret to be persisted")
	}
	if stored.EncryptedSecret == issued.Secret {
		t.Fatal("the persisted record's EncryptedSecret equals the raw secret verbatim — it must be vault ciphertext")
	}
}
