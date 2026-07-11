package organizations

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
)

// ErrNotFound is returned when no organization matches the requested id.
// Callers never distinguish "does not exist" from "belongs to another
// installation admin scope" — there is no cross-org leak to guard here since
// organizations are installation-level, but the same code is reused by every
// other module for that purpose.
func ErrNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "organization not found")
}

// ErrInvalidName is returned when the organization name fails validation.
func ErrInvalidName(field, issue string) *httpx.DomainError {
	return ErrValidation(field, issue)
}

// ErrValidation is the shared PD5 validation_failed shape for the
// organizations module's request-level checks (invalid name, malformed
// allowedRedirectUris body, ...).
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}

// ErrUserNotFound is returned when no user matches the requested id within
// the caller's organization. A user that belongs to another organization
// surfaces identically — no existence leak (PD5).
func ErrUserNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "user not found")
}
