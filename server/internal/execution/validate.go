package execution

import "beecon/internal/schema"

// validateArguments validates arguments against a tool's input JSON Schema
// (as parsed from its provider definition YAML) via the shared
// internal/schema validator (Phase 3, Slice 2's tidy-first extraction — the
// second consumer is triggers' config validation). A tool that declares no
// schema accepts any arguments. The returned error's message is safe to
// surface directly inside a tool-level failure (PD6) — Execute never calls
// the provider when this returns non-nil.
func validateArguments(inputSchema map[string]any, arguments map[string]any) error {
	return schema.Validate(inputSchema, arguments)
}
