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
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}
