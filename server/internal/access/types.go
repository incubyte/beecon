// Package access owns the ServerApiKey entity: org-scoped API key issue,
// list, revoke, verification (PD3), and rotation with an overlap window
// (PD23, Slice 8). A key's secrets exist only at issue/rotation time — from
// then on only each secret's lookup prefix and hash are ever persisted or
// returned.
package access

import (
	"time"

	"beecon/internal/organizations"
)

// KeyID is minted only by Issue.
type KeyID string

// ServerApiKey is the persisted record of an issued key: its identity,
// creation time, and whether it has been revoked. The key itself never
// carries secret material — every secret that can currently authenticate on
// its behalf is a separate ApiKeySecret row, because a rotation keeps up to
// two live secrets at once (the fresh one and the outgoing one, during the
// overlap window).
type ServerApiKey struct {
	ID        KeyID
	OrgID     organizations.OrgID
	CreatedAt time.Time
	RevokedAt *time.Time
}

// IsRevoked reports whether the key has been revoked.
func (k ServerApiKey) IsRevoked() bool {
	return k.RevokedAt != nil
}

// ApiKeySecretID identifies one secret version behind a ServerApiKey. It is
// purely internal bookkeeping — never returned by any API response; only the
// key's own KeyID and the raw secret text (at issue/rotation time) ever
// leave the module.
type ApiKeySecretID string

// ApiKeySecret is one secret that can currently — or, during an overlap
// window, still — authenticate on behalf of its ServerApiKey (PD23). Issue
// creates the first with ExpiresAt nil; Rotate adds a second with ExpiresAt
// nil and schedules the first's ExpiresAt for the end of the overlap window,
// so at most two secrets are ever live per key.
type ApiKeySecret struct {
	ID           ApiKeySecretID
	KeyID        KeyID
	LookupPrefix string
	SecretHash   []byte
	CreatedAt    time.Time
	ExpiresAt    *time.Time
}

// IsExpired reports whether the secret's overlap window has passed as of
// now. A secret with no ExpiresAt (the currently active one) never expires.
func (s ApiKeySecret) IsExpired(now time.Time) bool {
	return s.ExpiresAt != nil && !now.Before(*s.ExpiresAt)
}

// KeyListing is List's per-key result: identity, creation time, and rotation
// state (RotatedAt/OverlapExpiresAt, Slice 8 AC5) derived from the key's
// secrets, plus the currently active secret's lookup prefix for admin
// recognition (PD3) — the same cosmetic field List has always shown. The
// full secret and its hash are never included.
type KeyListing struct {
	ID               KeyID
	Prefix           string
	CreatedAt        time.Time
	RevokedAt        *time.Time
	RotatedAt        *time.Time
	OverlapExpiresAt *time.Time
}

// SigningSecretID is minted only by IssueSigningSecret (PD20).
type SigningSecretID string

// SigningSecret is the persisted record of an issued user-token signing
// secret: never the raw secret, only its vault ciphertext (EncryptedSecret)
// and a display prefix for admin recognition. Unlike ServerApiKey's
// LookupPrefix, DisplayPrefix is never used to find this record — a user
// token's JWT header carries the SigningSecretID itself (as "kid"), so
// VerifyUserToken looks this record up by id, not by prefix.
type SigningSecret struct {
	ID              SigningSecretID
	OrgID           organizations.OrgID
	DisplayPrefix   string
	EncryptedSecret string
	CreatedAt       time.Time
}
