// Package vault is Beecon's shared AES-256-GCM encryption primitive (PD12):
// a leaf infrastructure package, like internal/httpx or internal/idgen, that
// imports no domain module and may be imported by any of them. It started
// inside connections/ (Phase 1, tokens only); Phase 2 adds a second and
// third consumer — catalog (Integration client secrets, PD17) and, later,
// access (user-token signing secrets) — which is what justified lifting it
// out to shared infra (the "rule of three" for extraction, applied to a
// module boundary rather than a function).
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Vault encrypts and decrypts secrets for storage. It is the only place a
// raw secret value (an OAuth token, an Integration's client secret, a
// signing secret) is ever held outside the moment it is minted or used —
// every other layer (domain structs, DTOs, logs) only ever sees the
// ciphertext Encrypt returns.
type Vault struct {
	aead cipher.AEAD
}

// NewVault builds a Vault from a 32-byte AES-256 key. Callers get the key
// via config.DecodeEncryptionKey, which already validates its length and
// encoding; NewVault re-checks the length so it can never be constructed
// with a key AES-256-GCM can't use.
func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("vault key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build AES-GCM: %w", err)
	}
	return &Vault{aead: aead}, nil
}

// Encrypt seals plaintext with a fresh random nonce and returns the
// base64-encoded (nonce || ciphertext), ready to persist.
func (v *Vault) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := v.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt: it decodes and opens ciphertext, returning the
// original plaintext, or an error if ciphertext is malformed or was sealed
// under a different key.
func (v *Vault) Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	nonceSize := v.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("ciphertext shorter than nonce size")
	}
	nonce, sealed := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := v.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("open ciphertext: %w", err)
	}
	return string(plaintext), nil
}
