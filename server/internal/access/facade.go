package access

import (
	"context"
	"time"

	"beecon/internal/organizations"
)

// Facade is the access module's only public surface.
type Facade struct {
	repo         Repository
	prefixLookup PrefixLookup
	newID        func() string
	now          func() time.Time
}

// NewFacade wires the facade with an injected id minter and clock so tests
// can supply deterministic ids and a fixed time.
func NewFacade(repo Repository, prefixLookup PrefixLookup, newID func() string, now func() time.Time) *Facade {
	return &Facade{repo: repo, prefixLookup: prefixLookup, newID: newID, now: now}
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

// Issue mints a new server API key for org and returns the full secret
// exactly once.
func (f *Facade) Issue(ctx context.Context, org organizations.OrgID) (IssuedKey, error) {
	secret, err := generateSecret()
	if err != nil {
		return IssuedKey{}, err
	}
	key := ServerApiKey{
		ID:           KeyID(f.newID()),
		OrgID:        org,
		LookupPrefix: lookupPrefix(secret),
		SecretHash:   hashSecretRemainder(secret),
		CreatedAt:    f.now(),
	}
	if err := f.repo.Save(ctx, key); err != nil {
		return IssuedKey{}, err
	}
	return IssuedKey{
		ID:        key.ID,
		Secret:    secret,
		Prefix:    key.LookupPrefix,
		CreatedAt: key.CreatedAt,
	}, nil
}

// List returns org's keys — id, prefix, and created date only; the full
// secret is never recoverable once Issue returns.
func (f *Facade) List(ctx context.Context, org organizations.OrgID) ([]ServerApiKey, error) {
	return f.repo.ListByOrg(ctx, org)
}

// Revoke marks a key belonging to org as revoked. A key belonging to
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

// Verify authenticates a presented secret and returns the organization it
// belongs to. A missing, malformed, unknown, or revoked secret is
// unauthorized (PD5) — the caller never learns which.
func (f *Facade) Verify(ctx context.Context, secret string) (organizations.OrgID, error) {
	if !hasSecretPrefix(secret) {
		return "", ErrUnauthorized()
	}
	candidates, err := f.prefixLookup.FindByPrefix(ctx, lookupPrefix(secret))
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if !secretMatchesHash(secret, candidate.SecretHash) {
			continue
		}
		if candidate.IsRevoked() {
			return "", ErrUnauthorized()
		}
		return candidate.OrgID, nil
	}
	return "", ErrUnauthorized()
}
