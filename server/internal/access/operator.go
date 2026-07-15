package access

import (
	"strings"
	"time"
)

// OperatorID is minted only by OperatorFacade.Bootstrap (and, from Slice 4
// on, CreateOperator) — "op_<cuid2>" (PD58).
type OperatorID string

// OperatorStatus is an operator account's login eligibility (PD57: flat
// authz — status is the only access dimension this sub-phase has).
type OperatorStatus string

const (
	OperatorStatusActive   OperatorStatus = "ACTIVE"
	OperatorStatusDisabled OperatorStatus = "DISABLED"
)

// Operator is the persisted record of one console operator account
// (PD49/PD58): installation-level, not org-scoped — an operator administers
// the whole installation, like the admin key it replaces. PasswordHash is
// always an Argon2id PHC string (operator_password.go); the plaintext
// password is never stored, logged, or returned. FailedAttempts and
// LockedUntil (migration 0022, Slice 5) back the per-account brute-force
// lockout (FD-G): FailedAttempts counts consecutive wrong-password guesses
// since the last successful login or lockout reset; LockedUntil, once set,
// is the moment the account becomes loggable-into again.
type Operator struct {
	ID             OperatorID
	Email          string
	PasswordHash   string
	Status         OperatorStatus
	FailedAttempts int
	LockedUntil    *time.Time
	CreatedAt      time.Time
}

// IsActive reports whether the operator is currently allowed to log in.
func (o Operator) IsActive() bool {
	return o.Status == OperatorStatusActive
}

// IsLockedOut reports whether the account's brute-force lockout (Slice 5,
// FD-G) is still in effect at now — the injected clock, never wall-clock
// (the Slice 4 flaky-test lesson: lockout expiry must be comparable against
// whatever time a test pins).
func (o Operator) IsLockedOut(now time.Time) bool {
	return o.LockedUntil != nil && now.Before(*o.LockedUntil)
}

// normalizeEmail lowercases and trims email for case-insensitive lookup —
// used on the login path, which must never reveal (via a different error
// path or timing) whether a malformed/unknown email belongs to a real
// account (Login's own decoy-hash comparison already handles the "unknown"
// case uniformly).
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeAndValidateEmail is normalizeEmail plus a basic shape check,
// used at account-creation time (Bootstrap, and CreateOperator from Slice
// 4): a malformed address is rejected up front rather than silently stored.
func normalizeAndValidateEmail(email string) (string, error) {
	normalized := normalizeEmail(email)
	if !looksLikeEmail(normalized) {
		return "", ErrInvalidEmail()
	}
	return normalized, nil
}

// looksLikeEmail is a deliberately basic shape check (RFC 5322-complete
// validation is YAGNI here) — exactly one "@", a non-empty local part with
// no further "@", and a domain part containing at least one ".".
func looksLikeEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	local, domain := email[:at], email[at+1:]
	return !strings.Contains(local, "@") && strings.Contains(domain, ".")
}
