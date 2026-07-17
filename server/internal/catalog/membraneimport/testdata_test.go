package membraneimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"beecon/internal/catalog"
)

// loadTestdataFile reads one fixture under testdata/ into a SourceFile, the
// same shape the CLI shell hands Convert after reading a real Membrane export
// directory from disk. Used by 3+ tests across this package's test files, so
// it is extracted here rather than duplicated per test file.
func loadTestdataFile(t *testing.T, name string) SourceFile {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return SourceFile{Name: name, Content: content}
}

// reportSection returns the body of one "## <header>" section of a rendered
// report (up to, but not including, the next "## " header) — used by 3+
// tests across this package's test files to assert an item appears under one
// specific section (e.g. Partial) and not another (e.g. Converted) without
// each test re-implementing the same string-slicing.
func reportSection(t *testing.T, report, header string) string {
	t.Helper()
	marker := "## " + header
	start := strings.Index(report, marker)
	if start == -1 {
		t.Fatalf("report missing section %q:\n%s", header, report)
	}
	rest := report[start+len(marker):]
	if end := strings.Index(rest, "\n## "); end != -1 {
		return rest[:end]
	}
	return rest
}

// loadSingleEmittedDefinition feeds a Convert result's one emitted provider
// definition through the real catalog.LoadProviderDefinitions (the same
// strict, KnownFields decode the server boots with), the same way the
// Slice 1 round-trip test does — extracted here since 3+ Slice 3 tests
// across this package each need one emitted definition loaded and parsed
// this way. Fails the test outright (not just reports) if Convert did not
// produce exactly one provider or the emitted YAML does not parse, since
// every assertion after this call assumes a loaded definition exists.
func loadSingleEmittedDefinition(t *testing.T, result Result) catalog.ProviderDefinition {
	t.Helper()
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
	return defs[0]
}
