package membraneimport

import (
	"fmt"
	"sort"
	"strings"
)

// membraneInputVarPrefix is the Membrane expression prefix this importer
// recognizes as "read from the tool's own input" (ADR-0012: the target is
// Beecon's token grammar, not an expression engine, so only this
// straightforward shape — and $firstNotEmpty wrapping it — translates;
// everything else is reported, never silently emitted as a literal
// `$`-string).
const membraneInputVarPrefix = "$.input."

// untranslatedPathPlaceholder is emitted when a Membrane action's request
// path is neither a plain string nor a recognized $case shape — an
// untranslatable path is never guessed at.
const untranslatedPathPlaceholder = "TODO://membrane-path-not-yet-translated"

// simpleVarInputName reports the input property name a value names via the
// single-key Membrane shape `{$var: $.input.NAME}`, and false for anything
// else (a literal, a `$case`, `$and`, `$eval`, `$firstNotEmpty`, or any other
// construct).
func simpleVarInputName(value any) (string, bool) {
	asMap, ok := value.(map[string]any)
	if !ok || len(asMap) != 1 {
		return "", false
	}
	expr, ok := asMap["$var"].(string)
	if !ok || !strings.HasPrefix(expr, membraneInputVarPrefix) {
		return "", false
	}
	return strings.TrimPrefix(expr, membraneInputVarPrefix), true
}

// inputToken renders a Beecon `{input.NAME}` token for the given input
// property name.
func inputToken(name string) string {
	return "{input." + name + "}"
}

// firstNotEmptyInputDefault reports the input property name and literal
// default a value names via the Membrane shape
// `{$firstNotEmpty: [{$var: $.input.NAME}, literal]}` (Gap C: this becomes
// an inputSchema property default), and false for anything else.
func firstNotEmptyInputDefault(value any) (name string, literal any, ok bool) {
	asMap, isMap := value.(map[string]any)
	if !isMap || len(asMap) != 1 {
		return "", nil, false
	}
	args, isList := asMap["$firstNotEmpty"].([]any)
	if !isList || len(args) != 2 {
		return "", nil, false
	}
	name, ok = simpleVarInputName(args[0])
	if !ok {
		return "", nil, false
	}
	return name, args[1], true
}

// schemaDefault is one inputSchema property default this run inferred from a
// Membrane `$firstNotEmpty($.input.NAME, literal)` expression (Gap C),
// applied to the emitted tool's inputSchema once every mapping value has
// been translated.
type schemaDefault struct {
	Name  string
	Value any
}

// mappingValue is the result of translating one Membrane query or
// pathParameters value into its Beecon mapping equivalent. Included reports
// whether the key belongs in the emitted mapping at all — an unsupported
// construct is dropped, never emitted as a literal `$`-string. Caveat, when
// non-empty, is this value's report line. Default, when non-nil, is an
// inputSchema property default this value's translation inferred.
type mappingValue struct {
	Token    string
	Included bool
	Caveat   string
	Default  *schemaDefault
}

// translateMappingValue classifies one Membrane query/pathParameters value:
// a simple `{$var: $.input.NAME}` becomes the whole token `{input.NAME}`; a
// `{$firstNotEmpty: [{$var: $.input.NAME}, literal]}` becomes the same token
// plus an inferred inputSchema default; a plain string passes through
// unchanged; anything else (an `$and`/`isNot`/`isNotEmpty` predicate,
// `$eval`, or other embedded-code expression) is reported as a dropped,
// needs-human construct named by fieldLabel.
func translateMappingValue(fieldLabel string, value any) mappingValue {
	if name, ok := simpleVarInputName(value); ok {
		return mappingValue{Token: inputToken(name), Included: true}
	}
	if name, literal, ok := firstNotEmptyInputDefault(value); ok {
		return mappingValue{
			Token:    inputToken(name),
			Included: true,
			Caveat:   fmt.Sprintf("%s: inferred inputSchema default %v for input %q from $firstNotEmpty", fieldLabel, literal, name),
			Default:  &schemaDefault{Name: name, Value: literal},
		}
	}
	if literal, ok := value.(string); ok {
		return mappingValue{Token: literal, Included: true}
	}
	return mappingValue{
		Caveat: fmt.Sprintf("%s: dropped unsupported construct %s — needs human translation", fieldLabel, describeUnsupportedConstruct(value)),
	}
}

// describeUnsupportedConstruct names, for the report, the specific Membrane
// DSL construct a value could not be translated by — the first `$`-prefixed
// (or predicate) key it carries, preferring the constructs the spec names,
// or a generic description when the value is not a recognizable expression
// map at all. Never returns the raw expression itself.
func describeUnsupportedConstruct(value any) string {
	asMap, ok := value.(map[string]any)
	if !ok {
		return "an unrecognized expression"
	}
	for _, known := range []string{"$and", "$eval", "$case", "isNot", "isNotEmpty", "is", "$plain"} {
		if _, present := asMap[known]; present {
			return "`" + known + "`"
		}
	}
	keys := make([]string, 0, len(asMap))
	for key := range asMap {
		if strings.HasPrefix(key, "$") {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return "an unrecognized expression"
	}
	sort.Strings(keys)
	return "`" + keys[0] + "`"
}

// translateValueMap converts a Membrane query map into Beecon mapping
// tokens: keys are visited in sorted order so the resulting caveats are
// deterministic across runs, independent of Go's randomized map iteration.
func translateValueMap(fieldPrefix string, source map[string]any) (tokens map[string]string, caveats []string, defaults []schemaDefault) {
	if len(source) == 0 {
		return nil, nil, nil
	}
	for _, key := range sortedKeys(source) {
		translated := translateMappingValue(fieldPrefix+"."+key, source[key])
		if translated.Included {
			if tokens == nil {
				tokens = make(map[string]string, len(source))
			}
			tokens[key] = translated.Token
		}
		if translated.Caveat != "" {
			caveats = append(caveats, translated.Caveat)
		}
		if translated.Default != nil {
			defaults = append(defaults, *translated.Default)
		}
	}
	return tokens, caveats, defaults
}

// inlinePathParameters inlines every pathParameters entry into path's
// matching `{name}` segment: a translatable value (a simple `$var` or a
// `$firstNotEmpty` default) becomes the Beecon token in place; an
// unsupported value is left un-inlined and reported as a caveat rather than
// silently guessed at. Keys are visited in sorted order for deterministic
// caveat ordering.
func inlinePathParameters(path string, pathParameters map[string]any) (string, []string, []schemaDefault) {
	var caveats []string
	var defaults []schemaDefault
	for _, name := range sortedKeys(pathParameters) {
		translated := translateMappingValue("pathParameters."+name, pathParameters[name])
		if translated.Included {
			path = strings.ReplaceAll(path, "{"+name+"}", translated.Token)
		}
		if translated.Caveat != "" {
			caveats = append(caveats, translated.Caveat)
		}
		if translated.Default != nil {
			defaults = append(defaults, *translated.Default)
		}
	}
	return path, caveats, defaults
}

func sortedKeys(source map[string]any) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
