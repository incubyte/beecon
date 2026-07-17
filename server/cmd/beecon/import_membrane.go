package main

import (
	"fmt"
	"os"
	"path/filepath"

	"beecon/internal/catalog/membraneimport"
)

// importMembraneUsage is shown on stderr for a missing/invalid flag or
// argument (spec: "a missing -o fails with a usage message").
const importMembraneUsage = "usage: beecon import-membrane <in-dir> -o <out-dir>"

// runImportMembrane is the thin CLI shell over the membraneimport converter
// package: it parses flags, reads every *.yaml file in <in-dir>, calls the
// delivery-independent Convert, and writes the emitted provider definitions
// plus the conversion report under <out-dir>. It holds no conversion logic
// of its own (spec's package-layout note).
func runImportMembrane(args []string, stdout, stderr *os.File) error {
	inDir, outDir, err := parseImportMembraneArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, importMembraneUsage)
		return err
	}

	sources, err := readMembraneSourceFiles(inDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", inDir, err)
	}

	result, err := membraneimport.Convert(sources)
	if err != nil {
		return fmt.Errorf("convert %s: %w", inDir, err)
	}
	reportSkippedItems(stderr, result)

	if err := writeImportResult(outDir, result); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %d provider definition(s) and import-report.md to %s\n", len(result.Providers), outDir)
	return nil
}

// parseImportMembraneArgs reads the `<in-dir> -o <out-dir>` shape (the `-o`
// flag may appear anywhere among args, matching the spec's exact CLI form,
// which is not what Go's flag package parses since it stops at the first
// non-flag argument). Both <in-dir> and -o <out-dir> are required.
func parseImportMembraneArgs(args []string) (inDir, outDir string, err error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" || args[i] == "--o" {
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("import-membrane: -o requires a value")
			}
			outDir = args[i+1]
			i++
			continue
		}
		positional = append(positional, args[i])
	}
	if len(positional) != 1 {
		return "", "", fmt.Errorf("import-membrane: <in-dir> is required")
	}
	if outDir == "" {
		return "", "", fmt.Errorf("import-membrane: -o <out-dir> is required")
	}
	return positional[0], outDir, nil
}

// reportSkippedItems surfaces every skipped input as it happens (spec: "an
// error naming the file and the missing field... not silently dropped"),
// distinct from the fuller reasons also recorded in the written report.
func reportSkippedItems(stderr *os.File, result membraneimport.Result) {
	for _, item := range result.Skipped {
		fmt.Fprintf(stderr, "skip: %s: %s\n", item.Source, item.Reason)
	}
}

func readMembraneSourceFiles(inDir string) ([]membraneimport.SourceFile, error) {
	entries, err := os.ReadDir(inDir)
	if err != nil {
		return nil, err
	}

	var files []membraneimport.SourceFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(inDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, membraneimport.SourceFile{Name: entry.Name(), Content: content})
	}
	return files, nil
}

func writeImportResult(outDir string, result membraneimport.Result) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", outDir, err)
	}
	for _, provider := range result.Providers {
		path := filepath.Join(outDir, provider.Slug+".yaml")
		if err := os.WriteFile(path, provider.YAML, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	reportPath := filepath.Join(outDir, "import-report.md")
	if err := os.WriteFile(reportPath, result.Report, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", reportPath, err)
	}
	return nil
}
