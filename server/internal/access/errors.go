package access

import (
	"fmt"
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound           = "not_found"
	CodeValidationFailed   = "validation_failed"
	CodeOperatorExists     = "operator_exists"
	CodeCSRF               = "csrf_failed"
	CodeEmailExists        = "email_exists"
	CodeOperatorNotFound   = "operator_not_found"
	CodeLastActiveOperator = "last_active_operator"
	CodeAccountLocked      = "account_locked"
)

// ErrNotFound is returned when no key matches the requested id within the
// caller's organization.
func ErrNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "api key not found")
}

// ErrValidation is the shared PD5 validation_failed shape for the access
// module's request-level checks (Slice 8: Rotate's optional overlapHours
// body).
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}

// ErrUnauthorized is returned when a presented secret is missing, malformed,
// unknown, or revoked (PD5) — the caller never learns which.
func ErrUnauthorized() *httpx.DomainError {
	return httpx.Unauthorized("invalid or revoked api key")
}

// ErrInvalidCredentials is Login's single generic failure (PD49 AC7): a
// wrong password and an unknown email render identically — the caller never
// learns which was wrong.
func ErrInvalidCredentials() *httpx.DomainError {
	return httpx.Unauthorized("invalid credentials")
}

// ErrSessionUnauthorized is returned when a presented session token is
// missing, malformed, unknown, revoked (Slice 2), or expired, or its
// operator is no longer ACTIVE — the caller never learns which.
func ErrSessionUnauthorized() *httpx.DomainError {
	return httpx.Unauthorized("invalid or expired session")
}

// ErrOperatorExists is Bootstrap's rejection once an operator account
// already exists (PD54): bootstrap is first-account-only.
func ErrOperatorExists() *httpx.DomainError {
	return httpx.New(http.StatusConflict, CodeOperatorExists, "an operator account already exists")
}

// ErrPasswordTooShort names the minimum length requirement a rejected
// password fell short of (PD49's own AC: "naming the requirement").
func ErrPasswordTooShort(minLength int) *httpx.DomainError {
	return ErrValidation("password", fmt.Sprintf("must be at least %d characters", minLength))
}

// ErrInvalidEmail is the shape-validation failure Bootstrap (and, from
// Slice 4, CreateOperator) returns for a malformed email address.
func ErrInvalidEmail() *httpx.DomainError {
	return ErrValidation("email", "must be a valid email address")
}

// ErrCSRF is authmw.ConsoleAuth/OperatorSession's rejection for a
// session-authenticated mutating request whose X-CSRF-Token header is
// missing or does not match the session's own CSRF token (PD52, Slice 3).
// The message is deliberately generic — it must NEVER echo the expected
// token value, or reveal anything else about it, back to the caller.
func ErrCSRF() *httpx.DomainError {
	return httpx.New(http.StatusForbidden, CodeCSRF, "csrf token invalid or missing")
}

// ErrEmailAlreadyExists is CreateOperator's rejection when another operator
// already holds the (normalized) email presented (Slice 4): distinct from
// ErrOperatorExists, which is Bootstrap's own "an operator account already
// exists at all" 409 — this one names the actual conflict, one email address
// already in use.
func ErrEmailAlreadyExists() *httpx.DomainError {
	return httpx.New(http.StatusConflict, CodeEmailExists, "an operator account with this email already exists")
}

// ErrOperatorNotFound is returned when a targeted operator id (Deactivate,
// the break-glass ResetPassword) matches no operator account.
func ErrOperatorNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeOperatorNotFound, "operator not found")
}

// ErrLastActiveOperator is Deactivate's rejection when the target is the
// installation's only remaining ACTIVE operator (Slice 4's total-lock-out
// guard): deactivating it would leave nobody able to log in, so it is
// refused outright.
func ErrLastActiveOperator() *httpx.DomainError {
	return httpx.New(http.StatusConflict, CodeLastActiveOperator, "cannot deactivate the last active operator")
}

// ErrAccountLocked is Login's rejection while an account's brute-force
// lockout (Slice 5, FD-G) is in effect: a 429, deliberately as generic as
// ErrInvalidCredentials' 401 — it never confirms the email exists, only that
// *if* an account is behind it, further guesses are throttled right now. The
// 429-vs-401 split between this and ErrInvalidCredentials is a mild,
// accepted enumeration oracle (an attacker who already suspects an email is
// registered can distinguish "wrong password, keep guessing" from "locked,
// stop for now"); closing it fully would need a per-IP throttle applied
// uniformly regardless of account existence, which is deferred (architecture
// §9 evolution triggers) — not an operator-auth-sub-phase requirement.
func ErrAccountLocked() *httpx.DomainError {
	return httpx.New(http.StatusTooManyRequests, CodeAccountLocked, "too many failed attempts, try again later")
}
