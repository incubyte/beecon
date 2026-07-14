// Package schema_test documents internal/schema's contract directly
// (Phase 3, Slice 2's tidy-first extraction): execution/validate_test.go
// already pins this behavior indirectly through validateArguments, but a
// second consumer (triggers' config validation) arrived once this package
// existed as its own leaf package, so a small direct suite belongs here too
// — the shared contract both consumers rely on, tested at its own source.
package schema_test

import (
	"testing"

	"beecon/internal/schema"
)

func TestValidate_AcceptsAnInstanceMatchingEveryDeclaredProperty(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"folderId": map[string]any{"type": "string"},
		},
	}

	err := schema.Validate(doc, map[string]any{"folderId": "Inbox"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AcceptsAnyInstanceWhenTheSchemaDocumentIsEmpty(t *testing.T) {
	err := schema.Validate(map[string]any{}, map[string]any{"anything": "goes"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AcceptsAnyInstanceWhenTheSchemaDocumentIsNil(t *testing.T) {
	err := schema.Validate(nil, map[string]any{"anything": "goes"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RejectsAWrongTypeForADeclaredProperty(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"folderId": map[string]any{"type": "string"},
		},
	}

	err := schema.Validate(doc, map[string]any{"folderId": float64(123)})

	if err == nil {
		t.Fatal("expected a validation error for folderId being a number instead of a string")
	}
}

func TestValidate_RejectsAMissingRequiredProperty(t *testing.T) {
	doc := map[string]any{
		"type":       "object",
		"required":   []any{"folderId"},
		"properties": map[string]any{"folderId": map[string]any{"type": "string"}},
	}

	err := schema.Validate(doc, map[string]any{})

	if err == nil {
		t.Fatal("expected a validation error for a missing required property")
	}
}

func TestValidate_ReturnsAnErrorRatherThanPanickingWhenTheSchemaDocumentItselfIsInvalid(t *testing.T) {
	// "type" must be a string or array of strings per JSON Schema — this
	// document is itself malformed, which must surface as a returned error
	// (compilation failure), never a panic.
	doc := map[string]any{"type": 123}

	err := schema.Validate(doc, map[string]any{})

	if err == nil {
		t.Fatal("expected an error compiling an invalid schema document")
	}
}

func TestCompile_SucceedsForAValidSchemaDocument(t *testing.T) {
	doc := map[string]any{
		"type":       "object",
		"required":   []any{"folderId"},
		"properties": map[string]any{"folderId": map[string]any{"type": "string"}},
	}

	compiled, err := schema.Compile(doc)

	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled == nil {
		t.Fatal("expected a non-nil compiled schema")
	}
}

func TestCompile_ReturnsAnErrorForAnInvalidSchemaDocument(t *testing.T) {
	_, err := schema.Compile(map[string]any{"type": 123})

	if err == nil {
		t.Fatal("expected an error compiling an invalid schema document")
	}
}
