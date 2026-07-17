package membraneimport

import (
	"strings"
	"testing"
	"testing/fstest"

	"beecon/internal/catalog"
)

// loadOneEmittedTool runs Convert over the given files, requires it emitted
// exactly one provider with exactly one tool, and returns that tool loaded
// back through the real catalog loader — so every DSL-translation assertion
// below is checked against the same shape the server actually boots with,
// not against membraneimport's own intermediate representation.
func loadOneEmittedTool(t *testing.T, files []SourceFile) catalog.ProviderTool {
	t.Helper()
	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(result.Providers))
	}

	emitted := result.Providers[0]
	fsys := fstest.MapFS{emitted.Slug + ".yaml": {Data: emitted.YAML}}
	defs, err := catalog.LoadProviderDefinitions(fsys)
	if err != nil {
		t.Fatalf("emitted definition did not parse under the real loader: %v", err)
	}
	if len(defs[0].Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(defs[0].Tools))
	}
	return defs[0].Tools[0]
}

// TestConvert_TranslatesASimpleVarQueryValueToAnInputToken is Slice 1's $var
// translation AC: a Membrane query value shaped as {$var: $.input.NAME}
// becomes the whole Beecon token "{input.NAME}", not the literal Membrane
// expression.
func TestConvert_TranslatesASimpleVarQueryValueToAnInputToken(t *testing.T) {
	tool := loadOneEmittedTool(t, []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	})

	got := tool.Mapping.Query["select"]
	if got != "{input.select}" {
		t.Errorf(`Mapping.Query["select"] = %q, want %q`, got, "{input.select}")
	}
}

// TestConvert_InlinesPathParametersIntoAPlainStringPath is Slice 1's path AC:
// a Membrane action whose request.path is a plain string with a {name}
// segment bound in pathParameters to a simple {$var: $.input.NAME} value has
// that segment inlined as the Beecon token {input.NAME}.
func TestConvert_InlinesPathParametersIntoAPlainStringPath(t *testing.T) {
	tool := loadOneEmittedTool(t, []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	})

	if tool.Path != "/records/{input.recordId}" {
		t.Errorf("Path = %q, want %q", tool.Path, "/records/{input.recordId}")
	}
}

// TestConvert_SingleFallbackCasePathEmitsTheDefaultBranchAsPartial is Slice
// 2's $case rule: a single-fallback $case (one guarded branch + one plain
// default, action-case-path.yaml mirroring the real get-message action's
// /users/{userId}/... vs /me/... shape) emits the default branch's path
// (pathParameters still inlined as tokens) rather than the Slice 1
// TODO-placeholder that shape used to get, and records the dropped guard in
// the report as a partial conversion.
func TestConvert_SingleFallbackCasePathEmitsTheDefaultBranchAsPartial(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-case-path.yaml"),
	}
	tool := loadOneEmittedTool(t, files)

	const wantPath = "/me/messages/{input.messageId}"
	if tool.Path != wantPath {
		t.Errorf("Path = %q, want the default branch %q with pathParameters inlined", tool.Path, wantPath)
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	report := string(result.Report)
	if !strings.Contains(report, "## Partial") {
		t.Errorf("report missing a Partial section:\n%s", report)
	}
	if !strings.Contains(report, "dropped $case branch") {
		t.Errorf("report does not record the dropped $case guard as a partial caveat:\n%s", report)
	}
}

// TestConvert_MapsRequestMethodToMappingMethod is Slice 2's method AC: the
// Membrane action's config.request.method becomes the emitted tool's
// mapping.method verbatim.
func TestConvert_MapsRequestMethodToMappingMethod(t *testing.T) {
	tool := loadOneEmittedTool(t, []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	})

	if tool.Method != "GET" {
		t.Errorf("Method = %q, want %q (from config.request.method)", tool.Method, "GET")
	}
}

// TestConvert_InlinesMultiplePathParametersAsSeparateInputTokens extends the
// single-parameter inlining AC to a path with two path parameters
// (/users/{userId}/messages/{messageId}, mirroring the spec's example): each
// bound $var pathParameter becomes its own {input.NAME} token, independently
// of the others.
func TestConvert_InlinesMultiplePathParametersAsSeparateInputTokens(t *testing.T) {
	tool := loadOneEmittedTool(t, []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-multi-path-params.yaml"),
	})

	const wantPath = "/users/{input.userId}/messages/{input.messageId}"
	if tool.Path != wantPath {
		t.Errorf("Path = %q, want %q", tool.Path, wantPath)
	}
}

// TestConvert_SingleFallbackCasePathMapsMethodAndQueryAndIsClassifiedPartial
// deepens the single-fallback $case coverage on the real anchor fixture
// (action-case-path.yaml, copied verbatim from temp/outlook-get-message.yaml
// with only integrationUuid rewritten to match this package's synthetic
// integration record): beyond the path, the method and query mapping must
// still translate normally, the dropped guard's caveat must name the input it
// tested (userId), and the tool must be classified Partial — listed under the
// report's Partial section, not its Converted section.
func TestConvert_SingleFallbackCasePathMapsMethodAndQueryAndIsClassifiedPartial(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-case-path.yaml"),
	}
	tool := loadOneEmittedTool(t, files)

	if tool.Method != "GET" {
		t.Errorf("Method = %q, want %q", tool.Method, "GET")
	}
	if got := tool.Mapping.Query["$select"]; got != "{input.select}" {
		t.Errorf(`Mapping.Query["$select"] = %q, want %q`, got, "{input.select}")
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	report := string(result.Report)

	partialSection := reportSection(t, report, "Partial")
	if !strings.Contains(partialSection, "action-case-path.yaml") {
		t.Errorf("Partial section missing the tool's source file:\n%s", partialSection)
	}
	if !strings.Contains(partialSection, "input.userId") {
		t.Errorf("Partial section caveat does not name the dropped guard's input (userId):\n%s", partialSection)
	}

	convertedSection := reportSection(t, report, "Converted")
	if strings.Contains(convertedSection, "action-case-path.yaml") {
		t.Errorf("tool with a dropped $case guard must not be listed as cleanly Converted:\n%s", convertedSection)
	}
}
