package membraneimport

import (
	"strings"
	"testing"
)

// TestConvert_ReportConsolidatesConvertedPartialAndSkippedItemsWithBannerAndSummary
// is Slice 4's AC7 — the sole Slice-4-owned test. Slices 1-3 already built
// each section and caveat kind incrementally, one rule at a time (see the
// per-rule tests in this package); this test is the point the WHOLE report
// is asserted together, item-by-item, as one artifact: the opening IP/review
// banner, a Converted item, a Partial item with its exact caveat text, a
// Skipped item with its exact reason, correct section separation (an item
// never leaks into a section it doesn't belong to), and a summary line whose
// counts match the fixture set.
//
// The fixture set spans all three outcomes from the spec's own anchor
// samples, all sharing one integrationUuid (grp-test-crm-uuid) so a single
// Convert run produces a single provider carrying all three dispositions:
//   - action-get-record.yaml            -> Converted (clean $var-only mapping)
//   - action-case-path.yaml             -> Partial (mirrors temp/outlook-get-message.yaml's
//     dropped single-fallback $case guard)
//   - action-case-path-genuine-branching.yaml -> Skipped (genuine multi-branch $case)
func TestConvert_ReportConsolidatesConvertedPartialAndSkippedItemsWithBannerAndSummary(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
		loadTestdataFile(t, "action-case-path.yaml"),
		loadTestdataFile(t, "action-case-path-genuine-branching.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	report := string(result.Report)

	if !strings.HasPrefix(report, reportBanner) {
		t.Fatalf("report does not open with the IP/review banner:\n%s", report)
	}

	converted := reportSection(t, report, "Converted")
	for _, want := range []string{"action-get-record.yaml", "test-crm-get-record", `provider "test-crm"`} {
		if !strings.Contains(converted, want) {
			t.Errorf("Converted section missing %q:\n%s", want, converted)
		}
	}

	partial := reportSection(t, report, "Partial")
	for _, want := range []string{
		"action-case-path.yaml",
		"outlook_get_message",
		`provider "test-crm"`,
		"dropped $case branch",
		"integration.yaml",
		"oauth.authorizeUrl is a TODO placeholder",
		"mapping.baseUrl is a TODO placeholder",
	} {
		if !strings.Contains(partial, want) {
			t.Errorf("Partial section missing %q:\n%s", want, partial)
		}
	}

	skipped := reportSection(t, report, "Skipped")
	const skipReason = "conditional mapping has no Beecon equivalent"
	if !strings.Contains(skipped, "action-case-path-genuine-branching.yaml") || !strings.Contains(skipped, skipReason) {
		t.Errorf("Skipped section missing the source file and exact reason:\n%s", skipped)
	}

	// Section separation: each item's source appears ONLY under its own
	// section — a Skipped source must never read as Converted, etc.
	if strings.Contains(converted, "action-case-path-genuine-branching.yaml") {
		t.Errorf("Skipped source leaked into Converted section:\n%s", converted)
	}
	if strings.Contains(converted, "action-case-path.yaml") {
		t.Errorf("Partial source leaked into Converted section:\n%s", converted)
	}
	if strings.Contains(skipped, "action-get-record.yaml") {
		t.Errorf("Converted source leaked into Skipped section:\n%s", skipped)
	}
	if strings.Contains(partial, "action-case-path-genuine-branching.yaml") {
		t.Errorf("Skipped source leaked into Partial section:\n%s", partial)
	}

	if !strings.Contains(report, "converted: 1, skipped: 1, partial: 2") {
		t.Errorf("summary line does not match the fixture set's three outcomes (1 converted, 1 skipped, 2 partial -- the tool's dropped $case guard plus the unmatched connector's provider-level TODO OAuth/baseUrl item):\n%s", report)
	}
}
