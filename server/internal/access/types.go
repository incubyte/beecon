// Package access owns the ServerApiKey entity: org-scoped API key issue,
// list, revoke, and verification (PD3). A key's full secret exists only at
// issue time — from then on only its lookup prefix and secret hash are ever
// persisted or returned.
package access

import (
	"time"

	"beecon/internal/organizations"
)

// KeyID is minted only by Issue.
type KeyID string

// ServerApiKey is the persisted record of an issued key: never the full
// secret, only what is needed to recognize (LookupPrefix) and verify
// (SecretHash) a presented one.
type ServerApiKey struct {
	ID           KeyID
	OrgID        organizations.OrgID
	LookupPrefix string
	SecretHash   []byte
	CreatedAt    time.Time
	RevokedAt    *time.Time
}

// IsRevoked reports whether the key has been revoked.
func (k ServerApiKey) IsRevoked() bool {
	return k.RevokedAt != nil
}
