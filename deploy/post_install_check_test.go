package deploy

// Checklist row Q11 — "After Update, read live behaviour, not diffs."
//
// These tests run the REAL deploy/post-install-check.sh (the production script,
// not a copy of its logic) against log fixtures cut from real bot logs (see
// testdata/post-install/README.md for the source line numbers). Each test pins
// one thing the script must say, so taking that check out of the script turns
// exactly that test red.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	postInstallScript = "post-install-check.sh"
	postInstallData   = "testdata/post-install"

	// The revisions the fixture boots print (first 12 of the sha is all a boot
	// line carries; RELEASE holds the full sha).
	healthyRev = "5e50da0cea8a7cbd98d452d8eb88989aecefff77" // release #62
	outageRev  = "ab356a3a46e7"
)

// runPostInstall runs the script with the given log, release sha and database
// path, and returns its output and exit code.
func runPostInstall(t *testing.T, logPath, releaseSHA, dbPath string, env ...string) (string, int) {
	t.Helper()
	rel := filepath.Join(t.TempDir(), "RELEASE")
	if err := os.WriteFile(rel, []byte(releaseSHA+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return runPostInstallRelease(t, logPath, rel, dbPath, env...)
}

// runPostInstallRelease is runPostInstall with the RELEASE file given as a path,
// so a case can point it at a missing or empty file.
//
// The script judges liveness and "too early" against the current time. Unless
// the case sets POST_INSTALL_NOW itself, "now" is pinned to 60 s after the
// log's newest line: the moment a reader would run the check on that log.
func runPostInstallRelease(t *testing.T, logPath, rel, dbPath string, env ...string) (string, int) {
	t.Helper()
	return runPostInstallArgs(t, logPath, rel, dbPath, nil, env...)
}

// runPostInstallArgs is runPostInstallRelease with extra script arguments
// (--expect).
func runPostInstallArgs(t *testing.T, logPath, rel, dbPath string, args []string, env ...string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	if dbPath == "" {
		dbPath = filepath.Join(t.TempDir(), "no-such.db") // the database leg says WARN, never FAIL
	}
	hasNow := false
	for _, e := range env {
		if strings.HasPrefix(e, "POST_INSTALL_NOW=") {
			hasNow = true
		}
	}
	if !hasNow {
		env = append(env, fmt.Sprintf("POST_INSTALL_NOW=%d", logNewest(t, logPath).Unix()+60))
	}
	cmd := exec.Command("bash", append([]string{postInstallScript, "--log", logPath, "--release", rel, "--db", dbPath}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run %s: %v\n%s", postInstallScript, err, out)
		}
		rc = ee.ExitCode()
	}
	return string(out), rc
}

func fixture(name string) string {
	m, _ := filepath.Glob(filepath.Join(postInstallData, name, "vl_*.log"))
	if len(m) != 1 {
		panic("fixture " + name + ": want exactly one vl_*.log")
	}
	return m[0]
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}

// chicago is the zone every log stamp is written in.
func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	return loc
}

// ct turns a log stamp ("MM-DD HH:MM:SS", Chicago wall clock) in a given year
// into a time, the same reading the script makes.
func ct(t *testing.T, year int, stamp string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%d-%s", year, stamp), chicago(t))
	if err != nil {
		t.Fatalf("stamp %q: %v", stamp, err)
	}
	return at
}

var stampRE = regexp.MustCompile(`^[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} `)

// logNewest is the time of the log's last stamped line; the year comes from
// the log's name (vl_YYYY-MM-DD.log), as in the script.
func logNewest(t *testing.T, logPath string) time.Time {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	year := time.Now().Year()
	if m := regexp.MustCompile(`^vl_([0-9]{4})-`).FindStringSubmatch(filepath.Base(logPath)); m != nil {
		fmt.Sscan(m[1], &year)
	}
	stamp := ""
	for _, ln := range strings.Split(string(b), "\n") {
		if stampRE.MatchString(ln) {
			stamp = ln[:14]
		}
	}
	if stamp == "" {
		t.Fatalf("%s has no stamped line", logPath)
	}
	return ct(t, year, stamp)
}

func nowEnv(at time.Time) string { return fmt.Sprintf("POST_INSTALL_NOW=%d", at.Unix()) }

// writeLog writes lines as a log file named for the year the script reads.
func writeLog(t *testing.T, name string, lines []string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// without drops every line that contains any of the given texts.
func without(lines []string, texts ...string) []string {
	var out []string
next:
	for _, ln := range lines {
		for _, s := range texts {
			if strings.Contains(ln, s) {
				continue next
			}
		}
		out = append(out, ln)
	}
	return out
}

// redate gives a real line a new stamp (its first 14 characters).
func redate(line, stamp string) string { return stamp + line[14:] }

// lineWith returns the one fixture line containing text.
func lineWith(t *testing.T, lines []string, text string) string {
	t.Helper()
	for _, ln := range lines {
		if strings.Contains(ln, text) {
			return ln
		}
	}
	t.Fatalf("no fixture line contains %q", text)
	return ""
}

func requireVerdict(t *testing.T, out string, rc int, want string) {
	t.Helper()
	wantRC := 0
	switch want {
	case "FAIL":
		wantRC = 1
	case "TOO EARLY":
		wantRC = 3
	}
	got := lastLine(out)
	if !strings.HasPrefix(got, "POST-INSTALL: "+want) || rc != wantRC {
		t.Fatalf("want last line starting %q and exit %d, got %q and exit %d\n%s", "POST-INSTALL: "+want, wantRC, got, rc, out)
	}
	if want == "PASS" && got != "POST-INSTALL: PASS" {
		t.Fatalf("a PASS verdict carries nothing after it, got %q\n%s", got, out)
	}
}

func requireLine(t *testing.T, out, prefix string) {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, prefix) {
			return
		}
	}
	t.Errorf("no output line starts with %q\n%s", prefix, out)
}

func forbidLine(t *testing.T, out, prefix string) {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, prefix) {
			t.Errorf("unexpected line %q\n%s", ln, out)
		}
	}
}

// The release #62 boot (11-06) is clean: every check passes and nothing FAILs.
func TestPostInstallCheckHealthyBootPasses(t *testing.T) {
	out, rc := runPostInstall(t, fixture("healthy"), healthyRev, "")
	requireVerdict(t, out, rc, "PASS")
	forbidLine(t, out, "FAIL ")
	requireLine(t, out, "PASS boot integrity: OK")
	requireLine(t, out, "PASS boot rev: 5e50da0cea8a matches")
	requireLine(t, out, "PASS goldens: PASS")
	requireLine(t, out, "PASS errors: 0 [ERRO] lines since the process started")
	requireLine(t, out, "PASS alive: newest log line 11-06 16:10:48 CT")
	requireLine(t, out, "PASS ai: latest ai_call")
	requireLine(t, out, "PASS push: 2 sent, 0 send errors")
	requireLine(t, out, "INFO funnel: hoang [..246265]: 11-06 15:58:59 CT: entry intents 0 · placed 0 · filled 0")
}

// The 11-04 outage selection: hoang's pause and feed outage never close, and
// 12345's newest AI cycle is a 402. 12345's pause and feed outage DO close, so
// they must not be reported — that is what proves the per-trader pairing.
func TestPostInstallCheckOutageBootFails(t *testing.T) {
	out, rc := runPostInstall(t, fixture("outage"), outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "PASS boot rev: ab356a3a46e7 matches") // the FAIL comes from the outage, not the rev
	requireLine(t, out, "FAIL pause: hoang [..246265] — TRADING PAUSED")
	requireLine(t, out, "FAIL feed: hoang [..246265] — FEED DOWN")
	requireLine(t, out, "FAIL ai: AI credit exhausted — latest ai_call")
	requireLine(t, out, "FAIL ai per trader (log): 12345 [..870197] — AI credit exhausted (402)")
	requireLine(t, out, "WARN errors: 3 [ERRO] lines since the process started")
	forbidLine(t, out, "FAIL pause: 12345")
	forbidLine(t, out, "FAIL feed: 12345")
	// No full trader id may leak into the report.
	if strings.Contains(out, "8d5c8af5_396db319") || strings.Contains(out, "trader_id=8d5c") {
		t.Errorf("a full trader id is printed:\n%s", out)
	}
}

// Telegram was unreachable at 11-03 02:00 CT and nothing was sent afterwards.
func TestPostInstallCheckPushFailureFails(t *testing.T) {
	out, rc := runPostInstall(t, fixture("push-down"), outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL push: 4 Telegram send error(s) since boot and nothing sent after the last one")
}

// A binary that is not the one deploy/RELEASE names is a FAIL even when every
// other line is clean.
func TestPostInstallCheckWrongRevFails(t *testing.T) {
	out, rc := runPostInstall(t, fixture("healthy"), outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL boot rev: running 5e50da0cea8a but RELEASE names ab356a3a46e7")
}

// Only the LAST boot is judged: the outage process followed by the healthy one
// is a PASS. A line that merely contains the boot text, from a caller that is
// not main.go, does not move the window.
func TestPostInstallCheckJudgesOnlyTheLastRealBoot(t *testing.T) {
	outage, err := os.ReadFile(fixture("outage"))
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := os.ReadFile(fixture("healthy"))
	if err != nil {
		t.Fatal(err)
	}
	// The outage boot line, re-attributed to a non-main.go caller: what an echo of
	// the boot text in some other log line would look like. It is stamped with
	// the healthy log's last time, so it is also the newest line (the liveness
	// check reads the last stamped line).
	var echo string
	for _, ln := range strings.Split(string(outage), "\n") {
		if strings.Contains(ln, "BOOT INTEGRITY OK") {
			echo = redate(strings.Replace(ln, "vl/main.go:496", "telegram/bot.go:88", 1), "11-06 16:10:48")
		}
	}
	if echo == "" {
		t.Fatal("outage fixture has no boot line")
	}
	logPath := filepath.Join(t.TempDir(), "vl_2026-11-06.log")
	body := string(outage) + string(healthy) + echo + "\n"
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, rc := runPostInstall(t, logPath, healthyRev, "")
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "PASS boot rev: 5e50da0cea8a matches")
}

// A boot that refused trading, with goldens FAIL. The healthy boot line is
// rewritten into the shape a refused boot logs (main.go logs Line() with
// logger.Errorf, and kernel/boot_integrity.go prints REFUSED and "goldens FAIL").
func TestPostInstallCheckRefusedBootFails(t *testing.T) {
	healthy, err := os.ReadFile(fixture("healthy"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(healthy)
	okLine := "[INFO] vl/main.go:496 🔐 BOOT INTEGRITY OK — rev"
	if !strings.Contains(body, okLine) || !strings.Contains(body, "· goldens PASS") {
		t.Fatal("healthy fixture boot line changed shape")
	}
	body = strings.Replace(body, okLine, "[ERRO] vl/main.go:496 🔐 BOOT INTEGRITY REFUSED — rev", 1)
	body = strings.Replace(body, "· goldens PASS", "· goldens FAIL", 1)
	logPath := filepath.Join(t.TempDir(), "vl_2026-11-06.log")
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, rc := runPostInstall(t, logPath, healthyRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL boot integrity: REFUSED")
	requireLine(t, out, "FAIL goldens: FAIL")
}

// Unknown is never clear: if the log readers cannot run, the verdict is FAIL.
func TestPostInstallCheckBrokenReaderFails(t *testing.T) {
	bin := t.TempDir()
	stub := "#!/bin/sh\nexit 2\n"
	if err := os.WriteFile(filepath.Join(bin, "awk"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	out, rc := runPostInstall(t, fixture("healthy"), healthyRev, "", "PATH="+bin+":"+os.Getenv("PATH"))
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL pause: the log reader failed")
}

// The database leg: per trader, the newest decision row between the boot and
// the log's last line. Rows outside that window are ignored. The script must
// leave the database file and its directory exactly as it found them.
func TestPostInstallCheckDatabaseLegIsReadOnly(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	dbDir := t.TempDir()
	db := filepath.Join(dbDir, "data.db")
	// Real trader ids and the real error class; only the columns the script reads.
	// The healthy fixture boots 11-06 13:45:53 CT (19:45:53 UTC) and its last
	// line is 16:10:48 CT (22:10:48 UTC).
	const hoang = "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"
	const t12345 = "8d5c8af5_396db319-d3fe-4d63-9b97-516ff0008f53_deepseek_0751c0b6_1791870197"
	schema := `
CREATE TABLE traders (id text PRIMARY KEY, name text);
CREATE TABLE decision_records (id integer PRIMARY KEY AUTOINCREMENT, trader_id text NOT NULL,
  timestamp datetime NOT NULL, success numeric DEFAULT false, error_class text DEFAULT "");
INSERT INTO traders VALUES ('` + hoang + `', 'hoang'), ('` + t12345 + `', '12345');
INSERT INTO decision_records (trader_id, timestamp, success, error_class) VALUES
  ('` + t12345 + `', '2026-11-06 19:42:51.492236357+00:00', 1, ''),
  ('` + hoang + `',  '2026-11-06 21:59:44.835330586+00:00', 1, ''),
  ('` + t12345 + `', '2026-11-06 21:57:55.092954939+00:00', 0, 'ai_payment_402'),
  ('` + t12345 + `', '2026-11-06 23:30:00.000000000+00:00', 1, '');`
	if out, err := exec.Command("sqlite3", db, schema).CombinedOutput(); err != nil {
		t.Fatalf("build fixture db: %v\n%s", err, out)
	}
	before := dirSnapshot(t, dbDir)

	out, rc := runPostInstall(t, fixture("healthy"), healthyRev, db)
	requireVerdict(t, out, rc, "FAIL")
	// The row before the boot and the success after the log's last line are both
	// outside the window, so 12345's newest row is the 402.
	requireLine(t, out, "FAIL ai per trader (db): 12345 [..870197] — AI credit exhausted (402)")
	requireLine(t, out, "PASS ai per trader (db): hoang [..246265] — latest decision at 2026-11-06 21:59:44 UTC succeeded")

	if after := dirSnapshot(t, dbDir); after != before {
		t.Errorf("the script changed the database directory:\nbefore %s\nafter  %s", before, after)
	}
}

// The database window has a LOWER bound too: a trader whose only row is a 402
// from before the boot is not this process's problem and must not be reported,
// and a trader whose 402 is older than the boot (lower id) with a clean row
// inside the window passes.
func TestPostInstallCheckDatabaseLegIgnoresRowsBeforeTheProcess(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	db := filepath.Join(t.TempDir(), "data.db")
	// The healthy fixture's process starts 11-06 13:45:48 CT = 19:45:48 UTC.
	const hoang = "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"
	const t4567 = "8d5c8af5_396db319-d3fe-4d63-9b97-516ff0008f53_deepseek_0751c0b6_1792604566"
	schema := `
CREATE TABLE traders (id text PRIMARY KEY, name text);
CREATE TABLE decision_records (id integer PRIMARY KEY AUTOINCREMENT, trader_id text NOT NULL,
  timestamp datetime NOT NULL, success numeric DEFAULT false, error_class text DEFAULT "");
INSERT INTO traders VALUES ('` + hoang + `', 'hoang'), ('` + t4567 + `', '4567489 mess.');
INSERT INTO decision_records (trader_id, timestamp, success, error_class) VALUES
  ('` + hoang + `', '2026-11-06 19:40:00.000000000+00:00', 0, 'ai_payment_402'),
  ('` + t4567 + `', '2026-11-06 19:41:00.000000000+00:00', 0, 'ai_payment_402'),
  ('` + hoang + `', '2026-11-06 21:59:44.835330586+00:00', 1, '');`
	if out, err := exec.Command("sqlite3", db, schema).CombinedOutput(); err != nil {
		t.Fatalf("build fixture db: %v\n%s", err, out)
	}
	out, rc := runPostInstall(t, fixture("healthy"), healthyRev, db)
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "PASS ai per trader (db): hoang [..246265] — latest decision at 2026-11-06 21:59:44 UTC succeeded")
	if strings.Contains(out, "ai per trader (db): 4567489") {
		t.Errorf("a trader with no row since the process started must not be reported:\n%s", out)
	}
}

// Finding (m468 review): an install made while the CME is closed (Sunday, or
// the 16:00-17:00 break) pauses every trader on data_integrity_unknown about a
// minute after boot, because the boot hole scan needs a live bar. That is
// expected until the reopen — a check run then must not say FAIL, and must not
// say PASS either: it says TOO EARLY and when to re-run. Real 11-01 Sunday
// boot, before the 17:00 reopen.
func TestPostInstallCheckClosedMarketUnknownPauseIsTooEarlyNotFail(t *testing.T) {
	lines := fixtureLines(t, "closed-boot")
	var before []string
	for _, ln := range lines {
		if ln[:5] == "11-01" && ln[6:14] >= "17:00:00" {
			break
		}
		before = append(before, ln)
	}
	logPath := writeLog(t, "vl_2026-11-01.log", before)
	out, rc := runPostInstall(t, logPath, outageRev, "", nowEnv(ct(t, 2026, "11-01 16:32:37")))
	requireVerdict(t, out, rc, "TOO EARLY")
	forbidLine(t, out, "FAIL ")
	requireLine(t, out, "WARN pause: hoang [..246265] — TRADING PAUSED (new entries refused) (data_integrity_unknown) since 11-01 15:56:21 CT, still open — expected while the CME is closed")
	requireLine(t, out, "WARN pause: 4567489 mess. [..604566] —")
	if !strings.Contains(lastLine(out), "re-run after 11-01 17:45 CT") {
		t.Errorf("the verdict must say when to re-run (the 17:00 reopen + 45 min), got %q", lastLine(out))
	}
}

// The same process after the reopen: every pause resolved at 17:26 → PASS.
func TestPostInstallCheckClosedMarketBootPassesAfterTheReopen(t *testing.T) {
	out, rc := runPostInstall(t, fixture("closed-boot"), outageRev, "")
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "PASS pause: 3 start line(s)")
}

// But an unknown pause that is still open well after the reopen is real.
func TestPostInstallCheckUnknownPauseStillOpenAfterTheReopenGraceFails(t *testing.T) {
	lines := without(fixtureLines(t, "closed-boot"), "▶ TRADING RESUMED")
	// A later real line, so the log is still being written at 17:50.
	lines = append(lines, redate(lineWith(t, lines, "clock step BACKWARD"), "11-01 17:50:00"))
	logPath := writeLog(t, "vl_2026-11-01.log", lines)
	out, rc := runPostInstall(t, logPath, outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL pause: hoang [..246265] — TRADING PAUSED (new entries refused) (data_integrity_unknown) since 11-01 15:56:21 CT, never resolved")
}

// Finding: the traders start and log BEFORE main.go prints the boot line, so
// the judged window starts at the process banner. The real 11-01 boot has two
// "mentor seed REFUSING ALL entries" [ERRO] lines in that gap.
func TestPostInstallCheckCountsErrorsBeforeTheBootLine(t *testing.T) {
	out, _ := runPostInstall(t, fixture("closed-boot"), outageRev, "")
	requireLine(t, out, "WARN errors: 2 [ERRO] lines since the process started")
	if !strings.Contains(out, "mentor seed REFUSING ALL entries") {
		t.Errorf("the most common [ERRO] message must be listed:\n%s", out)
	}
}

// Finding: a check run straight after boot saw nothing yet (no integrity
// beat, no feed watcher, no AI cycle) and said PASS.
func TestPostInstallCheckRightAfterBootIsTooEarly(t *testing.T) {
	lines := fixtureLines(t, "healthy")
	var early []string
	for _, ln := range lines {
		if ln[6:14] > "13:45:55" {
			break
		}
		early = append(early, ln)
	}
	logPath := writeLog(t, "vl_2026-11-06.log", early)
	out, rc := runPostInstall(t, logPath, healthyRev, "", nowEnv(ct(t, 2026, "11-06 13:46:53")))
	requireVerdict(t, out, rc, "TOO EARLY")
	if !strings.Contains(lastLine(out), "60 s since boot") || !strings.Contains(lastLine(out), "re-run after 11-06 13:57 CT") {
		t.Errorf("the verdict must say how long after boot it looked and when to re-run, got %q", lastLine(out))
	}
}

// Finding: while the CME has been open past the settle time, no AI call since
// boot means the bot is not deciding anything: FAIL, not WARN.
func TestPostInstallCheckNoAICallWhileTheCMEIsOpenFails(t *testing.T) {
	logPath := writeLog(t, "vl_2026-11-01.log", without(fixtureLines(t, "closed-boot"), " ai_call model="))
	out, rc := runPostInstall(t, logPath, outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL ai: no ai_call line since the process started, and the CME has been open")
}

// Findings: a booted process that then hung or died writes nothing more; every
// check reads clean. The newest line's age is the liveness check.
func TestPostInstallCheckDeadProcessFails(t *testing.T) {
	newest := logNewest(t, fixture("healthy"))
	out, rc := runPostInstall(t, fixture("healthy"), healthyRev, "", nowEnv(newest.Add(6*24*time.Hour)))
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL alive: the newest log line (11-06 16:10:48 CT) is 8640 min old")
	// 16 minutes is past the 15-minute limit; 14 is not.
	out, _ = runPostInstall(t, fixture("healthy"), healthyRev, "", nowEnv(newest.Add(16*time.Minute)))
	requireLine(t, out, "FAIL alive:")
	out, rc = runPostInstall(t, fixture("healthy"), healthyRev, "", nowEnv(newest.Add(14*time.Minute)))
	requireVerdict(t, out, rc, "PASS")
}

// A RELEASE file that is missing or empty cannot confirm the running binary.
func TestPostInstallCheckMissingOrEmptyReleaseFails(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "RELEASE")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{filepath.Join(dir, "no-such-RELEASE"), empty} {
		out, rc := runPostInstallRelease(t, fixture("healthy"), rel, "")
		requireVerdict(t, out, rc, "FAIL")
		requireLine(t, out, "FAIL boot rev: cannot read the release sha")
	}
}

// A binary built from a tree with uncommitted changes is named as such.
func TestPostInstallCheckDirtyBuildWarns(t *testing.T) {
	lines := fixtureLines(t, "healthy")
	for i, ln := range lines {
		lines[i] = strings.Replace(ln, "— rev 5e50da0cea8a · pid", "— rev 5e50da0cea8a +dirty · pid", 1)
	}
	out, rc := runPostInstall(t, writeLog(t, "vl_2026-11-06.log", lines), healthyRev, "")
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "WARN boot rev: binary 5e50da0cea8a was built from a tree with uncommitted changes (+dirty)")
	requireLine(t, out, "PASS boot rev: 5e50da0cea8a matches")
}

// A 402 that a later AI call for the same trader followed is recovered, not a
// FAIL. The real 11-04 402 line, placed before 12345's real 11-06 AI call.
func TestPostInstallCheck402FollowedByAnAICallIsNotAFailure(t *testing.T) {
	lines := fixtureLines(t, "healthy")
	p402 := redate(lineWith(t, fixtureLines(t, "outage"), "💸 DEEPSEEK PAYMENT FAILURE"), "11-06 15:50:00")
	var out []string
	for _, ln := range lines {
		if strings.Contains(ln, "⏱️ AI call (") {
			out = append(out, p402)
		}
		out = append(out, ln)
	}
	res, rc := runPostInstall(t, writeLog(t, "vl_2026-11-06.log", out), healthyRev, "")
	requireVerdict(t, res, rc, "PASS")
	requireLine(t, res, "PASS ai per trader (log): no trader's latest AI cycle is a 402")
}

// Telegram failed once and then sent again: recovered is a WARN, not a FAIL.
// Held pushes discarded as stale never reached the owner: WARN.
func TestPostInstallCheckPushRecoveredAndDroppedWarn(t *testing.T) {
	lines := fixtureLines(t, "healthy")
	failed := redate(lineWith(t, fixtureLines(t, "push-down"), "owner push: SEND FAILED "), "11-06 14:00:00")
	// A real line from ~/vl/data/vl_2026-11-06.log (06:34:52, ownerpush.go:462),
	// re-stamped to sit inside this process.
	stale := redate(`11-06 06:34:52 [WARN] ownerpush/ownerpush.go:462 owner push: held "data-paused-8d5c8af5_396db319-d3fe-4d63-9b97-516ff0008f53_deepseek_0751c0b6_1791870197|1793963093" is older than 1h30m0s - discarded as stale`, "11-06 14:01:00")
	var body []string
	for _, ln := range lines {
		if strings.Contains(ln, `SENT "data-report|`) {
			body = append(body, failed, stale)
		}
		body = append(body, ln)
	}
	out, rc := runPostInstall(t, writeLog(t, "vl_2026-11-06.log", body), healthyRev, "")
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "WARN push: 1 Telegram send error(s) since boot, recovered")
	requireLine(t, out, "WARN push: 1 held message(s) dropped")
}

// dirSnapshot lists every file in dir with the sha256 of its contents, so any
// write, new journal file or removal shows up as a difference.
func dirSnapshot(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var lines []string
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%s=%x", n, sha256.Sum256(b)))
	}
	return strings.Join(lines, " ")
}

// m471 L4-1: a pause that comes back within the merge window of its own resume
// logs "⏸ TRADING PAUSED AGAIN (" (trader/data_integrity_gate.go, m469) and
// sends the owner nothing new. It refuses entries exactly like the first one,
// so a re-pause that is never resumed must FAIL, not read as "every trader
// resolved". hoang's real 15:56 PAUSED line, re-worded to the PAUSED AGAIN
// shape and re-stamped 17:30, after its real 17:26 RESUMED.
func TestPostInstallCheckPausedAgainAfterResumeAndNeverResumedFails(t *testing.T) {
	lines := fixtureLines(t, "closed-boot")
	paused := lineWith(t, lines, "trader_name=hoang] ⏸ TRADING PAUSED (data_integrity_unknown)")
	again := strings.Replace(paused, "⏸ TRADING PAUSED (data_integrity_unknown)", "⏸ TRADING PAUSED AGAIN (data_integrity_hole)", 1)
	lines = append(lines, redate(again, "11-01 17:30:00"))
	// the process is still writing at 17:50 (a later real line, re-stamped)
	lines = append(lines, redate(lineWith(t, lines, "clock step BACKWARD"), "11-01 17:50:00"))
	out, rc := runPostInstall(t, writeLog(t, "vl_2026-11-01.log", lines), outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL pause: hoang [..246265] — TRADING PAUSED (new entries refused) (data_integrity_hole) since 11-01 17:30:00 CT, never resolved")
	forbidLine(t, out, "FAIL pause: 12345")
	forbidLine(t, out, "PASS pause:")
}

// ...and a PAUSED AGAIN that a later RESUMED closes is resolved like any pause.
func TestPostInstallCheckPausedAgainThenResumedPasses(t *testing.T) {
	lines := fixtureLines(t, "closed-boot")
	paused := lineWith(t, lines, "trader_name=hoang] ⏸ TRADING PAUSED (data_integrity_unknown)")
	resumed := lineWith(t, lines, "trader_name=hoang] ▶ TRADING RESUMED:")
	again := strings.Replace(paused, "⏸ TRADING PAUSED (data_integrity_unknown)", "⏸ TRADING PAUSED AGAIN (data_integrity_hole)", 1)
	lines = append(lines, redate(again, "11-01 17:30:00"), redate(resumed, "11-01 17:34:00"))
	lines = append(lines, redate(lineWith(t, lines, "clock step BACKWARD"), "11-01 17:50:00"))
	out, rc := runPostInstall(t, writeLog(t, "vl_2026-11-01.log", lines), outageRev, "")
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "PASS pause: 4 start line(s) since the process started, every trader resolved")
}

// failingAICall turns the closed-boot fixture's real ok=true ai_call line into
// the shape mcp/client.go logs for a failed call (WARN, ok=false, a class and a
// status), at each given stamp.
func failingAICall(t *testing.T, lines []string, status, class string, stamps ...string) []string {
	t.Helper()
	okLine := lineWith(t, lines, " ai_call model=")
	failed := strings.Replace(okLine, "[INFO] mcp/client.go:379", "[WARN] mcp/client.go:407", 1)
	failed = strings.Replace(failed, "finish_reason=stop ok=true", "finish_reason=n/a ok=false", 1)
	failed = strings.Replace(failed, "http_status=200", "deadline_s=600 class="+class+" provider_side=true http_status="+status, 1)
	if !strings.Contains(failed, " ok=false") || !strings.Contains(failed, " http_status="+status) {
		t.Fatalf("closed-boot ai_call line changed shape: %q", okLine)
	}
	var out []string
	for _, st := range stamps {
		out = append(out, redate(failed, st))
	}
	return out
}

// m471 L4-4: with the CME open past the settle time and EVERY ai_call since the
// process started failing for a non-402 reason (401 here; a 429 or a timeout
// reads the same), the bot is deciding nothing: FAIL, not WARN.
func TestPostInstallCheckEveryAICallFailingWhileTheCMEIsOpenFails(t *testing.T) {
	lines := fixtureLines(t, "closed-boot")
	failed := failingAICall(t, lines, "401", "auth", "11-01 17:15:00", "11-01 17:18:00", "11-01 17:21:00", "11-01 17:24:00")
	lines = append(without(lines, " ai_call model="), failed...)
	lines = append(lines, redate(lineWith(t, lines, "clock step BACKWARD"), "11-01 17:26:50"))
	out, rc := runPostInstall(t, writeLog(t, "vl_2026-11-01.log", lines), outageRev, "")
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL ai: no successful AI call since the process started — 4 ai_call(s), every one failed; latest ai_call (deepseek-v4-pro, 11-01 17:24:00 CT) ok=false http_status=401 class=auth")
}

// A failing latest call after an EARLIER success in the same process is a WARN:
// the AI has worked for this process, the newest call is just a failure.
func TestPostInstallCheckAICallFailingAfterAnEarlierSuccessWarns(t *testing.T) {
	lines := fixtureLines(t, "closed-boot")
	lines = append(lines, failingAICall(t, lines, "429", "rate_limit", "11-01 17:24:00")...)
	lines = append(lines, redate(lineWith(t, lines, "clock step BACKWARD"), "11-01 17:26:50"))
	out, rc := runPostInstall(t, writeLog(t, "vl_2026-11-01.log", lines), outageRev, "")
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "WARN ai: latest ai_call (deepseek-v4-pro, 11-01 17:24:00 CT) ok=false http_status=429 class=rate_limit — 1 of 2 ai_call(s) since the process started succeeded")
}

// While the CME is closed, every call failing cannot be judged yet: WARN and
// TOO EARLY until the reopen plus the settle time — never FAIL, and never PASS.
// The closed-boot fixture cut before the 17:00 reopen, with its pauses left out
// so the AI leg is the only thing still pending.
func TestPostInstallCheckEveryAICallFailingWhileTheCMEIsClosedIsTooEarly(t *testing.T) {
	var before []string
	for _, ln := range fixtureLines(t, "closed-boot") {
		if ln[:5] == "11-01" && ln[6:14] >= "17:00:00" {
			break
		}
		before = append(before, ln)
	}
	failed := failingAICall(t, before, "401", "auth", "11-01 16:31:37")
	before = append(without(before, " ai_call model=", "⏸ TRADING PAUSED"), failed...)
	logPath := writeLog(t, "vl_2026-11-01.log", before)
	out, rc := runPostInstall(t, logPath, outageRev, "", nowEnv(ct(t, 2026, "11-01 16:32:37")))
	requireVerdict(t, out, rc, "TOO EARLY")
	forbidLine(t, out, "FAIL ")
	requireLine(t, out, "WARN ai: no successful AI call since the process started (1 ai_call(s), every one failed;")
	if !strings.Contains(lastLine(out), "no successful AI call yet") || !strings.Contains(lastLine(out), "re-run after 11-01 17:12 CT") {
		t.Errorf("the verdict must name the AI leg and the reopen + settle time, got %q", lastLine(out))
	}
}

// m471 L4-2: deploy/RELEASE is written by the same install and restored with the
// binary by a rollback, so it agrees with whatever runs. --expect names the
// release the reader MEANT to install; a different running rev is a FAIL.
func TestPostInstallCheckExpectFailsWhenTheMeantReleaseIsNotRunning(t *testing.T) {
	rel := filepath.Join(t.TempDir(), "RELEASE")
	if err := os.WriteFile(rel, []byte(healthyRev+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notGit := t.TempDir()

	// The Update to 0247b6c6f rolled back to 5e50da0ce: RELEASE still matches.
	out, rc := runPostInstallArgs(t, fixture("healthy"), rel, "", []string{"--expect", "0247b6c6f"})
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "PASS boot rev: 5e50da0cea8a matches")
	requireLine(t, out, "FAIL expect: running 5e50da0cea8a but you expected 0247b6c6f (0247b6c6f)")

	out, rc = runPostInstallArgs(t, fixture("healthy"), rel, "", []string{"--expect", "5e50da0ce"})
	requireVerdict(t, out, rc, "PASS")
	requireLine(t, out, "PASS expect: running 5e50da0cea8a is 5e50da0ce")

	// A name that is not a sha and not a tag cannot confirm anything.
	out, rc = runPostInstallArgs(t, fixture("healthy"), rel, "", []string{"--expect", "v2026.11.07.1"}, "VL_REPO="+notGit)
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL expect: cannot resolve 'v2026.11.07.1' to a commit")

	// A tag is resolved in the install dir's git: here it names a commit that is
	// not the running one, and the resolved sha is printed.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitc := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	gitc("init", "-q")
	gitc("commit", "-q", "--allow-empty", "-m", "x")
	gitc("tag", "-a", "v2026.11.07.1", "-m", "x")
	sha := gitc("rev-parse", "HEAD")
	out, rc = runPostInstallArgs(t, fixture("healthy"), rel, "", []string{"--expect", "v2026.11.07.1"}, "VL_REPO="+repo)
	requireVerdict(t, out, rc, "FAIL")
	requireLine(t, out, "FAIL expect: running 5e50da0cea8a but you expected v2026.11.07.1 ("+sha[:12]+")")
}
