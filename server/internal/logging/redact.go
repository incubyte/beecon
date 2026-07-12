package logging

import (
	"encoding/json"
	"strings"
)

// RedactedPlaceholder replaces every sensitive field's value before a log
// entry is ever persisted (AC9).
const RedactedPlaceholder = "[REDACTED]"

// sensitiveKeys are the JSON object keys AC9 requires Redact to strip,
// matched case-insensitively so "Authorization", "authorization", and
// "AUTHORIZATION" are all caught: Authorization headers, access/refresh
// tokens, and OAuth client secrets.
var sensitiveKeys = map[string]bool{
	"authorization": true,
	"access_token":  true,
	"accesstoken":   true,
	"refresh_token": true,
	"refreshtoken":  true,
	"client_secret": true,
	"clientsecret":  true,
}

// Redact returns body with every sensitive field's value replaced by
// RedactedPlaceholder (AC9). body is expected to be the JSON object/array
// shape execution and connections construct their log bodies in; a body
// that is not valid JSON (e.g. a plain error message) is returned
// unchanged — there is nothing structured to redact.
func Redact(body string) string {
	if body == "" {
		return body
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return body
	}
	out, err := json.Marshal(redactValue(parsed))
	if err != nil {
		return body
	}
	return string(out)
}

func redactValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return redactObject(typed)
	case []any:
		return redactArray(typed)
	default:
		return v
	}
}

func redactObject(object map[string]any) map[string]any {
	redacted := make(map[string]any, len(object))
	for key, value := range object {
		if sensitiveKeys[strings.ToLower(key)] {
			redacted[key] = RedactedPlaceholder
			continue
		}
		redacted[key] = redactValue(value)
	}
	return redacted
}

func redactArray(array []any) []any {
	redacted := make([]any, len(array))
	for i, value := range array {
		redacted[i] = redactValue(value)
	}
	return redacted
}
