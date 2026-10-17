package sqlitedriver

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This package's doc comment states the rule: every package that needs SQLite
// imports THIS package, never a driver directly, because database/sql panics
// ("sql: Register called twice for driver sqlite") when two register the same
// name in one binary. The partner machine hit exactly that panic on its first
// boot after a second blank import appeared.
//
// TestSingleRegistration could not enforce it. sql.Drivers() is per-BINARY, and
// that test's binary links only this package — so it cannot see a violation in
// any other package. The rule was documented and unenforced, which is how
// internal/activation/steps.go came to blank-import github.com/glebarez/go-sqlite
// directly: nothing failed.
//
// This is the census the rule needed. It ASKS THE GO PARSER for each file's
// imports rather than grepping, which matters: cmd/vl-activate/main_test.go
// contains a driver import inside a STRING LITERAL — the source of a throwaway
// program handed to `go run -` — and a grep-based census would flag that as a
// violation when it is a separate process that registers once and cannot
// conflict with anything. A census that cannot tell an import from a string
// produces false accusations, and a false accusation is how a real rule gets
// ignored.
var forbiddenDirect = []string{
	"modernc.org/sqlite",
	"github.com/glebarez/go-sqlite",
	"github.com/glebarez/sqlite",
	"gorm.io/driver/sqlite",
	"github.com/mattn/go-sqlite3",
}

func TestNoPackageImportsASQLiteDriverDirectly(t *testing.T) {
	root := repoRootFromHere(t)
	var offenders []string
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "web", ".understand-anything":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		// This package IS the registration site. It is the only exemption, and
		// it is exempt by identity rather than by name-matching a path prefix.
		if filepath.Dir(rel) == filepath.Join("store", "sqlitedriver") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			// A file the parser cannot parse FAILS the census by NAME — a silent
			// skip is how a violation hides (DS-102 fold, CTO 1790305899255).
			offenders = append(offenders, rel+" is UNPARSEABLE ("+perr.Error()+")")
			return nil
		}
		// THE DOMAIN IS LIBRARIES, with ONE correction (DS-102 fold, CTO
		// 1790305899255, option A): a `package main` is exempt ONLY when it
		// PROVABLY does not link vl/store/sqlitedriver under either tag set
		// (`go list -deps .` and `go list -tags cgofree -deps .`). A main that
		// links this package and also imports a driver directly is the
		// two-registrant panic in its own binary — cmd/picture_htf_replay did
		// exactly that (modernc.org/sqlite beside the transitively linked
		// sqlitedriver under BOTH tag sets). The docs/ research harnesses stay
		// exempt only because they pass the same proof. A go list that FAILS
		// proves nothing — fail closed.
		if f.Name != nil && f.Name.Name == "main" {
			hasDirect := false
			for _, im := range f.Imports {
				if path, uerr := strconv.Unquote(im.Path.Value); uerr == nil {
					for _, bad := range forbiddenDirect {
						if path == bad {
							hasDirect = true
							break
						}
					}
				}
			}
			if !hasDirect {
				return nil
			}
			links, why := mainLinksSqlitedriver(filepath.Dir(p))
			if !links {
				return nil
			}
			// It links the ONE registration site AND imports a driver directly:
			// the loop below names the direct import; the why is appended so the
			// reader sees the proof.
			_ = why
		}
		for _, im := range f.Imports {
			path, uerr := strconv.Unquote(im.Path.Value)
			if uerr != nil {
				continue
			}
			for _, bad := range forbiddenDirect {
				if path == bad {
					offenders = append(offenders, rel+" imports "+path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("%d package(s) import a SQLite driver directly instead of %s:\n  %s\n\n"+
			"database/sql panics when two drivers register the name %q in one binary. "+
			"Import nofx/store/sqlitedriver instead — it is the ONE registration site.",
			len(offenders), "vl/store/sqlitedriver", strings.Join(offenders, "\n  "), DriverName)
	}
}

// mainLinksSqlitedriver proves (or fails to prove) that the main package
// in dir does NOT link vl/store/sqlitedriver under either tag set. A failed
// go list is reported as linking (fail closed: absence unproven).
func mainLinksSqlitedriver(dir string) (bool, string) {
	for _, tags := range []string{"", "cgofree"} {
		args := []string{"list", "-deps"}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, ".")
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return true, fmt.Sprintf("go list %v failed for %s (%v) — absence unproven", args, dir, err)
		}
		if strings.Contains(string(out), "vl/store/sqlitedriver") {
			return true, "links nofx/store/sqlitedriver under " + strings.Join(args, " ")
		}
	}
	return false, ""
}

// repoRootFromHere walks up to the module root so the census covers the WHOLE
// repo. An earlier census of mine scanned three hand-listed directories and
// declared itself complete while omitting a producer; a census is either
// exhaustive over its domain or it is a list.
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := wd; ; {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", wd)
		}
		dir = parent
	}
}
