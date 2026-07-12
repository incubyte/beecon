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

// ErrValidation is the shared PD5 validation_failed shape for the catalog
// module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}
