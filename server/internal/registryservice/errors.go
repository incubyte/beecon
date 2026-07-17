package registryservice

import (
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention, reused here — the registry
// service renders the same envelope shape the installation binary does).
const (
	CodeNotFound             = "not_found"
	CodeValidationFailed     = "validation_failed"
	CodeStrictParseFailed    = "strict_parse_failed"
	CodeMissingOutputSchema  = "missing_output_schema"
	CodeMissingSample        = "missing_sample"
	CodeOutputSchemaVsSample = "output_schema_vs_sample_mismatch"
	CodeVersionConflict      = "version_conflict"
	CodeIllegalSemverBump    = "illegal_semver_bump"
)

// ErrBundleVersionNotFound is returned when Pull is asked for a version the
// registry has never published for providerSlug.
func ErrBundleVersionNotFound(providerSlug, version string) *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "bundle version not found").
		WithDetails(map[string]any{"provider": providerSlug, "version": version})
}

// ErrValidation is the shared PD5 validation_failed shape for the registry
// service's own request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}

// ErrStrictParseFailed is Slice 2's strict-parse gate (PD63): the published
// bundle body does not parse under the same KnownFields strictness the
// installation's embedded-YAML loader applies — an unknown/misspelled field,
// or any other JSON decode failure.
func ErrStrictParseFailed(issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeStrictParseFailed, "bundle does not parse under the strict formatVersion: 1 loader").
		WithDetails(map[string]any{"issue": issue})
}

// ErrMissingOutputSchema is Slice 2's output-schema-vs-sample gate (PD63):
// toolSlug declares no output schema at all.
func ErrMissingOutputSchema(toolSlug string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeMissingOutputSchema, "tool has no declared output schema").
		WithDetails(map[string]any{"tool": toolSlug})
}

// ErrMissingSample is Slice 2's output-schema-vs-sample gate (PD63): toolSlug
// has no recorded sample response for its output schema to validate.
func ErrMissingSample(toolSlug string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeMissingSample, "tool has no recorded sample response").
		WithDetails(map[string]any{"tool": toolSlug})
}

// ErrOutputSchemaSampleMismatch is Slice 2's output-schema-vs-sample gate
// (PD63): toolSlug's declared output schema does not validate its own
// recorded sample response; issue is the field-naming validation message
// internal/schema's compiled schema produced.
func ErrOutputSchemaSampleMismatch(toolSlug, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeOutputSchemaVsSample, "tool's output schema does not validate its recorded sample response").
		WithDetails(map[string]any{"tool": toolSlug, "issue": issue})
}

// ErrVersionConflict is Slice 2's version-conflict gate (PD62): providerSlug
// has already published version — re-publishing it is rejected and changes
// nothing.
func ErrVersionConflict(providerSlug, version string) *httpx.DomainError {
	return httpx.New(http.StatusConflict, CodeVersionConflict, "version already published").
		WithDetails(map[string]any{"provider": providerSlug, "version": version})
}

// ErrIllegalSemverBump is Slice 2's bump-direction gate (PD62): the
// requested version is not strictly greater than the provider's current
// latest version, or its bump direction doesn't match what changed
// (additive requires at least a minor bump, removal requires a major bump).
func ErrIllegalSemverBump(providerSlug, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeIllegalSemverBump, "illegal semver version bump").
		WithDetails(map[string]any{"provider": providerSlug, "issue": issue})
}
