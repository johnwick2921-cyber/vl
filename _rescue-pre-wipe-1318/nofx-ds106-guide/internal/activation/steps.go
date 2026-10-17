package activation

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	// vl/store/sqlitedriver is the ONE place in this repo that registers the
	// "sqlite" driver. A LIBRARY must never import a driver directly: anything
	// may link it, and database/sql panics when two register the same name in
	// one binary. This file imported github.com/glebarez/go-sqlite, which made
	// it a landmine for the M4 worker — the worker links this package AND
	// internal/updaterbootstrap, and the two registrants would have panicked the
	// process at init, before main ran.
	_ "vl/store/sqlitedriver"
)

// Backup takes an ONLINE copy of the database and proves the copy is readable
// before returning OK. A backup nobody has opened is not a backup — it is a
// file, and the moment you need it is the worst moment to discover that.
//
// It uses the driver's VACUUM INTO rather than shelling out to the sqlite3
// CLI (as store/ab_confirm.go does) so an unattended worker does not depend on
// a binary being installed on the box. Both are genuine online backups.
func Backup(dbPath, dest string) (Receipt, error) {
	rc := newReceipt("backup")
	rc.Evidence["db"] = dbPath
	rc.Evidence["dest"] = dest
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		// Idempotent at its own step: a crashed worker re-runs it.
		if err := verifyBackup(dest); err == nil {
			rc.Evidence["already"] = "true"
			rc.Evidence["bytes"] = fmt.Sprintf("%d", st.Size())
			return rc.done()
		}
		// A dest that exists but does NOT verify is worse than none: remove it
		// rather than leaving a file that looks like a backup.
		_ = os.Remove(dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return rc.fail(fmt.Errorf("cannot create backup dir: %w", err))
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return rc.fail(fmt.Errorf("cannot open %s: %w", dbPath, err))
	}
	defer db.Close()
	if _, err := db.Exec("VACUUM INTO ?", dest); err != nil {
		return rc.fail(fmt.Errorf("online backup failed: %w", err))
	}
	if err := verifyBackup(dest); err != nil {
		return rc.fail(err)
	}
	st, err := os.Stat(dest)
	if err != nil {
		return rc.fail(fmt.Errorf("backup vanished after writing: %w", err))
	}
	rc.Evidence["bytes"] = fmt.Sprintf("%d", st.Size())
	rc.Evidence["integrity_check"] = "ok"
	return rc.done()
}

// verifyBackup opens the copy and asks SQLite whether it is intact. Anything
// other than exactly "ok" is a refusal — integrity_check reports its findings
// as rows of prose, so a non-"ok" value is a real problem, never a warning.
func verifyBackup(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("backup %s does not open: %w", path, err)
	}
	defer db.Close()
	var verdict string
	if err := db.QueryRow("pragma integrity_check").Scan(&verdict); err != nil {
		return fmt.Errorf("backup %s: integrity_check did not run: %w", path, err)
	}
	if verdict != "ok" {
		return fmt.Errorf("backup %s failed integrity_check: %s", path, verdict)
	}
	return nil
}

// Activate swaps a release into place and restarts the process.
//
// Order matters and is not negotiable: every file is put in place FIRST, then
// the process is killed. A kill before the swap leaves the old binary running
// against new files, or nothing running at all while the copy proceeds.
//
// The kill is guarded by Identity: the pid is signalled ONLY if it is still the
// same process that Identity names. A recycled pid is a different program
// wearing the same number.
func Activate(rel, prev Release, id Identity) (Identity, Receipt, error) {
	rc := newReceipt("activate")
	rc.Evidence["release"] = rel.SHA
	rc.Evidence["prev"] = prev.SHA
	rc.Evidence["old_pid"] = fmt.Sprintf("%d", id.PID)

	for _, half := range []struct{ src, dst string }{
		{rel.Binary, prev.Binary},
		{rel.ReleaseFile, prev.ReleaseFile},
	} {
		if err := atomicCopy(half.src, half.dst); err != nil {
			return rc.failID(fmt.Errorf("install %s: %w", filepath.Base(half.dst), err))
		}
	}
	if err := atomicSwapDir(rel.Dist, prev.Dist); err != nil {
		return rc.failID(fmt.Errorf("install dist: %w", err))
	}
	rc.Evidence["installed"] = "binary,dist,RELEASE"

	// REFUSED, not "skipped", when the pid is no longer ours: we were told to
	// replace a specific process. Killing whatever holds that number now is the
	// exact bug Identity exists to prevent. One guard, used by all three steps.
	next, err := killAndAwait(rc, id)
	if err != nil {
		return rc.failID(err)
	}
	rc.Evidence["killed"] = "SIGKILL"
	rc.Evidence["new_pid"] = fmt.Sprintf("%d", next.PID)
	r, _ := rc.done()
	return next, r, nil
}

// waitForNewIdentity waits for systemd to relaunch the unit and returns the
// identity of the NEW process — never the old one, which is how a restart that
// silently did not happen gets reported as success.
func waitForNewIdentity(old Identity, within time.Duration) (Identity, error) {
	deadline := sys.Now().Add(within)
	for sys.Now().Before(deadline) {
		id, err := CurrentIdentity()
		if err == nil && id.PID != 0 && (id.PID != old.PID || id.StartTicks != old.StartTicks) {
			return id, nil
		}
		sys.Sleep(500 * time.Millisecond)
	}
	return Identity{}, fmt.Errorf("no new process within %s — the unit did not come back", within)
}

// Watch proves the NEW process is the release we installed.
//
// CONTRACT NOTE: §2 wrote this as Watch(id, logPath, healthURL, within) with a
// comment saying it must match "rel.SHA" — but no rel is passed. The sha cannot
// come from /api/health, because then health would be validating itself: a
// stale process answering with its own (old) sha would define its own success.
// The sha must come from the release we INTENDED to activate, so rel is a
// parameter. Flagged to the CTO; 3b-B imports this signature.
//
// GREEN requires BOTH signals, and the boot line must be NEWER than the kill —
// a boot line read from a log that predates the restart is the previous boot's
// line, and it says nothing about the process running now (boot lines are READ,
// never literal).
func Watch(rel Release, id Identity, opts WatchOpts) (Receipt, error) {
	rc := newReceipt("watch")
	rc.Evidence["expect_sha"] = rel.SHA
	rc.Evidence["pid"] = fmt.Sprintf("%d", id.PID)
	within := opts.Within
	if within <= 0 {
		within = 90 * time.Second
	}
	logPath, healthURL := opts.LogPath, opts.HealthURL
	// THE KILL INSTANT, not "now". A worker persists its receipt BEFORE the
	// side effect and may crash between the restart and the proof; when it
	// resumes it calls Watch again, minutes or hours later. Anchoring on the
	// current time would reject the boot line the restart actually wrote —
	// the step would fail because the worker was slow, not because the boot
	// failed, and the job would roll back a release that came up correctly.
	// So the caller passes the instant it killed, read back from its own
	// persisted state.
	since := opts.Since
	if since.IsZero() {
		// No kill instant supplied: the strictest honest anchor is now, which
		// is right for a caller watching a restart it is about to perform.
		since = rc.StartedAt
	}
	rc.Evidence["since"] = since.Format(time.RFC3339)
	deadline := sys.Now().Add(within)
	var sawLog, sawHealth bool
	for sys.Now().Before(deadline) {
		if !sawLog {
			if ok, err := bootLineAfter(logPath, rel.SHA, since); err == nil && ok {
				sawLog = true
				rc.Evidence["boot_line"] = "found after the kill"
			}
		}
		if !sawHealth {
			if sha, err := healthSHA(healthURL); err == nil {
				rc.Evidence["health_sha"] = sha
				if revisionsAgree(sha, rel.SHA) {
					sawHealth = true
				}
			}
		}
		if sawLog && sawHealth {
			return rc.done()
		}
		sys.Sleep(time.Second)
	}
	// Name which leg failed. "Timed out" alone sends the reader to look at
	// everything; the failing leg sends them to one place.
	var missing []string
	if !sawLog {
		missing = append(missing, fmt.Sprintf("no boot line for %s in %s after %s", rel.SHA, logPath, since.Format(time.RFC3339)))
	}
	if !sawHealth {
		missing = append(missing, fmt.Sprintf("%s never reported %s (last: %q)", healthURL, rel.SHA, rc.Evidence["health_sha"]))
	}
	return rc.fail(fmt.Errorf("not proven within %s: %s", within, strings.Join(missing, "; ")))
}

// WatchOpts carries what a proof needs to be repeatable.
//
// It is a struct rather than more positional parameters because the worker
// (3b-B) persists these values as job state and hands them back on resume —
// a caller assembling them from a database row should be able to see which
// field is which.
type WatchOpts struct {
	// LogPath is the file the RUNNING process writes. Logs are named by BOOT
	// date, not calendar date, so build it with NewestLogPath rather than
	// from today's date.
	LogPath string
	// HealthURL is asked for the revision it is serving.
	HealthURL string
	// Since is THE KILL INSTANT — the moment the old process was signalled.
	// A boot line older than this belongs to a previous boot and proves
	// nothing about the process running now. Zero means "now", which is
	// correct only for a caller that is watching a restart it just performed.
	Since time.Time
	// Within bounds the wait; zero means 90s.
	Within time.Duration
}

// bootLineAfter looks for a boot line naming sha whose timestamp is AFTER
// since. The file is re-read each call because the process writing it is the
// one we are waiting for.
func bootLineAfter(path, sha string, since time.Time) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return false, err
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if !lineNamesRevision(ln, sha) {
			continue
		}
		ts, ok := lineTime(ln)
		if !ok {
			continue
		}
		if ts.After(since) || ts.Equal(since) {
			return true, nil
		}
	}
	return false, nil
}

// lineNamesRevision reports whether a log line names this revision.
//
// FOUND ON THE LIVE BOX, and worse than the health case: the boot line prints
// the SHORT rev —
//
//	🔐 BOOT INTEGRITY OK — rev 662c79bd236f · built 2026-09-23T23:45:35Z · …
//
// and the full 40-hex sha appears ZERO times in the live log. A
// strings.Contains(line, fullSHA) could therefore NEVER match, so the
// boot-line leg of Watch was as unfalsifiable as the health leg was. Both legs
// of a two-leg proof could not pass, which would have made every real
// activation roll back and left the reason looking like "the bot did not come
// up" (CLASS 240 twice in one function).
//
// It scans the line's whitespace-separated tokens rather than substring-
// matching, so a hex-looking fragment inside some other value is not mistaken
// for the revision.
func lineNamesRevision(ln, sha string) bool {
	for _, tok := range strings.Fields(ln) {
		tok = strings.Trim(tok, ".,;:()[]")
		if revisionsAgree(tok, sha) {
			return true
		}
	}
	return false
}

// lineTime reads the timestamp a vl log line starts with: "MM-DD HH:MM:SS".
// The year is absent from the format, so it is taken from the current year —
// stated rather than hidden, because it is the one assumption here.
func lineTime(ln string) (time.Time, bool) {
	fields := strings.Fields(ln)
	if len(fields) < 2 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05",
		fmt.Sprintf("%d-%s %s", sys.Now().Year(), fields[0], fields[1]), time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// revisionsAgree compares a revision the machine REPORTED against the one we
// expect, allowing the reported value to be an abbreviation.
//
// FOUND ON THE LIVE BOX, not in a test: /api/health reports the SHORT sha
// ("662c79bd236f"), while a release names the full 40 hex. A strict equality
// check therefore could NEVER have matched in production — the health leg of
// Watch would have failed every real activation, which is CLASS 240: a
// verification step that cannot succeed as written, whose realistic fate is
// deletion by whoever hits it mid-incident. kernel/boot_integrity.go already
// had this right ("prefix match so a short SHA in deploy/RELEASE matches the
// full one"); this package did not.
//
// The comparison is deliberately one-directional and length-floored: the
// REPORTED value may abbreviate the EXPECTED one, never the reverse, and an
// abbreviation shorter than 7 characters is not evidence of anything.
func revisionsAgree(reported, expected string) bool {
	if reported == "" || expected == "" {
		return false
	}
	if reported == expected {
		return true
	}
	const minAbbrev = 7
	if len(reported) < minAbbrev || len(reported) >= len(expected) {
		return false
	}
	return strings.HasPrefix(expected, reported)
}

func healthSHA(url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("no health url")
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		return "", err
	}
	for _, k := range []string{"release", "rev", "sha", "revision", "version"} {
		if v, ok := payload[k].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("health payload names no revision")
}

// Rollback puts the PREVIOUS release back and hands the caller the identity to
// prove it with.
//
// "Restore the binary" is the version of this step that gets written, and it is
// the version that fails: the dist and the RELEASE marker are halves too. A
// rollback that restores only the binary leaves the UI serving the new bundle
// and the marker claiming the new sha — so the box lies about what it is
// running at exactly the moment someone is trying to find out.
//
// prev is a Release DIRECTORY (NOFX_RELEASE_DIR/<sha>/). Under that layout all
// three halves move together by repointing the `current` symlink, which is
// atomic and cannot leave a mixed install. The v6 script kept siblings named
// vl-bin.old.<sha>.<timestamp>, which could collide and could not carry the
// dist or the marker alongside the binary they belonged to.
//
// Rollback does NOT Watch: the caller persists the receipt, then watches, so a
// crash between the restart and the proof is recoverable. Use Watch(prev, ...)
// with the returned identity.
func Rollback(prev Release, id Identity) (Identity, Receipt, error) {
	rc := newReceipt("rollback")
	rc.Evidence["restore"] = prev.SHA
	rc.Evidence["kill_pid"] = fmt.Sprintf("%d", id.PID)

	current := filepath.Join(filepath.Dir(prev.Dir), "current")
	if err := atomicSymlink(prev.Dir, current); err != nil {
		return rc.failID(fmt.Errorf("repoint %s to %s: %w", current, prev.Dir, err))
	}
	// Evidence records what was ACTUALLY done, never a hopeful list: under the
	// release-dir layout one symlink moves all three halves at once.
	rc.Evidence["method"] = "current symlink repointed"
	rc.Evidence["current"] = current
	rc.Evidence["target"] = prev.Dir

	next, err := killAndAwait(rc, id)
	if err != nil {
		return rc.failID(err)
	}
	rc.Evidence["new_pid"] = fmt.Sprintf("%d", next.PID)
	r, _ := rc.done()
	return next, r, nil
}

// killAndAwait is the guarded restart shared by Activate, Rollback and
// RollbackTo: signal ONLY the process the Identity names, then wait for the
// unit to come back and read the NEW identity.
func killAndAwait(rc Receipt, id Identity) (Identity, error) {
	alive, err := id.stillAlive()
	if err != nil {
		return Identity{}, fmt.Errorf("cannot confirm the identity of pid %d: %w", id.PID, err)
	}
	if !alive {
		return Identity{}, fmt.Errorf(
			"pid %d is no longer the process this step measured (recycled or already gone); refusing to signal it", id.PID)
	}
	if err := sys.Kill(id.PID); err != nil {
		return Identity{}, fmt.Errorf("kill -9 %d: %w", id.PID, err)
	}
	return waitForNewIdentity(id, 90*time.Second)
}

// atomicSymlink points name at target without ever unlinking name first: a
// symlink created beside it and renamed over it means a reader never sees a
// moment with no `current` at all.
func atomicSymlink(target, name string) error {
	tmp := name + ".swapping"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, name); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RollbackTo restores prev's three halves INTO the live install paths and then
// restarts. It is the form the CLI and the worker use; Rollback above is kept
// for the in-place case where prev already sits at the install paths.
func RollbackTo(prev Release, install Release, id Identity) (Identity, Receipt, error) {
	rc := newReceipt("rollback")
	rc.Evidence["restore"] = prev.SHA
	rc.Evidence["kill_pid"] = fmt.Sprintf("%d", id.PID)
	if err := atomicCopy(prev.Binary, install.Binary); err != nil {
		return rc.failID(fmt.Errorf("restore binary: %w", err))
	}
	if err := atomicCopy(prev.ReleaseFile, install.ReleaseFile); err != nil {
		return rc.failID(fmt.Errorf("restore RELEASE: %w", err))
	}
	if err := atomicSwapDir(prev.Dist, install.Dist); err != nil {
		return rc.failID(fmt.Errorf("restore dist: %w", err))
	}
	rc.Evidence["restored"] = "binary,dist,RELEASE"

	next, err := killAndAwait(rc, id)
	if err != nil {
		return rc.failID(fmt.Errorf("ROLLBACK FAILED — %w", err))
	}
	rc.Evidence["new_pid"] = fmt.Sprintf("%d", next.PID)
	r, _ := rc.done()
	return next, r, nil
}

// Snapshot copies the THREE HALVES of a live install out to a directory, so a
// later RollbackTo has something to restore from.
//
// Without it the rollback story has a hole nobody notices until they need it:
// Activate overwrites the install in place, and once it has, the previous
// binary, bundle and marker exist only wherever someone happened to put them.
// Under NOFX_RELEASE_DIR the old release stays in its own directory and this
// is unnecessary; without it, this is the step that makes rollback possible at
// all. Taking it is cheap; discovering it was skipped is not.
//
// The receipt records what was actually copied. A half that was missing at the
// source is named in the error, not silently omitted from a success.
func Snapshot(install Release, dest string) (Receipt, error) {
	rc := newReceipt("snapshot")
	rc.Evidence["from"] = install.Dir
	rc.Evidence["dest"] = dest
	if dest == "" {
		return rc.fail(fmt.Errorf("snapshot needs a destination"))
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return rc.fail(fmt.Errorf("cannot create %s: %w", dest, err))
	}
	out := Release{
		Dir:         dest,
		SHA:         install.SHA,
		Binary:      filepath.Join(dest, filepath.Base(install.Binary)), // snapshot keeps the install's basename (vl-bin or nofx-bin — R5 removes the nofx name)
		Dist:        filepath.Join(dest, "web", "dist"),
		ReleaseFile: filepath.Join(dest, "RELEASE"),
	}
	if err := atomicCopy(install.Binary, out.Binary); err != nil {
		return rc.fail(fmt.Errorf("snapshot binary: %w", err))
	}
	if err := atomicCopy(install.ReleaseFile, out.ReleaseFile); err != nil {
		return rc.fail(fmt.Errorf("snapshot RELEASE: %w", err))
	}
	if err := copyTree(install.Dist, out.Dist); err != nil {
		return rc.fail(fmt.Errorf("snapshot dist: %w", err))
	}
	rc.Evidence["captured"] = "binary,dist,RELEASE"
	if b, err := os.ReadFile(out.ReleaseFile); err == nil {
		rc.Evidence["release_marker"] = strings.TrimSpace(string(b))
	}
	return rc.done()
}
