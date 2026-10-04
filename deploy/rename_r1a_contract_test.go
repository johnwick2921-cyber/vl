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

// RENAME-R5: the bars-key liveness refusal matches vl-bin ONLY —
// re-adding the retired binary pattern must fail this test.
func TestBarsKeyRollbackRefusesTheVlBinaryOnly(t *testing.T) {
	sh := repoFile(t, "deploy/bars-key-rollback.sh")
	if !strings.Contains(sh, "pgrep -f vl-bin") {
		t.Fatalf("the liveness refusal must match vl-bin")
	}
	if strings.Contains(sh, "no"+"fx-bin") {
		t.Fatalf("R5 retired the old binary pattern — it must not match it anymore")
	}
}

// TestInstallUpdaterWorkerEnvFileReadsVlNamesOnly (RENAME-R5): the env file is
// read by the VL_ names ONLY — a file holding just the retired names must be
// refused with the missing-key message, and leftover retired lines beside the
// VL_ keys are ignored (VL wins, no twin).
func TestInstallUpdaterWorkerEnvFileReadsVlNamesOnly(t *testing.T) {
	script := "install-updater-worker.sh"
	oldUpper := strings.ToUpper("no" + "fx")
	for name, c := range map[string]struct {
		lines    []string
		wantOK   bool
		wantMiss bool
	}{
		"VL only":                {[]string{"VL_RELEASE_DIR=/outside", "VL_CUTOVER_TOKEN=tok"}, true, false},
		"retired names only":     {[]string{oldUpper + "_RELEASE_DIR=/outside", oldUpper + "_CUTOVER_TOKEN=tok"}, false, true},
		"VL wins over leftovers": {[]string{"VL_RELEASE_DIR=/outside", "VL_CUTOVER_TOKEN=tok", oldUpper + "_RELEASE_DIR=/also", oldUpper + "_CUTOVER_TOKEN=stale"}, true, false},
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
			if c.wantOK && !strings.Contains(combined, "env ok (release dir outside the install") {
				t.Fatalf("env file %v refused or never reached the env check:\n%s", c.lines, combined)
			}
			if c.wantMiss && !strings.Contains(combined, "must set VL_RELEASE_DIR") {
				t.Fatalf("a file holding only the retired names must be refused naming VL_RELEASE_DIR:\n%s", combined)
			}
		})
	}
}

// TestVlDbBackupInstallRootDefault (RENAME-R5): with no VL_DB, the default DB
// is the install root's data/data.db — ALWAYS $HOME/vl (the retired install-dir
// fallback is gone) — never a hardcoded /home/hoang.
func TestVlDbBackupInstallRootDefault(t *testing.T) {
	script := "vl-db-backup.sh"
	for name, c := range map[string]struct {
		dirs    []string
		wantSfx string
	}{
		"vl dir exists":               {[]string{"vl"}, "/vl/data/data.db"},
		"only the retired dir exists": {[]string{"no" + "fx"}, "/vl/data/data.db"},
		"both dirs":                   {[]string{"vl", "no" + "fx"}, "/vl/data/data.db"},
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
				t.Fatalf("the DB default is not the install root's; got:\n%s", combined)
			}
			if strings.Contains(combined, "/home/hoang/") {
				t.Fatalf("a hardcoded /home/hoang leaked into the default:\n%s", combined)
			}
		})
	}
}
