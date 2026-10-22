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

func init() { renamePause = time.Millisecond; firstFetchPause = time.Millisecond }

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

// TestRunInstalledOpensDashboardBeforeLocalCopyAndSaysVersion — owner laptop
// 10-21: the window sat on "already set up" (the local copy ran first, and a
// fresh exe can be held by Defender) and never said it was updating. Order is
// now: dashboard → update check → "Updating VL old → new…" → copy. MUTANT:
// move ensureLocalExe back above openURL → RED; drop the status line → RED.
func TestRunInstalledOpensDashboardBeforeLocalCopyAndSaysVersion(t *testing.T) {
	prev := appVersion
	appVersion = "9.9.9"
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
	if err := os.WriteFile(localExeVersionPath(d), []byte("1.1.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://%s:%d", ic.Host, 18081)
	fetch, signers := fetchMap(t, base, []byte(manifestText("37")), addonBody())
	d.fetch = fetch
	d.allowedSigners = signers

	var seq []string
	realOpen := d.openURL
	d.openURL = func(ctx context.Context, u string) error {
		seq = append(seq, "open")
		return realOpen(ctx, u)
	}
	realFetch := d.fetch
	d.fetch = func(ctx context.Context, u string) ([]byte, error) {
		if strings.HasSuffix(u, "/api/friend/manifest") {
			seq = append(seq, "check")
		}
		return realFetch(ctx, u)
	}
	realWrite := d.writeFile
	d.writeFile = func(p string, b []byte, m os.FileMode) error {
		if strings.HasPrefix(p, localExePath(d)) {
			seq = append(seq, "copy")
		}
		return realWrite(p, b, m)
	}

	if err := runInstalled(context.Background(), d, home); err != nil {
		t.Fatalf("runInstalled: %v", err)
	}
	got := strings.Join(seq, ",")
	// (A "check" before "open" is this fixture's S4 add-on resume — no DLL.)
	o := strings.Index(got, "open")
	c := strings.Index(got[o+1:], "check")
	p := strings.Index(got, "copy")
	if o < 0 || c < 0 || p < 0 || p < o+1+c {
		t.Fatalf("want dashboard, then update check, then the local copy; got %q", got)
	}
	joined := strings.Join(rec.prints, "\n")
	for _, want := range []string{"Checking for updates…", "Updating VL 1.1.5 → 9.9.9", "VL 9.9.9 is ready on this PC ✓"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

// TestWaitFlatIsBounded — gate-3 R4 10-21: waitFlat had no deadline of its own
// (60s was the poll interval), so an open trade sat for the run's 30 min.
// MUTANT: drop the WithTimeout → this test hangs past its deadline → RED.
func TestWaitFlatIsBounded(t *testing.T) {
	pm, pp := flatWaitMax, flatWaitPoll
	flatWaitMax, flatWaitPoll = 50*time.Millisecond, 5*time.Millisecond
	defer func() { flatWaitMax, flatWaitPoll = pm, pp }()
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	d.positions = func(context.Context) (float64, error) { return 1, nil } // a trade stays open
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	err := waitFlat(ctx, d)
	if !errors.Is(err, errNotFlat) {
		t.Fatalf("want errNotFlat, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waitFlat must stop at flatWaitMax, took %v", time.Since(start))
	}
}

// TestShortcutScriptRetargetsStaleLnk — F4 10-21 (proven on real PS 5.1: a
// stale VL.lnk pointing at notepad with --app was re-targeted to the local exe
// + --open; a second run left the mtime unchanged). An EXISTING old-version
// VL.lnk must be re-targeted, an up-to-date one left alone. MUTANT: back to
// "create only when missing" → RED.
func TestShortcutScriptRetargetsStaleLnk(t *testing.T) {
	s := shortcutScript()
	want := "if((-not (Test-Path -LiteralPath $p)) -or $l.TargetPath -ne $exe -or $l.Arguments -ne '--open'){"
	if !strings.Contains(s, want) {
		t.Fatalf("shortcutScript must re-target a stale VL.lnk; script:\n%s", s)
	}
}

// TestInstallFirstRunRetriesFirstFetch — F1 10-21: the first manifest fetch runs
// over a tunnel seconds old. Two transient failures must not fail the install;
// three must, with "run VL-Setup again". MUTANT: firstFetchTries = 1 → RED.
func TestInstallFirstRunRetriesFirstFetch(t *testing.T) {
	for _, tc := range []struct {
		fails  int
		wantOK bool
	}{{2, true}, {3, false}} {
		home := t.TempDir()
		d, _ := flowFakeDeps(t, home)
		ic := goodCode()
		fetch, signers := fetchMap(t, ic.BaseURL(), []byte(manifestText("37")), addonBody())
		left := tc.fails
		d.fetch = func(ctx context.Context, u string) ([]byte, error) {
			if strings.HasSuffix(u, "/api/friend/manifest") && left > 0 {
				left--
				return nil, errors.New("dial tcp: i/o timeout")
			}
			return fetch(ctx, u)
		}
		d.allowedSigners = signers
		err := installFirstRun(context.Background(), d, encodeInstallCode(ic))
		if tc.wantOK && err != nil {
			t.Fatalf("%d transient failures must be retried: %v", tc.fails, err)
		}
		if !tc.wantOK && (err == nil || !strings.Contains(err.Error(), "run VL-Setup again")) {
			t.Fatalf("%d failures must fail with a re-run hint, got %v", tc.fails, err)
		}
	}
}

// TestCreateDesktopShortcutGoFallback — 10-21 laptop: the PowerShell icon step
// failed ("exit status 1"). The icon must not depend on PowerShell: when it
// fails, Go writes VL.url into the real Desktop + Start Menu. MUTANT: return
// psErr without the fallback → RED.
func TestCreateDesktopShortcutGoFallback(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	desk, progs := filepath.Join(home, "Desktop"), filepath.Join(home, "Programs")
	d.iconDirs = func() ([]string, error) { return []string{desk, progs}, nil }
	d.exec = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	_ = os.MkdirAll(filepath.Dir(localExePath(d)), 0o755)
	_ = os.WriteFile(localExePath(d), []byte("MZ"), 0o755)
	_ = os.MkdirAll(progs, 0o755)
	_ = os.WriteFile(filepath.Join(progs, "VL.url"), []byte("[InternetShortcut]\r\nURL=old\r\n"), 0o644) // an older Edge-logo one
	// A stale old VL.lnk on the Desktop must NOT stop the fallback (laptop 10-21).
	_ = os.MkdirAll(desk, 0o755)
	_ = os.WriteFile(filepath.Join(desk, "VL.lnk"), []byte("stale"), 0o644)
	if err := createDesktopShortcut(context.Background(), d, "http://100.77.150.109:18081"); err != nil {
		t.Fatalf("the Go fallback must succeed when PowerShell fails: %v", err)
	}
	for _, dir := range []string{desk, progs} {
		b, err := os.ReadFile(filepath.Join(dir, "VL.url"))
		if err != nil || !strings.Contains(string(b), "URL=http://100.77.150.109:18081") {
			t.Fatalf("VL.url not written in %s: %v %q", dir, err, b)
		}
		if !strings.Contains(string(b), "IconFile=") {
			t.Fatalf("VL.url must carry the VL logo (IconFile): %q", b)
		}
	}
}

// TestReportDiagSendsRedactedReason — 10-21: a failed step reports its reason
// to the friend's own bot (manifest URL + vl_diag query) so the owner's log
// shows it. Secrets must never ride along. MUTANT: drop the redaction → RED.
func TestReportDiagSendsRedactedReason(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	var got string
	d.fetch = func(_ context.Context, u string) ([]byte, error) { got = u; return nil, nil }
	reportDiag(context.Background(), d, "http://h:1", "icon-ps", errors.New("boom tskey-auth-SECRET123-abcdef"))
	if !strings.HasPrefix(got, "http://h:1/api/friend/manifest?vl_diag=") || !strings.Contains(got, "boom") {
		t.Fatalf("diag not sent: %q", got)
	}
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("a secret leaked into the diag: %q", got)
	}
}

// TestShortcutStaleMovedAndGoodCopied — owner 10-21: the app-window VL.lnk was
// made (Start Menu) but not on the desktop, where a stale first-version VL.lnk
// sat. The stale one is MOVED aside (never deleted) before PowerShell, and the
// good one is COPIED to the desktop. MUTANT: drop copyGoodLnk → RED.
func TestShortcutStaleMovedAndGoodCopied(t *testing.T) {
	home := t.TempDir()
	d, _ := flowFakeDeps(t, home)
	desk, progs := filepath.Join(home, "Desktop"), filepath.Join(home, "Programs")
	_ = os.MkdirAll(desk, 0o755)
	_ = os.MkdirAll(progs, 0o755)
	_ = os.WriteFile(filepath.Join(desk, "VL.lnk"), utf16le("msedge"), 0o644)                          // stale
	_ = os.WriteFile(filepath.Join(progs, "VL.lnk"), append([]byte("L"), utf16le("--open")...), 0o644) // current
	d.iconDirs = func() ([]string, error) { return []string{desk, progs}, nil }
	d.exec = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("killed") }
	if err := createDesktopShortcut(context.Background(), d, "http://h:1"); err != nil {
		t.Fatalf("createDesktopShortcut: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(desk, "VL.lnk"))
	if !lnkIsCurrent(b) {
		t.Fatalf("the good app-window shortcut must be copied to the desktop; got %q", b)
	}
	moved, _ := filepath.Glob(filepath.Join(d.localAppDataDir(), "VL", "old", "VL-*.lnk"))
	if len(moved) != 1 {
		t.Fatalf("the stale VL.lnk must be MOVED to VL\\old, got %v", moved)
	}
	if _, err := os.Stat(filepath.Join(desk, "VL.url")); err == nil {
		t.Fatal("no web-link icon is needed when a current .lnk is on the desktop")
	}
}
