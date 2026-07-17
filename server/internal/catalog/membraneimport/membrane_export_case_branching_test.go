package membraneimport

import (
	"strings"
	"testing"
	"testing/fstest"

	"beecon/internal/catalog"
)

// TestConvert_CasePathWithThreeSubstantiveBranchesSkipsToolWithExactReason is
// Slice 2's other $case rule: a $case with two or more substantive value
// branches (here, three: team-scoped, user-scoped, and a global default) is
// genuine branching logic with no Beecon equivalent — unlike the
// single-fallback shape, it must not emit a guessed default path. The whole
// tool is skipped with the reason the spec names verbatim.
func TestConvert_CasePathWithThreeSubstantiveBranchesSkipsToolWithExactReason(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-case-path-genuine-branching.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	item, ok := findSkippedBySource(result.Skipped, "action-case-path-genuine-branching.yaml")
	if !ok {
		t.Fatalf("Skipped = %+v, want an entry naming %q", result.Skipped, "action-case-path-genuine-branching.yaml")
	}
	const wantReason = "conditional mapping has no Beecon equivalent"
	if item.Reason != wantReason {
		t.Errorf("Reason = %q, want exactly %q", item.Reason, wantReason)
	}

	report := string(result.Report)
	skippedSection := reportSection(t, report, "Skipped")
	if !strings.Contains(skippedSection, "action-case-path-genuine-branching.yaml") || !strings.Contains(skippedSection, wantReason) {
		t.Errorf("Skipped section missing the file and exact reason:\n%s", skippedSection)
	}
}

// TestConvert_ProviderStillEmittedWhenItsOnlyActionIsGenuineBranching proves
// a genuinely-branching $case only skips its own tool, not the whole
// provider: the group's integration record still yields a provider
// definition (with zero tools) that still parses through the real loader —
// mirroring Slice 1's "one bad input doesn't fail the run" guarantee, applied
// to a Slice 2 conversion failure rather than a grouping failure.
func TestConvert_ProviderStillEmittedWhenItsOnlyActionIsGenuineBranching(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-case-path-genuine-branching.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1 (the provider identity still converts)", len(result.Providers))
	}

	emitted := result.Providers[0]
	fsys := fstest.MapFS{emitted.Slug + ".yaml": {Data: emitted.YAML}}
	defs, err := catalog.LoadProviderDefinitions(fsys)
	if err != nil {
		t.Fatalf("emitted definition did not parse under the real loader: %v", err)
	}
	if len(defs[0].Tools) != 0 {
		t.Errorf("len(Tools) = %d, want 0 (the only action skipped)", len(defs[0].Tools))
	}
}
