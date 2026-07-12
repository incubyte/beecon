// Package logging_test exercises Redact directly: AC9's security-critical
// guarantee that Authorization headers, access/refresh tokens, and OAuth
// client secrets are stripped from a log body's JSON before it is ever
// persisted, no matter how they are cased or nested.
package logging_test

import (
	"encoding/json"
	"strings"
	"testing"

	"beecon/internal/logging"
)

func decodeJSONObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("Redact produced invalid JSON: %v; got: %s", err, raw)
	}
	return parsed
}

func TestRedact_RedactsAnAuthorizationHeaderValue(t *testing.T) {
	body := `{"headers":{"Authorization":"Bearer raw-access-token-value"}}`

	got := decodeJSONObject(t, logging.Redact(body))

	headers := got["headers"].(map[string]any)
	if headers["Authorization"] != logging.RedactedPlaceholder {
		t.Errorf("Authorization = %v, want %q", headers["Authorization"], logging.RedactedPlaceholder)
	}
}

func TestRedact_MatchesTheAuthorizationKeyCaseInsensitively(t *testing.T) {
	for _, key := range []string{"authorization", "Authorization", "AUTHORIZATION", "AuthOrization"} {
		t.Run(key, func(t *testing.T) {
			body := `{"` + key + `":"Bearer secret-value"}`

			got := decodeJSONObject(t, logging.Redact(body))

			if got[key] != logging.RedactedPlaceholder {
				t.Errorf("%s = %v, want %q", key, got[key], logging.RedactedPlaceholder)
			}
		})
	}
}

func TestRedact_RedactsAccessAndRefreshTokenFields(t *testing.T) {
	body := `{"access_token":"raw-access-token","refresh_token":"raw-refresh-token"}`

	got := decodeJSONObject(t, logging.Redact(body))

	if got["access_token"] != logging.RedactedPlaceholder {
		t.Errorf("access_token = %v, want %q", got["access_token"], logging.RedactedPlaceholder)
	}
	if got["refresh_token"] != logging.RedactedPlaceholder {
		t.Errorf("refresh_token = %v, want %q", got["refresh_token"], logging.RedactedPlaceholder)
	}
}

func TestRedact_RedactsClientSecretField(t *testing.T) {
	body := `{"client_secret":"the-outlook-client-secret"}`

	got := decodeJSONObject(t, logging.Redact(body))

	if got["client_secret"] != logging.RedactedPlaceholder {
		t.Errorf("client_secret = %v, want %q", got["client_secret"], logging.RedactedPlaceholder)
	}
}

func TestRedact_RedactsAnOAuthAuthorizationCodeInATokenExchangeRequestBody(t *testing.T) {
	body := `{"grant_type":"authorization_code","code":"M.C123-secret-auth-code","redirect_uri":"https://consumer.example.com/callback"}`

	got := decodeJSONObject(t, logging.Redact(body))

	if got["code"] != logging.RedactedPlaceholder {
		t.Errorf("code = %v, want %q", got["code"], logging.RedactedPlaceholder)
	}
	if got["grant_type"] != "authorization_code" {
		t.Errorf("grant_type = %v, want it left untouched", got["grant_type"])
	}
	if got["redirect_uri"] != "https://consumer.example.com/callback" {
		t.Errorf("redirect_uri = %v, want it left untouched", got["redirect_uri"])
	}
}

func TestRedact_MatchesTheCodeKeyCaseInsensitively(t *testing.T) {
	for _, key := range []string{"code", "Code", "CODE", "CoDe"} {
		t.Run(key, func(t *testing.T) {
			body := `{"` + key + `":"M.C123-secret-auth-code"}`

			got := decodeJSONObject(t, logging.Redact(body))

			if got[key] != logging.RedactedPlaceholder {
				t.Errorf("%s = %v, want %q", key, got[key], logging.RedactedPlaceholder)
			}
		})
	}
}

func TestRedact_RedactsSensitiveFieldsNestedInsideAnObject(t *testing.T) {
	body := `{"request":{"headers":{"Authorization":"Bearer nested-token"}}}`

	got := decodeJSONObject(t, logging.Redact(body))

	request := got["request"].(map[string]any)
	headers := request["headers"].(map[string]any)
	if headers["Authorization"] != logging.RedactedPlaceholder {
		t.Errorf("nested Authorization = %v, want %q", headers["Authorization"], logging.RedactedPlaceholder)
	}
}

func TestRedact_RedactsSensitiveFieldsInsideAnArray(t *testing.T) {
	body := `{"attempts":[{"access_token":"first-token"},{"access_token":"second-token"}]}`

	got := decodeJSONObject(t, logging.Redact(body))

	attempts := got["attempts"].([]any)
	for i, attempt := range attempts {
		token := attempt.(map[string]any)["access_token"]
		if token != logging.RedactedPlaceholder {
			t.Errorf("attempts[%d].access_token = %v, want %q", i, token, logging.RedactedPlaceholder)
		}
	}
}

func TestRedact_LeavesNonSensitiveFieldsUntouched(t *testing.T) {
	body := `{"method":"GET","url":"https://graph.microsoft.com/v1.0/me/messages","status":200}`

	got := decodeJSONObject(t, logging.Redact(body))

	if got["method"] != "GET" {
		t.Errorf("method = %v, want %q", got["method"], "GET")
	}
	if got["url"] != "https://graph.microsoft.com/v1.0/me/messages" {
		t.Errorf("url = %v, want the unredacted URL", got["url"])
	}
	if got["status"] != float64(200) {
		t.Errorf("status = %v, want %v", got["status"], float64(200))
	}
}

func TestRedact_NeverLeavesTheRawSensitiveValueAnywhereInTheOutput(t *testing.T) {
	const rawSecret = "super-secret-raw-value-12345"
	body := `{"headers":{"Authorization":"Bearer ` + rawSecret + `"},"access_token":"` + rawSecret + `"}`

	got := logging.Redact(body)

	if strings.Contains(got, rawSecret) {
		t.Fatalf("Redact output %q still contains the raw secret %q", got, rawSecret)
	}
}

func TestRedact_ReturnsANonJSONBodyUnchanged(t *testing.T) {
	const plainMessage = "the provider could not be reached"

	got := logging.Redact(plainMessage)

	if got != plainMessage {
		t.Errorf("Redact(%q) = %q, want it returned unchanged (nothing structured to redact)", plainMessage, got)
	}
}

func TestRedact_ReturnsAnEmptyStringUnchanged(t *testing.T) {
	got := logging.Redact("")

	if got != "" {
		t.Errorf("Redact(\"\") = %q, want \"\"", got)
	}
}
