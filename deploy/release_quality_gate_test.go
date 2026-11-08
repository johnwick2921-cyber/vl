package deploy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// m468 Q5 — deploy/release/quality-gate.sh, driven end to end.
//
// The script runs for real against a real git repo built in t.TempDir (real
// merge commits, real ^2, real trees). Only gh and sleep are faked: gh answers
// from the JSON fixtures in testdata/quality-gate, keyed by commit sha, and
// sleep only records that it was asked to wait. So these tests read the same
// git output and the same check-run JSON shape the release job does.

// qgRepo holds the commits the cases point at.
type qgRepo struct {
	dir       string
	plain     string // a single-parent commit
	mergeSame string // a 2-parent merge whose tree equals its ^2
	headSame  string // ^2 of mergeSame
	mergeDiff string // a 2-parent merge whose tree differs from its ^2
	headDiff  string // ^2 of mergeDiff
}

func qgGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=qg", "GIT_AUTHOR_EMAIL=qg@example.invalid",
		"GIT_COMMITTER_NAME=qg", "GIT_COMMITTER_EMAIL=qg@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func qgBuildRepo(t *testing.T) qgRepo {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(name, body string) string {
		write(name, body)
		qgGit(t, dir, "add", name)
		qgGit(t, dir, "commit", "-q", "-m", name)
		return qgGit(t, dir, "rev-parse", "HEAD")
	}
	qgGit(t, dir, "init", "-q", "-b", "main")
	commit("base.txt", "base\n")

	// PR 1: main does not move while the PR is open, so the --no-ff merge has
	// exactly the PR head's tree (the shape of every recent vl tag).
	qgGit(t, dir, "checkout", "-q", "-b", "pr1")
	headSame := commit("pr1.txt", "pr1\n")
	qgGit(t, dir, "checkout", "-q", "main")
	qgGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge pr1", "pr1")
	mergeSame := qgGit(t, dir, "rev-parse", "HEAD")

	// PR 2: main moves after the PR branched, so the merge's tree differs from
	// the PR head's — the PR's checks tested other code.
	qgGit(t, dir, "checkout", "-q", "-b", "pr2")
	headDiff := commit("pr2.txt", "pr2\n")
	qgGit(t, dir, "checkout", "-q", "main")
	plain := commit("main-moved.txt", "moved\n")
	qgGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge pr2", "pr2")
	mergeDiff := qgGit(t, dir, "rev-parse", "HEAD")

	r := qgRepo{dir: dir, plain: plain, mergeSame: mergeSame, headSame: headSame, mergeDiff: mergeDiff, headDiff: headDiff}
	// The fixtures only mean something if the repo has the shapes they claim.
	if qgGit(t, dir, "rev-parse", mergeSame+"^{tree}") != qgGit(t, dir, "rev-parse", headSame+"^{tree}") {
		t.Fatal("fixture repo: mergeSame must have its ^2's tree")
	}
	if qgGit(t, dir, "rev-parse", mergeDiff+"^{tree}") == qgGit(t, dir, "rev-parse", headDiff+"^{tree}") {
		t.Fatal("fixture repo: mergeDiff must NOT have its ^2's tree")
	}
	if qgGit(t, dir, "rev-parse", mergeSame+"^2") != headSame || qgGit(t, dir, "rev-parse", mergeDiff+"^2") != headDiff {
		t.Fatal("fixture repo: ^2 is not the PR head")
	}
	return r
}

// fakeGh answers `gh api ... commits/<sha>/check-runs...` from a map file of
// "<sha> <fixture>" lines. A fixture of "!403" prints gh's real 403 wording and
// exits 1. An unmapped sha exits 99 so a case that reads a commit it did not
// set up shows up as "could not read checks", never as a pass.
//
// A comma list "a.json,b.json" answers the Nth read of that sha with the Nth
// fixture (the last one repeats), so a case can play a suite that is still
// running at the first poll and finished at the next — the real order of
// events on a tag push.
const fakeGh = `#!/usr/bin/env bash
path=""
for a in "$@"; do case "$a" in *commits/*/check-runs*) path="$a" ;; esac; done
sha="${path#*commits/}"; sha="${sha%%/*}"
echo "$sha" >> "$QG_GH_LOG"
n="$(grep -cx "$sha" "$QG_GH_LOG")"
ans="$(awk -v s="$sha" '$1==s {print $2}' "$QG_MAP")"
IFS=, read -r -a seq <<<"$ans"
if [ "${#seq[@]}" -gt 1 ]; then
  i=$((n - 1)); [ "$i" -lt "${#seq[@]}" ] || i=$((${#seq[@]} - 1))
  ans="${seq[$i]}"
fi
case "$ans" in
  "") echo "fake gh: no fixture for $sha" >&2; exit 99 ;;
  '!403') echo "gh: Resource not accessible by integration (HTTP 403)" >&2; exit 1 ;;
  *) cat "$QG_FIX/$ans" ;;
esac
`

const fakeSleep = `#!/usr/bin/env bash
echo "$1" >> "$QG_SLEEP_LOG"
`

type qgResult struct {
	out    string
	code   int
	sleeps []string
}

func runQualityGate(t *testing.T, repo string, sha string, fixtures map[string]string, maxWaitMin string) qgResult {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "deploy/release/quality-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// A missing script exits 127 — non-zero, so every "it refused" case would
	// pass on "it was not there".
	if st, serr := os.Stat(script); serr != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("deploy/release/quality-gate.sh must exist and be executable (%v)", serr)
	}
	fix, err := filepath.Abs(filepath.Join("testdata", "quality-gate"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	gh := filepath.Join(work, "gh")
	slp := filepath.Join(work, "sleep")
	for p, body := range map[string]string{gh: fakeGh, slp: fakeSleep} {
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var m strings.Builder
	for s, f := range fixtures {
		m.WriteString(s + " " + f + "\n")
	}
	mapFile := filepath.Join(work, "map")
	if err := os.WriteFile(mapFile, []byte(m.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sleepLog := filepath.Join(work, "sleep.log")
	cmd := exec.Command("bash", script, sha)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GH_BIN="+gh, "SLEEP_BIN="+slp, "GIT_BIN=git",
		"GITHUB_REPOSITORY=example/repo", "GATE_TAG=v9.9.9",
		"GATE_MAX_WAIT_MIN="+maxWaitMin,
		"QG_MAP="+mapFile, "QG_FIX="+fix,
		"QG_GH_LOG="+filepath.Join(work, "gh.log"), "QG_SLEEP_LOG="+sleepLog,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	res := qgResult{out: string(out)}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.code = ee.ExitCode()
	default:
		t.Fatalf("running quality-gate.sh: %v\n%s", err, out)
	}
	if b, rerr := os.ReadFile(sleepLog); rerr == nil {
		res.sleeps = strings.Fields(string(b))
	}
	return res
}

func (r qgResult) verdict(t *testing.T) string {
	t.Helper()
	for _, l := range strings.Split(r.out, "\n") {
		if strings.HasPrefix(l, "QUALITY GATE: ") {
			return l
		}
	}
	t.Fatalf("no one-line verdict (QUALITY GATE: ...) in:\n%s", r.out)
	return ""
}

func TestQualityGatePassesWhenTheRequiredChecksAreGreenOnTheCommit(t *testing.T) {
	r := qgBuildRepo(t)
	// green.json also carries a FAILED non-required check and a failed
	// "Frontend Tests (React/TypeScript)" — neither is a required NAME.
	res := runQualityGate(t, r.dir, r.plain, map[string]string{r.plain: "green.json"}, "300")
	if res.code != 0 {
		t.Fatalf("all three required checks are success on the commit: want exit 0, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "PASS") || !strings.Contains(v, "on the tagged commit") {
		t.Fatalf("verdict = %q", v)
	}
	if len(res.sleeps) != 0 {
		t.Fatalf("a green commit must not wait, slept %v", res.sleeps)
	}
}

func TestQualityGatePassesOnThePRHeadWhenTheTreesAreIdentical(t *testing.T) {
	r := qgBuildRepo(t)
	// The merge's own fast suites are green; its own slow race suite (~2-3 h)
	// is still running, and the PR head ran that suite on the same code.
	res := runQualityGate(t, r.dir, r.mergeSame, map[string]string{r.mergeSame: "fast-green-slow-running.json", r.headSame: "green.json"}, "300")
	if res.code != 0 {
		t.Fatalf("own fast suites green, ^2 green with an identical tree: want exit 0, got %d\n%s", res.code, res.out)
	}
	v := res.verdict(t)
	if !strings.Contains(v, "PASS") || !strings.Contains(v, "Go Unit Tests & Coverage success on the PR head "+r.headSame[:12]) {
		t.Fatalf("the verdict must say the PR head answered for the slow suite, got %q", v)
	}
	if !strings.Contains(v, "Backend Tests, Frontend Tests success on the tagged commit") {
		t.Fatalf("the verdict must say the fast suites passed on the tagged commit itself, got %q", v)
	}
	if len(res.sleeps) != 0 {
		t.Fatalf("an identical-tree PR head that is green needs no wait for the slow suite, slept %v", res.sleeps)
	}
}

// The real tag-push timeline of b81b03152 (v2026.10.28.1): the release run
// started at 13:16:12Z, the merge commit's own Backend Tests at 13:16:13Z, and
// that check finished "failure" at 13:23:19Z (check-run 122838886282). The PR
// head bb0c410f had been green since 10-26. At the first poll the commit's own
// suite is still running; the PR head must NOT answer for it, or the gate
// passes a release whose own tests then go red.
func TestQualityGateWaitsForTheCommitsOwnFastSuitesEvenWhenThePRHeadIsGreen(t *testing.T) {
	r := qgBuildRepo(t)
	res := runQualityGate(t, r.dir, r.mergeSame, map[string]string{
		r.mergeSame: "running.json,own-backend-red-slow-running.json",
		r.headSame:  "green.json",
	}, "300")
	if res.code != 1 {
		t.Fatalf("the commit's own Backend Tests went red after the first poll: want exit 1, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "FAIL") || !strings.Contains(v, "Backend Tests = failure") || !strings.Contains(v, "on the tagged commit") {
		t.Fatalf("verdict = %q", v)
	}
	if strings.Join(res.sleeps, ",") != "120" {
		t.Fatalf("want one 120 s poll (running, then red), got %v", res.sleeps)
	}
}

// The same timeline when the suite comes back green: one wait, then PASS.
func TestQualityGatePassesOnceTheCommitsOwnFastSuitesFinishGreen(t *testing.T) {
	r := qgBuildRepo(t)
	res := runQualityGate(t, r.dir, r.mergeSame, map[string]string{
		r.mergeSame: "running.json,fast-green-slow-running.json",
		r.headSame:  "green.json",
	}, "300")
	if res.code != 0 {
		t.Fatalf("want exit 0, got %d\n%s", res.code, res.out)
	}
	if strings.Join(res.sleeps, ",") != "120" {
		t.Fatalf("want exactly one 120 s poll while the fast suites ran, got %v", res.sleeps)
	}
}

// The PR head never answers for the fast suites: with nothing reported on the
// tagged commit, a green ^2 is not enough and the gate times out.
func TestQualityGateDoesNotLetThePRHeadAnswerForTheFastSuites(t *testing.T) {
	r := qgBuildRepo(t)
	res := runQualityGate(t, r.dir, r.mergeSame, map[string]string{r.mergeSame: "empty.json", r.headSame: "green.json"}, "4")
	if res.code != 1 {
		t.Fatalf("own fast suites never reported: want exit 1, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "FAIL") || !strings.Contains(v, "timed out") {
		t.Fatalf("want a timed-out FAIL verdict, got %q", v)
	}
	if strings.Join(res.sleeps, ",") != "120,120" {
		t.Fatalf("want two 120 s polls before giving up, got %v", res.sleeps)
	}
}

// The pass rule is "every required check green". Two green and one still
// running, or two green and one missing, is unknown — and unknown is not clear.
func TestQualityGateNeedsEveryRequiredCheckGreen(t *testing.T) {
	r := qgBuildRepo(t)
	for _, fix := range []string{"fast-green-slow-running.json", "backend-missing.json"} {
		res := runQualityGate(t, r.dir, r.plain, map[string]string{r.plain: fix}, "4")
		if res.code != 1 {
			t.Fatalf("%s: two of three green must not pass: want exit 1, got %d\n%s", fix, res.code, res.out)
		}
		if v := res.verdict(t); !strings.Contains(v, "FAIL") || !strings.Contains(v, "timed out") {
			t.Fatalf("%s: want a timed-out FAIL verdict, got %q", fix, v)
		}
		if strings.Join(res.sleeps, ",") != "120,120" {
			t.Fatalf("%s: want two 120 s polls before giving up, got %v", fix, res.sleeps)
		}
	}
}

func TestQualityGateIgnoresAGreenPRHeadWhoseTreeDiffersAndTimesOut(t *testing.T) {
	r := qgBuildRepo(t)
	// 4 minutes = two 120 s polls, then the verdict. The commit's own fast
	// suites are green, so only the slow suite is open — exactly the one the
	// PR head could answer for if its tree were identical.
	res := runQualityGate(t, r.dir, r.mergeDiff, map[string]string{r.mergeDiff: "fast-green-slow-running.json", r.headDiff: "green.json"}, "4")
	if res.code != 1 {
		t.Fatalf("the PR head tested OTHER code: its green must not pass the gate. want exit 1, got %d\n%s", res.code, res.out)
	}
	v := res.verdict(t)
	if !strings.Contains(v, "FAIL") || !strings.Contains(v, "timed out") {
		t.Fatalf("want a timed-out FAIL verdict, got %q", v)
	}
	if !strings.Contains(res.out, "different tree") {
		t.Fatalf("the output must say why ^2 was not used:\n%s", res.out)
	}
	if strings.Join(res.sleeps, ",") != "120,120" {
		t.Fatalf("want two 120 s polls before giving up, got %v", res.sleeps)
	}
}

func TestQualityGateFailsAtOnceWhenARequiredCheckFailed(t *testing.T) {
	r := qgBuildRepo(t)
	res := runQualityGate(t, r.dir, r.plain, map[string]string{r.plain: "failed-backend.json"}, "300")
	if res.code != 1 {
		t.Fatalf("Backend Tests failed: want exit 1, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "FAIL") || !strings.Contains(v, "Backend Tests = failure") {
		t.Fatalf("the verdict must name the failed test job, got %q", v)
	}
	for _, want := range []string{
		"gh run rerun 40694885384 --failed --repo example/repo",
		"gh workflow run release.yml --repo example/repo -f tag=v9.9.9",
	} {
		if !strings.Contains(res.out, want) {
			t.Fatalf("the retry recipe must carry %q:\n%s", want, res.out)
		}
	}
	if strings.Contains(res.out, "gh run rerun 40694885280") {
		t.Fatalf("the recipe must re-run only the run that failed, not the green Go run:\n%s", res.out)
	}
	if len(res.sleeps) != 0 {
		t.Fatalf("a finished failure must fail AT ONCE, not wait; slept %d times", len(res.sleeps))
	}
}

// The shape of b81b03152 (release #52): the merge's own suite went red while
// the PR head was green on the identical tree. That exact code was tested and
// failed, so the PR head must not overrule it.
func TestQualityGateFailsWhenTheCommitItselfIsRedEvenIfThePRHeadIsGreen(t *testing.T) {
	r := qgBuildRepo(t)
	res := runQualityGate(t, r.dir, r.mergeSame, map[string]string{r.mergeSame: "failed-backend.json", r.headSame: "green.json"}, "300")
	if res.code != 1 {
		t.Fatalf("the tagged commit failed Backend Tests: want exit 1, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "FAIL") || !strings.Contains(v, "on the tagged commit") {
		t.Fatalf("verdict = %q", v)
	}
	if len(res.sleeps) != 0 {
		t.Fatalf("must fail at once, slept %v", res.sleeps)
	}
}

func TestQualityGateFailsAtOnceWhenChecksCannotBeRead(t *testing.T) {
	r := qgBuildRepo(t)
	res := runQualityGate(t, r.dir, r.plain, map[string]string{r.plain: "!403"}, "300")
	if res.code != 1 {
		t.Fatalf("HTTP 403: want exit 1, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "FAIL") || !strings.Contains(v, "cannot read checks") || !strings.Contains(v, "HTTP 403") {
		t.Fatalf("verdict = %q", v)
	}
	if len(res.sleeps) != 0 {
		t.Fatalf("a 403 is never 'still pending' — it must fail at once, slept %d times", len(res.sleeps))
	}
}

func TestQualityGateCountsOnlyTheLatestAttemptOfACheck(t *testing.T) {
	r := qgBuildRepo(t)
	// Backend Tests: id 100 failure (first in the list), id 300 success (the
	// re-run), id 99 cancelled (last in the list). Only the highest id counts,
	// so neither "first match" nor "last match" can pass this.
	res := runQualityGate(t, r.dir, r.plain, map[string]string{r.plain: "rerun-green.json"}, "300")
	if res.code != 0 {
		t.Fatalf("the latest Backend Tests attempt succeeded: want exit 0, got %d\n%s", res.code, res.out)
	}
	if v := res.verdict(t); !strings.Contains(v, "PASS") {
		t.Fatalf("verdict = %q", v)
	}
}
