package membraneimport

// fields.go holds small accessor helpers over the generic
// map[string]any shape gopkg.in/yaml.v3 decodes a Membrane export into.
// Reading Membrane records this way (instead of a fully typed struct per
// export shape) keeps the converter honest about which parts of the source
// DSL it actually understands: an unrecognized shape simply yields a zero
// value the caller treats as "not present" rather than a decode error.

// valueAt walks a chain of nested map[string]any keys, returning nil if any
// step is missing or not itself a map.
func valueAt(fields map[string]any, keys ...string) any {
	var current any = fields
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = asMap[key]
		if !ok {
			return nil
		}
	}
	return current
}

// stringAt returns the string at the given key path, or "" if it is absent
// or not a string.
func stringAt(fields map[string]any, keys ...string) string {
	value, _ := valueAt(fields, keys...).(string)
	return value
}

// mapAt returns the map[string]any at the given key path, or nil if it is
// absent or not a map.
func mapAt(fields map[string]any, keys ...string) map[string]any {
	value, _ := valueAt(fields, keys...).(map[string]any)
	return value
}
