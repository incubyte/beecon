package execution

import (
	"net/http"
	"strconv"
	"time"

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

// CodeFileNotFound is Execute's tool-level failure code when a file-typed
// argument names a file_ id that does not exist or belongs to another
// organization (PD22, Slice 7, AC5) — resolved, and reported, before the
// provider is ever called.
const CodeFileNotFound = "file_not_found"

// CodeNotFound is the execution module's own not-found code for its Files
// entity (AC2's download endpoint) — module-local, the same convention every
// other module already follows for its own CodeNotFound.
const CodeNotFound = "not_found"

// ErrFileNotFound is returned when DownloadFile is given a file id that does
// not exist or belongs to another organization (AC2) — no existence leak
// across organizations.
func ErrFileNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "file not found")
}

// CodeRateLimited is PD21's deliberate carve-out from the PD6 envelope
// (ADR-0009, AC3): a normalized upstream rate limit that survives every
// retry surfaces as an HTTP 429 with a Retry-After header, never as a
// tool-level Result.
const CodeRateLimited = "rate_limited"

// ErrRateLimited builds PD21's HTTP 429 carve-out (ADR-0009, AC3): the
// rate_limited PD5-shaped body plus a Retry-After header carrying retryAfter
// rounded up to whole seconds.
func ErrRateLimited(retryAfter time.Duration) *httpx.DomainError {
	return httpx.New(http.StatusTooManyRequests, CodeRateLimited, "upstream rate limit exceeded; retry after the given delay").
		WithHeader("Retry-After", strconv.Itoa(retryAfterSeconds(retryAfter)))
}

// retryAfterSeconds rounds retryAfter up to whole seconds for the Retry-After
// header — a zero or negative duration is reported as 1 second, since a
// consumer must never be told to retry with no delay at all.
func retryAfterSeconds(retryAfter time.Duration) int {
	seconds := int(retryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

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
