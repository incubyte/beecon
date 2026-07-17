// Package membraneimport converts Membrane (integration.app) connector-export
// YAML into Beecon formatVersion:1 provider-definition YAML, plus a
// human-readable conversion report. It is a delivery-independent, pure
// []byte-in/[]byte-out transform (spec:
// docs/specs/beecon-phase-5-membrane-importer-spec.md) — the CLI shell in
// cmd/beecon owns all file I/O and calls Convert.
//
// The importer is a migration aid, not a runtime path: every emitted
// definition and the report itself are machine-scaffolded and must be
// reviewed and completed by a human before any provider goes live.
package membraneimport

import "fmt"

// SourceFile is one Membrane export file the caller has already read from
// disk: Name is used only for error messages and the report, never for
// grouping (grouping is by shared integrationUuid, not file name).
type SourceFile struct {
	Name    string
	Content []byte
}

// ProviderOutput is one emitted Beecon provider-definition file: Slug names
// the file the CLI shell writes it under (<slug>.yaml), YAML is the rendered
// formatVersion:1 definition.
type ProviderOutput struct {
	Slug string
	YAML []byte
}

// SkippedItem is one input file (or group) Convert could not turn into a
// provider definition: Source names the file, Reason is a specific,
// human-readable cause (never a silent drop).
type SkippedItem struct {
	Source string
	Reason string
}

// Result is everything one Convert run produces: the emitted provider
// definitions, every skipped item (surfaced by the CLI as it runs and in the
// report), and the rendered Markdown report.
type Result struct {
	Providers []ProviderOutput
	Skipped   []SkippedItem
	Report    []byte
}

// Convert groups files that share a Membrane integrationUuid into providers
// and emits one Beecon provider-definition YAML per group, plus a
// human-readable report. It never fails the whole run over one bad input
// file: an unreadable, unrecognized, or ungroupable file is recorded as a
// SkippedItem and the run continues. Convert returns an error only when the
// output itself cannot be produced at all (there is no such case in Slice 1;
// the return keeps the signature stable as later slices add fallible steps).
func Convert(files []SourceFile) (Result, error) {
	groups, skipped := groupRecords(files)

	var providers []ProviderOutput
	var converted []convertedItem
	var partial []partialItem
	for _, key := range groups.order {
		group := groups.byKey[key]
		if group.Integration == nil {
			skipped = append(skipped, SkippedItem{
				Source: group.firstSourceName(),
				Reason: fmt.Sprintf("no integration record found for integrationUuid %s", key),
			})
			continue
		}

		output, items, partialItems, actionSkips, err := buildProviderDefinition(*group)
		if err != nil {
			skipped = append(skipped, SkippedItem{Source: group.Integration.Name, Reason: err.Error()})
			continue
		}
		providers = append(providers, output)
		converted = append(converted, items...)
		partial = append(partial, partialItems...)
		skipped = append(skipped, actionSkips...)
	}

	return Result{
		Providers: providers,
		Skipped:   skipped,
		Report:    buildReport(converted, partial, skipped),
	}, nil
}
