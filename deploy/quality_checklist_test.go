package deploy

// The quality checklist (docs/QUALITY-CHECKLIST.md) is one page the owner and
// every lane read before coding. A checklist rots the same way the old 10,000-
// line audit file did: it grows, it names tests that were later renamed, and
// "a reviewer checks it" quietly becomes the answer for every row. This census
// pins the page so it can only get shorter and more machine-checked:
//
//   - at most 120 lines;
//   - rows are exactly Q1..Q13, in order, one table row each;
//   - a row enforced by "test: Name" names a func Name( that exists in some
//     *_test.go in the repo;
//   - the number of "self" rows (author proof + reviewer) equals the cap below,
//     so moving a row to a machine check forces the cap down with it, and a
//     row can never move back to "self" without editing this file.
//
// It deliberately does NOT read .github/workflows: the partner sync runs
// `go test ./deploy/...` with the workflows carved out, so a "ci: ..." row is
// accepted on its word. Workflow pins live in release_contract_test.go.
//
// The second half runs deploy/quality-checklist-pr.sh (the PR-body check the
// "Quality checklist" workflow calls) on fixtures in testdata/quality-checklist.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	qualityChecklistPath     = "docs/QUALITY-CHECKLIST.md"
	qualityChecklistMaxLines = 120
	qualityChecklistRowCount = 13
	// qualityChecklistSelfCap is today's number of "self" rows. When a row gets
	// a machine check (Q6's P0 census is next), lower this by one in the same PR.
	qualityChecklistSelfCap = 12
)

var (
	qualityRowRe    = regexp.MustCompile(`^\|\s*(Q\d+)\s*\|`)
	goTestFuncRe    = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
	goTestNameRe    = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)
	walkSkippedDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true}
)

// checkQualityChecklist returns one plain sentence per broken rule. testFuncs is
// the set of Go test function names that exist in the repo.
func checkQualityChecklist(doc string, testFuncs map[string]bool) []string {
	var problems []string

	lines := strings.Count(doc, "\n")
	if doc != "" && !strings.HasSuffix(doc, "\n") {
		lines++
	}
	if lines > qualityChecklistMaxLines {
		problems = append(problems, fmt.Sprintf(
			"%s is %d lines; the cap is %d. Cut words, not rows — the full history belongs in AUDIT-CHECKLIST.md",
			qualityChecklistPath, lines, qualityChecklistMaxLines))
	}

	var ids []string
	self := 0
	for _, ln := range strings.Split(doc, "\n") {
		m := qualityRowRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		id := m[1]
		ids = append(ids, id)
		cells := strings.Split(strings.Trim(strings.TrimSpace(ln), "|"), "|")
		if len(cells) != 5 {
			problems = append(problems, fmt.Sprintf(
				"%s: the row has %d cells, want 5 (# | rule | bug type | proof | enforced by)", id, len(cells)))
			continue
		}
		enforced := strings.TrimSpace(cells[4])
		switch {
		case enforced == "self":
			self++
		case strings.HasPrefix(enforced, "test:"):
			name := strings.TrimSpace(strings.TrimPrefix(enforced, "test:"))
			if !goTestNameRe.MatchString(name) {
				problems = append(problems, fmt.Sprintf(
					"%s: %q is not a Go test name; write \"test: TestName\"", id, name))
			} else if !testFuncs[name] {
				problems = append(problems, fmt.Sprintf(
					"%s: names test %s, but no func %s( exists in any *_test.go. Was it renamed or removed?", id, name, name))
			}
		case strings.HasPrefix(enforced, "ci:"):
			if strings.TrimSpace(strings.TrimPrefix(enforced, "ci:")) == "" {
				problems = append(problems, fmt.Sprintf("%s: \"ci:\" must name the CI check", id))
			}
		default:
			problems = append(problems, fmt.Sprintf(
				"%s: enforced-by %q must be \"self\", \"test: TestName\" or \"ci: <check>\"", id, enforced))
		}
	}

	var want []string
	for i := 1; i <= qualityChecklistRowCount; i++ {
		want = append(want, fmt.Sprintf("Q%d", i))
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		problems = append(problems, fmt.Sprintf(
			"rows are %v; want exactly %v in that order, one table row each", ids, want))
	}

	if self > qualityChecklistSelfCap {
		problems = append(problems, fmt.Sprintf(
			"%d rows are \"self\"; the cap is %d. A row may only move from self to a machine check, never back",
			self, qualityChecklistSelfCap))
	} else if self < qualityChecklistSelfCap {
		problems = append(problems, fmt.Sprintf(
			"%d rows are \"self\" but the cap is still %d. Lower qualityChecklistSelfCap to %d in this PR so the gain is locked in",
			self, qualityChecklistSelfCap, self))
	}
	return problems
}

// collectGoTestFuncs walks the repo and returns every Go test function name
// declared at the top level of a *_test.go file.
func collectGoTestFuncs(t *testing.T, root string) map[string]bool {
	t.Helper()
	funcs := map[string]bool{}
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && walkSkippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files++
		for _, m := range goTestFuncRe.FindAllStringSubmatch(string(b), -1) {
			funcs[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if files == 0 {
		t.Fatalf("found no *_test.go under %s; the census would pass on nothing", root)
	}
	return funcs
}

// TestQualityChecklistCensus checks the real page against the real repo.
func TestQualityChecklistCensus(t *testing.T) {
	doc := repoFile(t, qualityChecklistPath)
	funcs := collectGoTestFuncs(t, repoRoot(t))
	// A walker that silently scans nothing would make every "test:" row pass.
	if !funcs["TestQualityChecklistCensus"] {
		t.Fatalf("the test-name walk did not find this very test; it is not scanning the repo")
	}
	if problems := checkQualityChecklist(doc, funcs); len(problems) > 0 {
		t.Fatalf("docs/QUALITY-CHECKLIST.md breaks the census:\n  %s", strings.Join(problems, "\n  "))
	}
}

// setEnforcedBy returns doc with row id's last cell replaced by value.
func setEnforcedBy(t *testing.T, doc, id, value string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	for i, ln := range lines {
		if m := qualityRowRe.FindStringSubmatch(ln); m != nil && m[1] == id {
			cut := strings.LastIndex(strings.TrimRight(ln, " |"), "|")
			lines[i] = ln[:cut] + "| " + value + " |"
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("row %s not found in the checklist", id)
	return ""
}

func rowLine(t *testing.T, doc, id string) string {
	t.Helper()
	for _, ln := range strings.Split(doc, "\n") {
		if m := qualityRowRe.FindStringSubmatch(ln); m != nil && m[1] == id {
			return ln
		}
	}
	t.Fatalf("row %s not found in the checklist", id)
	return ""
}

// TestQualityChecklistCensusCatchesEachBreak breaks the real page one way at a
// time and checks the census names the break. Without this, a census whose
// guards were all switched off would still pass on today's healthy page.
func TestQualityChecklistCensusCatchesEachBreak(t *testing.T) {
	doc := repoFile(t, qualityChecklistPath)
	funcs := map[string]bool{"TestQualityChecklistCensus": true}
	if p := checkQualityChecklist(doc, funcs); len(p) > 0 {
		t.Fatalf("baseline page should pass: %v", p)
	}

	q2, q3, q13 := rowLine(t, doc, "Q2"), rowLine(t, doc, "Q3"), rowLine(t, doc, "Q13")
	swapped := strings.Replace(doc, q2, "\x00", 1)
	swapped = strings.Replace(swapped, q3, q2, 1)
	swapped = strings.Replace(swapped, "\x00", q3, 1)
	cases := []struct {
		name string
		doc  string
		want string // substring of one problem; "" means the page must pass
	}{
		{"too long", doc + strings.Repeat("filler\n", qualityChecklistMaxLines), "the cap is 120"},
		{"named test missing", setEnforcedBy(t, doc, "Q5", "test: TestNoSuchTestAnywhere"), "no func TestNoSuchTestAnywhere("},
		{"named test present", setEnforcedBy(t, doc, "Q5", "test: TestQualityChecklistCensus"), ""},
		{"not a test name", setEnforcedBy(t, doc, "Q5", "test: run it by hand"), "not a Go test name"},
		{"one more self row", setEnforcedBy(t, doc, "Q5", "self"), "the cap is 12"},
		{"one fewer self row", setEnforcedBy(t, doc, "Q6", "ci: p0 census"), "Lower qualityChecklistSelfCap to 11"},
		{"unknown enforced-by", setEnforcedBy(t, doc, "Q5", "reviewer"), "must be \"self\""},
		{"empty ci", setEnforcedBy(t, doc, "Q5", "ci:"), "must name the CI check"},
		{"rows out of order", swapped, "in that order"},
		{"row missing", strings.Replace(doc, q13+"\n", "", 1), "in that order"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := checkQualityChecklist(c.doc, funcs)
			if c.want == "" {
				if len(got) > 0 {
					t.Fatalf("want a pass, got: %v", got)
				}
				return
			}
			for _, p := range got {
				if strings.Contains(p, c.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got: %v", c.want, got)
		})
	}
}

// TestQualityChecklistPRScript runs the PR-body check on fixtures: a good body
// passes; a missing row, a bare tick, a short n/a, a cited test that is not in
// the diff, and a row hidden in an HTML comment each fail and name their row.
func TestQualityChecklistPRScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	const dir = "testdata/quality-checklist"
	diff := filepath.Join(dir, "pr.diff")

	good, err := os.ReadFile(filepath.Join(dir, "good.md"))
	if err != nil {
		t.Fatal(err)
	}
	// GitHub often stores PR bodies with CRLF line ends; a good body must still pass.
	crlf := filepath.Join(t.TempDir(), "good-crlf.md")
	if err := os.WriteFile(crlf, []byte(strings.ReplaceAll(string(good), "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		body     string
		wantExit int
		want     string
		diff     string // "" = pr.diff
		problems int    // 0 = exactly one when failing
	}{
		{body: filepath.Join(dir, "good.md"), want: "all 13 rows have proof or a reason"},
		{body: crlf, want: "all 13 rows have proof or a reason"},
		{body: filepath.Join(dir, "missing-row.md"), wantExit: 1, want: "Q7: the row is missing"},
		{body: filepath.Join(dir, "bare-tick.md"), wantExit: 1, want: "Q4: no proof"},
		{body: filepath.Join(dir, "short-na.md"), wantExit: 1, want: "Q2: the n/a reason is too short"},
		{body: filepath.Join(dir, "test-not-in-diff.md"), wantExit: 1, want: "Q9: cites TestFixtureNotInThisDiff, but TestFixtureNotInThisDiff is not in this PR's diff"},
		{body: filepath.Join(dir, "hidden-in-comment.md"), wantExit: 1, want: "Q4: the row is missing"},
		// m468 review: each claimed failure mode gets its own fixture.
		{body: filepath.Join(dir, "dup-row.md"), wantExit: 1, want: "Q3: the row appears more than once"},
		{body: filepath.Join(dir, "unknown-row.md"), wantExit: 1, want: "Q14: there is no row Q14"},
		// Three checkboxes and two ticks: 9 characters if the boxes were not
		// stripped, 0 once they are.
		{body: filepath.Join(dir, "tick-proof.md"), wantExit: 1, want: "Q3: the proof is empty or too short"},
		{body: filepath.Join(dir, "short-proof.md"), wantExit: 1, want: "Q9: the proof is empty or too short"},
		// TestFixtureGuard is a prefix of TestFixtureGuardTurnsRed, which IS in
		// the diff: only a whole-name match may count.
		{body: filepath.Join(dir, "prefix-name.md"), wantExit: 1, want: "Q9: cites TestFixtureGuard, but TestFixtureGuard is not in this PR's diff"},
		// A diff that DELETES the cited tests does not prove them.
		{body: filepath.Join(dir, "good.md"), wantExit: 1, diff: "deleted-tests.diff", problems: 2,
			want: "Q3: cites TestFixtureGuardTurnsRed, but TestFixtureGuardTurnsRed is not in this PR's diff"},
		// Nor does one where the cited test is only a context line around an edit.
		{body: filepath.Join(dir, "good.md"), wantExit: 1, diff: "context-only.diff",
			want: "Q3: cites TestFixtureGuardTurnsRed, but TestFixtureGuardTurnsRed is not in this PR's diff"},
	}
	for _, c := range cases {
		d := diff
		if c.diff != "" {
			d = filepath.Join(dir, c.diff)
		}
		t.Run(filepath.Base(c.body)+"+"+filepath.Base(d), func(t *testing.T) {
			out, err := exec.Command("bash", "quality-checklist-pr.sh", c.body, d).CombinedOutput()
			exit := 0
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("run: %v", err)
			}
			if exit != c.wantExit {
				t.Fatalf("exit %d, want %d; output:\n%s", exit, c.wantExit, out)
			}
			if !strings.Contains(string(out), c.want) {
				t.Fatalf("output lacks %q:\n%s", c.want, out)
			}
			// Each failing fixture breaks exactly one row; any other complaint
			// means the fixture or the script drifted.
			n := c.problems
			if n == 0 {
				n = 1
			}
			if c.wantExit == 1 && !strings.Contains(string(out), fmt.Sprintf("quality checklist: %d problem(s)", n)) {
				t.Fatalf("want exactly %d problem(s):\n%s", n, out)
			}
		})
	}
}
