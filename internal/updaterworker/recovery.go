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
			p("%d. Nothing was installed for the BOT BINARY: the job stopped in %s before it read the install, so no build is on record for this job. The bot keeps whatever build it booted; this job proves nothing about it. Do NOT restore the bot binary.", step, j.State)
		} else {
			p("%d. Nothing was installed for the BOT BINARY: the job stopped before the activate, so the pre-update build %s still runs. Do NOT restore the bot binary.", step, oldSHA)
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
			p("%d. Restore the pre-update install from this job's snapshot (the failed artifact is moved aside as evidence, never overwritten in place; each command aborts if a leftover cannot be preserved):", step)
			p("     %s", restoreFileLine(in.Binary, s.Binary, j.JobID, "cp -p"))
			p("     %s", restoreFileLine(in.ReleaseFile, s.ReleaseFile, j.JobID, "cp -p"))
			p("     %s", restoreFileLine(in.Dist, s.Dist, j.JobID, "cp -a"))
			// A crash BETWEEN the two mv's leaves no live artifact; a re-run then
			// stops at its first mv -T (safe, but it cannot finish), so name the
			// one command that does (U4F defect 5,
			// TestRecoveryRestoreFinishesAfterACrashBetweenTheTwoMoves).
			p("     If %s is ABSENT (a crash between the two mv's above), run only: %s", in.Binary, crashLineCmd(in.Binary))
			p("     If %s is ABSENT (a crash between the two mv's above), run only: %s", in.ReleaseFile, crashLineCmd(in.ReleaseFile))
			p("     If %s is ABSENT (a crash between the two mv's above), run only: %s", in.Dist, crashLineCmd(in.Dist))
		} else {
			p("%d. The activate ran but this job recorded no snapshot of the install: the install cannot be restored from it — stop and restore it by hand.", step)
		}
	}
	step++
	// The AddOn state comes from the job's RECORDED transitions and receipts,
	// never from j.Addon.BackupDir alone: that field is empty both when the box
	// had no previous DLL AND when the place step did not run (or was deferred).
	// A PROVEN rollback already restored the old AddOn (addon_restore receipt),
	// and a PROVEN release already reconnected the new one (addon_reconnected).
	// Only a placed-but-unproven (or ambiguous) AddOn needs an operator step,
	// and it always keeps the hold until the AddOn's state is proven.
	addon := recoveryAddon(j)
	addonHoldCond := "" // the extra condition the hold-clear must name ("" = none)
	switch addon {
	case addonProvenNew, addonRestored, addonUnknown:
		// Proven new, proven restored, or never touched: nothing to restore.
	case addonAmbiguous:
		p("%d. The AddOn placement is INCOMPLETE or UNPROVEN: the job reached addon_placed but did not finish it. Inspect the AddOns folder by hand — the new DLL may or may not be in place, and the previous DLL may or may not be backed up. Do NOT clear the hold until the AddOn state is proven.", step)
		addonHoldCond = "AND the AddOn state is proven"
		step++
	case addonPlaced:
		act := "was NOT activated (it still runs the pre-update build)"
		if reach == reachUnproven {
			act = "activation is UNPROVEN (it may run either build)"
		}
		if j.Addon != nil && j.Addon.BackupDir != "" {
			p("%d. The AddOn was already placed before this failure: the NEW AddOn DLL is in the AddOns folder and the PREVIOUS one is backed up at %s. The bot binary %s, so leaving the new DLL would run mixed, unverified bot and AddOn versions — the demonstrated risk, not an established incompatibility. Restore the previous AddOn by FIRST moving the current (new) AddOn DLL aside (never overwrite or delete it — it is the evidence), then COPYING the backup back into place (never MOVE or delete the backup — it is the only copy of the previous DLL) BEFORE opening NinjaTrader, then verify the AddOn reconnects on the previous build. Do NOT clear the hold until that is proven.", step, j.Addon.BackupDir, act)
			addonHoldCond = "AND the previous AddOn is restored + reconnected"
		} else {
			p("%d. The AddOn was already placed but NO previous DLL was recorded (the box had none to back up): there is nothing to restore, but the new DLL is unproven and the bot binary %s. Do NOT clear the hold until the AddOn reconnects and the boot is proven.", step, act)
			addonHoldCond = "AND the new AddOn's build is confirmed"
		}
		step++
	}
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
		p(`     grep -a "BOOT INTEGRITY OK — rev %s ·" %s/vl_$(TZ=America/Chicago date +%%F).log`, short, t.LogDir)
		p("     curl -s http://127.0.0.1:%d/api/health    (revision must be %s)", t.Port, short)
	}
	if j.BackupPath != "" {
		p("   If the database itself must be restored, use this job's backup %s with deploy/RESTORE.md's database-restore section only.", j.BackupPath)
	}
	step++
	if s, _ := ReadHoldFor(t.DataDir, j.JobID); s == HoldAbsent {
		p("%d. There is no hold to clear (the job stopped before the hold step).", step)
	} else if addonHoldCond != "" {
		p("%d. LAST, once the boot is proven %s, clear this job's hold:", step, addonHoldCond)
		p("     maintenance-hold --install-dir %s clear --job %s", t.InstallDir, j.JobID)
	} else {
		p("%d. LAST, once the boot is proven, clear this job's hold:", step)
		p("     maintenance-hold --install-dir %s clear --job %s", t.InstallDir, j.JobID)
	}
	p("")
	p("Then restart vl-updater (the restart is the acknowledgement; install stays refused until then).")
	return b.String()
}

// restoreFileLine emits ONE no-delete, no-clobber restore command for a single
// artifact (the binary, the RELEASE file, or the web dist directory). TS is
// captured ONCE so every guard and move name the SAME timestamped file. In
// order:
//
//  1. If a leftover <dst>.recovery.tmp exists — including a DANGLING symlink,
//     which [ -e ] alone would miss — move it aside to
//     <dst>.recovery.tmp.previous.$TS. A failed preservation (or a
//     .previous.$TS that already exists) ABORTS the whole command.
//  2. Copy the snapshot artifact to <dst>.recovery.tmp.
//  3. Move the live artifact aside to <dst>.failed.<jobID>.$TS (evidence,
//     never overwritten — an existing .failed.$TS aborts instead).
//  4. Move the snapshot copy into place — an existing <dst> (a recreated
//     file, directory, or symlink) aborts instead of being overwritten.
//
// Every path is single-quoted (spaces safe); only $TS is expanded.
func restoreFileLine(dst, src, jobID, cpFlag string) string {
	return fmt.Sprintf(`TS="$(date +%%Y%%m%%dT%%H%%M%%S.%%N)"; { { [ ! -e '%[1]s.recovery.tmp' ] && [ ! -L '%[1]s.recovery.tmp' ]; } || { [ ! -e '%[1]s.recovery.tmp.previous.'"$TS" ] && [ ! -L '%[1]s.recovery.tmp.previous.'"$TS" ] && mv -T '%[1]s.recovery.tmp' '%[1]s.recovery.tmp.previous.'"$TS"; }; } && %[4]s '%[2]s' '%[1]s.recovery.tmp' && { [ ! -e '%[1]s.failed.%[3]s.'"$TS" ] && [ ! -L '%[1]s.failed.%[3]s.'"$TS" ] && mv -T '%[1]s' '%[1]s.failed.%[3]s.'"$TS"; } && { [ ! -e '%[1]s' ] && [ ! -L '%[1]s' ] && mv -T '%[1]s.recovery.tmp' '%[1]s'; }`,
		dst, src, jobID, cpFlag)
}

// crashLineCmd is the one no-clobber move that finishes a restore interrupted
// between the two mv's (the live artifact already moved aside, the snapshot
// copy waiting at <dst>.recovery.tmp). It refuses if <dst> reappears.
func crashLineCmd(dst string) string {
	return fmt.Sprintf("{ [ ! -e '%[1]s' ] && [ ! -L '%[1]s' ] && mv -T '%[1]s.recovery.tmp' '%[1]s'; }", dst)
}

// restartLine restarts the unit by its MainPID and refuses a pid below 2 (a
// stopped unit reports 0; kill -9 0 would signal the whole process group).
// The unit is vl (R5 removed the old vl fallback).
const restartLine = `pid=$(systemctl show -p MainPID --value vl); if [ "$pid" -gt 1 ] 2>/dev/null; then kill -9 "$pid"; else echo "vl is not running (MainPID '$pid'); start it: sudo systemctl start vl"; fi`

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

// addonReach classifies what the job PROVED about the AddOn, from its recorded
// transitions and receipts — never from j.Addon.BackupDir alone, which is empty
// both when the box had no previous DLL AND when the place step never ran (or
// was deferred to the restart step).
type addonReach int

const (
	addonUnknown   addonReach = iota // no addon_placed transition recorded
	addonAmbiguous                   // addon_placed STARTED but not finished
	addonPlaced                      // addon_placed finished; not restored, not proven new
	addonRestored                    // an addon_restore receipt (outcome=restored)
	addonProvenNew                   // addon_reconnected reached (new build proven)
)

// recoveryAddon reads the job's AddOn history: the addon_placed / nt8_restarted
// / addon_reconnected transitions and the addon_restore receipt.
func recoveryAddon(j updaterjob.Job) addonReach {
	placedStarted, placedDone, reconnected := false, false, false
	for _, tr := range j.Transitions {
		switch tr.State {
		case updaterjob.StateAddonPlaced:
			if tr.Phase == updaterjob.PhaseDone {
				placedDone = true
			} else {
				placedStarted = true
			}
		case updaterjob.StateNT8Restarted:
			placedDone = true
		case updaterjob.StateAddonReconnected:
			placedDone = true
			if tr.Phase == updaterjob.PhaseDone {
				reconnected = true
			}
		}
	}
	restored := false
	for _, rc := range j.Receipts {
		if rc.Step == "addon_restore" && rc.OK && rc.Evidence != nil && rc.Evidence["outcome"] == "restored" {
			restored = true
		}
	}
	switch {
	case reconnected:
		return addonProvenNew
	case restored:
		return addonRestored
	case placedDone:
		return addonPlaced
	case placedStarted:
		return addonAmbiguous
	}
	return addonUnknown
}
