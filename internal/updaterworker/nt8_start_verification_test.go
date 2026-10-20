package updaterworker

import (
	"context"
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

// A canceled context during the post-start liveness wait must abort the step
// (never hang, never silently succeed). The caller routes that error to
// recovery_needed.
func TestNT8StartVerificationCancelledContextAborts(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startDelayPolls = 100 }) // NT8 "not visible" so the wait sleeps
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := newNT8Restart(r.box.fakeRunWin, addonExe(), nil)
	if err := r.w.startAndVerifyNT8(ctx, rr, map[string]string{}); err == nil {
		t.Fatalf("startAndVerifyNT8 must return an error when the context is canceled mid-wait")
	}
}
