package access

import (
	"context"
	"time"

	"beecon/internal/organizations"
)

// Repository is the access module's org-scoped driven port over the
// ServerApiKey identity itself: creation, listing, lookup by id, and
// revocation. Every method takes the owning OrgID as its second parameter,
// so a query without org scope cannot be expressed. The secrets that
// actually authenticate on a key's behalf live behind ApiKeySecrets (PD23,
// Slice 8) — a separate port, because Repository and ApiKeySecrets both need
// a method to persist a new row and Go has no method overloading between the
// two entity shapes (the same reason SigningSecrets is its own port below).
type Repository interface {
	SaveKey(ctx context.Context, key ServerApiKey) error
	ListByOrg(ctx context.Context, org organizations.OrgID) ([]ServerApiKey, error)
	FindByID(ctx context.Context, org organizations.OrgID, id KeyID) (*ServerApiKey, error)
	MarkRevoked(ctx context.Context, org organizations.OrgID, id KeyID, revokedAt time.Time) error
}

// ApiKeySecretCandidate is what PrefixLookup returns for a matching lookup
// prefix: enough of the parent key (its id, organization, and revocation
// state) alongside the specific secret version for Verify to pick the right
// one by hash and reject a revoked key's secret, or an expired one, without
// a second round trip.
type ApiKeySecretCandidate struct {
	KeyID     KeyID
	OrgID     organizations.OrgID
	RevokedAt *time.Time
	Secret    ApiKeySecret
}

// IsRevoked reports whether the candidate's parent key has been revoked.
func (c ApiKeySecretCandidate) IsRevoked() bool {
	return c.RevokedAt != nil
}

// PrefixLookup is deliberately installation-level, not org-scoped: Verify
// authenticates a presented secret before the caller's organization is
// known — the lookup prefix is how Verify discovers which secret (and so
// which key and organization) was presented in the first place. Because the
// lookup prefix carries only LookupPrefixLength characters, more than one
// secret may share a prefix, so this returns every candidate; Verify picks
// the one whose secret hash actually matches.
type PrefixLookup interface {
	FindByPrefix(ctx context.Context, prefix string) ([]ApiKeySecretCandidate, error)
}

// ApiKeySecrets is the access module's org-scoped driven port for the
// secrets behind a ServerApiKey (PD23, Slice 8). Issue and Rotate both write
// through Save; Rotate first reads the key's current secrets through
// ListByKeyID to find the one to retire, then schedules its expiry through
// MarkExpiring. Every method takes the owning OrgID even though a secret is
// identified by the key it belongs to (not an organization directly),
// mirroring Repository's own org-scoping rule.
type ApiKeySecrets interface {
	Save(ctx context.Context, org organizations.OrgID, secret ApiKeySecret) error
	ListByKeyID(ctx context.Context, org organizations.OrgID, keyID KeyID) ([]ApiKeySecret, error)
	MarkExpiring(ctx context.Context, org organizations.OrgID, id ApiKeySecretID, expiresAt time.Time) error
}

// SigningSecrets is the access module's org-scoped driven port for
// user-token signing secrets (PD20).
type SigningSecrets interface {
	Save(ctx context.Context, secret SigningSecret) error
	ListByOrg(ctx context.Context, org organizations.OrgID) ([]SigningSecret, error)
}

// SigningSecretLookup is deliberately installation-level, not org-scoped:
// VerifyUserToken authenticates a presented JWT before the caller's
// organization is known — the token's "kid" header is how VerifyUserToken
// discovers which signing secret (and so which organization) minted it,
// mirroring PrefixLookup's own pre-auth lookup shape. FindByKid returns
// (nil, nil) when no signing secret matches id.
type SigningSecretLookup interface {
	FindByKid(ctx context.Context, id SigningSecretID) (*SigningSecret, error)
}

// WebhookSecrets is the access module's org-scoped driven port for webhook
// endpoint signing secrets (PD27/PD31, Phase 3 Slice 3). Unlike
// SigningSecrets, a WebhookSigningSecret belongs directly to an
// organization (there is no intermediate "key" entity), but it rotates with
// an overlap window like ApiKeySecret does — so this port mirrors
// ApiKeySecrets' own shape (Save, a listing method, MarkExpiring) rather
// than SigningSecrets' simpler one.
type WebhookSecrets interface {
	Save(ctx context.Context, secret WebhookSigningSecret) error
	ListByOrg(ctx context.Context, org organizations.OrgID) ([]WebhookSigningSecret, error)
	MarkExpiring(ctx context.Context, org organizations.OrgID, id WebhookSecretID, expiresAt time.Time) error
}
