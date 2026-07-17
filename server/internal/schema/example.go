package schema

// Example generates a minimal instance satisfying doc's declared shape well
// enough to pass Validate — used only by the one-time embedded-provider
// registry migration (Phase 5 registry sub-phase, Slice 6, PD63/PD68): the
// embedded YAML seed was written before the registry's publish-time
// output-schema-vs-sample gate existed and carries no recorded sample
// response, so the migration synthesizes one directly from each tool's own
// declared outputSchema rather than hand-writing provider-specific fixtures.
// Every leaf value is the type's zero value (empty string, 0, false, an
// empty object/array) — the schemas this needs to satisfy declare only
// property types, never a required/enum/format/minLength constraint a zero
// value could fail. An unrecognized or missing "type" (including the schema
// document being empty) falls back to an empty object, which Validate
// itself already treats as "no constraint" against any instance.
func Example(doc map[string]any) map[string]any {
	if instance, ok := exampleValue(doc).(map[string]any); ok {
		return instance
	}
	return map[string]any{}
}

func exampleValue(doc map[string]any) any {
	switch schemaType(doc) {
	case "object":
		return exampleObject(doc)
	case "array":
		return exampleArray(doc)
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "string":
		return ""
	default:
		return map[string]any{}
	}
}

// schemaType reads doc's declared "type", falling back to "object" when
// "type" is absent but "properties" is present — the same shape every
// outputSchema in the embedded providers uses.
func schemaType(doc map[string]any) string {
	if declared, ok := doc["type"].(string); ok && declared != "" {
		return declared
	}
	if _, ok := doc["properties"]; ok {
		return "object"
	}
	return ""
}

func exampleObject(doc map[string]any) map[string]any {
	properties, _ := doc["properties"].(map[string]any)
	instance := make(map[string]any, len(properties))
	for name, propertyDoc := range properties {
		nested, _ := propertyDoc.(map[string]any)
		instance[name] = exampleValue(nested)
	}
	return instance
}

func exampleArray(doc map[string]any) any {
	items, ok := doc["items"].(map[string]any)
	if !ok {
		return []any{}
	}
	return []any{exampleValue(items)}
}
