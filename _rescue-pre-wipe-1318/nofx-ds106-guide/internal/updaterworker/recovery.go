package updaterworker

import (
	"fmt"
	"strings"

	"vl/internal/updaterjob"
)

// ── recovery_needed: the job-specific manual steps (C16 as ruled) ──────────
//
// deploy/RESTORE.md kills with pgrep, proves boots with journalctl and
// rebuilds from the source tree; none of that is safe here. These steps name
// THIS job's snapshot, backup and shas, restart by the unit's MainPID only
// (kill -9: SIGTERM exits 0 and systemd's Restart=on-failure would not
// relaunch), prove the boot from the data log + /api/health, and clear the
// hold LAST — until then every entry stays refused. RESTORE.md is cited only
// for its database-restore section. TestRollbackFailureIsRecoveryNeededAndStops
// pins the text free of pgrep, journalctl and the version-control tool.

// RecoveryText renders the manual steps for a recovery_needed job.
func RecoveryText(j updaterjob.Job, t Target) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	p("job %s — release %s — state %s", j.JobID, j.ReleaseID, j.State)
	p("reason: %s", orNA(j.RecoveryReason))
	if j.LastGoodReceipt != nil && *j.LastGoodReceipt < len(j.Receipts) {
		r := j.Receipts[*j.LastGoodReceipt]
		p("last good receipt: #%d %s (ended %s)", *j.LastGoodReceipt, r.Step, r.EndedAt.UTC().Format("2006-01-02T15:04:05Z"))
	} else {
		p("last good receipt: n/a")
	}
	if j.State != updaterjob.StateRecoveryNeeded {
		p("this job is not recovery_needed; nothing to do.")
		return b.String()
	}
	// The hold line is what is ON DISK, not a claim (#206 review fold): a
	// pre-hold recovery_needed job has NO hold — entries are NOT refused —
	// and the old text said the hold was KEPT unconditionally.
	hold := HoldFileForDisplay(t.DataDir)
	switch s, st := ReadHoldFor(t.DataDir, j.JobID); s {
	case HoldOurs:
		p("")
		p("The installation hold is KEPT (%s): every new entry stays refused until the last step.", hold)
	case HoldAbsent:
		p("")
		p("No installation hold is on disk: entries are NOT refused. The job stopped before the hold step — recover before trading.")
	default:
		p("")
		p("The hold on disk is NOT this job's (%s): do not clear it from this job's steps — recover the hold by hand first.", holdSummary(s, st))
	}
	oldSHA, newSHA := "n/a", "n/a"
	if j.Install != nil {
		oldSHA = j.Install.SHA
	}
	if j.Release != nil {
		newSHA = j.Release.SHA
	}
	p("Pre-update build: %s   release being installed: %s", oldSHA, newSHA)
	p("")
	// What is running decides the steps (verifier D3): a job that never
	// reached the activate installed nothing; a proven rollback or a proven
	// release must NOT be rolled back from the snapshot. Only an activate
	// that was never proven either way restores the snapshot.
	reach := recoveryReach(j)
	prove, restart := oldSHA, true
	step := 1
	switch reach {
	case reachNothingInstalled:
		if oldSHA == "n/a" {
			// The job never read the install: it can name no build, so no
			// restart of it and no boot proof of it can be printed — a
			// kill whose condition is "health does not serve n/a" is
			// always true and proves nothing (#206 review fold).
			p("%d. Nothing was installed: the job stopped in %s before it read the install, so no build is on record for this job. The bot keeps whatever build it booted; this job proves nothing about it. Do NOT restore anything.", step, j.State)
		} else {
			p("%d. Nothing was installed: the job stopped before the activate, so the pre-update build %s still runs. Do NOT restore anything.", step, oldSHA)
		}
		restart = false
	case reachRolledBack:
		p("%d. The rollback to the pre-update build %s was PROVEN (rolled_back reached). Do NOT restore anything.", step, oldSHA)
		restart = false
	case reachReleaseProven:
		p("%d. The release %s was PROVEN running (boot_verified finished). Do NOT restore the snapshot: that would roll back a proven release.", step, newSHA)
		prove, restart = newSHA, false
	default:
		if j.Snapshot != nil && j.Install != nil {
			s, in := *j.Snapshot, *j.Install
			p("%d. Restore the pre-update install from this job's snapshot (copy to a temp name, then mv -f over the live one):", step)
			p("     cp -p %s %s.recovery.tmp && mv -f %s.recovery.tmp %s", s.Binary, in.Binary, in.Binary, in.Binary)
			p("     cp -p %s %s.recovery.tmp && mv -f %s.recovery.tmp %s", s.ReleaseFile, in.ReleaseFile, in.ReleaseFile, in.ReleaseFile)
			// The failed dist is moved aside under a UNIQUE name (job id +
			// a nanosecond timestamp), never deleted — it is the evidence —
			// and mv -T refuses to move it INTO an existing directory, so a
			// repeat run can never nest the live dist in an earlier leftover
			// (U4 re-verify note 7, TestRecoveryRestoreIsSafeToRunTwice). The
			// SECOND mv's -T is the same guard for a <dist> re-created between
			// the two moves: it refuses rather than nest the snapshot copy in
			// it (U4F verify note 1,
			// TestRecoveryRestoreRefusesADistRecreatedBetweenTheTwoMoves).
			p("     rm -rf %s.recovery.tmp && cp -a %s %s.recovery.tmp && mv -T %s %s.failed.%s.$(date +%%Y%%m%%dT%%H%%M%%S.%%N) && mv -T %s.recovery.tmp %s",
				in.Dist, s.Dist, in.Dist, in.Dist, in.Dist, j.JobID, in.Dist, in.Dist)
			// A crash BETWEEN the two mv's leaves no live dist; a re-run then
			// stops at its first mv -T (safe, but it cannot finish), so name
			// the one command that does (U4F defect 5,
			// TestRecoveryRestoreFinishesAfterACrashBetweenTheTwoMoves).
			p("     If %s is ABSENT (a crash between the two mv's above), run only: mv -T %s.recovery.tmp %s", in.Dist, in.Dist, in.Dist)
		} else {
			p("%d. The activate ran but this job recorded no snapshot of the install: the install cannot be restored from it — stop and restore it by hand.", step)
		}
	}
	step++
	short := prove
	if len(short) > 12 {
		short = short[:12]
	}
	if prove == "n/a" {
		// No build can be named, so no restart of the bot and no boot proof
		// can be printed: a kill whose condition is "health does not serve
		// n/a" is always met, and "revision must be n/a" can never pass.
		p("%d. No bot restart and no boot proof: this job never read the install, so there is nothing it can name to restart or prove.", step)
	} else {
		if restart {
			p("%d. Restart the bot by its identity, never by name (systemd's Restart=on-failure relaunches it):", step)
		} else {
			p("%d. ONLY if health (step %d) does not serve %s: restart the bot by its identity, never by name:", step, step+1, short)
		}
		// MainPID is 0 for a unit that is not running, and kill -9 0 signals
		// the operator's whole process group: never kill a pid below 2.
		p("     %s", restartLine)
		step++
		p("%d. Prove the boot: the data log must show the OK line for %s, and health must serve it:", step, short)
		p(`     grep -a "BOOT INTEGRITY OK — rev %s ·" %s/{vl,nofx}_$(date +%%F).log`, short, t.LogDir) // R5 removes the vl log
		p("     curl -s http://127.0.0.1:%d/api/health    (revision must be %s)", t.Port, short)
	}
	if j.BackupPath != "" {
		p("   If the database itself must be restored, use this job's backup %s with deploy/RESTORE.md's database-restore section only.", j.BackupPath)
	}
	step++
	if s, _ := ReadHoldFor(t.DataDir, j.JobID); s == HoldAbsent {
		p("%d. There is no hold to clear (the job stopped before the hold step).", step)
	} else {
		p("%d. LAST, once the boot is proven, clear this job's hold:", step)
		p("     maintenance-hold --install-dir %s clear --job %s", t.InstallDir, j.JobID)
	}
	p("")
	p("Then restart vl-updater (the restart is the acknowledgement; install stays refused until then).")
	return b.String()
}

// restartLine restarts the unit by its MainPID and refuses a pid below 2 (a
// stopped unit reports 0; kill -9 0 would signal the whole process group).
// The unit is vl, falling back to nofx while the rename is in flight (R5
// removes the fallback).
const restartLine = `pid=$(systemctl show -p MainPID --value vl); if [ "$pid" -le 1 ] 2>/dev/null; then pid=$(systemctl show -p MainPID --value nofx); fi; if [ "$pid" -gt 1 ] 2>/dev/null; then kill -9 "$pid"; else echo "vl is not running (MainPID '$pid'); start it: sudo systemctl start vl"; fi` // R5 removes the vl unit

type reach int

const (
	reachUnproven         reach = iota // the activate ran; neither build is proven
	reachNothingInstalled              // the job never entered activated
	reachRolledBack                    // rolled_back was entered: the old build is proven
	reachReleaseProven                 // boot_verified finished, no rollback began
)

// recoveryReach reads the job's HISTORY (never its current state alone: a
// recovery_needed job's state is recovery_needed).
func recoveryReach(j updaterjob.Job) reach {
	activated, rollingBack, rolledBack, proven := false, false, false, false
	for _, tr := range j.Transitions {
		switch {
		case tr.State == updaterjob.StateActivated:
			activated = true
		case tr.State == updaterjob.StateRollingBack:
			rollingBack = true
		case tr.State == updaterjob.StateRolledBack:
			rolledBack = true
		case tr.State == updaterjob.StateBootVerified && tr.Phase == updaterjob.PhaseDone,
			tr.State == updaterjob.StateComplete:
			proven = true
		}
	}
	switch {
	case !activated:
		return reachNothingInstalled
	case rolledBack:
		return reachRolledBack
	case proven && !rollingBack:
		return reachReleaseProven
	}
	return reachUnproven
}
