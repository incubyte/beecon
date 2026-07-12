package execution

import (
	"net/http"

	"beecon/internal/httpx"
)

// Tool-level failure codes (PD6): these live inside a successful HTTP 200
// Result, never as an httpx.DomainError.
const (
	CodeInvalidArguments    = "invalid_arguments"
	CodeConnectionNotActive = "connection_not_active"
	CodeProviderError       = "provider_error"
	CodeProviderUnavailable = "provider_unavailable"
)

// CodeValidationFailed is the PD5 code for a malformed request body itself
// (not to be confused with an invalid tool argument, which is a tool-level
// failure, AC2).
const CodeValidationFailed = "validation_failed"

// ErrValidation is the shared PD5 validation_failed shape for the execution
// module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}
