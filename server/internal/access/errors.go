package access

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
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
