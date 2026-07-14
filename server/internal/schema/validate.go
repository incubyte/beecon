// Package schema is a shared-infra leaf package (like httpx/vault): a
// JSON-Schema-subset compiler and validator over Go values (map[string]any,
// as parsed from either YAML provider definitions or JSON request bodies).
// Extracted from execution/validate.go (Phase 3, Slice 2's tidy-first) once a
// second consumer arrived — triggers validates a trigger instance's config
// against its definition's configSchema the same way execution validates a
// tool's arguments against its inputSchema. Behavior is unchanged: this is a
// pure move, pinned by execution's existing validate_test.go.
package schema

import (
	"bytes"
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// resourceURL is an opaque resource id for the compiler — never
// dereferenced, just a handle for the one schema being compiled per call.
const resourceURL = "beecon://schema"

// Validate compiles doc as a JSON Schema and validates instance against it.
// A caller that declares no schema (an empty or nil doc) accepts any
// instance — the same "no schema means no constraint" rule
// execution.validateArguments always applied.
func Validate(doc map[string]any, instance map[string]any) error {
	if len(doc) == 0 {
		return nil
	}
	compiled, err := Compile(doc)
	if err != nil {
		return err
	}
	value, err := toJSONValue(instance)
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

// Compile compiles doc — a JSON-Schema-shaped map, as parsed from YAML or
// JSON — into a reusable *jsonschema.Schema.
func Compile(doc map[string]any) (*jsonschema.Schema, error) {
	value, err := toJSONValue(doc)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(resourceURL, value); err != nil {
		return nil, err
	}
	return compiler.Compile(resourceURL)
}

// toJSONValue round-trips v through JSON so YAML-sourced Go values (e.g.
// int, map[string]any) become the exact shapes jsonschema/v6 expects — the
// same shapes its own UnmarshalJSON produces from wire JSON.
func toJSONValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
}
