package triggers

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
)

// ErrNotFound is returned when no trigger instance matches the requested id
// within the caller's organization. An instance belonging to another
// organization surfaces identically — no existence leak (PD33's "cross-org
// anything is not-found").
func ErrNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "trigger instance not found")
}

// ErrValidation is the shared PD5 validation_failed shape for the triggers
// module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}

// ErrInvalidCursor is returned when List is given a pagination cursor that is
// not valid base64, or does not decode to the created_at/id shape List
// itself encodes.
func ErrInvalidCursor() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "cursor", "issue": "malformed pagination cursor"})
}

// ErrInvalidConfig is returned when Create's config fails validation against
// the trigger definition's config schema (PD33): the schema library's own
// message becomes the issue, the same convention execution's tool-argument
// validation already established (validate.go), now shared via
// internal/schema. No instance is created.
func ErrInvalidConfig(cause error) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "config", "issue": cause.Error()})
}

// ErrConnectionNotActive is returned when Create is asked to bind a new
// instance to a connection whose status is not ACTIVE (PD33): the error
// names the connection's actual status so the consumer understands why
// creation was rejected, rather than a generic validation failure.
func ErrConnectionNotActive(status string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "connectionId", "issue": "connection is " + status})
}
