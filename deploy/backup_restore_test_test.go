package deploy

// m448 contract tests for deploy/backup-restore-test.sh — the weekly proof that
// the NEWEST DB backup restores. Driven against real small SQLite databases
// built with python3 stdlib, gzipped into a scratch backup dir, with a scratch
// HOME. The live DB and the real ~/vl-backups are never named.
//
// Pins: a good backup passes and writes ONE result line; a corrupt / truncated /
// empty / stale / bad-gzip backup FAILS non-zero with a reason; the NEWEST file
// is the one tested; the source backup is never modified; the script's own
// mktemp dir is gone afterwards and nothing else was removed; a second run never
// overwrites the first run's result file.

import (
	"compress/gzip"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const restoreScript = "backup-restore-test.sh"

// mkRestoreDB builds a DB shaped like the real one (the tables the checker reads).
func mkRestoreDB(t *testing.T, path string, traders, positions int, newestBarMs int64) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not found: %v", err)
	}
	py := `import sqlite3,sys
p,tr,po,nb=sys.argv[1],int(sys.argv[2]),int(sys.argv[3]),int(sys.argv[4])
c=sqlite3.connect(p)
c.execute('CREATE TABLE traders(id TEXT PRIMARY KEY, name TEXT)')
c.execute('CREATE TABLE trader_positions(id INTEGER PRIMARY KEY, trader_id TEXT, note TEXT)')
c.execute('CREATE TABLE bars(symbol TEXT, tf TEXT, open_time_ms INTEGER, c REAL, PRIMARY KEY(symbol,tf,open_time_ms))')
c.executemany('INSERT INTO traders VALUES (?,?)',[('t%d'%i,'n') for i in range(tr)])
c.executemany('INSERT INTO trader_positions(trader_id,note) VALUES (?,?)',[('t0','x'*200) for i in range(po)])
c.executemany('INSERT INTO bars VALUES (?,?,?,?)',[('MNQ','1m',nb-i*60000,1.0) for i in range(5000)])
c.execute('CREATE INDEX bars_c ON bars(c, open_time_ms)')
c.commit(); c.close()`
	out, err := exec.Command("python3", "-c", py, path,
		itoa(traders), itoa(positions), itoa64(newestBarMs)).CombinedOutput()
	if err != nil {
		t.Fatalf("mk db: %v\n%s", err, out)
	}
}

func itoa(i int) string     { return itoa64(int64(i)) }
func itoa64(i int64) string { return strconv.FormatInt(i, 10) }

func gzipTo(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	f.Close()
}

type restoreEnv struct {
	home, backups, results, work string
}

func newRestoreEnv(t *testing.T) restoreEnv {
	t.Helper()
	root := t.TempDir()
	e := restoreEnv{
		home:    filepath.Join(root, "home"),
		backups: filepath.Join(root, "backups", "daily"),
		results: filepath.Join(root, "results"),
		work:    filepath.Join(root, "work"),
	}
	for _, d := range []string{e.home, e.backups} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// addBackup writes backup `name` as a good DB whose newest bar is `barAge`
// before the backup's own mtime, then stamps the file mtime to `at`.
func (e restoreEnv) addBackup(t *testing.T, name string, traders, positions int, barAge time.Duration, at time.Time) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "x.db")
	mkRestoreDB(t, tmp, traders, positions, at.Add(-barAge).UnixMilli())
	dst := filepath.Join(e.backups, name)
	gzipTo(t, tmp, dst)
	if err := os.Chtimes(dst, at, at); err != nil {
		t.Fatal(err)
	}
	return dst
}

func (e restoreEnv) run(t *testing.T, extra ...string) (stdout string, rc int) {
	t.Helper()
	cmd := exec.Command("bash", restoreScript)
	cmd.Env = append([]string{
		"HOME=" + e.home,
		"PATH=" + os.Getenv("PATH"),
		"VL_RESTORE_TEST_BACKUP_DIRS=" + e.backups,
		"VL_RESTORE_TEST_DIR=" + e.results,
		"VL_RESTORE_TEST_TMP=" + e.work,
		"VL_RESTORE_TEST_MIN_FREE_GB=0",
	}, extra...)
	// stdout only: the result line. (stderr carries the box's safe-rm notice.)
	out, err := cmd.Output()
	rc = 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimSpace(string(out)), rc
}

func (e restoreEnv) resultFiles(t *testing.T) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(e.results, "*.result"))
	return m
}

func (e restoreEnv) leftovers(t *testing.T) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(e.work, "restore-test.*"))
	return m
}

func sum(t *testing.T, p string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

var restoreOKLine = regexp.MustCompile(`^RESTORE-TEST OK ts=\d+ src=\S+ integrity=ok traders=\d+ positions=\d+ bars=\d+ bars_newest=\S+ bars_newest_age_h=[\d.]+ secs=\d+$`)

func TestRestoreTestPassesAGoodBackupAndWritesOneResultLine(t *testing.T) {
	e := newRestoreEnv(t)
	at := time.Now().Add(-2 * time.Hour)
	bk := e.addBackup(t, "vl-2026-10-29_050015.db.gz", 3, 40, 3*time.Hour, at)
	before := sum(t, bk)
	// Another file in the dir that must survive the run untouched.
	keep := filepath.Join(e.backups, "do-not-touch.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, rc := e.run(t)
	if rc != 0 {
		t.Fatalf("good backup rc=%d: %s", rc, out)
	}
	if !restoreOKLine.MatchString(out) {
		t.Fatalf("result line shape wrong: %q", out)
	}
	if !strings.Contains(out, "src=vl-2026-10-29_050015.db.gz") || !strings.Contains(out, "traders=3") || !strings.Contains(out, "positions=40") {
		t.Fatalf("row counts not read back: %q", out)
	}
	files := e.resultFiles(t)
	if len(files) != 1 {
		t.Fatalf("want one result file, got %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.TrimSpace(string(b)) != out {
		t.Fatalf("result file %q != printed line %q", b, out)
	}
	if sum(t, bk) != before {
		t.Fatal("the source backup was modified")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("an unrelated file was removed")
	}
	if l := e.leftovers(t); len(l) != 0 {
		t.Fatalf("the script's own mktemp dir was not cleaned: %v", l)
	}
}

// The bar-age bound is measured against the BACKUP's own time. A month-old
// backup whose newest bar is 3 h older than ITSELF is healthy; judged against
// "now" it would read as a 700 h gap and fail every restore test of an old file.
func TestRestoreTestMeasuresBarAgeAgainstTheBackupNotNow(t *testing.T) {
	e := newRestoreEnv(t)
	e.addBackup(t, "vl-2026-09-29_050015.db.gz", 3, 40, 3*time.Hour, time.Now().Add(-30*24*time.Hour))
	out, rc := e.run(t)
	if rc != 0 || !strings.Contains(out, "RESTORE-TEST OK") || !strings.Contains(out, "bars_newest_age_h=3.0") {
		t.Fatalf("a 30-day-old backup with fresh-for-its-time bars must pass: rc=%d out=%q", rc, out)
	}
}

func TestRestoreTestTestsTheNEWESTBackup(t *testing.T) {
	e := newRestoreEnv(t)
	now := time.Now()
	e.addBackup(t, "vl-2026-10-28_050001.db.gz", 3, 40, time.Hour, now.Add(-30*time.Hour)) // older, good
	// The newer one has NO traders: it must be the one judged, and it must fail.
	e.addBackup(t, "vl-2026-10-29_050015.db.gz", 0, 40, time.Hour, now.Add(-2*time.Hour))
	out, rc := e.run(t)
	if rc == 0 || !strings.HasPrefix(out, "RESTORE-TEST FAIL") || !strings.Contains(out, "src=vl-2026-10-29_050015.db.gz") || !strings.Contains(out, "no-traders") {
		t.Fatalf("the newest backup (no traders) must be the one tested and must fail; rc=%d out=%q", rc, out)
	}
}

func TestRestoreTestFailsCorruptTruncatedStaleAndEmptyBackups(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		mutate func(t *testing.T, e restoreEnv)
		reason string
	}{
		{"not-gzip", func(t *testing.T, e restoreEnv) {
			p := filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz")
			os.WriteFile(p, []byte("this is not a gzip stream"), 0o644)
		}, "gunzip"},
		{"empty-db", func(t *testing.T, e restoreEnv) {
			p := filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz")
			gzipTo(t, os.DevNull, p)
		}, "empty-after-decompress"},
		{"not-a-database", func(t *testing.T, e restoreEnv) {
			tmp := filepath.Join(t.TempDir(), "junk")
			os.WriteFile(tmp, []byte(strings.Repeat("not sqlite at all. ", 500)), 0o644)
			gzipTo(t, tmp, filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz"))
		}, "error="},
		{"truncated-db", func(t *testing.T, e restoreEnv) {
			tmp := filepath.Join(t.TempDir(), "x.db")
			mkRestoreDB(t, tmp, 3, 400, now.Add(-time.Hour).UnixMilli())
			b, _ := os.ReadFile(tmp)
			os.WriteFile(tmp, b[:len(b)/2], 0o644)
			gzipTo(t, tmp, filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz"))
		}, ""},
		{"zeroed-pages", func(t *testing.T, e restoreEnv) {
			tmp := filepath.Join(t.TempDir(), "x.db")
			mkRestoreDB(t, tmp, 3, 400, now.Add(-time.Hour).UnixMilli())
			b, _ := os.ReadFile(tmp)
			for i := len(b) / 3; i < len(b)/3+8192 && i < len(b); i++ {
				b[i] = 0xFF
			}
			os.WriteFile(tmp, b, 0o644)
			gzipTo(t, tmp, filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz"))
		}, ""},
		{"integrity-check-only", func(t *testing.T, e restoreEnv) {
			// Every table the script READS is fine (no malformed page, row counts and
			// bar age are healthy): only PRAGMA integrity_check can see the index that
			// no longer matches its table. A
			// script that dropped the pragma and relied on its queries erroring would
			// pass this backup.
			tmp := filepath.Join(t.TempDir(), "x.db")
			mkRestoreDB(t, tmp, 3, 40, now.Add(-time.Hour).UnixMilli())
			out, err := exec.Command("python3", "-c", `import sqlite3,sys
c=sqlite3.connect(sys.argv[1])
c.execute('CREATE TABLE ix(a INTEGER, b INTEGER)')
c.execute('CREATE INDEX ix_b ON ix(b)')
c.executemany('INSERT INTO ix VALUES (?,?)',[(i,1000-i) for i in range(200)])
c.commit()
c.execute('PRAGMA writable_schema=ON')
c.execute("UPDATE sqlite_master SET sql='CREATE INDEX ix_b ON ix(a)' WHERE name='ix_b'")
c.commit(); c.close()
c=sqlite3.connect(sys.argv[1])
print(c.execute('PRAGMA integrity_check').fetchall())`, tmp).CombinedOutput()
			if err != nil || strings.Contains(string(out), "[('ok',)]") {
				t.Fatalf("fixture premise: the index must be inconsistent with its table: %v %s", err, out)
			}
			gzipTo(t, tmp, filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz"))
			if err := os.Chtimes(filepath.Join(e.backups, "vl-2026-10-29_050015.db.gz"), now, now); err != nil {
				t.Fatal(err)
			}
		}, "integrity="},
		{"no-positions", func(t *testing.T, e restoreEnv) {
			e.addBackup(t, "vl-2026-10-29_050015.db.gz", 3, 0, time.Hour, now)
		}, "no-positions"},
		{"stale-bars", func(t *testing.T, e restoreEnv) {
			e.addBackup(t, "vl-2026-10-29_050015.db.gz", 3, 40, 200*time.Hour, now)
		}, "bars-stale"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRestoreEnv(t)
			c.mutate(t, e)
			out, rc := e.run(t)
			if rc == 0 {
				t.Fatalf("a bad backup passed: %q", out)
			}
			if !strings.HasPrefix(out, "RESTORE-TEST FAIL ") {
				t.Fatalf("not a FAIL line: %q", out)
			}
			if c.reason != "" && !strings.Contains(out, c.reason) {
				t.Fatalf("want reason %q in %q", c.reason, out)
			}
			if len(e.resultFiles(t)) != 1 {
				t.Fatalf("a failure must still leave its result file: %v", e.resultFiles(t))
			}
			if l := e.leftovers(t); len(l) != 0 {
				t.Fatalf("failed run left its mktemp dir: %v", l)
			}
		})
	}
}

func TestRestoreTestWithNoBackupsFailsAndSaysSo(t *testing.T) {
	e := newRestoreEnv(t)
	out, rc := e.run(t)
	if rc == 0 || !strings.Contains(out, "reason=no-backup-file") {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	e2 := newRestoreEnv(t)
	out, rc = e2.run(t, "VL_RESTORE_TEST_BACKUP_DIRS="+filepath.Join(e2.home, "nope"))
	if rc == 0 || !strings.Contains(out, "reason=no-backup-dir") {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
}

func TestRestoreTestRefusesWhenThereIsNoRoomAndNeverTouchesTheBackup(t *testing.T) {
	e := newRestoreEnv(t)
	bk := e.addBackup(t, "vl-2026-10-29_050015.db.gz", 3, 40, time.Hour, time.Now())
	before := sum(t, bk)
	out, rc := e.run(t, "VL_RESTORE_TEST_MIN_FREE_GB=99999999")
	if rc == 0 || !strings.Contains(out, "reason=disk-space") {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	if sum(t, bk) != before {
		t.Fatal("backup touched")
	}
}

func TestRestoreTestNeverOverwritesAnEarlierResult(t *testing.T) {
	e := newRestoreEnv(t)
	e.addBackup(t, "vl-2026-10-29_050015.db.gz", 3, 40, time.Hour, time.Now())
	if err := os.MkdirAll(e.results, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two runs inside the same second share a stamp; the second must not
	// clobber the first.
	for i := 0; i < 2; i++ {
		if out, rc := e.run(t); rc != 0 {
			t.Fatalf("run %d: %s", i, out)
		}
	}
	if n := len(e.resultFiles(t)); n != 2 {
		t.Fatalf("two runs must leave two result files (append-only), got %d", n)
	}
}

func TestRestoreTestScriptRemovesOnlyItsOwnTempDir(t *testing.T) {
	b, err := os.ReadFile(restoreScript)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	// Every rm in the script is the guarded one; there is exactly one.
	rms := regexp.MustCompile(`(?m)^\s*(?:[^#\n]*\s)?rm\s+-[a-zA-Z]+\s+(\S+)`).FindAllStringSubmatch(src, -1)
	if len(rms) != 1 || rms[0][1] != `"${TH:?}"` {
		t.Fatalf("the only removal must be rm -rf \"${TH:?}\", found %v", rms)
	}
	for _, bad := range []string{`"$HOME"`, `"$HOME_DIR"/`, "/*", "find . -delete", "-delete", "truncate", "shred"} {
		if strings.Contains(src, bad) {
			t.Errorf("script contains %q", bad)
		}
	}
	// The retired prefix never appears as a literal (rename census).
	if strings.Contains(strings.ToLower(src), "no"+"fx") {
		t.Error("script names the retired prefix literally")
	}
}
