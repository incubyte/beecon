package membraneimport

import (
	"strings"
	"testing"
)

// TestConvert_ReportBannerRequiresConfirmingOAuthAgainstProviderDocs is
// Slice 3's AC6, banner half: the report's opening banner states that every
// OAuth/baseUrl value -- whether preset-filled or a TODO placeholder --
// must be confirmed against the provider's own developer documentation
// before use, not just that the output generally needs review.
func TestConvert_ReportBannerRequiresConfirmingOAuthAgainstProviderDocs(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration-hubspot.yaml"),
		loadTestdataFile(t, "action-hubspot-get-contact.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	report := string(result.Report)
	bannerEnd := strings.Index(report, "## Converted")
	if bannerEnd == -1 {
		t.Fatalf("report has no ## Converted section, cannot isolate the banner:\n%s", report)
	}
	banner := report[:bannerEnd]

	for _, want := range []string{"oauth", "baseUrl", "developer documentation"} {
		if !strings.Contains(strings.ToLower(banner), strings.ToLower(want)) {
			t.Errorf("report banner missing %q:\n%s", want, banner)
		}
	}
}

// TestConvert_PartialSectionRestatesOAuthDocConfirmationRequirement is Slice
// 3's AC6, Partial-section half: the same OAuth/baseUrl confirmation
// requirement is restated at the top of the Partial section itself, not
// left to the banner alone -- so an operator reading only the Partial
// section (where the TODO/preset caveats live) still sees it.
func TestConvert_PartialSectionRestatesOAuthDocConfirmationRequirement(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	partialSection := reportSection(t, string(result.Report), "Partial")
	for _, want := range []string{"oauth", "baseurl", "developer documentation"} {
		if !strings.Contains(strings.ToLower(partialSection), strings.ToLower(want)) {
			t.Errorf("Partial section missing %q:\n%s", want, partialSection)
		}
	}
}
