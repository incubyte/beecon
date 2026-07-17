package catalog

import (
	"fmt"
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound                 = "not_found"
	CodeValidationFailed         = "validation_failed"
	CodeRegistryUnavailable      = "registry_unavailable"
	CodeUnsupportedFormatVersion = "unsupported_format_version"
	CodeContentHashMismatch      = "content_hash_mismatch"
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

// ErrRegistryNotConfigured is returned when Activate is called on a facade
// with no RegistryClient wired (BEECON_REGISTRY_URL unset, PD59: a pinned
// installation runs fully offline by design) — a clear client-facing error,
// not a panic.
func ErrRegistryNotConfigured() *httpx.DomainError {
	return httpx.New(http.StatusServiceUnavailable, CodeRegistryUnavailable, "no registry is configured for this installation")
}

// ErrRegistryUnavailable is returned when the registry service cannot be
// reached at all, or answers with something other than success/not-found
// (the registryhttp adapter's own network/decode failures collapse to this
// one error so Activate's caller never depends on the adapter's shape;
// Slice 3/4 broaden this into a more specific registry-unavailable surface
// for diff/activation).
func ErrRegistryUnavailable() *httpx.DomainError {
	return httpx.New(http.StatusServiceUnavailable, CodeRegistryUnavailable, "the registry is unavailable")
}

// ErrBundleVersionNotFound is returned when the registry has no bundle at
// the requested provider/version.
func ErrBundleVersionNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "bundle version not found")
}

// ErrUnsupportedFormatVersion is returned when Activate pulls a bundle whose
// formatVersion this installation build does not support (PD66, Phase 5
// registry sub-phase Slice 4): ADR-0012 keeps the format at
// formatVersion: 1 for now, but an installation that has not yet been
// upgraded to a future format version must refuse to activate a bundle it
// cannot correctly interpret, leaving the previously active version fully
// in force.
func ErrUnsupportedFormatVersion(formatVersion int) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeUnsupportedFormatVersion, "bundle formatVersion is not supported by this installation build").
		WithDetails(map[string]any{"field": "formatVersion", "issue": fmt.Sprintf("got %d, this installation supports formatVersion 1", formatVersion)})
}

// ErrContentHashMismatch is returned when Activate recomputes a pulled
// bundle's content hash (registrybundle.ContentHash) and it does not match
// the hash the registry reported alongside it (PD67): a tampered or
// corrupted bundle is refused and activation aborts atomically — the
// previously active version stays fully in force, with no partial swap.
func ErrContentHashMismatch() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeContentHashMismatch, "bundle content hash does not match — refusing to activate a possibly corrupted or tampered bundle")
}
