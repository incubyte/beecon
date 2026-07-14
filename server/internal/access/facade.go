package access

import (
	"context"
	"time"

	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// Facade is the access module's only public surface.
type Facade struct {
	repo                Repository
	prefixLookup        PrefixLookup
	apiKeySecrets       ApiKeySecrets
	signingSecrets      SigningSecrets
	signingSecretLookup SigningSecretLookup
	webhookSecrets      WebhookSecrets
	vault               *vault.Vault
	newID               func() string
	newSecretID         func() string
	newSigningSecretID  func() string
	newWebhookSecretID  func() string
	now                 func() time.Time
}

// NewFacade wires the facade with the org-scoped API-key ports (the key
// identity itself, its secrets — PD23, Slice 8 — and the pre-auth,
// installation-level PrefixLookup), the user-token signing-secret ports
// (org-scoped SigningSecrets plus the pre-auth, installation-level
// SigningSecretLookup — PD20), the org-scoped webhook-secret port
// (WebhookSecrets — PD27/PD31, Phase 3 Slice 3), the shared vault a
// signing/webhook secret is encrypted under, and injected id minters and a
// clock so tests can supply deterministic ids and a fixed time.
func NewFacade(
	repo Repository,
	prefixLookup PrefixLookup,
	apiKeySecrets ApiKeySecrets,
	signingSecrets SigningSecrets,
	signingSecretLookup SigningSecretLookup,
	webhookSecrets WebhookSecrets,
	secretVault *vault.Vault,
	newID func() string,
	newSecretID func() string,
	newSigningSecretID func() string,
	newWebhookSecretID func() string,
	now func() time.Time,
) *Facade {
	return &Facade{
		repo:                repo,
		prefixLookup:        prefixLookup,
		apiKeySecrets:       apiKeySecrets,
		signingSecrets:      signingSecrets,
		signingSecretLookup: signingSecretLookup,
		webhookSecrets:      webhookSecrets,
		vault:               secretVault,
		newID:               newID,
		newSecretID:         newSecretID,
		newSigningSecretID:  newSigningSecretID,
		newWebhookSecretID:  newWebhookSecretID,
		now:                 now,
	}
}

// IssuedKey is Issue's result: the only place the full secret is ever
// available. Callers must show it to the admin immediately and never
// persist it themselves.
type IssuedKey struct {
	ID        KeyID
	Secret    string
	Prefix    string
	CreatedAt time.Time
}

// Issue mints a new server API key for org and its first secret, returning
// the full secret exactly once.
func (f *Facade) Issue(ctx context.Context, org organizations.OrgID) (IssuedKey, error) {
	secret, err := generateSecret()
	if err != nil {
		return IssuedKey{}, err
	}
	key := ServerApiKey{
		ID:        KeyID(f.newID()),
		OrgID:     org,
		CreatedAt: f.now(),
	}
	if err := f.repo.SaveKey(ctx, key); err != nil {
		return IssuedKey{}, err
	}
	firstSecret := ApiKeySecret{
		ID:           ApiKeySecretID(f.newSecretID()),
		KeyID:        key.ID,
		LookupPrefix: lookupPrefix(secret),
		SecretHash:   hashSecretRemainder(secret),
		CreatedAt:    key.CreatedAt,
	}
	if err := f.apiKeySecrets.Save(ctx, org, firstSecret); err != nil {
		return IssuedKey{}, err
	}
	return IssuedKey{
		ID:        key.ID,
		Secret:    secret,
		Prefix:    firstSecret.LookupPrefix,
		CreatedAt: key.CreatedAt,
	}, nil
}

// List returns org's keys with their rotation state derived from their
// secrets (Slice 8, AC5) — never a secret itself.
func (f *Facade) List(ctx context.Context, org organizations.OrgID) ([]KeyListing, error) {
	keys, err := f.repo.ListByOrg(ctx, org)
	if err != nil {
		return nil, err
	}
	listings := make([]KeyListing, 0, len(keys))
	for _, key := range keys {
		secrets, err := f.apiKeySecrets.ListByKeyID(ctx, org, key.ID)
		if err != nil {
			return nil, err
		}
		listings = append(listings, keyListingFrom(key, secrets))
	}
	return listings, nil
}

func keyListingFrom(key ServerApiKey, secrets []ApiKeySecret) KeyListing {
	rotatedAt, overlapExpiresAt := rotationState(secrets)
	return KeyListing{
		ID:               key.ID,
		Prefix:           activeSecretPrefix(secrets),
		CreatedAt:        key.CreatedAt,
		RevokedAt:        key.RevokedAt,
		RotatedAt:        rotatedAt,
		OverlapExpiresAt: overlapExpiresAt,
	}
}

// Revoke marks a key belonging to org as revoked, immediately rejecting
// every secret ever issued for it (Slice 8, AC6) — Verify checks the parent
// key's revocation state, not each secret individually. A key belonging to
// another organization is not found (PD5).
func (f *Facade) Revoke(ctx context.Context, org organizations.OrgID, id KeyID) error {
	key, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return err
	}
	if key == nil {
		return ErrNotFound()
	}
	return f.repo.MarkRevoked(ctx, org, id, f.now())
}

// RotateResult is Rotate's result: the new secret, returned exactly once,
// plus when the outgoing secret's overlap window ends (PD23).
type RotateResult struct {
	ID               KeyID
	Secret           string
	Prefix           string
	OverlapExpiresAt time.Time
}

// Rotate mints a fresh secret for an existing key — authenticating
// immediately (Slice 8, AC4) — and schedules the currently active secret's
// expiry for the end of an overlap window (default DefaultOverlapHours, or
// overlapHours when given — PD23, AC2). The outgoing secret keeps
// authenticating until that expiry (AC2) and is rejected once it passes
// (AC3). Any other secret still live from an earlier rotation (its own
// overlap window not yet lapsed) is force-expired immediately, so at most
// two secrets are ever live at once no matter how quickly Rotate is called
// again (types.go's ApiKeySecret doc comment). A key belonging to another
// organization, an unknown id, or an already-revoked key is not-found
// (PD5) — a revoked key has nothing left to rotate into working order. A
// negative overlapHours is rejected as invalid.
func (f *Facade) Rotate(ctx context.Context, org organizations.OrgID, id KeyID, overlapHours *int) (RotateResult, error) {
	key, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return RotateResult{}, err
	}
	if key == nil || key.IsRevoked() {
		return RotateResult{}, ErrNotFound()
	}
	if overlapHours != nil && *overlapHours < 0 {
		return RotateResult{}, ErrValidation("overlapHours", "must not be negative")
	}
	secrets, err := f.apiKeySecrets.ListByKeyID(ctx, org, id)
	if err != nil {
		return RotateResult{}, err
	}

	now := f.now()
	expiresAt := now.Add(time.Duration(overlapHoursOrDefault(overlapHours)) * time.Hour)
	if err := f.expireLiveSecrets(ctx, org, secrets, now, expiresAt); err != nil {
		return RotateResult{}, err
	}

	secret, err := generateSecret()
	if err != nil {
		return RotateResult{}, err
	}
	fresh := ApiKeySecret{
		ID:           ApiKeySecretID(f.newSecretID()),
		KeyID:        id,
		LookupPrefix: lookupPrefix(secret),
		SecretHash:   hashSecretRemainder(secret),
		CreatedAt:    now,
	}
	if err := f.apiKeySecrets.Save(ctx, org, fresh); err != nil {
		return RotateResult{}, err
	}

	return RotateResult{
		ID:               id,
		Secret:           secret,
		Prefix:           fresh.LookupPrefix,
		OverlapExpiresAt: expiresAt,
	}, nil
}

// expireLiveSecrets retires every secret in secrets that could still
// authenticate as of now, enforcing "at most two live secrets per key" even
// across back-to-back rotations: the currently active secret (ExpiresAt
// nil) — the one this Rotate call is retiring — gets the new overlap
// window's end, and any other secret still live from an earlier rotation
// (its own expiry scheduled but not yet reached) is force-expired
// immediately rather than left to keep authenticating past this rotation.
func (f *Facade) expireLiveSecrets(ctx context.Context, org organizations.OrgID, secrets []ApiKeySecret, now, overlapExpiresAt time.Time) error {
	for _, s := range secrets {
		if s.ExpiresAt == nil {
			if err := f.apiKeySecrets.MarkExpiring(ctx, org, s.ID, overlapExpiresAt); err != nil {
				return err
			}
			continue
		}
		if s.IsExpired(now) {
			continue
		}
		if err := f.apiKeySecrets.MarkExpiring(ctx, org, s.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// Verify authenticates a presented secret and returns the organization it
// belongs to. A missing, malformed, unknown, revoked, or expired (PD23)
// secret is unauthorized (PD5) — the caller never learns which.
func (f *Facade) Verify(ctx context.Context, secret string) (organizations.OrgID, error) {
	if !hasSecretPrefix(secret) {
		return "", ErrUnauthorized()
	}
	candidates, err := f.prefixLookup.FindByPrefix(ctx, lookupPrefix(secret))
	if err != nil {
		return "", err
	}
	now := f.now()
	for _, candidate := range candidates {
		if !secretMatchesHash(secret, candidate.Secret.SecretHash) {
			continue
		}
		if candidate.IsRevoked() {
			return "", ErrUnauthorized()
		}
		if candidate.Secret.IsExpired(now) {
			return "", ErrUnauthorized()
		}
		return candidate.OrgID, nil
	}
	return "", ErrUnauthorized()
}
