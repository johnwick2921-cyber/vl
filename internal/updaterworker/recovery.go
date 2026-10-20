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
	case addonProvenNew, addonUnknown:
		// Proven new, or never touched: nothing to restore.
	case addonRestored:
		// The previous DLL is restored AND (when the place restarted NT8) NT8
		// was proven restarted + reconnected on it: nothing to restore.
	case addonDiskRestored:
		p("%d. The previous AddOn DLL was restored to disk, but NinjaTrader was NOT proven restarted + reconnected on it (the place restarted NT8 and the rollback's restart/reconnect did not finish). Restart NinjaTrader, or verify it is running the previous build, and confirm the AddOn reconnects on the previous build. Do NOT clear the hold until that is proven.", step)
		addonHoldCond = "AND the AddOn is proven running on the previous build"
		step++
	case addonAmbiguous:
		p("%d. The AddOn placement is INCOMPLETE or UNPROVEN: the job reached addon_placed but did not finish it. Inspect the AddOns folder by hand — the new DLL may or may not be in place, and the previous DLL may or may not be backed up. Do NOT clear the hold until the AddOn state is proven.", step)
		addonHoldCond = "AND the AddOn state is proven"
		step++
	case addonPlaced:
		// addonPlaced co-occurs only with reachNothingInstalled: reaching
		// StateActivated requires addon_reconnected (which would classify as
		// addonProvenNew), and a rollback would classify as addonAmbiguous.
		act := "was NOT activated (it still runs the pre-update build)"
		dir, dirKnown := addonBackupDir(j)
		switch {
		case dir != "":
			p("%d. The AddOn was already placed before this failure: the NEW AddOn DLL is in the AddOns folder and the PREVIOUS one is backed up at %s. The bot binary %s, so leaving the new DLL would run mixed, unverified bot and AddOn versions — the demonstrated risk, not an established incompatibility. Restore the previous AddOn by FIRST moving the current (new) AddOn DLL aside (never overwrite or delete it — it is the evidence), then COPYING the backup back into place (never MOVE or delete the backup — it is the only copy of the previous DLL) BEFORE opening NinjaTrader, then verify the AddOn reconnects on the previous build. If you cannot determine the exact AddOns folder path, STOP and inspect it manually — do not guess. Do NOT clear the hold until that is proven.", step, dir, act)
			addonHoldCond = "AND the previous AddOn is restored + reconnected"
		case dirKnown:
			p("%d. The AddOn was already placed but NO previous DLL was recorded (the box had none to back up): there is nothing to restore, but the new DLL is unproven and the bot binary %s. Do NOT clear the hold until the AddOn reconnects and the boot is proven.", step, act)
			addonHoldCond = "AND the new AddOn's build is confirmed"
		default:
			p("%d. The AddOn was already placed but the previous-AddOn backup location is NOT recorded for this job (a deferred place records it later): inspect the AddOns folder and the vl-addon-backups folder by hand. The bot binary %s. Do NOT clear the hold until the AddOn state is proven.", step, act)
			addonHoldCond = "AND the AddOn state is proven"
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

// shellQuote single-quotes s for POSIX sh, escaping embedded apostrophes with
// the standard '...'\”...' idiom — so a path with spaces, quotes, or any
// other metacharacter is safe in the emitted commands.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// restoreFileLine emits ONE no-delete, no-clobber restore command for a single
// artifact (the binary, the RELEASE file, or the web dist directory). TS is
// captured ONCE so every guard and move name the SAME timestamped file. Every
// move uses `mv -n -T` (atomic no-clobber — a destination that exists at the
// move is left untouched, no check-to-move gap) followed by a verification that
// the SOURCE is gone: some no-clobber moves report success on a skip, so the
// source-absence check turns a silent skip into a refusal.
//
//  1. If a leftover <dst>.recovery.tmp exists — including a DANGLING symlink,
//     which [ -e ] alone would miss — move it aside to
//     <dst>.recovery.tmp.previous.$TS. A skip or failed preservation ABORTS the
//     whole command.
//  2. Copy the snapshot artifact to <dst>.recovery.tmp.
//  3. Move the live artifact aside to <dst>.failed.<jobID>.$TS (evidence,
//     never overwritten).
//  4. Move the snapshot copy into place — an existing <dst> aborts instead of
//     being overwritten.
//
// Every path is shell-quoted (spaces and apostrophes safe); only $TS expands.
func restoreFileLine(dst, src, jobID, cpFlag string) string {
	tmp := shellQuote(dst + ".recovery.tmp")
	prev := shellQuote(dst+".recovery.tmp.previous.") + `"$TS"`
	failed := shellQuote(dst+".failed."+jobID+".") + `"$TS"`
	live := shellQuote(dst)
	snap := shellQuote(src)
	return fmt.Sprintf(`TS="$(date +%%Y%%m%%dT%%H%%M%%S.%%N)"; { { [ ! -e %[1]s ] && [ ! -L %[1]s ]; } || { mv -n -T %[1]s %[2]s && [ ! -e %[1]s ] && [ ! -L %[1]s ]; }; } && %[6]s %[5]s %[1]s && { mv -n -T %[4]s %[3]s && [ ! -e %[4]s ] && [ ! -L %[4]s ]; } && { mv -n -T %[1]s %[4]s && [ ! -e %[1]s ] && [ ! -L %[1]s ]; }`,
		tmp, prev, failed, live, snap, cpFlag)
}

// crashLineCmd is the one no-clobber move that finishes a restore interrupted
// between the two mv's (the live artifact already moved aside, the snapshot
// copy waiting at <dst>.recovery.tmp). It refuses if <dst> reappears.
func crashLineCmd(dst string) string {
	live := shellQuote(dst)
	tmp := shellQuote(dst + ".recovery.tmp")
	return fmt.Sprintf("{ mv -n -T %[1]s %[2]s && [ ! -e %[1]s ] && [ ! -L %[1]s ]; }", tmp, live)
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
	addonUnknown      addonReach = iota // no addon_placed transition recorded
	addonAmbiguous                      // addon_placed STARTED but not finished (or restore unproven after a rollback)
	addonPlaced                         // addon_placed finished; not restored, not proven new
	addonDiskRestored                   // previous DLL restored to disk, NT8 reload NOT proven
	addonRestored                       // previous DLL restored AND NT8 reloaded + reconnected
	addonProvenNew                      // addon_reconnected reached (new build proven)
)

// recoveryAddon reads the job's AddOn history CHRONOLOGICALLY: the
// addon_placed / nt8_restarted / addon_reconnected transitions and the
// addon_restore receipt. A reconnect that happened BEFORE a rollback is STALE
// once the rollback runs: the rollback's restore (or its absence) is the
// latest word on which DLL is in place.
func recoveryAddon(j updaterjob.Job) addonReach {
	placedStarted, placedDone, reconnected := false, false, false
	rollingBack := false
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
		case updaterjob.StateRollingBack:
			rollingBack = true
		}
	}
	// The receipts are write-ordered, so the LAST receipt for each step is the
	// latest outcome — a later failed attempt invalidates an earlier success
	// (chronological, not an accumulated boolean). A FAILED or nil-evidence
	// receipt must still overwrite an earlier success, so it is NOT skipped.
	latest := map[string]bool{}
	for _, rc := range j.Receipts {
		ev := rc.Evidence
		switch rc.Step {
		case "addon_restore":
			latest["addon_restore"] = rc.OK && ev != nil && ev["outcome"] == "restored"
		case "nt8_rerestart":
			latest["nt8_rerestart"] = rc.OK && ev != nil && ev["outcome"] == "restarted"
		case "addon_rollback_reconnect":
			latest["addon_rollback_reconnect"] = rc.OK && ev != nil && ev["outcome"] == "reconnected"
		}
	}
	restored := latest["addon_restore"]
	rerestartOK := latest["nt8_rerestart"]
	reconnectOK := latest["addon_rollback_reconnect"]
	needsReload := j.Addon != nil && j.Addon.Restarted
	switch {
	case restored && (!needsReload || (rerestartOK && reconnectOK)):
		// The previous DLL is restored to disk AND (when the place restarted
		// NT8) the rollback proved NT8 restarted + reconnected on it.
		return addonRestored
	case restored && needsReload && !(rerestartOK && reconnectOK):
		// Disk restored, but the restart/reconnect pair did not finish: the
		// restored DLL may not be loaded. Distinct from full restoration.
		return addonDiskRestored
	case rollingBack && placedDone:
		// A rollback ran after the AddOn was placed but produced no proven
		// restore: the current DLL is UNPROVEN — a reconnect from before the
		// rollback is stale proof.
		return addonAmbiguous
	case reconnected:
		return addonProvenNew
	case placedDone:
		return addonPlaced
	case placedStarted:
		return addonAmbiguous
	}
	return addonUnknown
}

// addonBackupDir returns the previous-AddOn backup dir the job recorded, and
// whether that recording is COMPLETE. ("", true) = the place ran and there was
// no previous DLL to back up; ("", false) = the backup location is unknown
// (not recorded — e.g. a deferred place whose backup is recorded later, in the
// restart receipt, not in j.Addon.BackupDir).
func addonBackupDir(j updaterjob.Job) (string, bool) {
	if j.Addon != nil && j.Addon.BackupDir != "" {
		return j.Addon.BackupDir, true
	}
	for _, rc := range j.Receipts {
		if rc.Step == "addon_place" && rc.Evidence != nil {
			if b, ok := rc.Evidence["backup_dir"]; ok {
				return b, true // "" = no previous DLL existed
			}
		}
	}
	return "", false
}
