package deploy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// R1a shell twins, exercised through the real scripts.

// RENAME-R1a: the bars-key liveness refusal matches BOTH binary names —
// dropping the vl-bin match must fail this test.
func TestBarsKeyRollbackRefusesBothBinaryNames(t *testing.T) {
	sh := repoFile(t, "deploy/bars-key-rollback.sh")
	if !strings.Contains(sh, "pgrep -f nofx-bin") || !strings.Contains(sh, "pgrep -f vl-bin") {
		t.Fatalf("the liveness refusal must match BOTH vl-bin and nofx-bin")
	}
}

// TestInstallUpdaterWorkerEnvFileDual: the env file may carry the VL_ keys, the
// NOFX_ keys, or both (VL wins); each layout must pass the env check ("env ok")
// before the build stage (which fails on network in a test — irrelevant).
func TestInstallUpdaterWorkerEnvFileDual(t *testing.T) {
	script := "install-updater-worker.sh"
	for name, c := range map[string]struct {
		lines []string
	}{
		"VL only":      {[]string{"VL_RELEASE_DIR=/outside", "VL_CUTOVER_TOKEN=tok"}},
		"NOFX only":    {[]string{"NOFX_RELEASE_DIR=/outside", "NOFX_CUTOVER_TOKEN=tok"}},
		"VL wins both": {[]string{"VL_RELEASE_DIR=/outside", "NOFX_RELEASE_DIR=/also", "NOFX_CUTOVER_TOKEN=tok"}},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			envDir := filepath.Join(home, ".config", "vl-updater")
			if err := os.MkdirAll(envDir, 0o755); err != nil {
				t.Fatal(err)
			}
			envFile := filepath.Join(envDir, "env")
			if err := os.WriteFile(envFile, []byte(strings.Join(c.lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", script, strings.Repeat("a", 40))
			cmd.Env = append(os.Environ(), "HOME="+home)
			var out, errBuf bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errBuf
			_ = cmd.Run() // the build stage fails (no network); the env check is before it
			combined := out.String() + errBuf.String()
			if !strings.Contains(combined, "env ok (release dir outside the install") {
				t.Fatalf("env file %v refused or never reached the env check:\n%s", c.lines, combined)
			}
			if strings.Contains(combined, "must set VL_RELEASE_DIR/NOFX_RELEASE_DIR") {
				t.Fatalf("a valid dual env file was refused:\n%s", combined)
			}
		})
	}
}

// TestNoFxDbBackupInstallRootDefault: with no NOFX_DB/VL_DB, the default DB is
// the install-root's data/data.db — $HOME/vl when it exists, else $HOME/nofx —
// never a hardcoded /home/hoang.
func TestNoFxDbBackupInstallRootDefault(t *testing.T) {
	script := "nofx-db-backup.sh"
	for name, c := range map[string]struct {
		dirs    []string
		wantSfx string
	}{
		"vl dir wins":     {[]string{"vl"}, "/vl/data/data.db"},
		"nofx fallback":   {[]string{"nofx"}, "/nofx/data/data.db"},
		"vl wins on both": {[]string{"vl", "nofx"}, "/vl/data/data.db"},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			for _, d := range c.dirs {
				if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(), "HOME="+home)
			var out, errBuf bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errBuf
			_ = cmd.Run() // the DB does not exist; the refusal names the default path
			combined := out.String() + errBuf.String()
			if !strings.Contains(combined, "DB not found at "+filepath.Join(home, strings.TrimPrefix(c.wantSfx, "/"))) {
				t.Fatalf("the DB default is not the install-root's; got:\n%s", combined)
			}
			if strings.Contains(combined, "/home/hoang/") {
				t.Fatalf("a hardcoded /home/hoang leaked into the default:\n%s", combined)
			}
		})
	}
}
