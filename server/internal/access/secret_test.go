// White-box (package access) tests for secret.go's unexported helpers: the
// secret format, hashing determinism, and prefix extraction that Issue and
// Verify build on.
package access

import (
	"strings"
	"testing"
)

func TestGenerateSecret_HasTheBeeconSkPrefix(t *testing.T) {
	secret, err := generateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(secret, SecretPrefix) {
		t.Errorf("secret = %q, want it to start with %q", secret, SecretPrefix)
	}
}

func TestGenerateSecret_ProducesADifferentSecretOnEachCall(t *testing.T) {
	first, err := generateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := generateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first == second {
		t.Fatalf("two calls to generateSecret produced the same secret %q — random generation is not producing distinct entropy", first)
	}
}

func TestGenerateSecret_CarriesEnoughEntropyToFillTheLookupPrefixAndBeyond(t *testing.T) {
	secret, err := generateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// PD3: ~32 bytes of entropy after the prefix, base64-encoded, so a
	// well-formed secret is far longer than the 12-char lookup prefix alone.
	if len(secret) <= LookupPrefixLength+20 {
		t.Errorf("secret length = %d, want meaningfully longer than the %d-char lookup prefix (shape suggests insufficient entropy)", len(secret), LookupPrefixLength)
	}
}

func TestHasSecretPrefix_TrueForAWellFormedSecret(t *testing.T) {
	if !hasSecretPrefix("beecon_sk_abcdefghijklmnop") {
		t.Error("expected a secret carrying SecretPrefix and enough length to be recognized")
	}
}

func TestHasSecretPrefix_FalseWhenTheSecretPrefixIsMissing(t *testing.T) {
	if hasSecretPrefix("some-other-shaped-token-entirely") {
		t.Error("expected a secret without beecon_sk_ to be rejected")
	}
}

func TestHasSecretPrefix_FalseWhenShorterThanTheLookupPrefixLength(t *testing.T) {
	if hasSecretPrefix("beecon_sk") {
		t.Error("expected a secret shorter than LookupPrefixLength to be rejected even though it carries the literal SecretPrefix text")
	}
}

func TestLookupPrefix_ReturnsExactlyTheFirstTwelveCharacters(t *testing.T) {
	secret := "beecon_sk_AAAAAAAAtherestofthesecret"

	got := lookupPrefix(secret)

	want := "beecon_sk_AA"
	if got != want {
		t.Errorf("lookupPrefix(%q) = %q, want %q", secret, got, want)
	}
	if len(got) != LookupPrefixLength {
		t.Errorf("lookupPrefix returned %d chars, want %d (LookupPrefixLength)", len(got), LookupPrefixLength)
	}
}

func TestHashSecretRemainder_IsDeterministicForTheSameSecret(t *testing.T) {
	secret := "beecon_sk_AAAAAAsame-secret-value"

	first := hashSecretRemainder(secret)
	second := hashSecretRemainder(secret)

	if string(first) != string(second) {
		t.Errorf("hashing the same secret twice produced different hashes: %x vs %x", first, second)
	}
}

func TestHashSecretRemainder_DiffersForSecretsSharingAPrefixButNotTheRemainder(t *testing.T) {
	sharedPrefix := "beecon_sk_AA"

	first := hashSecretRemainder(sharedPrefix + "-remainder-one")
	second := hashSecretRemainder(sharedPrefix + "-remainder-two")

	if string(first) == string(second) {
		t.Fatal("two secrets sharing a lookup prefix but not their remainder hashed to the same value")
	}
}

func TestSecretMatchesHash_TrueWhenTheSecretHashesToTheStoredHash(t *testing.T) {
	secret := "beecon_sk_AAAAAAcorrect-secret"
	hash := hashSecretRemainder(secret)

	if !secretMatchesHash(secret, hash) {
		t.Error("expected the secret that produced hash to match it")
	}
}

func TestSecretMatchesHash_FalseWhenTheSecretDoesNotMatchTheStoredHash(t *testing.T) {
	hash := hashSecretRemainder("beecon_sk_AAAAAAcorrect-secret")

	if secretMatchesHash("beecon_sk_AAAAAAwrong-secret-value", hash) {
		t.Error("expected a different secret not to match the stored hash")
	}
}
