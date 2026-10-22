package updaterworker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// m246 (P0, the owner's #43 Update left NinjaTrader closed). Two bugs:
//   1. one transient tasklist/interop error aborted a bounded NT8 wait;
//   2. the revert path restored the old AddOn but never started NT8.

// isRunning must carry the tasklist stderr into its error so the receipt names
// the real failure (never a bare "exit status 1").
func TestNT8IsRunningCarriesStderr(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("tasklist error output: access denied\r\n"), errors.New("exit status 1")
	}
	r := newNT8Restart(run, "", nil)
	_, err := r.isRunning(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stderr: tasklist error output: access denied") {
		t.Fatalf("isRunning error must carry the stderr, got %v", err)
	}
}

// A couple of transient tasklist failures after Start-Process are retried and
// the restart completes. MUTANT: fail the wait on the first probe error (the
// old behavior) → the job ends recovery_needed → RED.
func TestNT8RestartRetriesTransientProbeError(t *testing.T) {
	r := newRig(t, withCSChanged())
	r.set(func() { r.startProbeErrorCount = 2 }) // two tasklist hiccups, then running
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s (error %q), want complete: two transient tasklist errors must be retried", j.State, j.Error)
	}
	if !r.box.nt8Running {
		t.Fatal("NT8 must be running after the transient errors were retried")
	}
}

// Once the updater STOPPED NinjaTrader (StoppedNT8 on the job), the revert must
// bring it back up — restoring the old AddOn alone leaves NT8 closed. MUTANT:
// drop the ensure in stepRevertClean → nt8Running stays false → RED.
func TestRevertCleanRestartsNT8AfterStop(t *testing.T) {
	r := newRig(t)
	r.hookAt("drained_acked/started", func() {
		if _, err := r.w.update(boxJobID, updaterjob.StateDrainedAcked, updaterjob.PhaseStarted, func(k *updaterjob.Job) error {
			k.Addon = &updaterjob.AddonState{StoppedNT8: true}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	// The app names another job → the drain never passes → reverted_clean.
	// NT8 starts CLOSED (the updater stopped it before the revert).
	r.set(func() { r.maintJob = "job-u4-9999abcd"; r.nt8Running = false })
	r.w.cfg.Budgets.Drain = 50 * time.Millisecond
	r.w.cfg.Budgets.Poll = 5 * time.Millisecond

	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("state=%s, want reverted_clean", j.State)
	}
	if !r.box.nt8Running {
		t.Fatal("the revert must restart NT8 after the updater stopped it (StoppedNT8)")
	}
	found := false
	for _, rc := range j.Receipts {
		if rc.Step == "nt8_restart_after_revert" {
			found = true
		}
	}
	if !found {
		t.Fatal("the revert must record the nt8_restart_after_revert receipt")
	}
}

// If the updater stopped NT8 and the revert CANNOT bring it back up, the job
// ends recovery_needed with the loud owner alert — never a silent reverted_clean
// that strands the owner on a closed NinjaTrader.
func TestRevertCleanNT8StartFailureIsRecoveryNeededWithAlert(t *testing.T) {
	r := newRig(t)
	r.hookAt("drained_acked/started", func() {
		if _, err := r.w.update(boxJobID, updaterjob.StateDrainedAcked, updaterjob.PhaseStarted, func(k *updaterjob.Job) error {
			k.Addon = &updaterjob.AddonState{StoppedNT8: true}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	r.set(func() {
		r.maintJob = "job-u4-9999abcd"
		r.nt8Running = false
		r.startFails = true // Start-Process errors during the revert's restart
	})
	r.w.cfg.Budgets.Drain = 50 * time.Millisecond
	r.w.cfg.Budgets.Poll = 5 * time.Millisecond

	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("state=%s, want recovery_needed: the revert could not restart NT8", j.State)
	}
	if !strings.Contains(j.RecoveryReason, "NT8 is CLOSED — open it") {
		t.Fatalf("reason %q must carry the loud owner alert", j.RecoveryReason)
	}
}
