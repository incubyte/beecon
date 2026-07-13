package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/access"
	"beecon/internal/vault"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// defaultTestVaultKey is a fixed 32-byte AES-256 key used when Overrides
// doesn't supply its own Vault — harmless for tests, since it never leaves
// the in-memory process.
var defaultTestVaultKey = []byte("access-test-vault-key-32-bytes!!")

// Overrides configures NewFacadeWithOverrides. Any zero-value field falls
// back to a deterministic in-memory default.
type Overrides struct {
	Repository          access.Repository
	PrefixLookup        access.PrefixLookup
	ApiKeySecrets       access.ApiKeySecrets
	SigningSecrets      access.SigningSecrets
	SigningSecretLookup access.SigningSecretLookup
	Vault               *vault.Vault
	NewID               func() string
	NewSecretID         func() string
	NewSigningSecretID  func() string
	Now                 func() time.Time
}

// NewFacadeWithOverrides builds an access.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids, a fixed
// clock, and a fixed-key Vault unless overridden.
func NewFacadeWithOverrides(o Overrides) *access.Facade {
	repository := o.Repository
	prefixLookup := o.PrefixLookup
	apiKeySecrets := o.ApiKeySecrets
	if repository == nil || prefixLookup == nil || apiKeySecrets == nil {
		shared := NewRepository()
		if repository == nil {
			repository = shared
		}
		if prefixLookup == nil {
			prefixLookup = shared
		}
		if apiKeySecrets == nil {
			apiKeySecrets = shared
		}
	}
	signingSecrets := o.SigningSecrets
	signingSecretLookup := o.SigningSecretLookup
	if signingSecrets == nil || signingSecretLookup == nil {
		shared := NewSigningSecretRepository()
		if signingSecrets == nil {
			signingSecrets = shared
		}
		if signingSecretLookup == nil {
			signingSecretLookup = shared
		}
	}
	secretVault := o.Vault
	if secretVault == nil {
		secretVault, _ = vault.NewVault(defaultTestVaultKey)
	}
	newID := o.NewID
	if newID == nil {
		newID = sequentialIDs("key_")
	}
	newSecretID := o.NewSecretID
	if newSecretID == nil {
		newSecretID = sequentialIDs("secret_")
	}
	newSigningSecretID := o.NewSigningSecretID
	if newSigningSecretID == nil {
		newSigningSecretID = sequentialIDs("usk_")
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}
	return access.NewFacade(repository, prefixLookup, apiKeySecrets, signingSecrets, signingSecretLookup, secretVault, newID, newSecretID, newSigningSecretID, now)
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
