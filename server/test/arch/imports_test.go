// Package arch holds Beecon's static architecture tests: they walk the
// production source tree and fail the build when a file violates a boundary
// rule, independent of any specific feature's behavior tests.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const modulePath = "beecon"

// featureDependencies is the Phase 1 subset of .claude/BOUNDARIES.md's module
// dependency graph, hardcoded here so the test has no runtime dependency on
// parsing the markdown file. Update this map when BOUNDARIES.md's "Depends
// on" lines change. Only modules that exist under internal/ in Phase 1 are
// listed; later phases add catalog, connections, execution, triggers,
// delivery as their slices land. Shared-infra leaf packages (httpx, idgen,
// config, db, and, since Phase 2 Slice 2, vault and metrics) are
// deliberately absent: BOUNDARIES.md's shared-infra line makes them
// importable by every module, so TestModuleImportsRespectBoundariesDependencyGraph
// below simply skips any import whose feature name isn't a key here.
//
// registryservice (BOUNDARIES.md's "registry-service", Phase 5 registry
// sub-phase, PD59) is the one deliberate exception to that shared-infra
// carve-out: BOUNDARIES.md documents it as depending on nothing but "shares
// the definition-format package" (registrybundle) with catalog, and PD59's
// whole point is that this separate deployable never depends on any domain
// module — an invariant worth pinning here, not leaving unenforced like a
// true leaf package would be. So, unlike httpx/idgen/schema, registrybundle
// is given its own entry too (an empty allow-list — it is pure wire-format
// data, PD62/registrybundle package doc, and imports nothing internal at
// all), which is what makes registryservice's and catalog's own imports of
// it require listing "registrybundle" explicitly below: the trade-off of
// pinning PD59 is that registrybundle stops being "importable by every
// module for free" and instead needs the same explicit allow-listing any
// other tracked module does.
var featureDependencies = map[string][]string{
	"organizations":   {},
	"access":          {"organizations"},
	"catalog":         {"organizations", "registrybundle"},
	"connections":     {"organizations", "access", "catalog"},
	"connectweb":      {"connections"},
	"execution":       {"connections", "catalog", "organizations"},
	"triggers":        {"connections", "catalog", "organizations"},
	"delivery":        {"access", "organizations"},
	"logging":         {"organizations"},
	"registrybundle":  {},
	"registryservice": {"registrybundle"},
}

// bunAdapterAllowedDirs are the only places allowed to import database/sql or
// uptrace/bun: the shared db package, the composition root, and each
// module's driven/bun adapter (matched by substring so future modules'
// driven/bun packages are covered without editing this test).
var bunAdapterAllowedPrefixes = []string{
	"internal/db/",
	"internal/app/",
}

func isBunAdapterAllowed(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	if strings.Contains(relPath, "/driven/bun/") {
		return true
	}
	for _, prefix := range bunAdapterAllowedPrefixes {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

// goFile is one parsed production source file: its path relative to the
// server module root, its feature module (the segment after "internal/", or
// "" if not under internal/), and its list of imported paths.
type goFile struct {
	relPath string
	feature string
	imports []string
}

func TestNoBunOrDatabaseSQLImportsOutsideDeclaredAdapters(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if isBunAdapterAllowed(f.relPath) {
			continue
		}
		for _, imp := range f.imports {
			if imp == "database/sql" || strings.HasPrefix(imp, "github.com/uptrace/bun") {
				t.Errorf("%s: imports %q, but only internal/db, internal/app, and */driven/bun/ may import bun or database/sql", f.relPath, imp)
			}
		}
	}
}

func TestModuleImportsRespectBoundariesDependencyGraph(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if f.feature == "" {
			continue // infra/composition packages (httpx, idgen, config, db, app) are not BOUNDARIES-governed feature modules.
		}
		allowed, governed := featureDependencies[f.feature]
		if !governed {
			continue
		}
		for _, imp := range f.imports {
			importedFeature, ok := featureOf(imp)
			if !ok || importedFeature == f.feature {
				continue
			}
			if _, isFeature := featureDependencies[importedFeature]; !isFeature {
				continue // not a BOUNDARIES feature module (e.g. an infra package under internal/).
			}
			if !contains(allowed, importedFeature) {
				t.Errorf("%s: module %q imports %q, which is not in its BOUNDARIES.md dependency list %v",
					f.relPath, f.feature, importedFeature, allowed)
			}
		}
	}
}

// featureOf extracts the feature-module name from a beecon internal import
// path, e.g. "beecon/internal/organizations/driven/bun" -> ("organizations", true).
func featureOf(importPath string) (string, bool) {
	const marker = modulePath + "/internal/"
	if !strings.HasPrefix(importPath, marker) {
		return "", false
	}
	rest := strings.TrimPrefix(importPath, marker)
	segment, _, _ := strings.Cut(rest, "/")
	if segment == "" {
		return "", false
	}
	return segment, true
}

func contains(set []string, value string) bool {
	for _, v := range set {
		if v == value {
			return true
		}
	}
	return false
}

// collectGoFiles parses every non-test .go file under the server module's
// internal/ directory (ImportsOnly is enough for boundary checks and keeps
// this fast).
func collectGoFiles(t *testing.T) []goFile {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")

	var files []goFile
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot(t), path)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)

		imports := make([]string, 0, len(parsed.Imports))
		for _, imp := range parsed.Imports {
			imports = append(imports, strings.Trim(imp.Path.Value, `"`))
		}
		feature, _ := featureOf(modulePath + "/" + strings.TrimSuffix(relPath, filepath.Base(relPath)))
		files = append(files, goFile{relPath: relPath, feature: feature, imports: imports})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })
	return files
}

// repoRoot walks up to the directory holding go.mod, so the test finds
// internal/ regardless of which package's tests are running from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod from working directory")
		}
		dir = parent
	}
}
