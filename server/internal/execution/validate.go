package execution

import (
	"bytes"
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaResourceURL is an opaque resource id for the compiler — never
// dereferenced, just a handle for the one schema being compiled per call.
const schemaResourceURL = "beecon://tool-input-schema"

// validateArguments compiles a tool's input JSON Schema (as parsed from its
// provider definition YAML) and validates arguments against it (AC2). A tool
// that declares no schema accepts any arguments. The returned error's
// message is safe to surface directly inside a tool-level failure (PD6) —
// Execute never calls the provider when this returns non-nil.
func validateArguments(inputSchema map[string]any, arguments map[string]any) error {
	if len(inputSchema) == 0 {
		return nil
	}
	schema, err := compileInputSchema(inputSchema)
	if err != nil {
		return err
	}
	instance, err := toJSONValue(arguments)
	if err != nil {
		return err
	}
	return schema.Validate(instance)
}

func compileInputSchema(inputSchema map[string]any) (*jsonschema.Schema, error) {
	doc, err := toJSONValue(inputSchema)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaResourceURL, doc); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaResourceURL)
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
