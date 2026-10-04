package api

// W-ONE-BUTTON M4 3b-B U5b (b) — the install gate's verifier reads the
// worker's VERDICT file, at the production call site (the gin install route
// through updatesGate, knob ON). The verdict is written by the PRODUCTION
// writer, internal/updaterworker.FetchRelease, over a release made exactly
// the way the U3 tests make one: 3a's deploy/release/package.sh stages it,
// manifest.sh writes the manifest (outside the stage, brief C8), the REAL
// `ssh-keygen -Y sign -n release` signs it, GNU tar packs it. The import of
// internal/updaterworker here is test-only: the trading-app import guard
// counts non-test files only (and the app binary never links it —
// TestTradingAppLinkageFromTheToolchain).

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/updateauth"
	"vl/internal/updaterjob"
	"vl/internal/updaterworker"
)

const verdictTestSHA = "c0ffeec0ffeec0ffeec0ffeec0ffeec0ffeeabcd"

func keygenTestEnv(t *testing.T) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "LC_ALL=C"}
}

func runTool(t *testing.T, dir string, stdoutOnly bool, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = keygenTestEnv(t)
	var out []byte
	var err error
	if stdoutOnly {
		out, err = cmd.Output()
	} else {
		out, err = cmd.CombinedOutput()
	}
	if err != nil {
		var stderr []byte
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = ee.Stderr
		}
		t.Fatalf("%s %v: %v\n%s%s", name, args, err, out, stderr)
	}
	return out
}

// fetchTestRelease builds a signed release for releaseID and runs the
// production FetchRelease into dataDir; it returns the verdict it wrote.
func fetchTestRelease(t *testing.T, dataDir, releaseID string) updaterjob.Verdict {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is absent — the verdict is written by the production fetch over a REAL ssh-keygen signature")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	src := filepath.Join(work, "src")
	for rel, body := range map[string]string{
		"vl-bin":                             "\x7fELF u5b stand-in binary\n",
		"LICENSE":                              "test licence\n",
		"ninjascript/vltrader_tcp_PROTOCOL.md": "protocol_version: 3\n",
		"ninjascript/VLTraderTCPClient.cs":           "public const string VL_BUILD_ID = \"2026-09-24-u5b\";\n",
		"web/dist/assets/app.js":               "console.log('u5b')\n",
		"web/dist/index.html":                  "<!doctype html><title>u5b</title>\n",
		"deploy/RELEASE":                       strings.Repeat("a", 40) + "\n",
	} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(src, "vl-bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(work, "stage")
	runTool(t, repo, false, "bash", "deploy/release/package.sh", src, stage, verdictTestSHA)
	manifest := runTool(t, repo, true, "bash", "deploy/release/manifest.sh", stage, verdictTestSHA, releaseID)
	mpath := filepath.Join(stage, "manifest.json")
	if err := os.WriteFile(mpath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(work, "release-signer")
	runTool(t, work, false, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "vl-test-u5b", "-f", priv)
	runTool(t, work, false, "ssh-keygen", "-Y", "sign", "-f", priv, "-n", "release", mpath)
	pub, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	signers := filepath.Join(work, "allowed_signers")
	if err := os.WriteFile(signers, []byte("release "+strings.TrimSpace(string(pub))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(work, releaseID+".tar.gz")
	runTool(t, work, false, "tar", "-C", stage, "-czf", archive, ".")
	releaseRoot := filepath.Join(work, "releases")
	if err := os.Mkdir(releaseRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	v, err := updaterworker.FetchRelease(updaterworker.FetchConfig{
		Archive: archive, ReleaseID: releaseID, ReleaseRoot: releaseRoot,
		AllowedSigners: signers, DataDir: dataDir, Now: func() time.Time { return time.Now() },
	})
	if err != nil {
		t.Fatalf("the production fetch refused a release made by 3a's own scripts: %v", err)
	}
	return v
}

func TestVerifierReadsTheVerdictFile(t *testing.T) {
	t.Setenv(updaterKnobEnv, "1")
	e := newUpdEnv(t)
	if name := updateVerifierName(e.s.updateVerifier); name != "verdict-file" {
		t.Fatalf("knob ON: the server holds verifier %q, want verdict-file", name)
	}

	// no verdict on disk ⇒ 422, unchanged (the job id is spent: M3's order)
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity || w.Body.String() != m3Install422 {
		t.Fatalf("knob ON, no verdict: install = %d %s, want 422 %s", w.Code, w.Body.String(), m3Install422)
	}

	v := fetchTestRelease(t, e.dataDir, updRelease)
	if v.ReleaseID != updRelease || v.SourceSHA != verdictTestSHA {
		t.Fatalf("fixture: verdict = %+v", v)
	}
	// the production verdict ⇒ past the verifier: no starter reachable ⇒ 503
	// (the stage AFTER the verifier), never 422
	w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease)))
	if w.Code == http.StatusUnprocessableEntity {
		t.Fatalf("knob ON with the production verdict on disk: install = 422 — the verifier did not read the verdict file")
	}
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"error":"installer unavailable"}` {
		t.Fatalf("knob ON with the verdict, no worker: install = %d %s, want 503 installer unavailable", w.Code, w.Body.String())
	}
	if m, err := e.s.updateVerifier.VerifiedManifest(updRelease); err != nil || m.ReleaseID != updRelease {
		t.Fatalf("VerifiedManifest(%s) = %+v, %v", updRelease, m, err)
	}

	// another release id: its own verdict is absent ⇒ ErrNoVerifiedManifest
	if _, err := e.s.updateVerifier.VerifiedManifest("v2026.09.24-2"); err == nil || !errors.Is(err, updateauth.ErrNoVerifiedManifest) {
		t.Fatalf("absent verdict: err = %v, want ErrNoVerifiedManifest", err)
	}

	// a mismatch: the SAME verdict bytes planted under another release's name
	// (ReadVerdict binds the file name to its release_id) ⇒ 422
	vp, err := updaterjob.VerdictPath(e.dataDir, updRelease)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(vp)
	if err != nil {
		t.Fatal(err)
	}
	const other = "v2026.09.24-9"
	op, _ := updaterjob.VerdictPath(e.dataDir, other)
	if err := os.WriteFile(op, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(other))); w.Code != http.StatusUnprocessableEntity || w.Body.String() != m3Install422 {
		t.Fatalf("a verdict naming %s planted as %s: install = %d %s, want 422", updRelease, other, w.Code, w.Body.String())
	}
	// a corrupt verdict (not a JSON object) ⇒ 422, never a 5xx or a pass
	if err := os.WriteFile(vp, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("corrupt verdict: install = %d %s, want 422", w.Code, w.Body.String())
	}
}

// OFF, the same production verdict on disk changes nothing: the stub still
// answers (M3's 422, M3's status bytes).
func TestVerdictOnDiskIsInertWithTheKnobOff(t *testing.T) {
	t.Setenv(updaterKnobEnv, "")
	e := newUpdEnv(t)
	fetchTestRelease(t, e.dataDir, updRelease)
	if _, err := os.Stat(filepath.Join(e.dataDir, "updater", "verdicts", updRelease+".json")); err != nil {
		t.Fatalf("fixture: no verdict on disk: %v", err)
	}
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity || w.Body.String() != m3Install422 {
		t.Fatalf("knob OFF with a verdict on disk: install = %d %s, want M3's 422", w.Code, w.Body.String())
	}
	if w := e.do("GET", "/api/updates", ""); w.Body.String() != m3StatusBody {
		t.Fatalf("knob OFF with a verdict on disk: status = %s, want %s", w.Body.String(), m3StatusBody)
	}
}
