// example_test.go exercises schema.Example: the one-time embedded-provider
// registry migration's sample synthesizer (Phase 5 registry sub-phase, Slice
// 6, PD63/PD68). Its whole job is to produce an instance that
// schema.Validate accepts against the very doc it was generated from —
// that's the property every test here actually checks, not the literal
// shape of the generated value, since the literal shape (zero values) is an
// implementation choice Example's own doc comment already documents.
package schema_test

import (
	"testing"

	"beecon/internal/schema"
)

// assertExampleValidatesAgainstItsOwnSchema is the one property every
// Example test in this file cares about: whatever Example(doc) produces,
// schema.Validate(doc, that value) must accept it — that's the whole
// contract the migration script (cmd/publishembeddedproviders) leans on to
// satisfy the registry's publish-time output-schema-vs-sample gate.
func assertExampleValidatesAgainstItsOwnSchema(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	instance := schema.Example(doc)
	if err := schema.Validate(doc, instance); err != nil {
		t.Fatalf("schema.Validate(doc, Example(doc)) = %v, want nil; doc=%+v instance=%+v", err, doc, instance)
	}
	return instance
}

func TestExample_EmptyDocProducesAnEmptyObject(t *testing.T) {
	instance := assertExampleValidatesAgainstItsOwnSchema(t, map[string]any{})
	if len(instance) != 0 {
		t.Errorf("instance = %+v, want an empty object for an empty schema doc", instance)
	}
}

func TestExample_NilDocProducesAnEmptyObject(t *testing.T) {
	instance := assertExampleValidatesAgainstItsOwnSchema(t, nil)
	if len(instance) != 0 {
		t.Errorf("instance = %+v, want an empty object for a nil schema doc", instance)
	}
}

func TestExample_ObjectWithTypedScalarPropertiesProducesTheirZeroValues(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"count":   map[string]any{"type": "integer"},
			"amount":  map[string]any{"type": "number"},
			"enabled": map[string]any{"type": "boolean"},
		},
	}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	if instance["name"] != "" {
		t.Errorf("name = %v, want the zero value \"\"", instance["name"])
	}
	if instance["count"] != 0 {
		t.Errorf("count = %v, want the zero value 0", instance["count"])
	}
	if instance["amount"] != 0 {
		t.Errorf("amount = %v, want the zero value 0", instance["amount"])
	}
	if instance["enabled"] != false {
		t.Errorf("enabled = %v, want the zero value false", instance["enabled"])
	}
}

func TestExample_ObjectWithNoDeclaredTypeButPropertiesIsTreatedAsAnObject(t *testing.T) {
	doc := map[string]any{
		"properties": map[string]any{
			"subject": map[string]any{"type": "string"},
		},
	}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	if _, ok := instance["subject"]; !ok {
		t.Errorf("instance = %+v, want a \"subject\" key even though \"type\" was never declared", instance)
	}
}

func TestExample_NestedObjectPropertyProducesANestedObjectInstance(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sender": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"email": map[string]any{"type": "string"},
				},
			},
		},
	}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	sender, ok := instance["sender"].(map[string]any)
	if !ok {
		t.Fatalf("instance[\"sender\"] = %T, want a nested object", instance["sender"])
	}
	if _, ok := sender["email"]; !ok {
		t.Errorf("sender = %+v, want an \"email\" key", sender)
	}
}

func TestExample_ArrayOfObjectsProducesASingleElementArray(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	value, ok := instance["value"].([]any)
	if !ok {
		t.Fatalf("instance[\"value\"] = %T, want a slice", instance["value"])
	}
	if len(value) != 1 {
		t.Fatalf("len(value) = %d, want 1 (a single synthesized element)", len(value))
	}
	item, ok := value[0].(map[string]any)
	if !ok {
		t.Fatalf("value[0] = %T, want an object carrying the declared \"id\" property", value[0])
	}
	if _, ok := item["id"]; !ok {
		t.Errorf("value[0] = %+v, want an \"id\" key", item)
	}
}

func TestExample_ArrayWithNoDeclaredItemsProducesAnEmptyArray(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{"type": "array"},
		},
	}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	tags, ok := instance["tags"].([]any)
	if !ok {
		t.Fatalf("instance[\"tags\"] = %T, want a slice", instance["tags"])
	}
	if len(tags) != 0 {
		t.Errorf("tags = %+v, want empty when the schema declares no \"items\"", tags)
	}
}

// TestExample_RequiredPropertiesAreAllPresentInTheGeneratedInstance proves
// half of the doc comment's claim: Example always fills in every declared
// property (nested or not), so a schema's "required" list is always
// satisfied regardless of which properties it names.
func TestExample_RequiredPropertiesAreAllPresentInTheGeneratedInstance(t *testing.T) {
	doc := map[string]any{
		"type":     "object",
		"required": []any{"status", "id"},
		"properties": map[string]any{
			"status": map[string]any{"type": "string"},
			"id":     map[string]any{"type": "string"},
		},
	}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	if _, ok := instance["id"]; !ok {
		t.Errorf("instance = %+v, missing required property \"id\"", instance)
	}
	if _, ok := instance["status"]; !ok {
		t.Errorf("instance = %+v, missing required property \"status\"", instance)
	}
}

// TestExample_AnEnumThatIncludesTheZeroValueStillValidates documents the
// realistic embedded-provider shape: none of the schemas the migration
// actually needs to satisfy declare an enum that excludes the zero value, so
// as long as that holds, Example's always-zero-value output keeps
// validating even against an enum-constrained property.
func TestExample_AnEnumThatIncludesTheZeroValueStillValidates(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "enum": []any{"", "sent", "draft"}},
		},
	}

	assertExampleValidatesAgainstItsOwnSchema(t, doc)
}

// TestExample_AnEnumThatExcludesTheZeroValueIsAKnownLimitation is an
// adversarial edge case, not a claim of correct behavior: Example always
// emits a string property's zero value ("") with no attempt to pick a
// member of a declared "enum" — so a schema whose enum excludes "" produces
// a sample that does NOT validate. This pins the current, documented
// limitation (schema.Example's own doc comment: "the schemas this needs to
// satisfy ... never a required/enum/... constraint a zero value could
// fail") rather than silently passing under an assumption the embedded
// providers happen to meet today.
func TestExample_AnEnumThatExcludesTheZeroValueIsAKnownLimitation(t *testing.T) {
	doc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "enum": []any{"sent", "draft"}},
		},
	}

	instance := schema.Example(doc)
	if err := schema.Validate(doc, instance); err == nil {
		t.Fatalf("schema.Validate unexpectedly accepted Example's zero-value output %+v against an enum excluding \"\" — Example may have started honoring enum; update this test's expectation if so", instance)
	}
}

// TestExample_ADocWithNeitherTypeNorPropertiesFallsBackToAnEmptyObject is
// the doc comment's "missing type" fallback case with other, unrelated keys
// present (not just a bare {} — TestExample_EmptyDocProducesAnEmptyObject
// already covers that) — schemaType has nothing to key off, so Example
// falls back to {}, and Validate imposes no constraint since the doc
// declares no type/properties either.
func TestExample_ADocWithNeitherTypeNorPropertiesFallsBackToAnEmptyObject(t *testing.T) {
	doc := map[string]any{"description": "a schema that declares no type or properties"}

	instance := assertExampleValidatesAgainstItsOwnSchema(t, doc)

	if len(instance) != 0 {
		t.Errorf("instance = %+v, want an empty object when neither \"type\" nor \"properties\" is declared", instance)
	}
}
