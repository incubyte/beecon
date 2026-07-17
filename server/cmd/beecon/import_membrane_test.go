package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseImportMembraneArgs_ParsesInDirAndOutDirRegardlessOfFlagPosition
// covers the CLI's exact form (`<in-dir> -o <out-dir>`), including the -o
// flag appearing before the positional argument, since Go's flag package
// alone cannot parse this shape (it stops at the first non-flag argument).
func TestParseImportMembraneArgs_ParsesInDirAndOutDirRegardlessOfFlagPosition(t *testing.T) {
	tests := map[string][]string{
		"in-dir then -o out-dir": {"in", "-o", "out"},
		"-o out-dir then in-dir": {"-o", "out", "in"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			inDir, outDir, err := parseImportMembraneArgs(args)
			if err != nil {
				t.Fatalf("parseImportMembraneArgs(%v) returned an error: %v", args, err)
			}
			if inDir != "in" {
				t.Errorf("inDir = %q, want %q", inDir, "in")
			}
			if outDir != "out" {
				t.Errorf("outDir = %q, want %q", outDir, "out")
			}
		})
	}
}

// TestParseImportMembraneArgs_FailsWhenOutDirFlagIsMissing is the spec's
// explicit AC: a missing -o fails (with the CLI shell then printing the
// usage message).
func TestParseImportMembraneArgs_FailsWhenOutDirFlagIsMissing(t *testing.T) {
	_, _, err := parseImportMembraneArgs([]string{"in"})
	if err == nil {
		t.Fatal("expected an error when -o is omitted, got nil")
	}
}

// TestParseImportMembraneArgs_FailsWhenInDirIsMissing covers the CLI's other
// required argument: -o alone with no in-dir positional is also invalid.
func TestParseImportMembraneArgs_FailsWhenInDirIsMissing(t *testing.T) {
	_, _, err := parseImportMembraneArgs([]string{"-o", "out"})
	if err == nil {
		t.Fatal("expected an error when <in-dir> is omitted, got nil")
	}
}

// TestParseImportMembraneArgs_FailsWhenOutDirFlagHasNoValue guards against a
// dangling -o at the end of the argument list being read past the slice.
func TestParseImportMembraneArgs_FailsWhenOutDirFlagHasNoValue(t *testing.T) {
	_, _, err := parseImportMembraneArgs([]string{"in", "-o"})
	if err == nil {
		t.Fatal("expected an error when -o has no following value, got nil")
	}
}

// TestRunImportMembrane_MissingOutDirFlagPrintsUsageAndFails is the CLI's
// user-facing contract for the missing-flag case: usage on stderr, a non-nil
// error (which main.go turns into exit code 1) — exercised through the
// converter's real CLI entry point rather than a spawned process.
func TestRunImportMembrane_MissingOutDirFlagPrintsUsageAndFails(t *testing.T) {
	inDir := t.TempDir()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer stderrR.Close()

	runErr := runImportMembrane([]string{inDir}, os.Stdout, stderrW)
	stderrW.Close()

	if runErr == nil {
		t.Fatal("expected an error when -o is omitted, got nil")
	}
	stderrOutput := readAllFromPipe(t, stderrR)
	if !strings.Contains(stderrOutput, importMembraneUsage) {
		t.Errorf("stderr = %q, want it to contain the usage message %q", stderrOutput, importMembraneUsage)
	}
}

func readAllFromPipe(t *testing.T, r *os.File) string {
	t.Helper()
	var buf strings.Builder
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		buf.Write(chunk[:n])
		if err != nil {
			break
		}
	}
	return buf.String()
}

// TestRunImportMembrane_WritesProviderDefinitionAndReportUnderOutDir is the
// CLI's happy path: given a directory of Membrane export YAML files, it
// reads them, converts them, and writes the emitted definition plus
// import-report.md under -o's out-dir — the thin shell's whole job, wrapped
// around the already-thoroughly-tested converter package.
func TestRunImportMembrane_WritesProviderDefinitionAndReportUnderOutDir(t *testing.T) {
	inDir := t.TempDir()
	writeFile(t, filepath.Join(inDir, "integration.yaml"), `
uuid: grp-cli-test-uuid
key: cli-test-crm
name: CLI Test CRM
logoUri: https://static.example.com/cli-test-crm.png
`)
	writeFile(t, filepath.Join(inDir, "action.yaml"), `
key: cli-test-crm-list-records
name: List Records
inputSchema:
  description: Lists records.
  type: object
  properties: {}
type: api-request-to-external-app
config:
  request:
    method: GET
    path: /records
customOutputSchema:
  type: object
  properties:
    id:
      type: string
integrationUuid: grp-cli-test-uuid
`)
	outDir := filepath.Join(t.TempDir(), "staging")

	if err := runImportMembrane([]string{inDir, "-o", outDir}, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("runImportMembrane returned an error: %v", err)
	}

	definitionPath := filepath.Join(outDir, "cli-test-crm.yaml")
	definition, err := os.ReadFile(definitionPath)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", definitionPath, err)
	}
	if !strings.Contains(string(definition), "slug: cli-test-crm") {
		t.Errorf("emitted definition missing expected slug:\n%s", definition)
	}

	reportPath := filepath.Join(outDir, "import-report.md")
	report, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", reportPath, err)
	}
	if !strings.Contains(string(report), "cli-test-crm-list-records") {
		t.Errorf("report missing the converted tool:\n%s", report)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
