package updaterworker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"vl/internal/updaterjob"
)

// W-ADDON-ONE-CLICK restart-evidence gap (release #40 follow-up): stepNT8Restart
// used to return success as soon as Start-Process returned without error, with
// no proof NinjaTrader was still alive. A launch-then-immediate-exit was
// indistinguishable from a healthy restart, so the step marked nt8_restarted
// "done" while NinjaTrader was down — the failure only surfaced five minutes
// later in stepAddonReconnect. These pins prove the step now verifies liveness,
// distinguishes "process started" from "AddOn ready", and routes a dead NT8 to
// rollback (never the false-clean reverted_clean that releases the hold).

// Start-Process succeeds but the process never comes up: the step must NOT
// report success. It ends recovery_needed with the honest "did not come up"
// reason and KEEPS the hold — never reverted_clean, which would release the
// hold and declare the old build "still running normally" while NinjaTrader is
// actually down.
func TestNT8StartVerificationLeavesNT8DownRollsBack(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startDoesNotBringUp = true })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded || !strings.Contains(j.Error, "did not come up") {
		t.Fatalf("job %s (error %q), want recovery_needed with a 'did not come up' error: NT8 never came up after start", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times before NT8 was proven up", r.callCount("activate"))
	}
	// The honest receipt records the failed liveness wait.
	found := false
	for _, rc := range j.Receipts {
		if rc.Step == "nt8_restart" {
			if ev := rc.Evidence; ev != nil && ev["nt8_start_wait"] == "timeout_not_running" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("the nt8_restart receipt must record nt8_start_wait=timeout_not_running")
	}
}

// Start-Process succeeds and NinjaTrader comes up after a few tasklist polls
// (delayed healthy start): the step waits for it and the install completes.
func TestNT8StartVerificationDelayedHealthyStartCompletes(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startDelayPolls = 3 })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete: NT8 came up after a short delay", j.State, j.Error)
	}
	if !r.box.nt8Running {
		t.Fatalf("NT8 must be running after the delayed healthy start")
	}
}

// The process probe (tasklist) errors after Start-Process: liveness is
// unproven, so the step must not report success. Same honest recovery_needed
// edge as the absent process — never a false-clean reverted_clean.
func TestNT8StartVerificationProbeErrorIsRecoveryNeeded(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startProbeError = true })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s (error %q), want recovery_needed: the process probe errored after start", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times before NT8 liveness was proven", r.callCount("activate"))
	}
}

// A canceled context mid-way through the post-start liveness wait (production
// path, via the rig's drive context) must abort the drive WITHOUT finishing the
// job: the started restart step stays on disk (a restart re-runs it) and the
// hold is KEPT — a cancellation must never read as reverted_clean or clear the
// hold.
func TestNT8StartVerificationCancelledContextAborts(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startDelayPolls = 1000 }) // NT8 "not visible" so the wait sleeps

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.box.driveCtx = ctx
	r.box.pollHook = func() {
		// Cancel only once we are INSIDE the post-start liveness wait (the
		// start set the delay countdown), not at any earlier sleep boundary.
		r.box.mu.Lock()
		delaying := r.box.startDelayRemaining > 0
		r.box.mu.Unlock()
		if delaying {
			cancel()
		}
	}
	r.install()
	if err := r.drive(); err == nil {
		t.Fatal("drive must abort on a cancelled context during the liveness wait")
	}
	r.noViolations(t)

	j := r.job()
	if updaterjob.Finished(j.State, j.Phase) {
		t.Fatalf("a cancelled restart finished the job %s/%s; it must stay started", j.State, j.Phase)
	}
	if j.State != updaterjob.StateNT8Restarted {
		t.Fatalf("job %s, want the interrupted restart step (nt8_restarted) still on disk", j.State)
	}
	if !r.hold().Present || !r.hold().Held {
		t.Fatalf("the hold must be KEPT after a cancelled restart: %+v", r.hold())
	}
}

// The start command itself errors (Start-Process fails, not launch-then-exit):
// the step must not report success. recovery_needed, hold kept.
func TestNT8StartVerificationStartCommandFailsIsRecoveryNeeded(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startFails = true })
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s (error %q), want recovery_needed: the start command failed", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times before NT8 was proven up", r.callCount("activate"))
	}
}

// The liveness probe errors during the exit-wait (after NT8 was stopped):
// liveness is unproven, so recovery_needed — never a false-clean revert.
func TestNT8StartVerificationExitWaitProbeErrorIsRecoveryNeeded(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() {
		r.nt8HangsOnClose = true // force-stop path
		r.nt8ExitLinger = 1      // the old process lingers one poll after the stop
		r.exitWaitProbeError = true
	})
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s (error %q), want recovery_needed: the exit-wait probe errored after the stop", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times before NT8 liveness was proven", r.callCount("activate"))
	}
}

// The migration-failure fallback start is ALSO liveness-verified: when the
// fallback Start-Process errors, the job is recovery_needed (not reverted_clean).
func TestNT8StartVerificationMigrationFallbackStartFailureIsRecoveryNeeded(t *testing.T) {
	r := newRig(t, withCSChanged())
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs"), "// the AddOn source\n")
	// The -Custom rebuild fails (type diff), so the worker falls back to a
	// restart on the old files.
	prev := r.box.fakeRunWin
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name, "powershell") && hasWinFlag(args, "-Custom") {
			return []byte("VL_CUSTOM_TYPES_DIFF\n"), errors.New("exit 1")
		}
		return prev(ctx, name, args...)
	}
	r.set(func() { r.startFails = true }) // the fallback Start-Process errors
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded || !strings.Contains(j.Error, "could not be restarted") {
		t.Fatalf("job %s (error %q), want recovery_needed: the migration fallback start failed", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under a failed migration fallback start", r.callCount("activate"))
	}
}

// The SPECIFIC migration-fallback regression: Start-Process SUCCEEDS but NT8
// never becomes running. The fallback must not report "restarted on the old
// files" (reverted_clean) on Start-Process success alone — liveness is proven
// first, so a launch-then-exit ends recovery_needed with the hold kept.
func TestNT8StartVerificationMigrationFallbackStartSuccessNotRunningIsRecoveryNeeded(t *testing.T) {
	r := newRig(t, withCSChanged())
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "NinjaTrader.Custom.dll"), "type "+addonTypeName+"\n")
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs"), "// the AddOn source\n")
	prev := r.box.fakeRunWin
	r.w.runWin = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name, "powershell") && hasWinFlag(args, "-Custom") {
			return []byte("VL_CUSTOM_TYPES_DIFF\n"), errors.New("exit 1")
		}
		return prev(ctx, name, args...)
	}
	r.set(func() { r.startDoesNotBringUp = true }) // Start-Process succeeds, NT8 never comes up
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded ||
		!strings.Contains(j.Error, "could not be restarted") ||
		!strings.Contains(j.Error, "did not come up") {
		t.Fatalf("job %s (error %q), want recovery_needed: the fallback start succeeded but NT8 never came up", j.State, j.Error)
	}
	if r.callCount("activate") != 0 {
		t.Fatalf("activate ran %d times under an unverified migration fallback start", r.callCount("activate"))
	}
	if !r.hold().Present || !r.hold().Held {
		t.Fatalf("the hold must be KEPT after an unverified fallback start: %+v", r.hold())
	}
}

// RecoveryText for a job that placed the AddOn but never activated the bot must
// NOT print the bare "nothing installed / do not restore anything" + hold-clear
// on old-Go-boot proof alone. It must name the backup, instruct restoring the
// previous AddOn (mixed versions as the risk, never asserted incompatibility),
// and gate the hold-clear on the AddOn being restored + reconnected.
func TestRecoveryTextAddonPlacedNotActivatedGatesHoldOnAddon(t *testing.T) {
	r := newRig(t, withCSChanged())
	// An OLD AddOn DLL on disk so addonbuild.Place records a non-empty backup dir.
	writeFile(r.t, filepath.Join(r.nt8docs, "bin", "Custom", "VL.AddOn.dll"), "old AddOn DLL\n")
	r.set(func() { r.startDoesNotBringUp = true })
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s (error %q), want recovery_needed", j.State, j.Error)
	}
	if j.Addon == nil || j.Addon.BackupDir == "" {
		t.Fatalf("the AddOn must be placed (backup dir set) for this regression")
	}
	text := RecoveryText(j, r.cfg.Target)
	for _, want := range []string{
		"The AddOn was already placed",
		"Restore the previous AddOn",
		"mixed, unverified",
		"not an established incompatibility",
		j.Addon.BackupDir,
		"AND the previous AddOn is restored",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("recovery text must contain %q:\n%s", want, text)
		}
	}
	// The bare "nothing installed / Do NOT restore anything" contradiction must
	// be GONE: the bot binary and the AddOn are named separately.
	for _, forbid := range []string{
		"Do NOT restore anything",
		"Nothing was installed:", // unqualified form
	} {
		if strings.Contains(text, forbid) {
			t.Fatalf("recovery text must not contain the unqualified %q:\n%s", forbid, text)
		}
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "clear this job's hold") && !strings.Contains(l, "AND the previous AddOn is restored") {
			t.Fatalf("an ungated hold-clear line exists while the AddOn is placed:\n%s", text)
		}
	}
}

// The no-previous-DLL case: the AddOn was placed but the box had NO old DLL to
// back up (BackupDir empty). That is NOT the same as "nothing to do" — the new
// DLL is still unproven. The text must name this explicitly and gate the
// hold-clear on the new AddOn's build, never on a backup that does not exist.
func TestRecoveryTextAddonPlacedNoPreviousDLLKeepsHold(t *testing.T) {
	r := newRig(t, withCSChanged())
	// NO old VL.AddOn.dll on disk → addonbuild.Place records an EMPTY backup dir.
	r.set(func() { r.startDoesNotBringUp = true })
	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("job %s (error %q), want recovery_needed", j.State, j.Error)
	}
	if j.Addon == nil || j.Addon.BackupDir != "" {
		t.Fatalf("the AddOn must be placed with an EMPTY backup dir (no previous DLL), got %+v", j.Addon)
	}
	text := RecoveryText(j, r.cfg.Target)
	for _, want := range []string{
		"NO previous DLL was recorded",
		"there is nothing to restore",
		"the new DLL is unproven",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("recovery text must contain %q:\n%s", want, text)
		}
	}
	// It must NOT name a backup to restore (there is none).
	if strings.Contains(text, "backed up at") {
		t.Fatalf("recovery text names a backup dir that does not exist:\n%s", text)
	}
	// The hold-clear must be gated on the new AddOn's build confirmation.
	gated := false
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "clear this job's hold") {
			gated = strings.Contains(l, "AND the new AddOn's build is confirmed")
		}
	}
	if !gated {
		t.Fatalf("the hold-clear is not gated on the new AddOn's build:\n%s", text)
	}
}
