package membraneimport

import (
	"strings"
	"testing"
)

// TestConvert_UnsupportedConstructsAreDroppedNeverEmittedAsLiteralDollarStrings
// is Slice 2's catch-all AC: an $and/isNot/isNotEmpty/$eval construct that is
// not part of a handled single-fallback $case must not survive translation as
// a literal `$`-string anywhere in the mapping. Instead, the affected query
// field is dropped entirely, the tool still emits (Partial, not skipped —
// the rest of the mapping is fine), and the report names the specific
// construct for each dropped field.
func TestConvert_UnsupportedConstructsAreDroppedNeverEmittedAsLiteralDollarStrings(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-unsupported-predicate.yaml"),
	}
	tool := loadOneEmittedTool(t, files)

	if _, present := tool.Mapping.Query["debugFlag"]; present {
		t.Errorf("Mapping.Query = %+v, want no debugFlag key — its $eval value is unsupported", tool.Mapping.Query)
	}
	if _, present := tool.Mapping.Query["priorityFlag"]; present {
		t.Errorf("Mapping.Query = %+v, want no priorityFlag key — its $and value is unsupported", tool.Mapping.Query)
	}
	for key, value := range tool.Mapping.Query {
		if strings.Contains(value, "$") {
			t.Errorf("Mapping.Query[%q] = %q, must never contain a literal $-prefixed construct", key, value)
		}
	}
	if strings.Contains(tool.Path, "$") {
		t.Errorf("Path = %q, must never contain a literal $-prefixed construct", tool.Path)
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	partialSection := reportSection(t, string(result.Report), "Partial")
	if !strings.Contains(partialSection, "action-unsupported-predicate.yaml") {
		t.Errorf("Partial section missing the tool's source file:\n%s", partialSection)
	}
	if !strings.Contains(partialSection, "$eval") {
		t.Errorf("Partial section caveat does not name the unsupported $eval construct:\n%s", partialSection)
	}
	if !strings.Contains(partialSection, "$and") {
		t.Errorf("Partial section caveat does not name the unsupported $and construct:\n%s", partialSection)
	}
}
