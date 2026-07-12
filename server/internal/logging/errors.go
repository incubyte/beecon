package logging

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const CodeValidationFailed = "validation_failed"

// ErrInvalidCursor is returned when Query is given a pagination cursor that
// is not valid base64, or does not decode to the created_at/id shape Query
// itself encodes (PD10).
func ErrInvalidCursor() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "cursor", "issue": "malformed pagination cursor"})
}

// ErrInvalidTimeRange is returned when Query is given a from/to value that
// does not parse as an RFC3339 timestamp.
func ErrInvalidTimeRange(field string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": "must be an RFC3339 timestamp"})
}

// ErrInvalidLimit is returned when Query is given a non-numeric limit value.
func ErrInvalidLimit() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "limit", "issue": "must be a positive integer"})
}
