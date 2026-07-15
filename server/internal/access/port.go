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
// prefix: enough of the parent key (its id, organization, scope — PD41 — and
// revocation state) alongside the specific secret version for Verify to pick
// the right one by hash, reject a revoked key's secret or an expired one,
// and return the key's scope alongside its organization, without a second
// round trip.
type ApiKeySecretCandidate struct {
	KeyID     KeyID
	OrgID     organizations.OrgID
	Scope     Scope
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

// Operators is the access module's installation-level driven port over
// operator accounts (PD49/PD58): deliberately not org-scoped — an operator
// administers the whole installation, the same reasoning
// PrefixLookup/SigningSecretLookup are already whitelisted under (§8 of the
// architecture doc; the orgscope arch test's whitelist gains this port).
// FindByEmail takes email pre-lowercased (normalizeEmail) — case-insensitive
// uniqueness is enforced by always looking up (and storing) the normalized
// form, never by the port doing its own case-folding. ListAll, UpdatePasswordHash,
// SetStatus, and CountActive are Slice 4 additions: ListAll backs
// ListOperators (never returns PasswordHash to a caller outside this
// package); UpdatePasswordHash backs ChangeMyPassword and the break-glass
// ResetPassword; SetStatus backs Deactivate (and ResetPassword's own
// reactivation); CountActive backs Deactivate's last-active-operator guard
// (never allow the count of ACTIVE operators to reach zero). RecordFailedAttempt
// and ResetFailedAttempts are Slice 5 additions backing the per-account
// brute-force lockout (FD-G): RecordFailedAttempt always increments the
// stored FailedAttempts counter by one and, only when lockedUntil is
// non-nil, also sets LockedUntil to it — the facade computes lockedUntil
// from the count it already holds, so this method never reads the counter
// back to decide; ResetFailedAttempts zeroes the counter and clears
// LockedUntil on a successful login. Both are idempotent no-ops for an
// unknown id (the facade has already confirmed the operator exists before
// calling either).
type Operators interface {
	Save(ctx context.Context, operator Operator) error
	FindByEmail(ctx context.Context, email string) (*Operator, error)
	FindByID(ctx context.Context, id OperatorID) (*Operator, error)
	Exists(ctx context.Context) (bool, error)
	ListAll(ctx context.Context) ([]Operator, error)
	UpdatePasswordHash(ctx context.Context, id OperatorID, passwordHash string) error
	SetStatus(ctx context.Context, id OperatorID, status OperatorStatus) error
	CountActive(ctx context.Context) (int, error)
	RecordFailedAttempt(ctx context.Context, id OperatorID, lockedUntil *time.Time) error
	ResetFailedAttempts(ctx context.Context, id OperatorID) error
}

// OperatorSessions is the access module's installation-level driven port
// over operator sessions (PD51) — the same installation-level reasoning as
// Operators above: a session belongs to one operator, not one organization.
// Revoke ends exactly one session (Slice 2's Logout); RevokeAllForOperator
// ends every one of an operator's sessions at once (Slice 4's deactivate and
// break-glass reset-password paths). RevokeAllForOperatorExcept is Slice 4's
// password-change semantics (spec Slice 2 AC4, carried forward): every
// session belonging to operatorID is revoked EXCEPT exceptSessionID — the
// acting session (the one that just changed its own password) stays alive.
// All three are idempotent: revoking an already-revoked session, or an
// operator with no sessions at all, is a no-op, never an error.
type OperatorSessions interface {
	Save(ctx context.Context, session OperatorSession) error
	FindByTokenHash(ctx context.Context, tokenHash []byte) (*OperatorSession, error)
	Revoke(ctx context.Context, id OperatorSessionID, at time.Time) error
	RevokeAllForOperator(ctx context.Context, operatorID OperatorID, at time.Time) error
	RevokeAllForOperatorExcept(ctx context.Context, operatorID OperatorID, exceptSessionID OperatorSessionID, at time.Time) error
}

// WebhookSecrets is the access module's org-scoped driven port for webhook
// endpoint signing secrets (PD27/PD31/PD45, Phase 3 Slice 3, Phase 4 Slice
// 8). Unlike SigningSecrets, a WebhookSigningSecret belongs directly to an
// organization (there is no intermediate "key" entity), but it rotates with
// an overlap window like ApiKeySecret does — so this port mirrors
// ApiKeySecrets' own shape (Save, a listing method, MarkExpiring) rather
// than SigningSecrets' simpler one. ListByEndpoint narrows to one specific
// endpoint's own secrets (Slice 8: many endpoints per org, each with its
// own secret lineage) rather than every secret ever issued for the org.
type WebhookSecrets interface {
	Save(ctx context.Context, secret WebhookSigningSecret) error
	ListByEndpoint(ctx context.Context, org organizations.OrgID, endpoint EndpointID) ([]WebhookSigningSecret, error)
	MarkExpiring(ctx context.Context, org organizations.OrgID, id WebhookSecretID, expiresAt time.Time) error
}
