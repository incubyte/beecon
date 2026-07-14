package catalog

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
)

// ErrIntegrationNotFound is returned when no integration matches the
// requested id.
func ErrIntegrationNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "integration not found")
}

// ErrToolNotFound is returned when FindToolBySlug is asked for a tool slug no
// loaded ProviderDefinition declares (AC3 of Slice 5: an unknown tool slug is
// a platform-level not-found, not a tool-level failure).
func ErrToolNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "tool not found")
}

// ErrUnknownProvider is returned when CreateIntegration is asked to create an
// integration for a providerSlug that names no loaded ProviderDefinition.
func ErrUnknownProvider(providerSlug string) *httpx.DomainError {
	return ErrValidation("providerSlug", "unknown provider "+providerSlug)
}

// ErrTriggerDefinitionNotFound is returned when TriggerDefinitionDetail is
// asked for a trigger slug no loaded ProviderDefinition declares (mirrors
// ErrToolNotFound's PD14 slug-addressing convention for triggers).
func ErrTriggerDefinitionNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "trigger definition not found")
}

// ErrProviderNotFound is returned when ListTools is asked to filter by a
// providerSlug that names no loaded ProviderDefinition — a not-found list
// target, not a request-validation error (unlike ErrUnknownProvider, which
// guards a create-time request body field).
func ErrProviderNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "provider not found")
}

// ErrInvalidCursor is returned when ListTools is given a pagination cursor
// that is not valid base64 (PD15's platform-wide cursor convention).
func ErrInvalidCursor() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "cursor", "issue": "malformed pagination cursor"})
}

// ErrValidation is the shared PD5 validation_failed shape for the catalog
// module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}
