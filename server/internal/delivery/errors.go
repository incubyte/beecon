package delivery

import (
	"fmt"
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
)

// ErrNotFound is returned when no event matches the requested id within
// the caller's organization.
func ErrNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "event not found")
}

// ErrValidation is the shared PD5 validation_failed shape for the delivery
// module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}

// ErrInvalidCursor is returned when ListEvents is given a pagination
// cursor that is not valid base64, or does not decode to the shape
// ListEvents itself encodes.
func ErrInvalidCursor() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "cursor", "issue": "malformed pagination cursor"})
}

// ErrNoEndpoint is returned when SendTest or RotateSecret is asked to act
// on an organization with no webhook endpoint configured yet.
func ErrNoEndpoint() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "endpoint", "issue": "no webhook endpoint is configured"})
}

// ErrEndpointCap is returned when CreateEndpoint would push an org past
// BEECON_WEBHOOK_ENDPOINT_CAP endpoints (Slice 8, PD45) — the message
// itself names the configured cap, per the AC ("rejected with a validation
// error naming the cap").
func ErrEndpointCap(cap int) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "url", "issue": fmt.Sprintf("organization already has the maximum of %d webhook endpoints", cap)})
}
