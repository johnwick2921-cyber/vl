package activation

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withSystem swaps the machine seam for the duration of one test.
func withSystem(t *testing.T, s *system) {
	t.Helper()
	old := sys
	sys = s
	t.Cleanup(func() { sys = old })
}

// statLine builds a /proc/<pid>/stat line whose field 22 is ticks. The comm
// deliberately contains a space and parentheses.
func statLine(pid int, ticks uint64) string {
	mid := ""
	for i := 4; i <= 21; i++ {
		mid += " 0"
	}
	return fmt.Sprintf("%d (nofx bin (x)) S%s %d 0 0", pid, mid, ticks)
}

func tempDB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`create table t (a int); insert into t values (1),(2);`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return p
}

func TestBackupProducesACopyThatOpensAndPassesIntegrityCheck(t *testing.T) {
	src := tempDB(t)
	dst := filepath.Join(t.TempDir(), "sub", "copy.db")
	rc, err := Backup(src, dst)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if !rc.OK || rc.Evidence["integrity_check"] != "ok" {
		t.Fatalf("receipt does not record the verification: %+v", rc.Evidence)
	}
	db, err := sql.Open("sqlite", dst)
	if err != nil {
		t.Fatalf("copy does not open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("select count(*) from t").Scan(&n); err != nil || n != 2 {
		t.Fatalf("copy has %d rows (err %v), want 2 — the backup did not carry the data", n, err)
	}
}

// A file that exists at the destination but is NOT a database must not be
// mistaken for an already-completed backup. That is the idempotence trap: the
// cheap check is "does the file exist", and the honest one is "does it open".
func TestBackupReplacesADestinationThatIsNotAValidDatabase(t *testing.T) {
	src := tempDB(t)
	dst := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(dst, []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, err := Backup(src, dst)
	if err != nil {
		t.Fatalf("Backup refused to replace a junk destination: %v", err)
	}
	if rc.Evidence["already"] == "true" {
		t.Fatal("Backup treated a non-database file as an existing backup")
	}
	if err := verifyBackup(dst); err != nil {
		t.Fatalf("destination still not a valid backup: %v", err)
	}
}

func TestBackupIsIdempotentOnAGoodExistingCopy(t *testing.T) {
	src := tempDB(t)
	dst := filepath.Join(t.TempDir(), "copy.db")
	if _, err := Backup(src, dst); err != nil {
		t.Fatalf("first: %v", err)
	}
	rc, err := Backup(src, dst)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if rc.Evidence["already"] != "true" {
		t.Fatal("a second Backup over a verified copy must be a no-op with already=true")
	}
}

// THE REFUSAL THAT MATTERS: the pid we were told to replace is now a different
// process. Signalling it would kill an innocent bystander.
func TestActivateRefusesToSignalARecycledPID(t *testing.T) {
	killed := false
	withSystem(t, &system{
		// field 22 differs from the Identity we pass in => recycled.
		ReadStat: func(pid int) (string, error) { return statLine(pid, 999999), nil },
		Kill:     func(pid int) error { killed = true; return nil },
		MainPID:  func() (int, error) { return 4242, nil },
		Now:      time.Now,
		Sleep:    func(time.Duration) {},
	})
	rel, prev := twoReleases(t)
	_, rc, err := Activate(rel, prev, Identity{PID: 4242, StartTicks: 111111})
	if err == nil {
		t.Fatal("Activate signalled a pid it could not confirm")
	}
	if killed {
		t.Fatal("Activate KILLED a recycled pid — the guard did not hold")
	}
	if !strings.Contains(err.Error(), "no longer the process") {
		t.Fatalf("refusal does not name the cause: %v", err)
	}
	if rc.Evidence["installed"] != "binary,dist,RELEASE" {
		t.Fatal("files should already be in place before the kill is attempted")
	}
}

// Files first, then the kill — and all three halves, not just the binary.
func TestActivateInstallsAllThreeHalvesBeforeKilling(t *testing.T) {
	var orderKill bool
	var distAtKill string
	rel, prev := twoReleases(t)
	withSystem(t, &system{
		ReadStat: func(pid int) (string, error) { return statLine(pid, 111111), nil },
		Kill: func(pid int) error {
			orderKill = true
			b, _ := os.ReadFile(filepath.Join(prev.Dist, "index.html"))
			distAtKill = string(b)
			return nil
		},
		MainPID: func() (int, error) { return 4243, nil },
		Now:     time.Now,
		Sleep:   func(time.Duration) {},
	})
	next, rc, err := Activate(rel, prev, Identity{PID: 4242, StartTicks: 111111})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !orderKill {
		t.Fatal("never killed")
	}
	if distAtKill != "NEW" {
		t.Fatalf("dist was %q at kill time — the swap must complete BEFORE the kill", distAtKill)
	}
	got, _ := os.ReadFile(prev.ReleaseFile)
	if strings.TrimSpace(string(got)) != rel.SHA {
		t.Fatalf("RELEASE holds %q, want %s — the marker is a half nobody remembers", got, rel.SHA)
	}
	if next.PID != 4243 {
		t.Fatalf("returned identity pid %d, want the NEW pid 4243", next.PID)
	}
	if !rc.OK {
		t.Fatal("receipt not OK")
	}
}

// twoReleases builds a "new" release dir and a "current install" dir.
func twoReleases(t *testing.T) (rel, prev Release) {
	t.Helper()
	newDir, curDir := t.TempDir(), t.TempDir()
	mk := func(dir, binBody, distBody, releaseBody string) Release {
		if err := os.MkdirAll(filepath.Join(dir, "web", "dist"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nofx-bin"), []byte(binBody), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "web", "dist", "index.html"), []byte(distBody), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "RELEASE"), []byte(releaseBody), 0o644); err != nil {
			t.Fatal(err)
		}
		return Release{
			Dir: dir, SHA: releaseBody,
			Binary:      filepath.Join(dir, "nofx-bin"),
			Dist:        filepath.Join(dir, "web", "dist"),
			ReleaseFile: filepath.Join(dir, "RELEASE"),
		}
	}
	rel = mk(newDir, "NEWBIN", "NEW", strings.Repeat("b", 40))
	prev = mk(curDir, "OLDBIN", "OLD", strings.Repeat("a", 40))
	return rel, prev
}

// Watch must not accept a boot line written BEFORE the restart: that is the
// previous boot's line and it says nothing about the process running now.
func TestWatchIsRedOnAPreKillBootLineAndGreenOnAPostKillOne(t *testing.T) {
	sha := strings.Repeat("c", 40)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nofx.log")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"release":%q}`, sha)
	}))
	defer srv.Close()

	// One boot line, timestamped an hour in the PAST, naming the right sha.
	past := time.Now().Add(-time.Hour)
	old := fmt.Sprintf("%s [INFO] booted %s\n", past.Format("01-02 15:04:05"), sha)
	if err := os.WriteFile(logPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	withSystem(t, &system{
		ReadStat: func(pid int) (string, error) { return statLine(pid, 1), nil },
		Kill:     func(int) error { return nil },
		MainPID:  func() (int, error) { return 1, nil },
		Now:      time.Now,
		Sleep:    func(time.Duration) {},
	})

	// RED: health agrees, but the only boot line predates the kill.
	if _, err := Watch(Release{SHA: sha}, Identity{PID: 1}, WatchOpts{LogPath: logPath, HealthURL: srv.URL, Within: 50 * time.Millisecond}); err == nil {
		t.Fatal("Watch accepted a boot line that predates the restart")
	} else if !strings.Contains(err.Error(), "no boot line") {
		t.Fatalf("refusal must name the failing leg, got: %v", err)
	}

	// GREEN: append a line written NOW.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, "%s [INFO] booted %s\n", time.Now().Add(time.Second).Format("01-02 15:04:05"), sha)
	f.Close()

	rc, err := Watch(Release{SHA: sha}, Identity{PID: 1}, WatchOpts{LogPath: logPath, HealthURL: srv.URL, Within: 3 * time.Second})
	if err != nil {
		t.Fatalf("Watch refused a genuine post-restart boot: %v", err)
	}
	if rc.Evidence["boot_line"] == "" || rc.Evidence["health_sha"] != sha {
		t.Fatalf("receipt must record BOTH signals: %+v", rc.Evidence)
	}
}

// Health alone is not proof: a process that never restarted still answers.
func TestWatchRefusesWhenHealthAgreesButTheLogNeverShowsARestart(t *testing.T) {
	sha := strings.Repeat("d", 40)
	logPath := filepath.Join(t.TempDir(), "nofx.log")
	if err := os.WriteFile(logPath, []byte("no boot lines here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"release":%q}`, sha)
	}))
	defer srv.Close()
	withSystem(t, &system{Now: time.Now, Sleep: func(time.Duration) {}})
	if _, err := Watch(Release{SHA: sha}, Identity{PID: 1}, WatchOpts{LogPath: logPath, HealthURL: srv.URL, Within: 30 * time.Millisecond}); err == nil {
		t.Fatal("Watch was satisfied by /api/health alone")
	}
}

// FOUND ON THE LIVE BOX: /api/health reports the SHORT sha while a release
// names the full 40 hex, so strict equality could never match in production.
// These pin the comparison in both directions and at the boundary.
func TestHealthMayReportAnAbbreviatedRevision(t *testing.T) {
	full := "662c79bd236f43fb15eb0c7950880123896be8c0"
	cases := []struct {
		name     string
		reported string
		want     bool
	}{
		{"the live box's actual short form", "662c79bd236f", true},
		{"identical full sha", full, true},
		{"a seven-character abbreviation", "662c79b", true},
		{"too short to mean anything", "662c79", false},
		{"a different revision", "deadbeefdead", false},
		{"empty", "", false},
		{"LONGER than expected is never an abbreviation", full + "00", false},
	}
	for _, c := range cases {
		if got := revisionsAgree(c.reported, full); got != c.want {
			t.Errorf("%s: revisionsAgree(%q, full) = %v, want %v", c.name, c.reported, got, c.want)
		}
	}
}

// The live boot line prints the SHORT rev and the full sha never appears in
// the log at all. This pins the real line shape, copied from the live box.
func TestBootLineIsRecognisedFromTheShortRevTheBotActuallyPrints(t *testing.T) {
	full := "662c79bd236f43fb15eb0c7950880123896be8c0"
	live := "09-23 18:50:09 [INFO] nofx-clean/main.go:322 🔐 BOOT INTEGRITY OK — rev 662c79bd236f · built 2026-09-23T23:45:35Z · expected 662c79bd236f"
	if !lineNamesRevision(live, full) {
		t.Fatal("the REAL boot line from the live box is not recognised — both legs of Watch would be unfalsifiable")
	}
	other := "09-23 18:50:09 [INFO] 🔐 BOOT INTEGRITY OK — rev 0e490e448279 · built …"
	if lineNamesRevision(other, full) {
		t.Fatal("a DIFFERENT revision's boot line was accepted")
	}
	if lineNamesRevision("09-23 18:50:09 [INFO] nothing to see", full) {
		t.Fatal("a line naming no revision was accepted")
	}
}

// Watch must accept a real boot line written after the kill even though that
// line carries only the short rev.
func TestWatchAcceptsTheShortRevBootLineTheBotWrites(t *testing.T) {
	full := "aabbccddeeff00112233445566778899aabbccdd"
	short := full[:12]
	logPath := filepath.Join(t.TempDir(), "nofx_2026-09-23.log")
	line := fmt.Sprintf("%s [INFO] 🔐 BOOT INTEGRITY OK — rev %s · built x\n",
		time.Now().Add(time.Second).Format("01-02 15:04:05"), short)
	if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"revision":%q}`, short) // the live box answers short too
	}))
	defer srv.Close()
	withSystem(t, &system{Now: time.Now, Sleep: func(time.Duration) {}})
	rc, err := Watch(Release{SHA: full}, Identity{PID: 1}, WatchOpts{LogPath: logPath, HealthURL: srv.URL, Within: 2 * time.Second})
	if err != nil {
		t.Fatalf("Watch refused a genuine boot proven by the forms the bot actually emits: %v", err)
	}
	if rc.Evidence["health_sha"] != short {
		t.Fatalf("receipt must record what health REPORTED, got %q", rc.Evidence["health_sha"])
	}
}

// LOGS ARE NAMED BY BOOT DATE, NOT CALENDAR DATE: on the live box at 08:04 on
// 09-24 the active file was nofx_2026-09-23.log.
func TestNewestLogPathIgnoresTheCalendarAndPicksTheActiveFile(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "nofx_2026-09-24.log") // today's DATE, but older
	active := filepath.Join(dir, "nofx_2026-09-23.log")
	for _, p := range []string{stale, active} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	got, err := NewestLogPath(dir)
	if err != nil {
		t.Fatalf("NewestLogPath: %v", err)
	}
	if got != active {
		t.Fatalf("picked %s, want the file actually being written (%s) — a date-built path points at the wrong log", got, active)
	}
}

// THE RESUMED-WORKER CASE. A worker persists its receipt before the side
// effect and may crash between the restart and the proof; on resume it calls
// Watch again, minutes later. Anchoring on "now" would reject the boot line
// the restart actually wrote — failing because the worker was slow, not
// because the boot failed, and rolling back a release that came up correctly.
func TestWatchProvesABootAgainstThePersistedKillInstantNotNow(t *testing.T) {
	full := "112233445566778899aabbccddeeff0011223344"
	short := full[:12]
	logPath := filepath.Join(t.TempDir(), "nofx_2026-09-24.log")

	// The restart happened 10 minutes ago and wrote its boot line then.
	killedAt := time.Now().Add(-10 * time.Minute)
	line := fmt.Sprintf("%s [INFO] 🔐 BOOT INTEGRITY OK — rev %s · built x\n",
		killedAt.Add(2*time.Second).Format("01-02 15:04:05"), short)
	if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"revision":%q}`, short)
	}))
	defer srv.Close()
	withSystem(t, &system{Now: time.Now, Sleep: func(time.Duration) {}})

	// Without the kill instant, "now" is the anchor and the genuine boot line
	// is rejected for being older than the resumed call.
	if _, err := Watch(Release{SHA: full}, Identity{PID: 1}, WatchOpts{
		LogPath: logPath, HealthURL: srv.URL, Within: 30 * time.Millisecond,
	}); err == nil {
		t.Fatal("anchoring on now accepted a boot line older than the call — the fixture is wrong")
	}

	// With it, the same log proves the same boot.
	rc, err := Watch(Release{SHA: full}, Identity{PID: 1}, WatchOpts{
		LogPath: logPath, HealthURL: srv.URL, Since: killedAt, Within: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("a resumed worker could not prove a boot that really happened: %v", err)
	}
	if rc.Evidence["since"] == "" {
		t.Fatal("the receipt must record WHICH instant the proof was anchored to")
	}
}

// A boot line from BEFORE the kill is still refused — the kill instant makes
// the anchor accurate, it does not make it lax.
func TestWatchStillRefusesABootLineOlderThanTheKillInstant(t *testing.T) {
	full := "99887766554433221100ffeeddccbbaa99887766"
	short := full[:12]
	logPath := filepath.Join(t.TempDir(), "nofx_2026-09-24.log")
	killedAt := time.Now().Add(-5 * time.Minute)
	// Written BEFORE the kill: the previous boot's line.
	line := fmt.Sprintf("%s [INFO] 🔐 BOOT INTEGRITY OK — rev %s · built x\n",
		killedAt.Add(-time.Hour).Format("01-02 15:04:05"), short)
	if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"revision":%q}`, short)
	}))
	defer srv.Close()
	withSystem(t, &system{Now: time.Now, Sleep: func(time.Duration) {}})
	if _, err := Watch(Release{SHA: full}, Identity{PID: 1}, WatchOpts{
		LogPath: logPath, HealthURL: srv.URL, Since: killedAt, Within: 30 * time.Millisecond,
	}); err == nil {
		t.Fatal("a boot line older than the kill was accepted as proof of the restart")
	}
}

// Snapshot is what makes RollbackTo possible when the install is overwritten
// in place: without it the previous three halves exist only wherever someone
// happened to put them.
func TestSnapshotCapturesAllThreeHalvesAndRollbackToCanUseThem(t *testing.T) {
	_, install := twoReleases(t)
	dest := filepath.Join(t.TempDir(), "prev")
	rc, err := Snapshot(install, dest)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if rc.Evidence["captured"] != "binary,dist,RELEASE" {
		t.Fatalf("receipt must name what it captured, got %+v", rc.Evidence)
	}
	for _, p := range []string{
		filepath.Join(dest, "nofx-bin"),
		filepath.Join(dest, "RELEASE"),
		filepath.Join(dest, "web", "dist", "index.html"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("half missing from the snapshot: %s", p)
		}
	}
	// And the snapshot is usable as prev.
	prev := Release{
		Dir: dest, SHA: rc.Evidence["release_marker"],
		Binary:      filepath.Join(dest, "nofx-bin"),
		Dist:        filepath.Join(dest, "web", "dist"),
		ReleaseFile: filepath.Join(dest, "RELEASE"),
	}
	withSystem(t, &system{
		ReadStat: func(pid int) (string, error) { return statLine(pid, 9), nil },
		Kill:     func(int) error { return nil },
		MainPID:  func() (int, error) { return 5, nil },
		Now:      time.Now,
		Sleep:    func(time.Duration) {},
	})
	if _, _, err := RollbackTo(prev, install, Identity{PID: 4, StartTicks: 9}); err != nil {
		t.Fatalf("a snapshot must be usable as the rollback source: %v", err)
	}
}

func TestSnapshotRefusesWithoutADestination(t *testing.T) {
	_, install := twoReleases(t)
	if _, err := Snapshot(install, ""); err == nil {
		t.Fatal("Snapshot accepted an empty destination")
	}
}
