// Package catalog_test (continues definition_test.go's package and helpers:
// validDefinitionYAML, mapFSWithFile, yamlWithoutLineContaining). This file
// covers the finalized definition format's v1-specific shape (PD13, slice 1):
// the full mapping block (baseUrl, path/method/query/header/body,
// pagination, deprecated), formatVersion dispatch failures naming the found
// and supported versions, per-tool input/output schema requirements, strict
// (KnownFields) YAML rejecting typos, and the reserved triggers key being
// accepted and ignored.
package catalog_test

import (
	"strings"
	"testing"

	"beecon/internal/catalog"
)

const validV1MappingYAML = `
formatVersion: 1
slug: outlook
name: Outlook
logo: https://static.beecon.dev/providers/outlook.png
authScheme: oauth2
oauth:
  authorizeUrl: https://example.com/authorize
  tokenUrl: https://example.com/token
  scopes:
    - offline_access
mapping:
  baseUrl: https://graph.microsoft.com/v1.0
tools:
  - slug: outlook-get-message
    name: Get email message
    description: Retrieves a message by its id.
    deprecated: true
    inputSchema:
      type: object
      properties:
        messageId:
          type: string
    outputSchema:
      type: object
      properties:
        id:
          type: string
    mapping:
      method: GET
      path: /me/messages/{input.messageId}
      query:
        $select: "{input.select}"
      header:
        Prefer: "{input.preference}"
      body:
        note: "{input.note}"
      pagination:
        pageSizeParam: top
        cursorParam: skip
        nextCursorPath: "@odata.nextLink"
`

func TestLoadProviderDefinitions_ParsesTheFullMappingBlockPathMethodQueryHeaderBodyPaginationAndDeprecated(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validV1MappingYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := defs[0]
	if d.BaseURL != "https://graph.microsoft.com/v1.0" {
		t.Errorf("BaseURL = %q, want %q", d.BaseURL, "https://graph.microsoft.com/v1.0")
	}
	if len(d.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(d.Tools))
	}
	tool := d.Tools[0]
	if tool.Method != "GET" {
		t.Errorf("Method = %q, want %q", tool.Method, "GET")
	}
	if tool.Path != "/me/messages/{input.messageId}" {
		t.Errorf("Path = %q, want %q", tool.Path, "/me/messages/{input.messageId}")
	}
	if !tool.Deprecated {
		t.Error("Deprecated = false, want true")
	}
	if got := tool.Mapping.Query["$select"]; got != "{input.select}" {
		t.Errorf("Mapping.Query[$select] = %q, want %q", got, "{input.select}")
	}
	if got := tool.Mapping.Header["Prefer"]; got != "{input.preference}" {
		t.Errorf("Mapping.Header[Prefer] = %q, want %q", got, "{input.preference}")
	}
	if got := tool.Mapping.Body["note"]; got != "{input.note}" {
		t.Errorf("Mapping.Body[note] = %q, want %q", got, "{input.note}")
	}
	if tool.Mapping.Pagination == nil {
		t.Fatal("Mapping.Pagination = nil, want the declared pagination block")
	}
	if tool.Mapping.Pagination.PageSizeParam != "top" {
		t.Errorf("Pagination.PageSizeParam = %q, want %q", tool.Mapping.Pagination.PageSizeParam, "top")
	}
	if tool.Mapping.Pagination.CursorParam != "skip" {
		t.Errorf("Pagination.CursorParam = %q, want %q", tool.Mapping.Pagination.CursorParam, "skip")
	}
	if tool.Mapping.Pagination.NextCursorPath != "@odata.nextLink" {
		t.Errorf("Pagination.NextCursorPath = %q, want %q", tool.Mapping.Pagination.NextCursorPath, "@odata.nextLink")
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileFoundVersionAndSupportedVersionWhenFormatVersionIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "formatVersion:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: formatVersion 0 is not supported (supported: 1)`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileFoundVersionAndSupportedVersionWhenFormatVersionIsUnsupported(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "formatVersion: 1", "formatVersion: 2", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: formatVersion 2 is not supported (supported: 1)`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolIsMissingItsInputSchema(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "    inputSchema:\n      type: object\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].inputSchema" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolIsMissingItsOutputSchema(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "    outputSchema:\n      type: object\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].outputSchema" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenMappingBaseURLIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "baseUrl:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "mapping.baseUrl" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolsMappingMethodIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "method: GET")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].mapping.method" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolsMappingPathIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "path: /v1.0/me/messages")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].mapping.path" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_RejectsAnUnknownTopLevelKey proves strict
// (KnownFields) YAML decoding: a typo'd top-level field must fail boot
// instead of silently being ignored.
func TestLoadProviderDefinitions_RejectsAnUnknownTopLevelKey(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "slug: outlook\n", "slug: outlook\nslugg: outlook\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	if err == nil {
		t.Fatal("expected an error for the unknown top-level field \"slugg\", got nil")
	}
	if !strings.Contains(err.Error(), "slugg") {
		t.Errorf("error = %q, want it to name the unknown field %q", err.Error(), "slugg")
	}
}

// TestLoadProviderDefinitions_RejectsAnUnknownKeyNestedInsideATool proves
// strict decoding applies recursively, not just at the top level.
func TestLoadProviderDefinitions_RejectsAnUnknownKeyNestedInsideATool(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML,
		"    mapping:\n      method: GET\n",
		"    notAField: typo\n    mapping:\n      method: GET\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	if err == nil {
		t.Fatal("expected an error for the unknown tool-level field \"notAField\", got nil")
	}
	if !strings.Contains(err.Error(), "notAField") {
		t.Errorf("error = %q, want it to name the unknown field %q", err.Error(), "notAField")
	}
}

// TestLoadProviderDefinitions_AcceptsAndIgnoresTheReservedTriggersKey is
// PD13/section-3: a Phase 3 definition may already carry a triggers block —
// its contents are deliberately unvalidated, so even a nonsensical shape
// must not fail boot.
func TestLoadProviderDefinitions_AcceptsAndIgnoresTheReservedTriggersKey(t *testing.T) {
	withTriggers := validDefinitionYAML + "\ntriggers:\n  anything: goes-here\n  nested:\n    - 1\n    - 2\n"

	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", withTriggers))

	if err != nil {
		t.Fatalf("unexpected error with a reserved triggers block present: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("len(defs) = %d, want 1", len(defs))
	}
}
