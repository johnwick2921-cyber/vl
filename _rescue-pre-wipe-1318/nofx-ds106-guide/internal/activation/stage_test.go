package activation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests build a REAL binary with the REAL toolchain, because the thing
// under test is what Go stamps into a binary and that cannot be faked
// convincingly. A hand-written fixture would only prove the fixture matches the
// assertion — the wave that produced this package lost a day to exactly that
// (CLASS 239: a test that pins a defect will defend the defect).

const tinyMain = "package main\n\nfunc main() {}\n"

// buildTiny compiles a one-file module in a temp dir. withGit controls whether
// the module is a git repository, which is what decides whether Go stamps
// vcs.* into the binary at all. dirty leaves an uncommitted change behind.
func buildTiny(t *testing.T, withGit, dirty bool) (bin string, sha string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module tiny\n\ngo 1.21\n")
	write("main.go", tinyMain)

	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	if withGit {
		run("git", "init", "-q", "-b", "main")
		run("git", "add", "-A")
		run("git", "commit", "-q", "-m", "tiny")
		sha = run("git", "rev-parse", "HEAD")
		if dirty {
			write("main.go", tinyMain+"\n// uncommitted\n")
		}
	}
	bin = filepath.Join(dir, "tiny-bin")
	run("go", "build", "-o", bin, ".")
	return bin, sha
}

// stageRelease assembles a release dir around an already-built binary.
func stageRelease(t *testing.T, bin, sourceSHA, md5sum string) Release {
	t.Helper()
	dir := t.TempDir()
	dst := filepath.Join(dir, "nofx-bin")
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	m := Manifest{SourceSHA: sourceSHA, BinaryMD5: md5sum, Signature: "verified"}
	mb, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	rel, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return rel
}

// CLASS 248. The refusal an operator is MOST likely to hit, because every lane
// builds in a linked git worktree and those builds carry no vcs stamps at all.
// It must say so, rather than accusing the binary of being the wrong one.
func TestStageRefusesABinaryWithNoVCSStampsAndSaysWhy(t *testing.T) {
	bin, _ := buildTiny(t, false, false)
	rel := stageRelease(t, bin, "0000000000000000000000000000000000000000", "")
	rc, err := Stage(rel)
	if err == nil {
		t.Fatal("Stage accepted a binary with no vcs stamps")
	}
	if rc.OK {
		t.Fatal("receipt says OK on a refusal")
	}
	if !strings.Contains(err.Error(), "NO vcs stamps at all") {
		t.Fatalf("refusal does not name the cause: %v", err)
	}
	if !strings.Contains(err.Error(), "worktree") || !strings.Contains(err.Error(), "clean clone") {
		t.Fatalf("refusal must name the cause AND the cure, got: %v", err)
	}
}

func TestStageRefusesAStampedBinaryWithADifferentRevision(t *testing.T) {
	bin, sha := buildTiny(t, true, false)
	if sha == "" {
		t.Fatal("no sha from the git build — the fixture is not proving anything")
	}
	rel := stageRelease(t, bin, "1111111111111111111111111111111111111111", "")
	rc, err := Stage(rel)
	if err == nil {
		t.Fatal("Stage accepted a binary stamped with a different revision")
	}
	// The two causes must NOT share a message: an unstamped binary and a
	// wrong-revision binary send the operator to different places.
	if strings.Contains(err.Error(), "NO vcs stamps") {
		t.Fatalf("wrong-revision refusal reused the unstamped message: %v", err)
	}
	if !strings.Contains(err.Error(), "is stamped, but with revision") {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if rc.Evidence["vcs.revision"] != sha {
		t.Fatalf("evidence must carry the revision READ from the binary, got %q want %q", rc.Evidence["vcs.revision"], sha)
	}
}

func TestStageRefusesABinaryBuiltFromADirtyTree(t *testing.T) {
	bin, sha := buildTiny(t, true, true)
	rel := stageRelease(t, bin, sha, "")
	_, err := Stage(rel)
	if err == nil {
		t.Fatal("Stage accepted a binary built from a dirty tree")
	}
	if !strings.Contains(err.Error(), "DIRTY tree") {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

func TestStageRefusesABinaryWhoseHashIsNotTheManifests(t *testing.T) {
	bin, sha := buildTiny(t, true, false)
	rel := stageRelease(t, bin, sha, "deadbeefdeadbeefdeadbeefdeadbeef")
	_, err := Stage(rel)
	if err == nil {
		t.Fatal("Stage accepted a binary whose md5 is not the manifest's")
	}
	if !strings.Contains(err.Error(), "hashes to") {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

// The happy path exists so the refusals above are known to be refusals and not
// a Stage that rejects everything.
func TestStageAcceptsTheBinaryTheManifestDescribes(t *testing.T) {
	bin, sha := buildTiny(t, true, false)
	rel := stageRelease(t, bin, sha, "")
	sum, err := fileMD5(rel.Binary)
	if err != nil {
		t.Fatalf("md5: %v", err)
	}
	rel2 := stageRelease(t, bin, sha, sum)
	rc, err := Stage(rel2)
	if err != nil {
		t.Fatalf("Stage refused a correct release: %v", err)
	}
	if !rc.OK {
		t.Fatal("receipt not OK on success")
	}
	if rc.Evidence["vcs.modified"] != "false" {
		t.Fatalf("evidence must record what it read, got %q", rc.Evidence["vcs.modified"])
	}
	_ = rel
}

func TestResolveRefusesAManifestWithNoSignatureVerdict(t *testing.T) {
	dir := t.TempDir()
	mb, _ := json.Marshal(Manifest{SourceSHA: strings.Repeat("a", 40)})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(dir); err == nil {
		t.Fatal("Resolve accepted a manifest with no signature verdict")
	} else if !strings.Contains(err.Error(), "no signature verdict") {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

// The classic /proc parsing trap: comm is in parentheses and may contain both
// spaces and parentheses, so splitting the whole line on whitespace shifts
// every field after it.
func TestStatField22SurvivesACommWithSpacesAndParentheses(t *testing.T) {
	// fields: 1=pid 2=comm 3=state, then 4..; field 22 must read 987654.
	tail := ""
	for i := 4; i <= 21; i++ {
		tail += " 0"
	}
	line := "4242 (my prog (2)) S" + tail + " 987654 rest here"
	got, err := statField22(line)
	if err != nil {
		t.Fatalf("statField22: %v", err)
	}
	if got != 987654 {
		t.Fatalf("field 22 = %d, want 987654 — the comm field shifted the parse", got)
	}
}
