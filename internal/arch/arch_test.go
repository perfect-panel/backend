// Package arch enforces the modular-monolith architecture boundaries described
// in docs/adr-001-modular-monolith.md.
//
// Two complementary mechanisms guard module boundaries:
//
//  1. Compiler-enforced isolation: a module's implementation lives under
//     internal/module/<name>/internal/..., so the Go compiler rejects any
//     import of another module's internals. Only the facade package
//     (internal/module/<name>) and its integration events
//     (internal/module/<name>/events) are importable from outside.
//  2. This test: freezes the pre-existing cross-package coupling in the legacy
//     internal/logic tree and keeps new module packages free of legacy
//     dependencies while domains are migrated.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importPrefix = "github.com/perfect-panel/server/"

// legacyLogicImports is the frozen baseline of cross-package imports inside
// internal/logic, keyed by importer directory. Removing an edge here is always
// welcome; adding one requires updating docs/adr-001-modular-monolith.md, as
// each new edge makes the future module split harder.
var legacyLogicImports = map[string][]string{
	"internal/logic/admin/server": {"internal/logic/nodeconfig"},
	"internal/logic/auth":         {"internal/logic/common"},
	"internal/logic/public/order": {"internal/logic/notify", "internal/logic/public/portal"},
	"internal/logic/public/user":  {"internal/logic/auth/registerpolicy", "internal/logic/common", "internal/logic/telegram"},
	"internal/logic/server":       {"internal/logic/nodeconfig"},
}

// skippedDirs are top-level directories that contain no production Go code
// relevant to boundary rules.
var skippedDirs = map[string]bool{
	".git":    true,
	".github": true,
	"build":   true,
	"doc":     true,
	"docs":    true,
	"etc":     true,
	"script":  true,
	"scripts": true,
}

type goFile struct {
	dir     string // repo-relative package directory, e.g. "internal/logic/auth"
	path    string // repo-relative file path, for error messages
	imports []string
}

func collectGoFiles(t *testing.T) []goFile {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	var files []goFile
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			base := d.Name()
			if skippedDirs[rel] || strings.HasPrefix(base, ".") || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		gf := goFile{dir: filepath.ToSlash(filepath.Dir(rel)), path: rel}
		for _, imp := range f.Imports {
			v := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(v, importPrefix) {
				gf.imports = append(gf.imports, strings.TrimPrefix(v, importPrefix))
			}
		}
		files = append(files, gf)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	return files
}

// within reports whether pkg equals dir or is nested underneath it.
func within(pkg, dir string) bool {
	return pkg == dir || strings.HasPrefix(pkg, dir+"/")
}

func allowedLegacyEdge(importer, imported string) bool {
	for _, allowed := range legacyLogicImports[importer] {
		if imported == allowed {
			return true
		}
	}
	return false
}

// TestLogicImportFreeze forbids new cross-package imports inside the legacy
// internal/logic tree. Same-domain imports (parent/child packages) are always
// fine; anything else must be in the frozen baseline above.
func TestLogicImportFreeze(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if !within(f.dir, "internal/logic") {
			continue
		}
		for _, imp := range f.imports {
			if !within(imp, "internal/logic") {
				continue
			}
			if within(f.dir, imp) || within(imp, f.dir) {
				continue
			}
			if allowedLegacyEdge(f.dir, imp) {
				continue
			}
			t.Errorf("%s: new cross-package logic import %q — move the shared code into the owning module (see docs/adr-001-modular-monolith.md) instead of coupling logic packages", f.path, imp)
		}
	}
}

// TestModulePurity keeps internal/module packages free of the legacy god
// object and legacy logic tree: modules receive dependencies via their facade
// constructors, never by reaching back into svc or logic.
func TestModulePurity(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if !within(f.dir, "internal/module") {
			continue
		}
		for _, imp := range f.imports {
			if within(imp, "internal/svc") {
				t.Errorf("%s: module code must not import internal/svc; declare the dependency on the module facade constructor instead", f.path)
			}
			if within(imp, "internal/logic") {
				t.Errorf("%s: module code must not import legacy internal/logic packages; migrate the logic into the module", f.path)
			}
		}
	}
}

// TestModuleLayout enforces that a module exposes only its facade package and
// an optional events package: every other .go file must live under the
// module's internal/ subtree where the compiler seals it off.
func TestModuleLayout(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		rest, ok := strings.CutPrefix(f.dir, "internal/module/")
		if !ok {
			continue
		}
		segs := strings.Split(rest, "/")
		if len(segs) < 2 {
			continue // facade package internal/module/<name>
		}
		if segs[1] == "internal" || segs[1] == "events" {
			continue
		}
		t.Errorf("%s: module %q may only expose its facade and events/ packages; implementation belongs under internal/module/%s/internal/", f.path, segs[0], segs[0])
	}
}
