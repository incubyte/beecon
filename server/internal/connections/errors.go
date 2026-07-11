package connections

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
)

// ErrNotFound is returned when no connection matches the requested id within
// the caller's organization. A connection belonging to another organization
// surfaces identically — no existence leak (PD5).
func ErrNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "connection not found")
}

// ErrRedirectURINotAllowed is returned when Initiate is asked to bind a
// connection attempt to a redirectUri not on the organization's allow-list
// (PD4). An empty allow-list always produces this error.
func ErrRedirectURINotAllowed() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "redirectUri", "issue": "not in organization's allowed redirect uris"})
}

// ErrValidation is the shared PD5 validation_failed shape for the
// connections module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}
