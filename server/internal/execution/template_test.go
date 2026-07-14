// Package execution_test exercises RenderPath and RenderMappedValue (the
// {input.x}/{params.x} template engine PD13's mapping format declares) in
// isolation from Facade.Execute's own orchestration (covered by
// facade_test.go): URL-escaping of substituted path values, a missing
// required path token failing loudly, optional query/header mapping values
// being dropped when their input is absent, and {params.x} tokens resolving
// against the params bag (nil until Slice 3 wires expected params).
package execution_test

import (
	"net/url"
	"strings"
	"testing"

	"beecon/internal/execution"
)

func TestRenderPath_URLEscapesASubstitutedValueContainingSpacesSlashesAndQueryCharacters(t *testing.T) {
	const messageID = "hello world/needs escaping?&stuff"
	want := "/me/messages/" + url.PathEscape(messageID)

	got, err := execution.RenderPath("/me/messages/{input.messageId}", map[string]any{"messageId": messageID}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("RenderPath = %q, want %q", got, want)
	}
}

func TestRenderPath_SubstitutesMultipleTokensFromBothInputsAndParamsInOneCall(t *testing.T) {
	got, err := execution.RenderPath(
		"/orgs/{params.orgId}/messages/{input.messageId}",
		map[string]any{"messageId": "msg-1"},
		map[string]any{"orgId": "org-1"},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/orgs/org-1/messages/msg-1" {
		t.Errorf("RenderPath = %q, want %q", got, "/orgs/org-1/messages/msg-1")
	}
}

func TestRenderPath_ReturnsAnErrorNamingTheTokenWhenARequiredPathInputIsMissing(t *testing.T) {
	_, err := execution.RenderPath("/me/messages/{input.messageId}", map[string]any{}, nil)

	if err == nil {
		t.Fatal("expected an error when the path's {input.messageId} token is not supplied, got nil")
	}
	if !strings.Contains(err.Error(), "{input.messageId}") {
		t.Errorf("error = %q, want it to name the missing token %q", err.Error(), "{input.messageId}")
	}
	if !strings.Contains(err.Error(), "not supplied") {
		t.Errorf("error = %q, want it to explain the token was not supplied", err.Error())
	}
}

func TestRenderPath_TreatsAMissingParamsTokenAsMissingWhenParamsIsNil(t *testing.T) {
	_, err := execution.RenderPath("/orgs/{params.orgId}/messages", map[string]any{}, nil)

	if err == nil {
		t.Fatal("expected an error for a {params.x} token when params is nil (Slice 1: params arrives in Slice 3), got nil")
	}
}

func TestRenderMappedValue_RendersALiteralExpressionWithNoTemplateTokenAsIs(t *testing.T) {
	rendered, ok := execution.RenderMappedValue("application/json", map[string]any{}, nil)

	if !ok {
		t.Fatal("ok = false, want true for a literal (token-free) mapping expression")
	}
	if rendered != "application/json" {
		t.Errorf("rendered = %q, want %q", rendered, "application/json")
	}
}

func TestRenderMappedValue_RendersTheSuppliedInputValue(t *testing.T) {
	rendered, ok := execution.RenderMappedValue("{input.select}", map[string]any{"select": "subject"}, nil)

	if !ok {
		t.Fatal("ok = false, want true when the input is supplied")
	}
	if rendered != "subject" {
		t.Errorf("rendered = %q, want %q", rendered, "subject")
	}
}

// TestRenderMappedValue_IsNotOKWhenItsInputIsAbsent is the query/header
// optionality contract: a mapping entry whose input the caller omitted must
// be dropped entirely by the caller (buildToolQuery/buildToolHeaders in
// facade.go), not sent as an empty string or the literal "{input.x}" token.
func TestRenderMappedValue_IsNotOKWhenItsInputIsAbsent(t *testing.T) {
	rendered, ok := execution.RenderMappedValue("{input.select}", map[string]any{}, nil)

	if ok {
		t.Fatalf("ok = true, want false when the input is absent (rendered = %q)", rendered)
	}
	if rendered != "" {
		t.Errorf("rendered = %q, want empty when not ok", rendered)
	}
}

func TestRenderMappedValue_ResolvesAParamsTokenAgainstTheParamsBag(t *testing.T) {
	rendered, ok := execution.RenderMappedValue("{params.orgId}", map[string]any{}, map[string]any{"orgId": "org-1"})

	if !ok {
		t.Fatal("ok = false, want true when the param is supplied")
	}
	if rendered != "org-1" {
		t.Errorf("rendered = %q, want %q", rendered, "org-1")
	}
}

// TestRenderMappedValue_IsNotOKForAParamsTokenWhenParamsIsNil pins today's
// wiring (Slice 1: Facade.callProvider always passes a nil params bag —
// expected params arrive in Slice 3): a {params.x} mapping entry is simply
// dropped, the same as any other unsupplied optional value.
func TestRenderMappedValue_IsNotOKForAParamsTokenWhenParamsIsNil(t *testing.T) {
	rendered, ok := execution.RenderMappedValue("{params.orgId}", map[string]any{}, nil)

	if ok {
		t.Fatalf("ok = true, want false when params is nil (rendered = %q)", rendered)
	}
}

// --- RenderPollTemplate ({config.x}/{watermark}, Slice 4, PD28/PD34): a
// poll mapping's own template engine, distinct from RenderPath/
// RenderMappedValue above in that a {watermark}/{config.x} token may sit
// embedded inside a larger literal (Outlook's OData filter), not just as a
// whole-value token. execution/poll_test.go's FetchTriggerRecords tests
// exercise this indirectly through a full poll mapping; these pin the
// template engine itself in isolation. ---

func TestRenderPollTemplate_SubstitutesAWatermarkTokenEmbeddedInsideALargerLiteral(t *testing.T) {
	got, err := execution.RenderPollTemplate("receivedDateTime gt {watermark}", nil, "2026-01-01T00:00:00Z", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "receivedDateTime gt 2026-01-01T00:00:00Z" {
		t.Errorf("rendered = %q, want the watermark substituted in place", got)
	}
}

func TestRenderPollTemplate_SubstitutesAConfigTokenFromTheMergedConfigMap(t *testing.T) {
	got, err := execution.RenderPollTemplate("/me/mailFolders/{config.folderId}/messages", map[string]any{"folderId": "Inbox"}, "2026-01-01T00:00:00Z", true)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/me/mailFolders/Inbox/messages" {
		t.Errorf("rendered = %q, want the config value substituted", got)
	}
}

func TestRenderPollTemplate_EscapesAConfigValueOnlyWhenEscapePathSegmentsIsTrue(t *testing.T) {
	config := map[string]any{"folderId": "My Folder"}

	escaped, err := execution.RenderPollTemplate("/me/mailFolders/{config.folderId}/messages", config, "", true)
	if err != nil {
		t.Fatalf("unexpected error (escaped): %v", err)
	}
	if escaped != "/me/mailFolders/My%20Folder/messages" {
		t.Errorf("escaped rendered = %q, want the space URL-escaped", escaped)
	}

	unescaped, err := execution.RenderPollTemplate("value eq {config.folderId}", config, "", false)
	if err != nil {
		t.Fatalf("unexpected error (unescaped): %v", err)
	}
	if unescaped != "value eq My Folder" {
		t.Errorf("unescaped rendered = %q, want the literal value, not escaped", unescaped)
	}
}

func TestRenderPollTemplate_AConfigTokenNamingAKeyConfigDoesNotCarryIsAnError(t *testing.T) {
	_, err := execution.RenderPollTemplate("/me/mailFolders/{config.folderId}/messages", map[string]any{}, "2026-01-01T00:00:00Z", true)

	if err == nil {
		t.Fatal("expected an error for a {config.x} token config does not carry, got nil")
	}
	if !strings.Contains(err.Error(), "config.folderId") {
		t.Errorf("error = %q, want it to name the missing token", err.Error())
	}
}
