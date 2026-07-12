// White-box (package execution) tests for validate.go's unexported
// validateArguments: AC2 requires arguments to be checked against a tool's
// input JSON Schema before the provider is ever called. These tests exercise
// the schema-compilation and validation logic directly, independent of
// Facade.Execute's own orchestration (covered by facade_test.go).
package execution

import "testing"

// outlookListMessagesSchema mirrors the actual outlook-list-messages tool's
// inputSchema (catalog/providers/outlook.yaml, PD8): top/skip are integers,
// select/filter are strings, none required.
func outlookListMessagesSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"top":    map[string]any{"type": "integer"},
			"skip":   map[string]any{"type": "integer"},
			"select": map[string]any{"type": "string"},
			"filter": map[string]any{"type": "string"},
		},
	}
}

func TestValidateArguments_AcceptsArgumentsMatchingEveryDeclaredProperty(t *testing.T) {
	err := validateArguments(outlookListMessagesSchema(), map[string]any{
		"top": float64(10), "skip": float64(0), "select": "subject", "filter": "isRead eq false",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArguments_AcceptsAnEmptyArgumentsMapWhenNoPropertyIsRequired(t *testing.T) {
	err := validateArguments(outlookListMessagesSchema(), map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArguments_AcceptsAnyArgumentsWhenTheToolDeclaresNoSchema(t *testing.T) {
	err := validateArguments(map[string]any{}, map[string]any{"anything": "goes"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArguments_RejectsAWrongTypeForADeclaredProperty(t *testing.T) {
	err := validateArguments(outlookListMessagesSchema(), map[string]any{"top": "not-a-number"})

	if err == nil {
		t.Fatal("expected a validation error for top being a string instead of an integer")
	}
}

func TestValidateArguments_RejectsAMissingRequiredProperty(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"required":   []any{"folderId"},
		"properties": map[string]any{"folderId": map[string]any{"type": "string"}},
	}

	err := validateArguments(schema, map[string]any{})

	if err == nil {
		t.Fatal("expected a validation error for a missing required property")
	}
}

func TestValidateArguments_RejectsAnUnknownPropertyWhenTheSchemaForbidsAdditionalProperties(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"top": map[string]any{"type": "integer"}},
		"additionalProperties": false,
	}

	err := validateArguments(schema, map[string]any{"top": float64(1), "unexpectedField": "value"})

	if err == nil {
		t.Fatal("expected a validation error for an unknown property when additionalProperties is false")
	}
}

func TestValidateArguments_AllowsAnUnknownPropertyWhenTheSchemaDoesNotForbidAdditionalProperties(t *testing.T) {
	// outlook-list-messages' own schema (PD8) declares no additionalProperties
	// restriction — this pins today's actual behavior rather than assuming one.
	err := validateArguments(outlookListMessagesSchema(), map[string]any{"top": float64(1), "somethingElse": "value"})

	if err != nil {
		t.Fatalf("unexpected error: %v (outlook-list-messages' schema does not set additionalProperties: false)", err)
	}
}

func TestValidateArguments_ErrorMessageIsNonEmptyAndSafeToSurfaceDirectly(t *testing.T) {
	err := validateArguments(outlookListMessagesSchema(), map[string]any{"top": "not-a-number"})

	if err == nil {
		t.Fatal("expected an error")
	}
	if err.Error() == "" {
		t.Error("validation error message must not be empty — it is surfaced directly inside a tool-level failure (PD6)")
	}
}
