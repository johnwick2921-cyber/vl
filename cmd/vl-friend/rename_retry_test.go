package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() { renamePause = time.Millisecond }

// TestRetryRenameSurvivesTransientLock — A7 (owner laptop 10-21: "being used
// by another process"): a rename that fails twice (Defender holding the
// just-written file) then succeeds must succeed. MUTANT: a single d.rename
// with no retry → RED.
func TestRetryRenameSurvivesTransientLock(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	real := d.rename
	fails := 2
	d.rename = func(a, b string) error {
		if fails > 0 {
			fails--
			return errors.New("The process cannot access the file because it is being used by another process.")
		}
		return real(a, b)
	}
	p := filepath.Join(home, "state.json")
	if err := writeFileAtomic(d, p, []byte("ok"), 0o600); err != nil {
		t.Fatalf("a transient lock must be retried: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "ok" {
		t.Fatalf("file not written: %q", b)
	}
}

// TestRetryRenameGivesUpOnPermanentLock — a lock that never clears still
// returns the error (bounded, never a hang).
func TestRetryRenameGivesUpOnPermanentLock(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	calls := 0
	d.rename = func(a, b string) error { calls++; return errors.New("in use") }
	if err := retryRename(d, "a", "b"); err == nil {
		t.Fatal("a permanent lock must return its error")
	}
	if calls != 20 {
		t.Fatalf("want 20 bounded attempts, got %d", calls)
	}
}

// TestRunInstalledOpensDashboardWhenLocalCopyLocked — A7: on an installed PC
// a locked local exe (rename always fails) must NOT stop the run: it warns and
// still opens the dashboard. MUTANT: `return err` on ensureLocalExe → RED.
func TestRunInstalledOpensDashboardWhenLocalCopyLocked(t *testing.T) {
	prev := appVersion
	appVersion = "9.9.9" // newer than the recorded copy → ensureLocalExe replaces
	defer func() { appVersion = prev }()

	home := t.TempDir()
	d, rec := flowFakeDeps(t, home)
	ic := goodCode()
	if err := writeConnectJSON(d, home, ic); err != nil {
		t.Fatal(err)
	}
	if err := writeAppState(d, home, appState{Name: "friend1", WebPort: 18081}); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledState(d, home, installedState{AddonBuild: "37", AppVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(localExePath(d)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExePath(d), []byte("MZ-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers
	real := d.rename
	d.rename = func(a, b string) error {
		if a == localExePath(d) || b == localExePath(d) {
			return errors.New("The process cannot access the file because it is being used by another process.")
		}
		return real(a, b)
	}

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("a locked local copy must not fail the run: %v", err)
	}
	opened := false
	for _, u := range rec.opened {
		if u == base {
			opened = true
		}
	}
	if !opened {
		t.Fatalf("the dashboard was not opened; opened=%v", rec.opened)
	}
	if !strings.Contains(strings.Join(rec.prints, "\n"), "warning: could not update VL's app copy") {
		t.Fatalf("the lock must be reported as a warning: %q", rec.prints)
	}
}
