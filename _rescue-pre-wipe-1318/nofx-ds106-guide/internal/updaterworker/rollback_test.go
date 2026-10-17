package updaterworker

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// PIN (dispatch §4(4)): a Watch that never proves the new build rolls back to
// the job's SNAPSHOT of the install (RollbackTo(snapshot, install, id)), then
// watches the OLD sha with since = the persisted rollback kill instant; the
// job reads rolled_back, the install's halves are the old ones again, and
// the hold is cleared.
func TestWatchTimeoutRollsBackToTheSnapshotAndReadsRolledBack(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRolledBack || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("job %s/%s (error %q), want rolled_back/done\n%v", j.State, j.Phase, j.Error, states(j))
	}
	steps := receiptSteps(j)
	tail := strings.Join(steps[len(steps)-7:], ",")
	if tail != "activate,watch(fail),rollback,watch,boot_verify,release_hold,main_tree_lock" {
		t.Fatalf("receipts end %s\nwant activate,watch(fail),rollback,watch,boot_verify,release_hold,main_tree_lock", tail)
	}
	if len(r.rollbackArg) != 1 {
		t.Fatalf("rollback ran %d times", len(r.rollbackArg))
	}
	prev, inst := r.rollbackArg[0][0], r.rollbackArg[0][1]
	wantSnap := filepath.Join(r.backupRoot, boxJobID, "install")
	if prev != *j.Snapshot || prev.Dir != wantSnap || prev.SHA != boxOld || prev.ReleaseFile != filepath.Join(wantSnap, "RELEASE") {
		t.Fatalf("RollbackTo restored from %+v, want the job's snapshot at %s (103 layout, sha %s)", prev, wantSnap, boxOld)
	}
	if inst != *j.Install || inst.Dir != r.inst {
		t.Fatalf("RollbackTo restored INTO %+v, want the install %s", inst, r.inst)
	}
	if got := r.rollbackIDs[0]; got != *j.IdentityRollback {
		t.Fatalf("RollbackTo killed %v, want the persisted identity_rollback %v", got, *j.IdentityRollback)
	}
	if len(r.watchSHAs) != 2 || r.watchSHAs[0] != boxNew || r.watchSHAs[1] != boxOld {
		t.Fatalf("Watch ran on %v, want [new, OLD]", r.watchSHAs)
	}
	if got := r.watchOpts[1]; !got.Since.Equal(*j.RollbackWatchSince) || got.LogPath != j.RollbackLogPath {
		t.Fatalf("the rollback's Watch since=%v log=%s, want the persisted kill instant %v and %s", got.Since, got.LogPath, *j.RollbackWatchSince, j.RollbackLogPath)
	}
	if sha, _ := parseBinaryBody(filepath.Join(r.inst, "nofx-bin")); sha != boxOld {
		t.Fatalf("the install binary is %s after the rollback, want %s", sha, boxOld)
	}
	if b, _ := os.ReadFile(filepath.Join(r.inst, "web", "dist", "index.html")); string(b) != "<html>old</html>\n" {
		t.Fatalf("the install dist is %q after the rollback", b)
	}
	if r.hold().Present {
		t.Fatalf("hold survives rolled_back: %+v", r.hold())
	}
}

// PIN (dispatch §4(5)): a rollback that fails is recovery_needed — the worker
// STOPS (no step runs again, install is refused until restart), the job names
// its last GOOD receipt, the hold is KEPT, and the recovery text is this
// job's steps: kill by MainPID only, never pgrep / journalctl / the
// version-control tool, the hold cleared last.
func TestRollbackFailureIsRecoveryNeededAndStops(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	r.rollbackFail = true
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s (error %q), want recovery_needed\n%v", j.State, j.Error, states(j))
	}
	if j.LastGoodReceipt == nil || !j.Receipts[*j.LastGoodReceipt].OK {
		t.Fatalf("last_good_receipt %v does not name an OK receipt (%v)", j.LastGoodReceipt, receiptSteps(j))
	}
	if got := j.Receipts[*j.LastGoodReceipt].Step; got != "activate" {
		t.Fatalf("last good receipt is %q, want activate (the last step that succeeded)", got)
	}
	if !strings.Contains(j.RecoveryReason, "rollback failed") {
		t.Fatalf("recovery_reason %q", j.RecoveryReason)
	}
	if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldOurs {
		t.Fatalf("the hold is %s after a failed rollback — it must be KEPT", s)
	}
	calls := len(r.calls)
	if err := r.drive(); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != calls {
		t.Fatalf("the worker kept going after recovery_needed: %v", r.calls[calls:])
	}
	if resp := r.w.Handle(updaterwireInstallOf("job-u4-0003abcd")); resp.OK || resp.Error != "recovery needed" {
		t.Fatalf("install after recovery_needed = %+v", resp)
	}
	if resp := r.w.Handle(updaterwireStatusOf("")); resp.State != StatusStopped {
		t.Fatalf("status = %+v", resp)
	}
	text := RecoveryText(j, r.cfg.Target)
	for _, bad := range []*regexp.Regexp{regexp.MustCompile(`pgrep`), regexp.MustCompile(`journalctl`), regexp.MustCompile(`\bgit\b`)} {
		if bad.MatchString(text) {
			t.Fatalf("recovery text uses %s:\n%s", bad, text)
		}
	}
	for _, want := range []string{`systemctl show -p MainPID --value nofx`, j.Snapshot.Binary, "BOOT INTEGRITY OK — rev " + boxOld[:12],
		"maintenance-hold --install-dir " + r.inst + " clear --job " + boxJobID, j.BackupPath} {
		if !strings.Contains(text, want) {
			t.Fatalf("recovery text lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "clear --job") < strings.Index(text, "kill -9") {
		t.Fatalf("the hold is not cleared LAST:\n%s", text)
	}
	t.Logf("recovery text:\n%s", text)
}

// PIN (verifier D3): the recovery text's restart never becomes "kill -9 0"
// (MainPID is 0 for a unit that is not running — exactly when recovery is
// needed — and kill -9 0 signals the operator's whole process group). The
// restart line is RUN here under sh with a stub systemctl and a kill
// function that only records: MainPID 0 and an empty MainPID kill nothing;
// a live MainPID is killed exactly once, by pid.
//
// U4 re-verify note 6: running the REAL line must never be able to signal a
// real process, even if the line drifts. So the "live" MainPID is 4194305 —
// above Linux's pid_max ceiling (4194304), a pid no process can hold — a
// recording stub `kill` EXECUTABLE sits first in PATH (it catches an
// `env kill` / `exec kill` drift the shell function cannot), and the line
// may name neither `command kill` (bypasses the function for the builtin)
// nor `/bin/kill` / `/usr/bin/kill` (bypasses both).
func TestRecoveryRestartNeverKillsTheProcessGroup(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	r.rollbackFail = true
	j := r.runToEnd(t)
	text := RecoveryText(j, r.cfg.Target)
	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "systemctl show -p MainPID --value nofx") {
			line = strings.TrimSpace(l)
		}
	}
	if line == "" {
		t.Fatalf("no restart line:\n%s", text)
	}
	for _, bypass := range []string{"command kill", "/bin/kill", "/usr/bin/kill", "env kill", "exec kill"} {
		if strings.Contains(line, bypass) {
			t.Fatalf("the restart line names %q, which bypasses the recording kill in this test:\n%s", bypass, line)
		}
	}
	const impossiblePID = "4194305" // > pid_max's ceiling (4194304): no process can hold it
	for _, tc := range []struct{ mainPID, want string }{{"0", ""}, {"", ""}, {"1", ""}, {impossiblePID, "KILL -9 " + impossiblePID}} {
		stub := t.TempDir()
		for name, body := range map[string]string{
			"systemctl": "#!/bin/sh\necho '" + tc.mainPID + "'\n",
			"kill":      "#!/bin/sh\necho \"KILL-EXE $*\"\n", // records; never signals
		} {
			writeFile(t, filepath.Join(stub, name), body)
			if err := os.Chmod(filepath.Join(stub, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("/bin/sh", "-c", `kill() { echo "KILL $*"; }; `+line)
		cmd.Env = []string{"PATH=" + stub + ":/usr/bin:/bin"}
		out, _ := cmd.CombinedOutput()
		var kills []string
		for _, l := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(l, "KILL-EXE") {
				t.Fatalf("MainPID %q: the restart line ran a kill EXECUTABLE from PATH (%q), not the shell's kill\nline: %s", tc.mainPID, l, line)
			}
			if strings.HasPrefix(l, "KILL") {
				kills = append(kills, l)
			}
		}
		got := strings.Join(kills, ";")
		if got != tc.want {
			t.Fatalf("MainPID %q: the restart line ran %q, want %q\nline: %s\noutput: %s", tc.mainPID, got, tc.want, line, out)
		}
	}
}

// PIN (#206 review fold, P1 recovery.go:96): a job swept to recovery_needed
// BEFORE the hold step has no hold on disk and never read the install. Its
// recovery text must not claim the hold is kept (entries are NOT refused),
// must not print a kill -9 whose condition is vacuous ("health does not serve
// n/a" is always true), and must not print a boot proof of a build the job
// never read (a proof of "n/a" can never pass).
func TestRecoveryTextPreHoldJobClaimsNothing(t *testing.T) {
	r := newRig(t)
	r.w.crash = func(q string) {
		if q == "downloaded/started" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("the job never reached downloaded/started")
	}
	r.clock.Advance(31 * time.Minute)
	r.w = r.newWorker()
	if _, err := r.w.sweep(); err != nil {
		t.Fatal(err)
	}
	j := r.job()
	if j.State != updaterjob.StateRecoveryNeeded || j.Install != nil {
		t.Fatalf("job %s (install %+v): want recovery_needed with no install read", j.State, j.Install)
	}
	if s, _ := ReadHoldFor(r.data, boxJobID); s != HoldAbsent {
		t.Fatalf("hold %s: a pre-hold job has no hold on disk", s)
	}
	text := RecoveryText(j, r.cfg.Target)
	for _, want := range []string{
		"entries are NOT refused",
		"No bot restart and no boot proof",
		"There is no hold to clear",
		"Do NOT restore anything",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the text lacks %q:\n%s", want, text)
		}
	}
	for _, forbid := range []string{
		"The installation hold is KEPT",
		"kill -9",
		"MainPID",
		"BOOT INTEGRITY OK — rev",
		"revision must be",
		"clear --job", // there is nothing of this job's to clear
	} {
		if strings.Contains(text, forbid) {
			t.Fatalf("the text claims %q the job never proved:\n%s", forbid, text)
		}
	}
}

// PIN (verifier D3, low): the steps follow what the job PROVED. A job that
// never reached the activate installed nothing; one whose release was proven
// (boot_verified done) must never be rolled back from the snapshot; only an
// unproven activate restores it. Each case is a real job file (a stale crash
// swept to recovery_needed at start, or a failed rollback).
func TestRecoveryTextFollowsWhatTheJobProved(t *testing.T) {
	staleAt := func(t *testing.T, point string) (*rig, updaterjob.Job) {
		r := newRig(t)
		r.w.crash = func(q string) {
			if q == point {
				panic(crashPanic{q})
			}
		}
		if !r.runCrashing(t) {
			t.Fatalf("no crash at %s", point)
		}
		r.clock.Advance(31 * time.Minute)
		r.w = r.newWorker()
		if _, err := r.w.sweep(); err != nil {
			t.Fatal(err)
		}
		j := r.job()
		if j.State != updaterjob.StateRecoveryNeeded {
			t.Fatalf("stale job at %s is %s", point, j.State)
		}
		return r, j
	}
	restore := func(j updaterjob.Job) string { return "cp -p " + j.Snapshot.Binary }
	for _, tc := range []struct {
		name        string
		build       func(t *testing.T) (*rig, updaterjob.Job)
		wantRestore bool
		prove       string
		say         string
	}{
		{"never activated", func(t *testing.T) (*rig, updaterjob.Job) { return staleAt(t, "nt8_skipped/done") }, false, boxOld, "Nothing was installed"},
		{"release proven", func(t *testing.T) (*rig, updaterjob.Job) { return staleAt(t, "boot_verified/done") }, false, boxNew, "Do NOT restore the snapshot"},
		{"release proven, complete started", func(t *testing.T) (*rig, updaterjob.Job) { return staleAt(t, "complete/started") }, false, boxNew, "Do NOT restore the snapshot"},
		{"activate unproven", func(t *testing.T) (*rig, updaterjob.Job) { return staleAt(t, "activated/effect") }, true, boxOld, "Restore the pre-update install"},
		{"rollback failed", func(t *testing.T) (*rig, updaterjob.Job) {
			r := newRig(t)
			r.watchFail[boxNew] = true
			r.rollbackFail = true
			return r, r.runToEnd(t)
		}, true, boxOld, "Restore the pre-update install"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, j := tc.build(t)
			if j.Snapshot == nil {
				t.Fatalf("no snapshot recorded (history %v)", states(j))
			}
			text := RecoveryText(j, r.cfg.Target)
			if got := strings.Contains(text, restore(j)); got != tc.wantRestore {
				t.Fatalf("restore-from-snapshot in the text = %v, want %v (history %v):\n%s", got, tc.wantRestore, states(j), text)
			}
			if !strings.Contains(text, "BOOT INTEGRITY OK — rev "+tc.prove[:12]) || !strings.Contains(text, "revision must be "+tc.prove[:12]) {
				t.Fatalf("the text does not prove %s:\n%s", tc.prove[:12], text)
			}
			if !strings.Contains(text, tc.say) {
				t.Fatalf("the text lacks %q:\n%s", tc.say, text)
			}
			if strings.Index(text, "clear --job") < strings.Index(text, "MainPID") {
				t.Fatalf("the hold is not cleared LAST:\n%s", text)
			}
		})
	}
}

// PIN (verifier D6): rolling_back entered by a failure edge runs at once as
// its FIRST attempt — never counted as a crash retry. So a rollback that
// crashes inside its effect twice still gets its third run (the attempts cap
// is about runs of the step, and RollbackTo is called exactly once per run),
// and the job reads rolled_back.
func TestAFailureEdgeIntoRollingBackIsItsFirstAttempt(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	crashes := 0
	r.w.crash = func(q string) {
		if q == "rolling_back/effect" && crashes < updaterjob.MaxAttempts-1 {
			crashes++
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("no crash in the rollback")
	}
	for crashes < updaterjob.MaxAttempts-1 {
		w := r.newWorker()
		w.crash = r.w.crash
		r.w = w
		if _, err := r.w.sweep(); err != nil {
			t.Fatal(err)
		}
		r.runCrashing(t)
	}
	j := r.restart(t)
	r.noViolations(t)
	want := make([]int, updaterjob.MaxAttempts)
	for i := range want {
		want[i] = i + 1
	}
	if fmt.Sprint(r.rollbackAtt) != fmt.Sprint(want) {
		t.Fatalf("RollbackTo ran at attempts %v, want %v (the failure edge is attempt 1)", r.rollbackAtt, want)
	}
	if j.State != updaterjob.StateRolledBack {
		t.Fatalf("job %s (reason %q), want rolled_back after %d crashed runs", j.State, j.RecoveryReason, crashes)
	}
}

// PIN (U4 re-verify note 7): the recovery text's restore is safe to run
// AGAIN. The failed dist is moved aside to a UNIQUE name each run (never
// deleted: it is the evidence of what failed), so a repeat run can never
// `mv` the live dist INTO the first run's leftover. The three restore lines
// are RUN here twice under sh, on the rig's temp install and snapshot only.
func TestRecoveryRestoreIsSafeToRunTwice(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	r.rollbackFail = true
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded || j.Install == nil || j.Snapshot == nil {
		t.Fatalf("fixture: job %s", j.State)
	}
	in, snap := *j.Install, *j.Snapshot
	restore := restoreLines(t, j, r.cfg.Target)
	relFiles := func(dir string) map[string]string { return relFiles(t, dir) }
	snapFiles := relFiles(snap.Dist)
	if len(snapFiles) == 0 {
		t.Fatal("fixture: the snapshot dist is empty")
	}
	for run := 1; run <= 2; run++ {
		// what the failed release left in the live dist this time
		writeFile(t, filepath.Join(in.Dist, "failed-run.txt"), fmt.Sprintf("run %d\n", run))
		cmd := exec.Command("/bin/sh", "-c", strings.Join(restore, "\n"))
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("restore run %d: %v\n%s", run, err, out)
		}
		if got := relFiles(in.Dist); !maps.Equal(got, snapFiles) {
			t.Fatalf("run %d: the live dist is %v, want the snapshot's %v", run, got, snapFiles)
		}
	}
	// both failed dists kept, side by side, neither inside the other
	parent, base := filepath.Dir(in.Dist), filepath.Base(in.Dist)
	ents, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), base+".failed."+j.JobID) {
			failed = append(failed, e.Name())
			if _, err := os.Lstat(filepath.Join(parent, e.Name(), base)); err == nil {
				t.Fatalf("a repeat run moved the live dist INTO %s", e.Name())
			}
		}
	}
	if len(failed) != 2 {
		t.Fatalf("failed dists kept = %v, want one per run (2)", failed)
	}
	seen := map[string]bool{}
	for _, f := range failed {
		b, err := os.ReadFile(filepath.Join(parent, f, "failed-run.txt"))
		if err != nil {
			t.Fatalf("%s lost its evidence: %v", f, err)
		}
		seen[string(b)] = true
	}
	if !seen["run 1\n"] || !seen["run 2\n"] {
		t.Fatalf("the evidence of each run is not kept: %v", seen)
	}
}

// restoreLines is the recovery text's three restore lines (binary, RELEASE,
// dist) for a recovery_needed job with a snapshot — and a refusal to hand
// them to a shell unless EVERY path they write or read is strictly below the
// temp dir (compared by path elements, never a string prefix: U4F defect 5).
func restoreLines(t *testing.T, j updaterjob.Job, tg Target) []string {
	t.Helper()
	if j.Install == nil || j.Snapshot == nil {
		t.Fatalf("fixture: job %s has no install/snapshot", j.State)
	}
	in, snap := *j.Install, *j.Snapshot
	tmp := filepath.Clean(os.TempDir())
	for _, p := range []string{in.Dist, in.Binary, in.ReleaseFile, snap.Dist, snap.Binary, snap.ReleaseFile} {
		if p == "" || filepath.Clean(p) == tmp {
			t.Fatalf("refusing to run the restore on %q: not strictly under the temp dir %s", p, tmp)
		}
		if in, err := PathWithin(p, tmp); err != nil || !in {
			t.Fatalf("refusing to run the restore on %q: not strictly under the temp dir %s (%v)", p, tmp, err)
		}
	}
	var restore []string
	for _, l := range strings.Split(RecoveryText(j, tg), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "cp -p ") || strings.HasPrefix(l, "rm -rf ") {
			restore = append(restore, l)
		}
	}
	if len(restore) != 3 {
		t.Fatalf("want the 3 restore lines (binary, RELEASE, dist), got %q", restore)
	}
	return restore
}

// relFiles is dir's files as relative path -> content.
func relFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for p, b := range allBytes(t, dir) {
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(b)
	}
	return out
}

// runRestore runs lines under sh with PATH=<extra>:/usr/bin:/bin.
func runRestore(lines []string, extraPath string) ([]byte, error) {
	path := "/usr/bin:/bin"
	if extraPath != "" {
		path = extraPath + ":" + path
	}
	cmd := exec.Command("/bin/sh", "-c", strings.Join(lines, "\n"))
	cmd.Env = []string{"PATH=" + path}
	return cmd.CombinedOutput()
}

// PIN (U4F defect 5): `mv -T` is the SECOND guard of the dist restore. With
// the failed-dist name forced to collide (a `date` that always prints the same
// stamp, first in PATH) and that name already a non-empty directory, the
// restore REFUSES — the live dist is neither moved INTO the leftover nor
// replaced, and the leftover keeps only its own evidence. Without -T, mv would
// nest the live dist inside it.
func TestRecoveryRestoreRefusesAnExistingFailedDistRatherThanNest(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	r.rollbackFail = true
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("fixture: job %s", j.State)
	}
	restore := restoreLines(t, j, r.cfg.Target)
	in := *j.Install
	const stamp = "20260924T000000.000000000"
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "date"), "#!/bin/sh\necho "+stamp+"\n")
	if err := os.Chmod(filepath.Join(bin, "date"), 0o755); err != nil {
		t.Fatal(err)
	}
	leftover := in.Dist + ".failed." + j.JobID + "." + stamp
	writeFile(t, filepath.Join(leftover, "evidence.txt"), "an earlier failed dist\n")
	writeFile(t, filepath.Join(in.Dist, "failed-run.txt"), "the live failed dist\n")
	live := relFiles(t, in.Dist)
	if out, err := runRestore(restore, bin); err == nil {
		t.Fatalf("the restore succeeded over an existing failed dist %s:\n%s", leftover, out)
	}
	if _, err := os.Lstat(filepath.Join(leftover, filepath.Base(in.Dist))); err == nil {
		t.Fatalf("the restore moved the live dist INTO %s", leftover)
	}
	if got := relFiles(t, leftover); !maps.Equal(got, map[string]string{"evidence.txt": "an earlier failed dist\n"}) {
		t.Fatalf("the leftover %s is now %v, want only its own evidence", leftover, got)
	}
	if got := relFiles(t, in.Dist); !maps.Equal(got, live) {
		t.Fatalf("the live dist changed on a refused restore: %v, want %v", got, live)
	}
}

// PIN (U4F defect 5): a crash BETWEEN the dist restore's two mv's (the live
// dist already moved aside, the snapshot copy not yet moved in) leaves no live
// dist. Re-running the full restore then stops at its first `mv -T` (nothing
// to move) — safe, but it cannot finish — so the recovery text names the one
// command that does: if the dist is absent, run only the second mv. Both are
// RUN here on the rig's temp paths. The crash itself is played with the
// PRODUCTION dist restore line cut before its last " && " (U4F verify note 6:
// never a copy of it), and the part cut off must BE the crash line — so a
// change to the chain's order or names moves the test with it.
func TestRecoveryRestoreFinishesAfterACrashBetweenTheTwoMoves(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	r.rollbackFail = true
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("fixture: job %s", j.State)
	}
	restore := restoreLines(t, j, r.cfg.Target)
	in, snap := *j.Install, *j.Snapshot
	snapFiles := relFiles(t, snap.Dist)
	only := crashLine(j, r.cfg.Target)
	if want := "mv -T " + in.Dist + ".recovery.tmp " + in.Dist; only != want {
		t.Fatalf("the recovery text has no crash-between-the-moves line (got %q, want %q):\n%s", only, want, RecoveryText(j, r.cfg.Target))
	}
	// the production dist line, cut before its last move: what ran before the crash
	distLine := restore[2]
	k := strings.LastIndex(distLine, " && ")
	if !strings.HasPrefix(distLine, "rm -rf "+in.Dist+".recovery.tmp ") || k < 0 {
		t.Fatalf("restore line 3 is not the dist restore: %q", distLine)
	}
	preCrash, lastMove := distLine[:k], distLine[k+len(" && "):]
	if lastMove != only {
		t.Fatalf("the dist restore's last move %q is not the crash-between-the-moves line %q", lastMove, only)
	}
	// play the crash: everything before the last move ran, the last move did not
	writeFile(t, filepath.Join(in.Dist, "failed-run.txt"), "the live failed dist\n")
	if out, err := runRestore([]string{preCrash}, ""); err != nil {
		t.Fatalf("playing the crash (%q): %v\n%s", preCrash, err, out)
	}
	aside, err := filepath.Glob(in.Dist + ".failed." + j.JobID + ".*")
	if err != nil || len(aside) != 1 {
		t.Fatalf("the crash left failed dists %v (%v), want exactly one", aside, err)
	}
	// a full re-run stops at its first mv -T and creates no dist
	if out, err := runRestore(restore, ""); err == nil {
		t.Fatalf("a full re-run with the dist absent succeeded:\n%s", out)
	}
	if _, err := os.Lstat(in.Dist); err == nil {
		t.Fatal("a failed re-run left something at the live dist path")
	}
	// the named line finishes it
	if out, err := runRestore([]string{only}, ""); err != nil {
		t.Fatalf("the crash-between-the-moves line: %v\n%s", err, out)
	}
	if got := relFiles(t, in.Dist); !maps.Equal(got, snapFiles) {
		t.Fatalf("after the named line the live dist is %v, want the snapshot's %v", got, snapFiles)
	}
	if b, err := os.ReadFile(filepath.Join(aside[0], "failed-run.txt")); err != nil || string(b) != "the live failed dist\n" {
		t.Fatalf("the failed dist's evidence was lost: %q %v", b, err)
	}
}

// crashLine is the recovery text's "if <dist> is ABSENT … run only: <cmd>"
// command, exactly as printed ("" when the text has none).
func crashLine(j updaterjob.Job, tg Target) string {
	in := *j.Install
	for _, l := range strings.Split(RecoveryText(j, tg), "\n") {
		if i := strings.Index(l, "run only: "); i >= 0 && strings.Contains(l, in.Dist+" is ABSENT") {
			return strings.TrimSpace(l[i+len("run only: "):])
		}
	}
	return ""
}

// PIN (U4F verify note 1): `mv -T` guards the dist restore's SECOND move too.
// Something re-creates <dist> (non-empty) between the two moves — played by an
// `mv` first in PATH that runs the real mv and, right after the move that
// takes <dist> aside, makes <dist> again. The production restore line then
// REFUSES: the snapshot copy is not moved INTO the re-created dist (plain mv
// would nest it there and report success), the intruder is untouched, and the
// copy waits at <dist>.recovery.tmp. The crash-between-the-moves line is the
// same move, so it refuses the same way while <dist> exists and finishes the
// restore once the operator has moved the intruder away.
func TestRecoveryRestoreRefusesADistRecreatedBetweenTheTwoMoves(t *testing.T) {
	r := newRig(t)
	r.watchFail[boxNew] = true
	r.rollbackFail = true
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("fixture: job %s", j.State)
	}
	restore := restoreLines(t, j, r.cfg.Target)
	in, snap := *j.Install, *j.Snapshot
	snapFiles := relFiles(t, snap.Dist)
	only := crashLine(j, r.cfg.Target)
	if only == "" {
		t.Fatal("the recovery text has no crash-between-the-moves line")
	}
	bin := t.TempDir()
	mvShim := "#!/bin/sh\n/usr/bin/mv \"$@\" || exit $?\nprev=; last=\nfor a in \"$@\"; do prev=$last; last=$a; done\n" +
		"if [ \"$prev\" = '" + in.Dist + "' ]; then mkdir '" + in.Dist + "' && echo intruder > '" + in.Dist + "/intruder.txt'; fi\n"
	writeFile(t, filepath.Join(bin, "mv"), mvShim)
	if err := os.Chmod(filepath.Join(bin, "mv"), 0o755); err != nil {
		t.Fatal(err)
	}
	intruder := map[string]string{"intruder.txt": "intruder\n"}
	nested := filepath.Join(in.Dist, filepath.Base(in.Dist)+".recovery.tmp")
	if out, err := runRestore(restore, bin); err == nil {
		t.Fatalf("the restore's second move succeeded over a re-created dist (nested at %s: %v):\n%s", nested, fileThere(nested), out)
	}
	if fileThere(nested) {
		t.Fatalf("the restore moved the snapshot copy INTO the re-created dist (%s)", nested)
	}
	if got := relFiles(t, in.Dist); !maps.Equal(got, intruder) {
		t.Fatalf("the re-created dist is now %v, want only the intruder %v", got, intruder)
	}
	if got := relFiles(t, in.Dist+".recovery.tmp"); !maps.Equal(got, snapFiles) {
		t.Fatalf("the snapshot copy is %v, want it waiting at %s.recovery.tmp as %v", got, in.Dist, snapFiles)
	}
	// the crash line's move is the same move: it refuses while <dist> exists …
	if out, err := runRestore([]string{only}, ""); err == nil {
		t.Fatalf("the crash-between-the-moves line succeeded over a re-created dist (nested: %v):\n%s", fileThere(nested), out)
	}
	if fileThere(nested) {
		t.Fatalf("the crash-between-the-moves line moved the snapshot copy INTO the re-created dist (%s)", nested)
	}
	// … and finishes the restore once the intruder is moved away
	if err := os.Rename(in.Dist, in.Dist+".intruder"); err != nil {
		t.Fatal(err)
	}
	if out, err := runRestore([]string{only}, ""); err != nil {
		t.Fatalf("the crash-between-the-moves line: %v\n%s", err, out)
	}
	if got := relFiles(t, in.Dist); !maps.Equal(got, snapFiles) {
		t.Fatalf("after the crash line the live dist is %v, want the snapshot's %v", got, snapFiles)
	}
}

func fileThere(p string) bool { _, err := os.Lstat(p); return err == nil }
