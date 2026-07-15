// White-box (package access) tests for operator_password.go's unexported
// crypto helpers (PD49/PD50): the security-critical invariant this file
// exists to prove is that a password is never recoverable from what
// hashPassword produces — only an Argon2id PHC string, which verifyPassword
// can check a candidate password against but never invert.
package access

import (
	"strings"
	"testing"
)

const testPassword = "correct horse battery staple 42"

func TestHashPassword_NeverContainsThePlaintextPassword(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(hash, testPassword) {
		t.Fatalf("hashPassword(%q) = %q, which contains the plaintext password — Argon2id output must never leak it", testPassword, hash)
	}
}

// TestHashPassword_ProducesAnArgon2idPHCString pins the self-describing PHC
// shape (PD50's own "self-describing -> future rehash-on-login" rationale):
// $argon2id$v=<version>$m=<mem>,t=<time>,p=<par>$<salt>$<hash>, carrying this
// package's own fixed cost parameters — not a hardcoded hash (the salt is
// random per call), just the shape and the params.
func TestHashPassword_ProducesAnArgon2idPHCString(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want it to start with %q", hash, "$argon2id$")
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("hash has %d $-delimited fields, want 6 ($, argon2id, v=.., m=..,t=..,p=.., salt, hash): %q", len(parts), hash)
	}
	wantParams := "m=19456,t=2,p=1"
	if parts[3] != wantParams {
		t.Errorf("params field = %q, want %q (PD50's fixed OWASP Argon2id cost constants)", parts[3], wantParams)
	}
}

func TestHashPassword_UsesAFreshRandomSaltEachCall(t *testing.T) {
	first, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first == second {
		t.Fatal("hashing the same password twice produced an identical PHC string — the salt is not varying per call")
	}
}

func TestVerifyPassword_TrueForTheExactPasswordThatWasHashed(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !verifyPassword(testPassword, hash) {
		t.Error("expected the password that produced hash to verify against it")
	}
}

func TestVerifyPassword_FalseForAWrongPassword(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if verifyPassword("a-completely-different-password", hash) {
		t.Error("expected a wrong password not to verify against another password's hash")
	}
}

func TestVerifyPassword_FalseForAnUnparseablePHCString(t *testing.T) {
	if verifyPassword(testPassword, "not-a-phc-string-at-all") {
		t.Error("expected an unparseable stored hash to be treated as a non-match, not an error/crash")
	}
}

func TestVerifyPassword_FalseForAPHCStringWithTheWrongFieldCount(t *testing.T) {
	if verifyPassword(testPassword, "$argon2id$v=19$missing-fields") {
		t.Error("expected a malformed (wrong-field-count) PHC string to be treated as a non-match")
	}
}

// --- decoyPasswordHash (PD50's enumeration-timing defense): Login compares
// an unknown email's attempt against this fixed hash so the response takes
// the same Argon2id-bound time whether or not the email exists. These tests
// pin its shape and its "never a real match" property, not its timing (which
// can't be asserted deterministically in a unit test — the crucial_path
// journey covers the observable, response-identical part of that defense).

func TestDecoyPasswordHash_IsAWellFormedArgon2idPHCString(t *testing.T) {
	if !strings.HasPrefix(decoyPasswordHash, "$argon2id$") {
		t.Fatalf("decoyPasswordHash = %q, want it to start with %q so verifyPassword actually performs a full Argon2id computation against it", decoyPasswordHash, "$argon2id$")
	}
}

func TestDecoyPasswordHash_NeverMatchesAnAttemptedPassword(t *testing.T) {
	attempts := []string{testPassword, "password", "", "the-decoy's-own-fixed-plaintext"}
	for _, attempt := range attempts {
		if verifyPassword(attempt, decoyPasswordHash) {
			t.Errorf("verifyPassword(%q, decoyPasswordHash) = true, want the decoy hash to never match any real attempt", attempt)
		}
	}
}
