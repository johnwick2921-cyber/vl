package deploy

// UPDATER-USABLE-V1 (item B) — pins for the updater worker's systemd --user
// unit template and the install script's refusals. Production call sites:
// the REAL template and the REAL script on disk, run with bash.
//
// The CTO's mutations hit the script refusals: a run whose output carries the
// token value, a VL_RELEASE_DIR inside ~/vl that passes, a non-hex sha
// that is accepted, a unit that re-joins the bot's cgroup or re-sets TZ.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdaterWorkerServiceTemplate(t *testing.T) {
	svc := repoFile(t, "deploy/systemd-user/vl-updater.service")
	for _, want := range []string{
		"[Unit]",
		"Type=simple",
		"ExecStart=%h/bin/vl-updater --install-dir %h/vl serve",
		"EnvironmentFile=%h/.config/vl-updater/env",
		"UnsetEnvironment=TZ",
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(svc, want) {
			t.Fatalf("unit template must carry %q:\n%s", want, svc)
		}
	}
	// NOT in vl.service's cgroup: a --user unit lives in its own cgroup
	// tree by construction, and no slice may pin it anywhere else (serve
	// itself refuses the bot's cgroup — ErrBotCgroup).
	for _, l := range strings.Split(svc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Slice=") {
			t.Fatalf("unit template must not pin a cgroup slice: line %q", l)
		}
	}
}

func TestInstallUpdaterWorkerScript(t *testing.T) {
	script := filepath.Join("..", "deploy", "install-updater-worker.sh")
	content := repoFile(t, "deploy/install-updater-worker.sh")

	// The build must be from a NAMED CLEAN sha, and the stamp must be READ
	// back out of the binary — a mis-labelled build is a refusal.
	for _, want := range []string{
		"-trimpath",
		"vcs.modified=false",
		"vcs.revision=$SHA",
		"40-hex",
		// item 7: builds cmd/vl-updater, stamps and installs the vl
		// binary, and installs the vl-updater unit (D2-OPS).
		"$BUILD_DIR/vl-updater",
		"$HOME/bin/vl-updater",
		"systemd-user/vl-updater.service",
		// P-E E4: the token is the cutover-worker credential enroll writes —
		// never a hand-minted 24-hour gate-jwt.
		"auth.ScopeCutoverWorker",
		"vl-updater-bootstrap --install-dir <bot> enroll <owner-email>",
		"never a hand-minted gate-jwt",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("install script must carry %q", want)
		}
	}
	// P-E E4 (2026-10-02 follow-up): the old 24-hour gate-jwt tail is DEAD —
	// the enroll token is the long-lived type, and the script's note must
	// never resurrect the false claim.
	if strings.Contains(content, "no longer-lived token type") {
		t.Fatalf("install script note must not carry the dead " +
			"\"no longer-lived token type\" tail")
	}
	// No privilege escalation anywhere: no line RUNS sudo.
	for _, l := range strings.Split(content, "\n") {
		line := strings.TrimSpace(l)
		if line == "sudo" || strings.HasPrefix(line, "sudo ") {
			t.Fatalf("install script must never run sudo: line %q", l)
		}
	}

	run := func(t *testing.T, home string, env string, args ...string) (string, int) {
		t.Helper()
		// NOTE: this box's toolchain rejects a two-literal + spread call
		// ("bash", script, args...) — compile error "too many arguments".
		// One literal + spread compiles; keep this shape.
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "VL_UPDATER_BUILD_REPO=/nonexistent-vl-mirror")
		out, err := cmd.CombinedOutput()
		rc := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				rc = ee.ExitCode()
			} else {
				t.Fatalf("running %v: %v", args, err)
			}
		}
		_ = env
		return string(out), rc
	}

	writeEnv := func(t *testing.T, home, body string, mode os.FileMode) {
		t.Helper()
		dir := filepath.Join(home, ".config", "vl-updater")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "env"), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no args is a usage refusal", func(t *testing.T) {
		out, rc := run(t, t.TempDir(), "")
		if rc != 2 || !strings.Contains(out, "usage:") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a non-hex sha is refused", func(t *testing.T) {
		out, rc := run(t, t.TempDir(), "", "abc123")
		if rc != 2 || !strings.Contains(out, "REFUSED") || !strings.Contains(out, "40-hex") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a missing env file is refused with its exact path", func(t *testing.T) {
		home := t.TempDir()
		out, rc := run(t, home, "", strings.Repeat("a", 40))
		if rc != 2 || !strings.Contains(out, ".config/vl-updater/env does not exist") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("an env file without the token is refused", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\n", 0o600)
		out, rc := run(t, home, "", strings.Repeat("b", 40))
		if rc != 2 || !strings.Contains(out, "must set VL_RELEASE_DIR") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a relative VL_RELEASE_DIR is refused", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR=releases\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, "", strings.Repeat("c", 40))
		if rc != 2 || !strings.Contains(out, "must be an absolute path") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("VL_RELEASE_DIR inside the install is refused", func(t *testing.T) {
		home := t.TempDir()
		inst := filepath.Join(home, "vl")
		if err := os.MkdirAll(inst, 0o755); err != nil {
			t.Fatal(err)
		}
		inside := filepath.Join(inst, "releases")
		writeEnv(t, home, "VL_RELEASE_DIR="+inside+"\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, "", strings.Repeat("d", 40))
		if rc != 2 || !strings.Contains(out, "must be OUTSIDE the install") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a run never prints the token value, on any refusal path", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home,
			"VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=SECRETMARKER123\n",
			0o600)
		// A valid sha + a valid env file: the NEXT refusal is the clone
		// (the repo is /nonexistent-vl-mirror). The output must not carry
		// the token anywhere on the path.
		out, rc := run(t, home, "", strings.Repeat("e", 40))
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		if strings.Contains(out, "SECRETMARKER123") {
			t.Fatalf("the token value leaked into the output:\n%s", out)
		}
	})

	t.Run("the env file must be mode 0600", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home,
			"VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\n",
			0o644)
		out, rc := run(t, home, "", strings.Repeat("f", 40))
		if rc != 2 || !strings.Contains(out, "must be mode 0600") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})
}
