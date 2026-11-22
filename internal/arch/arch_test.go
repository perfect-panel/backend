// Package arch enforces the modular-monolith architecture boundaries described
// in docs/design/adr-001-modular-monolith.md.
//
// Two complementary mechanisms guard module boundaries:
//
//  1. Compiler-enforced isolation: a module's implementation lives under
//     internal/module/<name>/internal/..., so the Go compiler rejects any
//     import of another module's internals. Only the facade, contract,
//     integration-event, entity and transport packages are importable from
//     outside; transport remains an inbound adapter, never a module dependency.
//  2. These tests: the directory layout, the dependency direction between the
//     shared layers, the composition root and the modules, and the ownership
//     rules the compiler cannot see.
package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importPrefix = "github.com/perfect-panel/backend/"

// appImporters is the closed composition-root boundary. Only cmd may import
// internal/app to build the application; every runtime consumer receives a
// module facade or a task/transport-specific dependency set.
var appImporters = map[string]bool{
	"cmd": true,
}

var internalRoots = map[string]bool{
	"app": true, "arch": true, "auth": true, "config": true,
	"infra": true, "module": true, "repository": true, "transport": true,
}

func TestRuntimePackagesStayInternal(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		for _, legacy := range []string{"queue", "scheduler", "adapter", "initialize"} {
			if within(f.dir, legacy) {
				t.Errorf("%s: runtime implementation belongs under internal, not %s", f.path, legacy)
			}
			for _, imp := range f.imports {
				if within(imp, legacy) {
					t.Errorf("%s: legacy root package import %q", f.path, imp)
				}
			}
		}
	}
}

func TestInternalLayout(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if f.dir == "internal" {
			t.Errorf("%s: application code belongs under internal/app, not the internal root", f.path)
		}
		if rest, ok := strings.CutPrefix(f.dir, "internal/"); ok && !internalRoots[strings.Split(rest, "/")[0]] {
			t.Errorf("%s: package must belong to an existing internal responsibility group", f.path)
		}
		if !within(f.dir, "internal/infra") && !within(f.dir, "internal/auth") {
			continue
		}
		for _, imp := range f.imports {
			moduleImpl := within(imp, "internal/module") && !strings.Contains(imp, "/entity/")
			if within(imp, "internal/app") || within(imp, "internal/transport") || moduleImpl {
				t.Errorf("%s: shared infrastructure/auth must not import application, transport or business implementation %q", f.path, imp)
			}
		}
	}
}

// Public shared packages are deliberately small in number. Application
// adapters belong under internal or the module that owns their protocol.
var sharedPackages = map[string]bool{
	"cache": true, "conf": true, "httpx": true, "logger": true,
	"orm": true, "random": true, "requestmeta": true, "slicesx": true,
	"templatex": true, "timeutil": true, "trace": true, "xerr": true,
}

func TestSharedPackageBoundary(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		rel, ok := strings.CutPrefix(f.dir, "pkg/")
		if !ok {
			continue
		}
		if !sharedPackages[strings.Split(rel, "/")[0]] {
			t.Errorf("%s: application-specific package must not be added to pkg", f.path)
		}
		for _, imported := range f.imports {
			if !within(imported, "pkg") {
				t.Errorf("%s: shared package depends on application code %q", f.path, imported)
			}
		}
	}
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
	dir     string // repo-relative package directory, e.g. "internal/module/billing/internal/checkout"
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

// TestModulePurity keeps modules independent of the composition root:
// modules receive dependencies via their facade constructors, never by
// reaching back into application assembly. (The removed legacy trees,
// internal/svc and internal/logic, cannot return: TestInternalLayout fixes
// the internal root directories.)
func TestModulePurity(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if !within(f.dir, "internal/module") {
			continue
		}
		for _, imp := range f.imports {
			if within(imp, "internal/app") && imp != "internal/app/buildinfo" {
				t.Errorf("%s: module code must not import application assembly; declare the dependency on the module facade constructor instead", f.path)
			}
		}
	}
}

// Module code declares only the repository capabilities it consumes. The full
// store remains an application assembly concern, including in type aliases.
func TestModulesDoNotDependOnFullStore(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if !within(f.dir, "internal/module") || strings.HasSuffix(f.path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", f.path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		repositoryAlias := ""
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == importPrefix+"internal/repository" {
				repositoryAlias = "repository"
				if imp.Name != nil {
					repositoryAlias = imp.Name.Name
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Store" {
				return true
			}
			if id, ok := selector.X.(*ast.Ident); ok && id.Name == repositoryAlias {
				t.Errorf("%s: module depends on repository.Store; define a consumer-owned capability instead", f.path)
			}
			return true
		})
	}
}

// TestAppImportBoundary keeps the composition root private to the CLI. Module
// facades and runtime adapters receive their own dependency sets instead.
func TestAppImportBoundary(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		for _, imp := range f.imports {
			if imp != "internal/app" {
				continue
			}
			if f.dir == "internal/app" || appImporters[f.dir] {
				continue
			}
			t.Errorf("%s: import of internal/app outside the CLI — inject dependencies through a module facade instead", f.path)
		}
	}
}

// TestModuleLayout enforces the public shape of a module. Contract contains
// module-owned command/query/result DTOs; transport/http contains inbound
// Hertz adapters. Every implementation package still belongs under internal/.
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
		if segs[1] == "internal" || segs[1] == "events" || segs[1] == "entity" || segs[1] == "contract" || segs[1] == "transport" {
			continue
		}
		t.Errorf("%s: module %q may only expose its facade, contract/, events/, entity/ and transport/ packages; implementation belongs under internal/module/%s/internal/", f.path, segs[0], segs[0])
	}
}

// TestModuleContractsAreIndependent prevents the old central DTO package and
// cross-module contract graphs from returning. Each module owns complete JSON
// snapshots for the data it exposes.
func TestModuleContractsAreIndependent(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		rest, isModuleFile := strings.CutPrefix(f.dir, "internal/module/")
		owner := ""
		if isModuleFile {
			owner = strings.Split(rest, "/")[0]
		}
		for _, imp := range f.imports {
			if within(imp, "internal/model/dto") {
				t.Errorf("%s: legacy central DTO import %q; use the owning module's contract package", f.path, imp)
			}
			imported, isModuleImport := strings.CutPrefix(imp, "internal/module/")
			if isModuleFile && isModuleImport {
				parts := strings.Split(imported, "/")
				if len(parts) >= 2 && parts[1] == "contract" && parts[0] != owner {
					t.Errorf("%s: module %s imports %s contract; expose a local snapshot or a narrow facade result instead", f.path, owner, parts[0])
				}
			}
		}
		if !strings.Contains(f.dir, "/contract") || !within(f.dir, "internal/module") {
			continue
		}
		for _, imp := range f.imports {
			if within(imp, "internal/module") {
				t.Errorf("%s: module contract must be self-contained, found module import %q", f.path, imp)
			}
		}
	}
}

// TestModuleTransportOwnership guarantees that handlers stay with the module
// facade they adapt. A transport may import its own facade and contract, but
// must not orchestrate another business module directly.
func TestModuleTransportOwnership(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		rest, ok := strings.CutPrefix(f.dir, "internal/module/")
		if !ok {
			continue
		}
		segs := strings.Split(rest, "/")
		if len(segs) < 3 || segs[1] != "transport" {
			continue
		}
		owner := segs[0]
		for _, imp := range f.imports {
			imported, isModule := strings.CutPrefix(imp, "internal/module/")
			if !isModule {
				continue
			}
			importedOwner := strings.Split(imported, "/")[0]
			if importedOwner != owner {
				t.Errorf("%s: %s transport imports %s module; route composition must select one module-owned handler", f.path, owner, importedOwner)
			}
		}
	}
}

// TestModuleCoreDoesNotImportTransport keeps the dependency direction from
// core/facade code toward inbound adapters closed. Only composition roots may
// depend on module transports.
func TestModuleCoreDoesNotImportTransport(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if !within(f.dir, "internal/module") || strings.Contains(f.dir, "/transport/") {
			continue
		}
		for _, imp := range f.imports {
			if within(imp, "internal/transport") || (strings.HasPrefix(imp, "internal/module/") && strings.Contains(imp, "/transport/")) {
				t.Errorf("%s: module core imports inbound transport %q; wire adapters only at a composition root", f.path, imp)
			}
		}
	}
}

// TestModuleContractNamesAreUnique ensures that cross-domain JSON snapshots
// carry explicit owner-qualified Go names. Duplicate exported names make
// generated Swagger identifiers depend on package-disambiguation internals.
func TestModuleContractNamesAreUnique(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	owners := make(map[string]string)
	fset := token.NewFileSet()
	err = filepath.WalkDir(filepath.Join(root, "internal", "module"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || !strings.Contains(filepath.ToSlash(path), "/contract/") {
			return walkErr
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, raw := range gen.Specs {
				name := raw.(*ast.TypeSpec).Name.Name
				if !ast.IsExported(name) {
					continue
				}
				if previous, exists := owners[name]; exists {
					t.Errorf("%s: exported contract type %s duplicates %s; qualify cross-domain snapshots with their owner", filepath.ToSlash(rel), name, previous)
					continue
				}
				owners[name] = filepath.ToSlash(rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan module contracts: %v", err)
	}
}

// TestLegacyHandlerTreeRemoved makes handler ownership irreversible. Routes
// may retain the old path only as a golden-test normalization string, never as
// an import or production package.
func TestLegacyHandlerTreeRemoved(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if within(f.dir, "internal/handler") {
			t.Errorf("%s: HTTP handlers belong under internal/module/<owner>/transport/http", f.path)
		}
		for _, imp := range f.imports {
			if within(imp, "internal/handler") {
				t.Errorf("%s: legacy handler import %q", f.path, imp)
			}
		}
	}
}

// TestGoFileNamesAreLowercase keeps the one naming style the tree was
// normalized to: Go files and their directories are lowercase snake_case, as
// the Go toolchain's own names are. The camelCase names the code generator
// used to write mixed with them.
func TestGoFileNamesAreLowercase(t *testing.T) {
	for _, f := range collectGoFiles(t) {
		if strings.ToLower(f.path) != f.path {
			t.Errorf("%s: name Go files and directories in lowercase snake_case", f.path)
		}
	}
}
