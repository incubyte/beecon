package logging

import (
	"encoding/json"
	"strings"
)

// RedactedPlaceholder replaces every sensitive field's value before a log
// entry is ever persisted (AC9).
const RedactedPlaceholder = "[REDACTED]"

// sensitiveKeys are redacted regardless of Kind (AC9): Authorization
// headers, access/refresh tokens, and OAuth client secrets are never
// legitimate to persist for any kind of log entry, matched
// case-insensitively so "Authorization", "authorization", and
// "AUTHORIZATION" are all caught.
var sensitiveKeys = map[string]bool{
	"authorization": true,
	"access_token":  true,
	"accesstoken":   true,
	"refresh_token": true,
	"refreshtoken":  true,
	"client_secret": true,
	"clientsecret":  true,
}

// codeRedactedKinds are the only Kind values whose "code" field is an OAuth
// authorization code rather than a legitimate application field (PD25): a
// tool-execution body's own "code" field (an error code, a country code,
// ...) is left untouched.
var codeRedactedKinds = map[Kind]bool{
	KindOAuthTokenExchange: true,
}

// Redact returns body with every sensitive field's value replaced by
// RedactedPlaceholder (AC9), scoped by kind (PD25): "code" is redacted only
// inside a KindOAuthTokenExchange entry, where it is an OAuth authorization
// code — every other kind (KindToolExecution) keeps its own "code" fields
// untouched. body is expected to be the JSON object/array shape execution
// and connections construct their log bodies in; a body that is not valid
// JSON (e.g. a plain error message) is returned unchanged — there is nothing
// structured to redact.
func Redact(kind Kind, body string) string {
	if body == "" {
		return body
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return body
	}
	out, err := json.Marshal(redactValue(parsed, codeRedactedKinds[kind]))
	if err != nil {
		return body
	}
	return string(out)
}

func redactValue(v any, redactCode bool) any {
	switch typed := v.(type) {
	case map[string]any:
		return redactObject(typed, redactCode)
	case []any:
		return redactArray(typed, redactCode)
	default:
		return v
	}
}

func redactObject(object map[string]any, redactCode bool) map[string]any {
	redacted := make(map[string]any, len(object))
	for key, value := range object {
		if isSensitiveKey(key, redactCode) {
			redacted[key] = RedactedPlaceholder
			continue
		}
		redacted[key] = redactValue(value, redactCode)
	}
	return redacted
}

// isSensitiveKey reports whether key (matched case-insensitively) must be
// redacted: the always-redacted set, plus "code" when redactCode is true
// (PD25 — only for a KindOAuthTokenExchange entry).
func isSensitiveKey(key string, redactCode bool) bool {
	lower := strings.ToLower(key)
	if sensitiveKeys[lower] {
		return true
	}
	return redactCode && lower == "code"
}

func redactArray(array []any, redactCode bool) []any {
	redacted := make([]any, len(array))
	for i, value := range array {
		redacted[i] = redactValue(value, redactCode)
	}
	return redacted
}
