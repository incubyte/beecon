// Package httpx holds the shared HTTP glue every driving adapter uses: the
// PD5 error envelope, the DomainError type domain code returns, and JSON
// decode helpers.
package httpx

import "net/http"

// DomainError is the error type facades return for expected failures; the
// ErrorRenderer maps it to the PD5 envelope. Errors that are not
// DomainErrors default to 500 internal_error.
type DomainError struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

// Error implements the error interface.
func (e *DomainError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// New constructs a DomainError. Code is the machine-readable string the
// client switches on; message is the human-readable text.
func New(status int, code, message string) *DomainError {
	return &DomainError{Status: status, Code: code, Message: message}
}

// WithDetails returns a copy of e with details set.
func (e *DomainError) WithDetails(d map[string]any) *DomainError {
	if e == nil {
		return nil
	}
	clone := *e
	clone.Details = d
	return &clone
}

// Unauthorized is the PD5 shape for a missing/wrong/revoked auth key.
func Unauthorized(message string) *DomainError {
	return New(http.StatusUnauthorized, "unauthorized", message)
}
