package membraneimport

import (
	"strings"
	"testing"
)

// findSkippedBySource locates one SkippedItem by its Source file name.
func findSkippedBySource(skipped []SkippedItem, source string) (SkippedItem, bool) {
	for _, item := range skipped {
		if item.Source == source {
			return item, true
		}
	}
	return SkippedItem{}, false
}

// TestConvert_SkipsAnActionFileMissingIntegrationUuidNamingFileAndField is
// Slice 1's skip AC (a): an action export with no integrationUuid cannot be
// grouped, so it is recorded in Result.Skipped naming the file and the
// missing field, and Convert returns no error for it.
func TestConvert_SkipsAnActionFileMissingIntegrationUuidNamingFileAndField(t *testing.T) {
	result, err := Convert([]SourceFile{loadTestdataFile(t, "action-missing-integration-uuid.yaml")})
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	item, ok := findSkippedBySource(result.Skipped, "action-missing-integration-uuid.yaml")
	if !ok {
		t.Fatalf("Skipped = %+v, want an entry naming %q", result.Skipped, "action-missing-integration-uuid.yaml")
	}
	if !strings.Contains(item.Reason, "integrationUuid") {
		t.Errorf("Reason = %q, want it to name the missing field %q", item.Reason, "integrationUuid")
	}
	if len(result.Providers) != 0 {
		t.Errorf("Providers = %+v, want none — the only input file could not be grouped", result.Providers)
	}
}

// TestConvert_SkipsAnIntegrationFileMissingItsOwnUuidNamingFileAndField is
// Slice 1's skip AC (b): an integration export with no own "uuid" field
// cannot be grouped either, and is likewise recorded rather than silently
// dropped.
func TestConvert_SkipsAnIntegrationFileMissingItsOwnUuidNamingFileAndField(t *testing.T) {
	result, err := Convert([]SourceFile{loadTestdataFile(t, "integration-missing-uuid.yaml")})
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	item, ok := findSkippedBySource(result.Skipped, "integration-missing-uuid.yaml")
	if !ok {
		t.Fatalf("Skipped = %+v, want an entry naming %q", result.Skipped, "integration-missing-uuid.yaml")
	}
	if !strings.Contains(item.Reason, "uuid") {
		t.Errorf("Reason = %q, want it to name the missing field %q", item.Reason, "uuid")
	}
	if len(result.Providers) != 0 {
		t.Errorf("Providers = %+v, want none — the only input file could not be grouped", result.Providers)
	}
}

// TestConvert_SkipsAGroupOfActionsWithNoMatchingIntegrationNamingTheReason
// covers the "or vice versa" half of the skip-and-continue AC: an action's
// integrationUuid that names no integration record in this run is a
// specific, reported reason ("no integration record found for
// integrationUuid ..."), not a silent drop, and the file that names the
// group is the one surfaced.
func TestConvert_SkipsAGroupOfActionsWithNoMatchingIntegrationNamingTheReason(t *testing.T) {
	result, err := Convert([]SourceFile{loadTestdataFile(t, "orphan-action.yaml")})
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	item, ok := findSkippedBySource(result.Skipped, "orphan-action.yaml")
	if !ok {
		t.Fatalf("Skipped = %+v, want an entry naming %q", result.Skipped, "orphan-action.yaml")
	}
	wantReason := "no integration record found for integrationUuid no-such-integration-uuid"
	if item.Reason != wantReason {
		t.Errorf("Reason = %q, want %q", item.Reason, wantReason)
	}
	if len(result.Providers) != 0 {
		t.Errorf("Providers = %+v, want none — the group has no integration record to build from", result.Providers)
	}
}

// TestConvert_StillEmitsWellFormedGroupsWhenOtherFilesAreSkipped is the
// "run continues" half of the AC: one malformed input file must not prevent
// an otherwise-complete group in the same run from being converted.
func TestConvert_StillEmitsWellFormedGroupsWhenOtherFilesAreSkipped(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
		loadTestdataFile(t, "action-missing-integration-uuid.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1 — the well-formed group must still convert", len(result.Providers))
	}
	if result.Providers[0].Slug != "test-crm" {
		t.Errorf("Providers[0].Slug = %q, want %q", result.Providers[0].Slug, "test-crm")
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("len(Skipped) = %d, want 1 (only the malformed action file)", len(result.Skipped))
	}
	if result.Skipped[0].Source != "action-missing-integration-uuid.yaml" {
		t.Errorf("Skipped[0].Source = %q, want %q", result.Skipped[0].Source, "action-missing-integration-uuid.yaml")
	}
}

// TestConvert_ReportListsBothConvertedAndSkippedItemsWithReasons pins the
// report artifact itself: it must name the converted tool (source + provider
// slug + tool slug) and the skipped file with its specific reason, plus a
// summary count line — the operator's map of what still needs a human.
func TestConvert_ReportListsBothConvertedAndSkippedItemsWithReasons(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
		loadTestdataFile(t, "action-missing-integration-uuid.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	report := string(result.Report)
	if !strings.Contains(report, "action-get-record.yaml") || !strings.Contains(report, "test-crm-get-record") || !strings.Contains(report, "test-crm") {
		t.Errorf("report's Converted section missing the converted tool's source/provider/tool slug:\n%s", report)
	}
	if !strings.Contains(report, "action-missing-integration-uuid.yaml") {
		t.Errorf("report missing the skipped file's name:\n%s", report)
	}
	if !strings.Contains(report, "integrationUuid") {
		t.Errorf("report missing the specific reason for the skip:\n%s", report)
	}
	if !strings.Contains(report, "converted: 1, skipped: 1") {
		t.Errorf("report missing the expected summary count line:\n%s", report)
	}
}
