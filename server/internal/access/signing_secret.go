package access

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"beecon/internal/organizations"
)

// signingSecretEntropyBytes is the amount of random data behind a minted
// user-token signing secret (PD20) — the same entropy budget as an issued
// server API key's secret (secret.go's secretEntropyBytes).
const signingSecretEntropyBytes = 32

// SigningSecretDisplayPrefixLength is how many characters of a minted
// signing secret ListSigningSecrets shows so an admin can recognize it —
// cosmetic only; unlike an API key's LookupPrefix, this is never used to
// find the secret again (VerifyUserToken looks up by the JWT's kid header,
// which is the SigningSecretID).
const SigningSecretDisplayPrefixLength = 8

// generateSigningSecret mints fresh random signing-secret bytes, base64
// encoded, using crypto/rand.
func generateSigningSecret() (string, error) {
	buf := make([]byte, signingSecretEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// signingSecretDisplayPrefix returns the cosmetic, admin-facing prefix of a
// freshly minted secret.
func signingSecretDisplayPrefix(secret string) string {
	if len(secret) < SigningSecretDisplayPrefixLength {
		return secret
	}
	return secret[:SigningSecretDisplayPrefixLength]
}

// IssuedSigningSecret is IssueSigningSecret's result: the only place the raw
// signing secret is ever available. Callers must show it to the admin
// immediately and never persist it themselves.
type IssuedSigningSecret struct {
	ID        SigningSecretID
	Secret    string
	Prefix    string
	CreatedAt time.Time
}

// IssueSigningSecret mints a new user-token signing secret for org (PD20)
// and returns the full secret exactly once; from then on only its vault
// ciphertext is ever persisted.
func (f *Facade) IssueSigningSecret(ctx context.Context, org organizations.OrgID) (IssuedSigningSecret, error) {
	secret, err := generateSigningSecret()
	if err != nil {
		return IssuedSigningSecret{}, err
	}
	encrypted, err := f.vault.Encrypt(secret)
	if err != nil {
		return IssuedSigningSecret{}, err
	}
	record := SigningSecret{
		ID:              SigningSecretID(f.newSigningSecretID()),
		OrgID:           org,
		DisplayPrefix:   signingSecretDisplayPrefix(secret),
		EncryptedSecret: encrypted,
		CreatedAt:       f.now(),
	}
	if err := f.signingSecrets.Save(ctx, record); err != nil {
		return IssuedSigningSecret{}, err
	}
	return IssuedSigningSecret{
		ID:        record.ID,
		Secret:    secret,
		Prefix:    record.DisplayPrefix,
		CreatedAt: record.CreatedAt,
	}, nil
}

// ListSigningSecrets returns org's signing secrets — id, display prefix, and
// created date only (PD20); the raw secret is never recoverable once
// IssueSigningSecret returns.
func (f *Facade) ListSigningSecrets(ctx context.Context, org organizations.OrgID) ([]SigningSecret, error) {
	return f.signingSecrets.ListByOrg(ctx, org)
}
