package membraneimport

import (
	"strings"
	"testing"
)

// schemaProperty reaches into a loaded tool's InputSchema for one named
// property, failing the test with the schema's shape if the path isn't a map
// all the way down — so a broken fixture fails loudly rather than a nil
// dereference.
func schemaProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema.properties is not a map: %#v", schema["properties"])
	}
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema.properties[%q] is not a map: %#v", name, properties[name])
	}
	return property
}

// TestConvert_FirstNotEmptyBecomesInputSchemaDefaultAndPartialCaveat is Slice
// 2's $firstNotEmpty AC: {$firstNotEmpty: [{$var: $.input.folderId}, "Inbox"]}
// still inlines the path token like a plain $var would, but also infers the
// literal as the input's inputSchema default, and the report notes the
// default was inferred (a partial conversion, not a clean one).
func TestConvert_FirstNotEmptyBecomesInputSchemaDefaultAndPartialCaveat(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-firstnotempty-default.yaml"),
	}
	tool := loadOneEmittedTool(t, files)

	const wantPath = "/me/mailFolders/{input.folderId}/messages"
	if tool.Path != wantPath {
		t.Errorf("Path = %q, want %q", tool.Path, wantPath)
	}

	property := schemaProperty(t, tool.InputSchema, "folderId")
	if got := property["default"]; got != "Inbox" {
		t.Errorf(`inputSchema.properties.folderId.default = %v, want "Inbox"`, got)
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	partialSection := reportSection(t, string(result.Report), "Partial")
	if !strings.Contains(partialSection, "action-firstnotempty-default.yaml") {
		t.Errorf("Partial section missing the tool's source file:\n%s", partialSection)
	}
	if !strings.Contains(partialSection, "inferred") || !strings.Contains(partialSection, "Inbox") {
		t.Errorf("Partial section caveat does not note the inferred default:\n%s", partialSection)
	}
}
