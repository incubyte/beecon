// template.go implements the {input.x}/{params.x} substitution the
// finalized definition format (PD13) declares for a tool's mapping: path
// templating, and the query/header mapping values Facade.callProvider
// evaluates against a call's arguments and a connection's decrypted pre-auth
// param values (Slice 3, AC8) — nil for a connection that collected none.
package execution

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// templateTokenFindPattern matches every {input.x}/{params.x} token anywhere
// inside a string (used by RenderPath, which may substitute more than one
// token per path).
var templateTokenFindPattern = regexp.MustCompile(`\{(input|params)\.([A-Za-z0-9_]+)\}`)

// templateTokenWholePattern matches a string that is exactly one
// {input.x}/{params.x} token, nothing else (used by RenderMappedValue, which
// evaluates one whole mapping expression at a time).
var templateTokenWholePattern = regexp.MustCompile(`^\{(input|params)\.([A-Za-z0-9_]+)\}$`)

// RenderPath substitutes every {input.x}/{params.x} token in path with its
// corresponding argument value, URL-escaping each substituted value so it
// cannot break out of its path segment (e.g. a messageId containing "/" or
// "?"). A token naming an input or param the call did not supply is an
// error — a path segment can never be silently dropped.
func RenderPath(path string, inputs, params map[string]any) (string, error) {
	var missing string
	rendered := templateTokenFindPattern.ReplaceAllStringFunc(path, func(token string) string {
		value, ok := lookupTemplateToken(templateTokenFindPattern.FindStringSubmatch(token), inputs, params)
		if !ok {
			missing = token
			return token
		}
		return url.PathEscape(fmt.Sprint(value))
	})
	if missing != "" {
		return "", fmt.Errorf("path template references %s, which was not supplied", missing)
	}
	return rendered, nil
}

// RenderMappedValue renders one query, header, or body mapping expression.
// A whole-token expression (e.g. "{input.select}") whose input/param is
// absent reports ok=false — the caller drops that query parameter, header,
// or body key entirely rather than sending an empty or literal "{input.x}"
// value (unchanged from before embedded interpolation existed). An
// expression that embeds one or more tokens inside a larger literal (e.g.
// "receivedDateTime gt {input.since}" or "{input.first} {input.last}") is
// substituted find-anywhere like RenderPath/RenderPollTemplate: every token
// present is replaced in place, but a token the call did not supply is an
// error naming the missing token — an embedded value can never be silently
// sent half-rendered, unlike the whole-token case's backward-compatible
// drop. An expression that carries no token at all is returned as-is (a
// literal mapping value).
func RenderMappedValue(expression string, inputs, params map[string]any) (rendered string, ok bool, err error) {
	if match := templateTokenWholePattern.FindStringSubmatch(expression); match != nil {
		value, found := lookupTemplateToken(match, inputs, params)
		if !found {
			return "", false, nil
		}
		return fmt.Sprint(value), true, nil
	}

	var missing string
	rendered = templateTokenFindPattern.ReplaceAllStringFunc(expression, func(token string) string {
		value, found := lookupTemplateToken(templateTokenFindPattern.FindStringSubmatch(token), inputs, params)
		if !found {
			missing = token
			return token
		}
		return fmt.Sprint(value)
	})
	if missing != "" {
		return "", false, fmt.Errorf("mapping value references %s, which was not supplied", missing)
	}
	return rendered, true, nil
}

// lookupTemplateToken resolves a regexp match ([full, source, name]) against
// inputs (source "input") or params (source "params").
func lookupTemplateToken(match []string, inputs, params map[string]any) (any, bool) {
	if match == nil {
		return nil, false
	}
	bag := inputs
	if match[1] == "params" {
		bag = params
	}
	value, ok := bag[match[2]]
	return value, ok
}

// pollTemplateTokenPattern matches every {config.x}/{watermark} token
// anywhere inside a string (Slice 4, PD28/PD34): unlike a tool mapping's
// query/header values (RenderMappedValue, whole-token only), a poll
// mapping's value may embed a token inside a larger literal — e.g. Outlook's
// OData filter "receivedDateTime gt {watermark}" — so this mirrors
// RenderPath's find-and-replace-anywhere behavior instead.
var pollTemplateTokenPattern = regexp.MustCompile(`\{(config\.[A-Za-z0-9_]+|watermark)\}`)

// RenderPollTemplate substitutes every {config.x}/{watermark} token in
// template with its value: {config.x} looks it up in config (the trigger
// instance's own config values, already merged with the definition's
// configSchema defaults by the caller — execution/poll.go); {watermark} is
// always watermark, the poll tick's current watermark already rendered as a
// string (RFC3339, execution/poll.go's own canonical format, applied
// uniformly across every provider). A {config.x} token naming a key config
// does not carry is an error — the same "never silently drop a segment"
// rule RenderPath applies to tool mapping paths; {watermark} is always
// supplied, so it can never be missing. escapePathSegments applies
// url.PathEscape to each substituted value, for use on the poll mapping's
// own request Path (mirrors RenderPath); query and body values pass the
// substituted value through unescaped, exactly like RenderMappedValue.
func RenderPollTemplate(template string, config map[string]any, watermark string, escapePathSegments bool) (string, error) {
	var missing string
	rendered := pollTemplateTokenPattern.ReplaceAllStringFunc(template, func(token string) string {
		value, ok := lookupPollToken(token, config, watermark)
		if !ok {
			missing = token
			return token
		}
		substituted := fmt.Sprint(value)
		if escapePathSegments {
			return url.PathEscape(substituted)
		}
		return substituted
	})
	if missing != "" {
		return "", fmt.Errorf("poll template references %s, which was not supplied", missing)
	}
	return rendered, nil
}

// lookupPollToken resolves one matched "{...}" token (config.x or watermark)
// against config or the already-rendered watermark string.
func lookupPollToken(token string, config map[string]any, watermark string) (any, bool) {
	inner := token[1 : len(token)-1]
	if inner == "watermark" {
		return watermark, true
	}
	value, ok := config[strings.TrimPrefix(inner, "config.")]
	return value, ok
}
