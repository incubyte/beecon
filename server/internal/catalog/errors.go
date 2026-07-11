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
