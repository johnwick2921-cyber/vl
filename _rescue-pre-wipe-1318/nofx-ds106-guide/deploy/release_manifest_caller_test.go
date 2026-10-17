package deploy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// SKEPTIC [2]. manifest.sh refuses to list itself at 0 bytes — a correct guard,
// added in 3b-A. Its ONE caller redirected the script's stdout INTO the stage:
//
//	bash deploy/release/manifest.sh /tmp/stage … > /tmp/stage/manifest.json
//
// The shell creates that file at 0 bytes BEFORE the script starts, because
// redirection truncates first. The script then enumerates the stage, finds its
// own output file empty, and refuses. Every tag would have failed, on a guard
// that was right, with a message about a placeholder manifest.
//
// The guard was written without running its caller. So this test IS the caller:
// it EXTRACTS the command line from release.yml and runs it through a real
// shell. A test carrying its own copy of the command would prove the copy, not
// the workflow (canon 53 — exercise production call sites), and would keep
// passing while the workflow drifted.
func TestTheWorkflowsOwnManifestCommandProducesAUsableManifest(t *testing.T) {
	root := repoRoot(t)
	y := repoFile(t, ".github/workflows/release.yml")

	script := extractManifestScript(t, y)

	// A fixture stage shaped like the real one: the allow-list artifacts plus
	// the RELEASE marker package.sh writes.
	stage := t.TempDir()
	sha := strings.Repeat("a", 40)
	mustWrite(t, filepath.Join(stage, "nofx-bin"), "ELF-ish")
	mustWrite(t, filepath.Join(stage, "web", "dist", "index.html"), "<html></html>")
	mustWrite(t, filepath.Join(stage, "deploy", "RELEASE"), sha+"\n")

	// Substitute the workflow's expressions with the fixture's values. The
	// COMMAND SHAPE — including where stdout goes — is the workflow's own.
	line := strings.ReplaceAll(script, "/tmp/stage", stage)
	line = regexp.MustCompile(`\$\{\{[^}]*outputs\.sha[^}]*\}\}`).ReplaceAllString(line, sha)
	line = regexp.MustCompile(`\$\{\{[^}]*release_id[^}]*\}\}`).ReplaceAllString(line, "v0.0.0-test")
	line = regexp.MustCompile(`\$\{\{[^}]*\}\}`).ReplaceAllString(line, "test")

	cmd := exec.Command("bash", "-c", line)
	cmd.Dir = root
	// PR B [11]: the workflow passes the user-influenced release_id via the
	// step's env:, and so does this caller — the command reads "$RELEASE_ID".
	cmd.Env = append(os.Environ(), "RELEASE_ID=v0.0.0-test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the workflow's own manifest command FAILED:\n  %s\n%s", line, out)
	}

	// Wherever the workflow put it, a manifest must exist and parse.
	manifestPath := manifestDestination(t, line, stage)
	b, rerr := os.ReadFile(manifestPath)
	if rerr != nil {
		t.Fatalf("no manifest at %s after the workflow's command: %v", manifestPath, rerr)
	}
	var m struct {
		Artifacts []struct {
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
		} `json:"artifacts"`
		SourceSHA string `json:"source_sha"`
	}
	if jerr := json.Unmarshal(b, &m); jerr != nil {
		t.Fatalf("manifest is not valid JSON: %v\n%s", jerr, b)
	}
	if m.SourceSHA != sha {
		t.Fatalf("manifest source_sha = %q, want the fixture's %q", m.SourceSHA, sha)
	}
	if len(m.Artifacts) == 0 {
		t.Fatal("manifest lists no artifacts at all")
	}
	// THE ASSERTION THAT WOULD HAVE CAUGHT [2]: the manifest must never
	// describe itself as an empty artifact.
	for _, a := range m.Artifacts {
		if filepath.Base(a.Path) == "manifest.json" && a.Bytes == 0 {
			t.Fatalf("the manifest lists ITSELF at 0 bytes (%s) — the sha256 of the empty string matches nothing that ships", a.Path)
		}
	}
}

// extractManifestScript pulls the WHOLE run block that builds the manifest out
// of the workflow, so the test runs the real sequence rather than one line of
// it. The fix for [2] adds a `mv` on a LATER line; a test that extracted only
// the `bash …` line would look for the manifest where the redirect left it and
// never check that it reaches the stage — passing while the step was still
// half-done.
func extractManifestScript(t *testing.T, workflow string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	start := -1
	for i, ln := range lines {
		if strings.Contains(ln, "bash deploy/release/manifest.sh") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("release.yml no longer invokes deploy/release/manifest.sh — this test has lost its subject and is proving nothing")
	}
	indent := len(lines[start]) - len(strings.TrimLeft(lines[start], " "))
	var block []string
	for i := start; i < len(lines); i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" {
			break
		}
		cur := len(ln) - len(strings.TrimLeft(ln, " "))
		if cur < indent || strings.HasPrefix(strings.TrimSpace(ln), "- name:") {
			break
		}
		block = append(block, strings.TrimSpace(ln))
	}
	return strings.Join(block, "\n")
}

// manifestDestination is always the STAGE: whatever route the step takes, the
// archive is built from the stage, so a manifest that never arrives there is
// not a manifest.
func manifestDestination(t *testing.T, line, stage string) string {
	t.Helper()
	return filepath.Join(stage, "manifest.json")
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
