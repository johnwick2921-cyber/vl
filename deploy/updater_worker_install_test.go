package deploy

// UPDATER-USABLE-V1 (item B) — pins for the updater worker's systemd --user
// unit template and the install script's refusals. Production call sites:
// the REAL template and the REAL script on disk, run with bash.
//
// The CTO's mutations hit the script refusals: a run whose output carries the
// token value, a VL_RELEASE_DIR inside ~/vl that passes, a non-hex sha
// that is accepted, a unit that re-joins the bot's cgroup or re-sets TZ.

import (
	"fmt"
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

	run := func(t *testing.T, home string, env []string, args ...string) (string, int) {
		t.Helper()
		// NOTE: this box's toolchain rejects a two-literal + spread call
		// ("bash", script, args...) — compile error "too many arguments".
		// One literal + spread compiles; keep this shape.
		// The real WSL_DISTRO_NAME is stripped so the script's distro
		// discovery is deterministic; callers re-add it (or a fake wslpath
		// on PATH) via env.
		var base []string
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "WSL_DISTRO_NAME=") {
				continue
			}
			base = append(base, e)
		}
		base = append(base, "HOME="+home, "VL_UPDATER_BUILD_REPO=/nonexistent-vl-mirror")
		base = append(base, env...)
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		cmd.Env = base
		out, err := cmd.CombinedOutput()
		rc := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				rc = ee.ExitCode()
			} else {
				t.Fatalf("running %v: %v", args, err)
			}
		}
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
		out, rc := run(t, t.TempDir(), nil)
		if rc != 2 || !strings.Contains(out, "usage:") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a non-hex sha is refused", func(t *testing.T) {
		out, rc := run(t, t.TempDir(), nil, "abc123")
		if rc != 2 || !strings.Contains(out, "REFUSED") || !strings.Contains(out, "40-hex") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a missing env file is refused with its exact path", func(t *testing.T) {
		home := t.TempDir()
		out, rc := run(t, home, nil, strings.Repeat("a", 40))
		if rc != 2 || !strings.Contains(out, ".config/vl-updater/env does not exist") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("an env file without the token is refused", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\n", 0o600)
		out, rc := run(t, home, nil, strings.Repeat("b", 40))
		if rc != 2 || !strings.Contains(out, "must set VL_RELEASE_DIR") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	t.Run("a relative VL_RELEASE_DIR is refused", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR=releases\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, nil, strings.Repeat("c", 40))
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
		out, rc := run(t, home, nil, strings.Repeat("d", 40))
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
		out, rc := run(t, home, nil, strings.Repeat("e", 40))
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
		out, rc := run(t, home, nil, strings.Repeat("f", 40))
		if rc != 2 || !strings.Contains(out, "must be mode 0600") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
	})

	// W-UPDATER-WINPATH: the script appends the Windows-tool PATH line to the
	// env file when absent (systemd does not inherit WSL's appendWindowsPath),
	// and never duplicates an existing PATH.
	t.Run("the Windows-tool PATH line is added once", func(t *testing.T) {
		home := t.TempDir()
		envBody := "VL_RELEASE_DIR=" + filepath.Join(home, "releases") + "\nVL_CUTOVER_TOKEN=tok\n"
		writeEnv(t, home, envBody, 0o600)
		out, rc := run(t, home, nil, strings.Repeat("e", 40))
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		envPath := filepath.Join(home, ".config", "vl-updater", "env")
		b, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		wantPath := "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/mnt/c/Windows/System32/WindowsPowerShell/v1.0:/mnt/c/Windows/System32:/mnt/c/Program Files/dotnet"
		if !strings.Contains(string(b), wantPath) {
			t.Fatalf("env file must carry the Windows-tool PATH line:\n%s", b)
		}
		if strings.Count(string(b), "PATH=") != 1 {
			t.Fatalf("PATH line must appear exactly once:\n%s", b)
		}
		// Run again: an existing PATH line is never duplicated.
		out, rc = run(t, home, nil, strings.Repeat("e", 40))
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("second run rc=%d out=%q", rc, out)
		}
		b, err = os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(b), "PATH=") != 1 {
			t.Fatalf("a second run must not duplicate the PATH line:\n%s", b)
		}
	})

	// W-UPDATER-WINPATH (REVIEW-BACK fix 2026-10-19): the distro name is the
	// LAST component of `wslpath -w /` (\\wsl.localhost\<distro>\ or
	// \\wsl$\<distro>\), never the awk $3 host. The pin asserts the VALUE of
	// the WSL_DISTRO_NAME line, not just its presence; a failed/empty
	// resolution must leave it UNSET (no guessed "Ubuntu" persisted as though
	// it were discovered).
	fakeWslpath := func(t *testing.T, out string, rc int) string {
		t.Helper()
		dir := t.TempDir()
		exe := filepath.Join(dir, "wslpath")
		body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\nexit %d\n", strings.ReplaceAll(out, "'", ""), rc)
		if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	wslpathOnPath := func(t *testing.T, out string, rc int) []string {
		t.Helper()
		return []string{"PATH=" + fakeWslpath(t, out, rc) + string(os.PathListSeparator) + os.Getenv("PATH")}
	}
	sha40 := strings.Repeat("9", 40)
	assertDistro := func(t *testing.T, home, want string) {
		t.Helper()
		envPath := filepath.Join(home, ".config", "vl-updater", "env")
		b, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if strings.Contains(string(b), "WSL_DISTRO_NAME=") {
				t.Fatalf("WSL_DISTRO_NAME must be left unset:\n%s", b)
			}
			return
		}
		if strings.Count(string(b), "WSL_DISTRO_NAME=") != 1 {
			t.Fatalf("WSL_DISTRO_NAME line must appear exactly once:\n%s", b)
		}
		if !strings.Contains(string(b), "WSL_DISTRO_NAME="+want+"\n") {
			t.Fatalf("WSL_DISTRO_NAME must be exactly %q:\n%s", want, b)
		}
	}

	t.Run("the distro is extracted from a wsl.localhost UNC root", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, wslpathOnPath(t, `\\wsl.localhost\Ubuntu-24.04\`, 0), sha40)
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		assertDistro(t, home, "Ubuntu-24.04")
	})

	t.Run("the wsl$ UNC root prefix is covered too", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, wslpathOnPath(t, `\\wsl$\Ubuntu-24.04\`, 0), sha40)
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		assertDistro(t, home, "Ubuntu-24.04")
	})

	t.Run("a shell WSL_DISTRO_NAME wins over wslpath", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		env := wslpathOnPath(t, `\\wsl.localhost\Other-Distro\`, 0)
		env = append(env, "WSL_DISTRO_NAME=MyDistro")
		out, rc := run(t, home, env, sha40)
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		assertDistro(t, home, "MyDistro")
	})

	t.Run("an existing WSL_DISTRO_NAME line is preserved", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\nWSL_DISTRO_NAME=Foo\n", 0o600)
		out, rc := run(t, home, wslpathOnPath(t, `\\wsl.localhost\Ubuntu-24.04\`, 0), sha40)
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		assertDistro(t, home, "Foo")
	})

	t.Run("a failed wslpath leaves the distro unset (no guess)", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, wslpathOnPath(t, "", 1), sha40)
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		assertDistro(t, home, "")
	})

	t.Run("an empty wslpath answer leaves the distro unset (no guess)", func(t *testing.T) {
		home := t.TempDir()
		writeEnv(t, home, "VL_RELEASE_DIR="+filepath.Join(home, "releases")+"\nVL_CUTOVER_TOKEN=tok\n", 0o600)
		out, rc := run(t, home, wslpathOnPath(t, "", 0), sha40)
		if rc != 2 || !strings.Contains(out, "clone failed") {
			t.Fatalf("rc=%d out=%q", rc, out)
		}
		assertDistro(t, home, "")
	})
}
