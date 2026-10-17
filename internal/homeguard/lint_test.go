// Package homeguard is the build-failing lint for THE ONE RULE (owner order
// 2026-10-17, after the 2026-10-16 19:05–19:14 CDT wipe of /home/hoang). It
// runs in every plain `go test ./...` and fails when:
//
//   - R1: a test package has no TestMain mounting testhome.Guard / testhome.Run
//     (so its tests would run with the REAL HOME and no wipe detection);
//   - R2: os.RemoveAll appears without a `homeguard:allow` marker on the same
//     or the previous line — every recursive delete goes through
//     internal/safepath (path check: not empty, not "/", not a home folder,
//     inside the expected subfolder) or carries a reviewed reason;
//   - R3: a file imports os/user outside internal/testhome and
//     internal/safepath — user.Current() names the REAL home even when HOME
//     is redirected, so it is the one door around the guard;
//   - R4: a Go file shells out to rm.
package homeguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// goFiles walks the module like `go test ./...` sees it: it skips testdata,
// vendor, node_modules, hidden and _-prefixed directories, and nested modules.
func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (n == "testdata" || n == "vendor" || n == "node_modules" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
				return filepath.SkipDir
			}
			if p != root {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var (
	testMainRe = regexp.MustCompile(`(?m)^func TestMain\(m \*testing\.M\)`)
	guardRe    = regexp.MustCompile(`testhome\.(Guard|Run)\(`)
	shellRmRe  = regexp.MustCompile(`(^|[\s;&|(])(rm|rmdir|shred)\s`)
)

// deletePkgs are the import paths whose delete entry points the lint tracks,
// with the selectors that delete a tree or bypass os entirely (DS-106
// adversarial review 2026-10-17: aliased imports, method values, dot-imports
// and raw syscalls all slipped past the first, regex-only version).
var deletePkgs = map[string]map[string]bool{
	"os":                    {"RemoveAll": true},
	"syscall":               {"Unlink": true, "Unlinkat": true, "Rmdir": true},
	"golang.org/x/sys/unix": {"Unlink": true, "Unlinkat": true, "Rmdir": true},
}

// deleteFindings parses one Go file and returns "line: why" for every
// reference (call OR value) to a tracked delete entry point, every dot-import
// of a tracked package, every os/user import, and every exec.Command that
// runs rm/rmdir/shred — before allow markers are applied.
func deleteFindings(name string, src []byte) ([]int, []string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}
	var lines []int
	var why []string
	add := func(pos token.Pos, w string) {
		lines = append(lines, fset.Position(pos).Line)
		why = append(why, w)
	}
	local := map[string]string{} // local name → import path
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		if path == "os/user" {
			add(im.Pos(), `imports "os/user" (user.Current names the REAL home past the HOME redirect)`)
		}
		if _, tracked := deletePkgs[path]; !tracked && path != "os/exec" {
			continue
		}
		ln := filepath.Base(path)
		if im.Name != nil {
			ln = im.Name.Name
		}
		if ln == "." && path != "os/exec" {
			add(im.Pos(), "dot-import of "+path+" hides its delete calls from review")
			continue
		}
		local[ln] = path
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok {
				return true
			}
			if sels, ok := deletePkgs[local[id.Name]]; ok && sels[x.Sel.Name] {
				add(x.Pos(), local[id.Name]+"."+x.Sel.Name+" without the safepath gate")
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || local[id.Name] != "os/exec" || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
				return true
			}
			for _, a := range x.Args {
				lit, ok := a.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, _ := strconv.Unquote(lit.Value)
				if b := filepath.Base(v); b == "rm" || b == "rmdir" || b == "shred" || shellRmRe.MatchString(" "+v+" ") {
					add(x.Pos(), "exec runs rm/rmdir/shred from Go")
					break
				}
			}
		}
		return true
	})
	return lines, why, nil
}

func rel(root, p string) string {
	r, _ := filepath.Rel(root, p)
	return filepath.ToSlash(r)
}

func TestEveryTestPackageMountsTheHomeGuard(t *testing.T) {
	root := repoRoot(t)
	pkgs := map[string]bool{} // dir → has guard
	for _, f := range goFiles(t, root) {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		d := filepath.Dir(f)
		if _, seen := pkgs[d]; !seen {
			pkgs[d] = false
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if testMainRe.Match(b) && guardRe.Match(b) {
			pkgs[d] = true
		}
	}
	var bad []string
	for d, ok := range pkgs {
		if !ok && rel(root, d) != "internal/testhome" { // the guard's own package tests itself via Run
			bad = append(bad, rel(root, d))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("THE ONE RULE: %d test package(s) run without the home guard — add\n\tfunc TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }\n(or wrap an existing TestMain with testhome.Run):\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
	if len(pkgs) < 40 {
		t.Fatalf("walked only %d test packages — the walk is broken, not the repo clean", len(pkgs))
	}
}

func TestNoUnguardedRecursiveDelete(t *testing.T) {
	root := repoRoot(t)
	exempt := map[string]bool{"internal/testhome": true, "internal/safepath": true} // the guards themselves (os/user, the one gated call)
	var bad []string
	for _, f := range goFiles(t, root) {
		r := rel(root, f)
		if r == "internal/homeguard/lint_test.go" || r == "internal/homeguard/lint_regex_holes_test.go" {
			continue // the lint and its fixtures name the patterns on purpose
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lines, why, err := deleteFindings(f, b)
		if err != nil {
			continue // a file the compiler would reject fails the build elsewhere
		}
		src := strings.Split(string(b), "\n")
		for k, ln := range lines {
			if exempt[filepath.ToSlash(filepath.Dir(r))] && !strings.HasPrefix(why[k], "dot-import") {
				continue
			}
			allowed := (ln-1 < len(src) && strings.Contains(src[ln-1], "homeguard:allow")) ||
				(ln-2 >= 0 && strings.Contains(src[ln-2], "homeguard:allow"))
			if !allowed {
				bad = append(bad, r+":"+itoa(ln)+": "+why[k])
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("THE ONE RULE: %d unguarded delete/home reference(s) — route a tree delete through safepath.RemoveAllUnder(root, path) / safepath.RemoveAllTemp(path), or mark a reviewed temp-dir cleanup with `//homeguard:allow — <why it cannot reach the home folder>`:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestCanonCarriesTheOneRule pins THE ONE RULE into the tracked canon that
// every agent's CLAUDE.md / AGENTS.md mirrors (owner order 2026-10-17, JOB 1
// step 4): a wave that drops or waters it down fails the build.
func TestCanonCarriesTheOneRule(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "superpowers", "CLAUDE-canon.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, must := range []string{
		"THE ONE RULE — NO DELETING",
		"Never delete files or folders",
		"mktemp -d",
		`"${VAR:?}"`,
		"~/trash/<date>/",
		"no `git clean`",
		"no `git reset --hard`",
		"no force push",
		"no DELETE, DROP or TRUNCATE",
		"Never bypass or disable",
	} {
		if !strings.Contains(s, must) {
			t.Errorf("CLAUDE-canon.md lost THE ONE RULE clause %q", must)
		}
	}
}
