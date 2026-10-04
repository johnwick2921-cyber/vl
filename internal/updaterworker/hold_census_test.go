package updaterworker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"vl/internal/censuswalk"
)

// ── census: the hold writer seams live in hold.go only ─────────────────────
//
// store/maintenance_hold_writers_test.go admits internal/updaterworker/hold.go
// BY NAME to call the store's hold writers. hold.go reaches them through two
// package vars (so a test can observe the file at the instant of the write);
// a call through a var is not a call the store census can see by name. So
// this census pins the rest: no other non-test file of the worker package
// names writeMaintenanceHold / clearMaintenanceHold — every hold write and
// clear goes through HoldForJob / ReleaseJob — and those two are called only
// from the steps that the state table says write or clear (steps.go).
func TestWorkerHoldSeamCensus(t *testing.T) {
	seamUsers, callers, scanned, err := workerHoldSeamUses(".")
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 10 {
		t.Fatalf("scanned %d files — the census is not covering the package", scanned)
	}
	for _, off := range seamUsers {
		t.Errorf("%s — only hold.go may name the hold writer seams", off)
	}
	for _, fn := range []string{"HoldForJob", "ReleaseJob"} {
		if got := fmt.Sprint(keysOf(callers[fn])); got != "[steps.go]" {
			t.Errorf("%s is named in %s, want exactly [steps.go] (stepHold / stepReleaseHold)", fn, got)
		}
	}
	// the census itself (verifier D7): a writer taken as a VALUE — a func
	// var, a method value, an argument — is a use the call-only census
	// missed; every naming counts, the declarations in hold.go do not
	dir := t.TempDir()
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("hold.go", "package updaterworker\n\nvar writeMaintenanceHold, clearMaintenanceHold func()\n\nfunc HoldForJob() { writeMaintenanceHold() }\nfunc ReleaseJob() { clearMaintenanceHold() }\n")
	put("steps.go", "package updaterworker\n\nfunc a() { HoldForJob(); ReleaseJob() }\n")
	put("worker.go", "package updaterworker\n\nfunc b() { clr := ReleaseJob; clr() }\n")
	put("socket.go", "package updaterworker\n\nfunc c(f func()) {}\nfunc d() { c(HoldForJob) }\n")
	put("runner.go", "package updaterworker\n\nfunc e() { f := clearMaintenanceHold; f() }\n")
	seamUsers, callers, _, err = workerHoldSeamUses(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(keysOf(callers["ReleaseJob"])) + fmt.Sprint(keysOf(callers["HoldForJob"])) + fmt.Sprint(seamUsers); got != "[steps.go worker.go][socket.go steps.go][runner.go names clearMaintenanceHold]" {
		t.Fatalf("synthetic census = %s", got)
	}
}

// workerHoldSeamUses scans dir's non-test files: every file other than
// hold.go naming a seam var, and every file NAMING (calling, taking as a
// value, passing) HoldForJob / ReleaseJob — their declarations excepted.
func workerHoldSeamUses(dir string) (seamUsers []string, callers map[string]map[string]bool, scanned int, err error) {
	seams := map[string]bool{"writeMaintenanceHold": true, "clearMaintenanceHold": true}
	callers = map[string]map[string]bool{"HoldForJob": {}, "ReleaseJob": {}}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, 0, err
	}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, nil, 0, err
		}
		scanned++
		decl := map[*ast.Ident]bool{}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				decl[fd.Name] = true
			}
		}
		seen := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			x, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if seams[x.Name] && name != "hold.go" && !seen[x.Name] {
				seen[x.Name] = true
				seamUsers = append(seamUsers, name+" names "+x.Name)
			}
			if m, ok := callers[x.Name]; ok && !decl[x] {
				m[name] = true
			}
			return true
		})
	}
	sort.Strings(seamUsers)
	return seamUsers, callers, scanned, nil
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── census: nothing outside the worker calls its hold writers ──────────────
//
// hold.go is admitted by the store census to call the store's writers, and it
// EXPORTS HoldForJob / ReleaseJob — a call to those is a hold write the store
// census cannot see (it matches the store's names). So the whole module's
// non-test files are scanned: any file importing vl/internal/updaterworker
// (under any name, or dot-imported) that names HoldForJob or ReleaseJob is an
// offender — cmd/vl-updater included (the CLI never touches the hold; its
// recovery text tells the OPERATOR to clear it with maintenance-hold).
func TestWorkerHoldWritersCensusModuleWide(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	off, scanned, err := workerHoldCallers(root)
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 100 {
		t.Fatalf("scanned %d files — not the module", scanned)
	}
	if len(off) > 0 {
		t.Fatalf("the worker's hold writers are called from outside it:\n%s", strings.Join(off, "\n"))
	}
	// the census itself, on a synthetic module: a CLI calling it, a renamed
	// import, a dot import are all caught; a read of the display path is not
	dir := t.TempDir()
	put := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	put("go.mod", "module vl\n\ngo 1.25\n")
	put("internal/updaterworker/hold.go", "package updaterworker\n\nfunc HoldForJob() {}\nfunc ReleaseJob() {}\nfunc HoldFileForDisplay() string { return \"\" }\n")
	put("cmd/vl-updater/main.go", "package main\n\nimport \"vl/internal/updaterworker\"\n\nfunc main() { updaterworker.ReleaseJob() }\n")
	put("api/a.go", "package api\n\nimport uw \"vl/internal/updaterworker\"\n\nvar _ = uw.HoldForJob\n")
	put("api/b.go", "package api\n\nimport . \"vl/internal/updaterworker\"\n\nfunc b() { HoldForJob() }\n")
	put("api/c.go", "package api\n\nimport \""+pkgImport(dir)+"\"\n\nvar _ = updaterworker.HoldFileForDisplay\n")
	off, _, err = workerHoldCallers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(off, "|"); got != "api/a.go: names updaterworker.HoldForJob|api/b.go: names updaterworker.HoldForJob|cmd/vl-updater/main.go: names updaterworker.ReleaseJob" {
		t.Fatalf("synthetic census offenders = %v", off)
	}
}

// pkgImport is the updaterworker import path as the census derives it from the
// module (never the hardcoded vl/ form — R5 makes the module vl).
func pkgImport(root string) string {
	if m, err := censuswalk.ModulePath(root); err == nil {
		return m + "/internal/updaterworker"
	}
	return "vl/internal/updaterworker"
}

func workerHoldCallers(root string) (offenders []string, scanned int, err error) {
	files, err := censuswalk.NonTestGoFiles(root)
	if err != nil {
		return nil, 0, err
	}
	module, err := censuswalk.ModulePath(root)
	if err != nil {
		return nil, 0, err
	}
	pkg := module + "/internal/updaterworker"
	writers := map[string]bool{"HoldForJob": true, "ReleaseJob": true}
	for _, file := range files {
		if path.Dir(file.Rel) == "internal/updaterworker" {
			continue // the package itself: TestWorkerHoldSeamCensus
		}
		f, perr := parser.ParseFile(token.NewFileSet(), file.Path, nil, parser.SkipObjectResolution)
		if perr != nil {
			offenders = append(offenders, file.Rel+": cannot be parsed")
			continue
		}
		scanned++
		local, dot := map[string]bool{}, false
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == pkg {
				switch {
				case imp.Name == nil:
					local["updaterworker"] = true
				case imp.Name.Name == ".":
					dot = true
				case imp.Name.Name != "_":
					local[imp.Name.Name] = true
				}
			}
		}
		if len(local) == 0 && !dot {
			continue
		}
		hit := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && local[id.Name] && writers[x.Sel.Name] {
					hit[x.Sel.Name] = true
				}
			case *ast.Ident:
				if dot && writers[x.Name] {
					hit[x.Name] = true
				}
			}
			return true
		})
		for _, name := range keysOf(hit) {
			offenders = append(offenders, file.Rel+": names updaterworker."+name)
		}
	}
	sort.Strings(offenders)
	return offenders, scanned, nil
}
