package access

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"beecon/internal/organizations"
)

// webhookSecretEntropyBytes is the amount of random data behind a minted
// webhook signing secret (PD27): 32 random bytes, the same entropy budget
// as every other issued secret in this module.
const webhookSecretEntropyBytes = 32

// WebhookSecretPrefix marks an issued webhook signing secret recognizable
// in config and logs — the Standard Webhooks convention PD27 chose over
// Beecon's own beecon_sk_ scheme.
const WebhookSecretPrefix = "whsec_"

// WebhookSecretDisplayPrefixLength is how many characters of a minted
// webhook secret GetEndpoint shows so an org recognizes which secret is
// live — cosmetic only, mirroring SigningSecretDisplayPrefixLength; never
// used to find the secret again (ActiveWebhookSecrets reads every
// non-expired secret for the org directly).
const WebhookSecretDisplayPrefixLength = 12

// generateWebhookSecret mints a fresh "whsec_<random>" secret (PD27: 32
// random bytes, base64 encoded) using crypto/rand.
func generateWebhookSecret() (string, error) {
	buf := make([]byte, webhookSecretEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return WebhookSecretPrefix + base64.StdEncoding.EncodeToString(buf), nil
}

// webhookSecretDisplayPrefix returns the cosmetic, org-facing prefix of a
// freshly minted webhook secret.
func webhookSecretDisplayPrefix(secret string) string {
	if len(secret) < WebhookSecretDisplayPrefixLength {
		return secret
	}
	return secret[:WebhookSecretDisplayPrefixLength]
}

// IssuedWebhookSecret is IssueWebhookSecret's result: the only place the
// raw webhook secret is ever available (PD31). Callers must return it to
// the consumer immediately, at creation — from then on only its vault
// ciphertext is ever persisted.
type IssuedWebhookSecret struct {
	ID        WebhookSecretID
	Secret    string
	Prefix    string
	CreatedAt time.Time
}

// IssueWebhookSecret mints org's first webhook signing secret (PD27/PD31)
// and returns the full secret exactly once.
func (f *Facade) IssueWebhookSecret(ctx context.Context, org organizations.OrgID) (IssuedWebhookSecret, error) {
	secret, err := generateWebhookSecret()
	if err != nil {
		return IssuedWebhookSecret{}, err
	}
	encrypted, err := f.vault.Encrypt(secret)
	if err != nil {
		return IssuedWebhookSecret{}, err
	}
	record := WebhookSigningSecret{
		ID:              WebhookSecretID(f.newWebhookSecretID()),
		OrgID:           org,
		DisplayPrefix:   webhookSecretDisplayPrefix(secret),
		EncryptedSecret: encrypted,
		CreatedAt:       f.now(),
	}
	if err := f.webhookSecrets.Save(ctx, record); err != nil {
		return IssuedWebhookSecret{}, err
	}
	return IssuedWebhookSecret{
		ID:        record.ID,
		Secret:    secret,
		Prefix:    record.DisplayPrefix,
		CreatedAt: record.CreatedAt,
	}, nil
}

// RotateWebhookSecretResult is RotateWebhookSecret's result: the new
// secret, returned exactly once, plus when the outgoing secret's overlap
// window ends (PD31, mirroring PD23's RotateResult).
type RotateWebhookSecretResult struct {
	ID               WebhookSecretID
	Secret           string
	Prefix           string
	OverlapExpiresAt time.Time
}

// RotateWebhookSecret mints a fresh webhook signing secret for org — it
// authenticates deliveries immediately — and schedules every currently live
// secret's expiry for the end of an overlap window (default
// DefaultOverlapHours, or overlapHours when given — PD31 mirrors PD23
// verbatim). At most two secrets are ever live at once, the same guarantee
// expireLiveSecrets gives ApiKeySecret rotation. A negative overlapHours is
// rejected as invalid.
func (f *Facade) RotateWebhookSecret(ctx context.Context, org organizations.OrgID, overlapHours *int) (RotateWebhookSecretResult, error) {
	if overlapHours != nil && *overlapHours < 0 {
		return RotateWebhookSecretResult{}, ErrValidation("overlapHours", "must not be negative")
	}
	secrets, err := f.webhookSecrets.ListByOrg(ctx, org)
	if err != nil {
		return RotateWebhookSecretResult{}, err
	}

	now := f.now()
	expiresAt := now.Add(time.Duration(overlapHoursOrDefault(overlapHours)) * time.Hour)
	if err := f.expireLiveWebhookSecrets(ctx, org, secrets, now, expiresAt); err != nil {
		return RotateWebhookSecretResult{}, err
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		return RotateWebhookSecretResult{}, err
	}
	encrypted, err := f.vault.Encrypt(secret)
	if err != nil {
		return RotateWebhookSecretResult{}, err
	}
	fresh := WebhookSigningSecret{
		ID:              WebhookSecretID(f.newWebhookSecretID()),
		OrgID:           org,
		DisplayPrefix:   webhookSecretDisplayPrefix(secret),
		EncryptedSecret: encrypted,
		CreatedAt:       now,
	}
	if err := f.webhookSecrets.Save(ctx, fresh); err != nil {
		return RotateWebhookSecretResult{}, err
	}

	return RotateWebhookSecretResult{
		ID:               fresh.ID,
		Secret:           secret,
		Prefix:           fresh.DisplayPrefix,
		OverlapExpiresAt: expiresAt,
	}, nil
}

// expireLiveWebhookSecrets retires every secret in secrets that could still
// authenticate as of now, mirroring secret.go's expireLiveSecrets for
// ApiKeySecret: the currently active secret (ExpiresAt nil) gets the new
// overlap window's end, and any other secret still live from an earlier
// rotation is force-expired immediately, so at most two secrets are ever
// live at once no matter how quickly RotateWebhookSecret is called again.
func (f *Facade) expireLiveWebhookSecrets(ctx context.Context, org organizations.OrgID, secrets []WebhookSigningSecret, now, overlapExpiresAt time.Time) error {
	for _, s := range secrets {
		if s.ExpiresAt == nil {
			if err := f.webhookSecrets.MarkExpiring(ctx, org, s.ID, overlapExpiresAt); err != nil {
				return err
			}
			continue
		}
		if s.IsExpired(now) {
			continue
		}
		if err := f.webhookSecrets.MarkExpiring(ctx, org, s.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// ActiveWebhookSecrets returns org's currently active webhook signing
// secrets, decrypted (1-2 during a rotation's overlap window, PD31) —
// expired secrets are filtered out using the facade's own injected clock,
// so tests can travel time past an overlap window without a real sleep.
// delivery's signer (signing.go) signs every delivery attempt with each of
// these, space-joined, so a verifier holding either secret passes. An org
// with no webhook secret yet returns an empty slice.
func (f *Facade) ActiveWebhookSecrets(ctx context.Context, org organizations.OrgID) ([]string, error) {
	secrets, err := f.webhookSecrets.ListByOrg(ctx, org)
	if err != nil {
		return nil, err
	}
	now := f.now()
	active := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s.IsExpired(now) {
			continue
		}
		plaintext, err := f.vault.Decrypt(s.EncryptedSecret)
		if err != nil {
			return nil, err
		}
		active = append(active, plaintext)
	}
	return active, nil
}

// WebhookSecretPrefix returns the display prefix of org's currently active
// webhook signing secret — GetEndpoint's own "secretPrefix" field, the one
// IssueWebhookSecret or the most recent RotateWebhookSecret call left with
// ExpiresAt nil. An org with no webhook secret yet returns "".
func (f *Facade) WebhookSecretPrefix(ctx context.Context, org organizations.OrgID) (string, error) {
	secrets, err := f.webhookSecrets.ListByOrg(ctx, org)
	if err != nil {
		return "", err
	}
	active, ok := activeWebhookSecretOf(secrets)
	if !ok {
		return "", nil
	}
	return active.DisplayPrefix, nil
}

// activeWebhookSecretOf returns the member of secrets currently able to
// authenticate indefinitely — the one IssueWebhookSecret or the most
// recent RotateWebhookSecret call left with ExpiresAt nil — mirroring
// secret.go's activeSecretOf for ApiKeySecret.
func activeWebhookSecretOf(secrets []WebhookSigningSecret) (active WebhookSigningSecret, ok bool) {
	for _, s := range secrets {
		if s.ExpiresAt == nil {
			active, ok = s, true
		}
	}
	return active, ok
}
