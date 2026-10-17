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
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	removeRe   = regexp.MustCompile(`\bos\.RemoveAll\(`)
	userImport = regexp.MustCompile(`(?m)^\s*(import\s+)?(\w+\s+)?"os/user"`)
	rmExecRe   = regexp.MustCompile(`exec\.Command(Context)?\([^)]*"(rm|/bin/rm|rmdir|shred)"`)
)

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
	var bad []string
	for _, f := range goFiles(t, root) {
		if rel(root, f) == "internal/homeguard/lint_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		for i, ln := range lines {
			code := ln
			if j := strings.Index(code, "//"); j >= 0 && !strings.Contains(code[j:], "homeguard:allow") {
				code = code[:j]
			}
			if !removeRe.MatchString(code) {
				continue
			}
			if strings.Contains(ln, "homeguard:allow") || (i > 0 && strings.Contains(lines[i-1], "homeguard:allow")) {
				continue
			}
			bad = append(bad, rel(root, f)+":"+itoa(i+1)+": "+strings.TrimSpace(ln))
		}
	}
	if len(bad) > 0 {
		t.Fatalf("THE ONE RULE: %d os.RemoveAll call(s) without a path check — use safepath.RemoveAllUnder(root, path) / safepath.RemoveAllTemp(path), or mark a reviewed temp-dir cleanup with `//homeguard:allow — <why it cannot reach the home folder>`:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}

func TestNoRealHomeLookupOutsideTheGuards(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{"internal/testhome": true, "internal/safepath": true}
	var bad []string
	for _, f := range goFiles(t, root) {
		r := rel(root, f)
		if allowed[filepath.ToSlash(filepath.Dir(r))] || r == "internal/homeguard/lint_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if userImport.Match(b) {
			bad = append(bad, r+": imports os/user (user.Current names the REAL home past the HOME redirect)")
		}
		if rmExecRe.Match(b) {
			bad = append(bad, r+": shells out to rm/rmdir/shred from Go")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("THE ONE RULE:\n  %s", strings.Join(bad, "\n  "))
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
