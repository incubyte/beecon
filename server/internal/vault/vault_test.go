// vault_test.go covers AC10 (Phase 1) at the unit level: the Vault is the
// only place a raw secret value is ever held outside the moment it is minted
// or used — round-trip correctness, ciphertext non-determinism (fresh nonce
// per call), tamper rejection (GCM authentication failure), and key-length
// validation (NewVault re-checks what config.DecodeEncryptionKey already
// validated). Moved verbatim from connections/vault_test.go (Phase 2 Slice 2)
// when the vault was extracted to shared infra.
package vault_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"beecon/internal/vault"
)

func testVaultKey() []byte {
	return []byte("01234567890123456789012345678901") // 32 bytes
}

func TestVault_EncryptThenDecryptRoundTripsToTheOriginalPlaintext(t *testing.T) {
	v, err := vault.NewVault(testVaultKey())
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	const plaintext = "EwAoA...a-real-looking-microsoft-access-token"

	ciphertext, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := v.Decrypt(ciphertext)

	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Errorf("Decrypt(Encrypt(plaintext)) = %q, want %q", got, plaintext)
	}
}

// TestVault_CiphertextNeverContainsTheRawPlaintext is AC10's core defensive
// check: whatever Encrypt returns must not leak the token in the clear.
func TestVault_CiphertextNeverContainsTheRawPlaintext(t *testing.T) {
	v, err := vault.NewVault(testVaultKey())
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	const plaintext = "super-secret-access-token-value"

	ciphertext, err := v.Encrypt(plaintext)

	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(ciphertext, plaintext) {
		t.Fatalf("ciphertext %q contains the raw plaintext %q", ciphertext, plaintext)
	}
}

// TestVault_EncryptProducesDifferentCiphertextForTheSamePlaintextEachTime
// proves a fresh random nonce is used per call — encrypting the same token
// twice must not produce identical, comparable ciphertext.
func TestVault_EncryptProducesDifferentCiphertextForTheSamePlaintextEachTime(t *testing.T) {
	v, err := vault.NewVault(testVaultKey())
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	const plaintext = "same-token-encrypted-twice"

	first, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt (first): %v", err)
	}
	second, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt (second): %v", err)
	}

	if first == second {
		t.Error("encrypting the same plaintext twice produced identical ciphertext — nonce must be fresh per call")
	}
}

// TestVault_DecryptRejectsTamperedCiphertext is AC10's tamper-safety net:
// flipping a single byte of sealed ciphertext must fail GCM's authentication
// check, not silently return corrupted plaintext.
func TestVault_DecryptRejectsTamperedCiphertext(t *testing.T) {
	v, err := vault.NewVault(testVaultKey())
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	ciphertext, err := v.Encrypt("a-token-that-will-be-tampered-with")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tampered := flipLastByte(t, ciphertext)

	_, err = v.Decrypt(tampered)

	if err == nil {
		t.Fatal("expected Decrypt to reject tampered ciphertext, got nil error")
	}
}

// TestVault_DecryptRejectsCiphertextSealedUnderADifferentKey proves the vault
// key itself is load-bearing: a different key must not be able to open
// another vault's ciphertext.
func TestVault_DecryptRejectsCiphertextSealedUnderADifferentKey(t *testing.T) {
	vaultA, err := vault.NewVault(testVaultKey())
	if err != nil {
		t.Fatalf("NewVault (A): %v", err)
	}
	vaultB, err := vault.NewVault([]byte("98765432109876543210987654321098"))
	if err != nil {
		t.Fatalf("NewVault (B): %v", err)
	}
	ciphertext, err := vaultA.Encrypt("sealed-under-key-a")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = vaultB.Decrypt(ciphertext)

	if err == nil {
		t.Fatal("expected vault B to reject ciphertext sealed under vault A's key, got nil error")
	}
}

func TestVault_DecryptRejectsMalformedBase64(t *testing.T) {
	v, err := vault.NewVault(testVaultKey())
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}

	_, err = v.Decrypt("not-valid-base64!!!")

	if err == nil {
		t.Fatal("expected Decrypt to reject malformed base64, got nil error")
	}
}

func TestNewVault_RejectsAKeyShorterThan32Bytes(t *testing.T) {
	_, err := vault.NewVault([]byte("too-short"))

	if err == nil {
		t.Fatal("expected NewVault to reject a key shorter than 32 bytes, got nil error")
	}
}

func TestNewVault_RejectsAKeyLongerThan32Bytes(t *testing.T) {
	_, err := vault.NewVault([]byte("012345678901234567890123456789012345"))

	if err == nil {
		t.Fatal("expected NewVault to reject a key longer than 32 bytes, got nil error")
	}
}

// flipLastByte decodes ciphertext, flips the last byte (inside the sealed
// portion, never the nonce prefix), and re-encodes — a minimal, deterministic
// tamper.
func flipLastByte(t *testing.T, ciphertext string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decode test ciphertext: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("ciphertext decoded to zero bytes")
	}
	raw[len(raw)-1] ^= 0xFF
	return base64.StdEncoding.EncodeToString(raw)
}
