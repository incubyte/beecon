package membraneimport

import (
	"fmt"
	"strings"
)

// reportBanner opens every report with the same IP/legal-stance banner every
// emitted definition carries (spec: "not optional").
const reportBanner = "" +
	"# Beecon Membrane Import Report\n\n" +
	"This output is machine-scaffolded and MUST be reviewed and completed by a\n" +
	"human before any converted provider goes live. Every OAuth authorizeUrl/\n" +
	"tokenUrl/userInfoUrl/scopes and mapping.baseUrl value below — whether\n" +
	"filled from the known-provider preset table or left as a TODO placeholder —\n" +
	"must be confirmed against the provider's own developer documentation\n" +
	"before use.\n"

// partialSectionOAuthNote restates the banner's OAuth/baseUrl confirmation
// requirement at the top of the Partial section itself (AC6 names both the
// banner and the Partial section as carrying this statement).
const partialSectionOAuthNote = "" +
	"All OAuth/baseUrl values (preset-matched or TODO placeholder) must be\n" +
	"confirmed against the provider's own developer documentation before use.\n"

// partialItem is one Membrane record this run converted but with one or more
// dropped or defaulted constructs (Slice 2): the report's Partial section
// names, per tool, exactly which constructs were affected, alongside the
// same source+slug identity a convertedItem carries.
type partialItem struct {
	Source       string
	ProviderSlug string
	ItemSlug     string
	Kind         string // "tool" (Slice 2); "provider" (Slice 3); "trigger" (Slice 5).
	Caveats      []string
}

// buildReport renders the Markdown conversion report: a Converted section
// (a clean translation), a Partial section (converted with caveats — Slice
// 2's dropped $case guards and inferred $firstNotEmpty defaults, plus Slice
// 3's provider-level TODO OAuth/baseUrl fallback fields), a Skipped section
// (naming every file and the specific reason it did not convert at all —
// never a silent drop), and a summary count line.
//
// This is Slice 4's whole contract: three clearly separated sections, each
// item naming its source+slug(s) plus (for Partial/Skipped) a specific
// reason, a converted/partial/skipped summary line, and the opening IP/
// review banner — all of which Slices 1-3 already built incrementally, one
// section and caveat kind at a time. Slice 4 adds no new section or field;
// it is the point at which the whole report is asserted item-by-item as one
// artifact.
func buildReport(converted []convertedItem, partial []partialItem, skipped []SkippedItem) []byte {
	var report strings.Builder
	report.WriteString(reportBanner)

	report.WriteString("\n## Converted\n\n")
	writeConvertedItems(&report, converted)

	report.WriteString("\n## Partial\n\n")
	report.WriteString(partialSectionOAuthNote + "\n")
	writePartialItems(&report, partial)

	report.WriteString("\n## Skipped\n\n")
	writeSkippedItems(&report, skipped)

	fmt.Fprintf(&report, "\n## Summary\n\nconverted: %d, skipped: %d, partial: %d\n", len(converted), len(skipped), len(partial))
	return []byte(report.String())
}

func writeConvertedItems(report *strings.Builder, converted []convertedItem) {
	if len(converted) == 0 {
		report.WriteString("(none)\n")
		return
	}
	for _, item := range converted {
		fmt.Fprintf(report, "- %s %q — provider %q, source %s\n", item.Kind, item.ItemSlug, item.ProviderSlug, item.Source)
	}
}

func writePartialItems(report *strings.Builder, partial []partialItem) {
	if len(partial) == 0 {
		report.WriteString("(none)\n")
		return
	}
	for _, item := range partial {
		fmt.Fprintf(report, "- %s %q — provider %q, source %s\n", item.Kind, item.ItemSlug, item.ProviderSlug, item.Source)
		for _, caveat := range item.Caveats {
			fmt.Fprintf(report, "  - %s\n", caveat)
		}
	}
}

func writeSkippedItems(report *strings.Builder, skipped []SkippedItem) {
	if len(skipped) == 0 {
		report.WriteString("(none)\n")
		return
	}
	for _, item := range skipped {
		fmt.Fprintf(report, "- %s: %s\n", item.Source, item.Reason)
	}
}
