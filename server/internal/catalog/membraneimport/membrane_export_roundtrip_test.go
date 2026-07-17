package membraneimport

import (
	"testing"
	"testing/fstest"

	"beecon/internal/catalog"
)

// TestConvert_EmitsProviderDefinitionThatParsesUnderTheRealLoader is the
// load-bearing AC for Slice 1: the importer's whole value proposition is
// that its output is real, loadable Beecon provider-definition YAML, not
// merely well-formed-looking YAML. It feeds Convert's output through the
// production catalog.LoadProviderDefinitions (the same strict, KnownFields
// decode the server boots with) scoped to an fstest.MapFS containing ONLY
// the emitted definition bytes — not the report, since the loader treats
// every file under fsys as a definition.
func TestConvert_EmitsProviderDefinitionThatParsesUnderTheRealLoader(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(result.Providers))
	}

	emitted := result.Providers[0]
	fsys := fstest.MapFS{
		emitted.Slug + ".yaml": {Data: emitted.YAML},
	}

	defs, err := catalog.LoadProviderDefinitions(fsys)
	if err != nil {
		t.Fatalf("emitted definition did not parse under the real loader: %v\n--- emitted YAML ---\n%s", err, emitted.YAML)
	}
	if len(defs) != 1 {
		t.Fatalf("len(defs) = %d, want 1", len(defs))
	}

	def := defs[0]
	if def.Slug != "test-crm" {
		t.Errorf("Slug = %q, want %q", def.Slug, "test-crm")
	}
	if def.Name != "Test CRM" {
		t.Errorf("Name = %q, want %q", def.Name, "Test CRM")
	}
	if len(def.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(def.Tools))
	}

	tool := def.Tools[0]
	if len(tool.InputSchema) == 0 {
		t.Error("tool.InputSchema is empty, want the Membrane action's inputSchema copied through")
	}
	if len(tool.OutputSchema) == 0 {
		t.Error("tool.OutputSchema is empty, want the Membrane action's customOutputSchema copied through")
	}
}
