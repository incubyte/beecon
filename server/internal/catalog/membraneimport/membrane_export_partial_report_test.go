package membraneimport

import (
	"strings"
	"testing"
)

// TestConvert_ReportSummaryLineCountsConvertedSkippedAndPartialSeparately is
// Slice 2's summary-line AC: over a run that produces one cleanly Converted
// tool, one Partial tool (a dropped $case guard), and one Skipped file, the
// summary line reports all three counts — extending Slice 1's
// "converted: N, skipped: N" line with the new partial count rather than
// replacing it.
func TestConvert_ReportSummaryLineCountsConvertedSkippedAndPartialSeparately(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),               // converted cleanly
		loadTestdataFile(t, "action-case-path.yaml"),                // partial: dropped $case guard
		loadTestdataFile(t, "action-missing-integration-uuid.yaml"), // skipped: ungroupable
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	report := string(result.Report)
	// Slice 1's original substring assertion must still hold: adding the
	// partial count must not have replaced or reordered the existing fields.
	if !strings.Contains(report, "converted: 1, skipped: 1") {
		t.Errorf("report summary regressed Slice 1's converted/skipped counts:\n%s", report)
	}
	// partial: 2, not 1 — "integration.yaml"'s connector ("test-crm") matches
	// no Slice 3 preset, so it contributes its own provider-level TODO
	// OAuth/baseUrl Partial item alongside action-case-path.yaml's dropped
	// $case guard.
	if !strings.Contains(report, "converted: 1, skipped: 1, partial: 2") {
		t.Errorf("report summary missing the partial count:\n%s", report)
	}
}

// TestConvert_ReportPartialSectionNamesSourceSlugAndEachCaveat is Slice 2's
// Partial-section-shape AC: each Partial item names its source file, the
// resulting provider/tool slug, and every specific caveat that was dropped or
// defaulted — not just that something was partial.
func TestConvert_ReportPartialSectionNamesSourceSlugAndEachCaveat(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-case-path.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	partialSection := reportSection(t, string(result.Report), "Partial")
	for _, want := range []string{
		"action-case-path.yaml", // source file
		"test-crm",              // provider slug
		"outlook_get_message",   // tool slug (the action's own key)
		"dropped $case branch",  // the specific caveat
	} {
		if !strings.Contains(partialSection, want) {
			t.Errorf("Partial section missing %q:\n%s", want, partialSection)
		}
	}
}
