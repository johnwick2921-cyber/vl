package updaterworker

import (
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// W-ADDON-ONE-CLICK self-recovery (DS-102). These pins exercise the PRODUCTION
// call sites: a pre-activation drain failure runs stepRevertClean, and the
// transitional wait is the pollPlanner loop the steps all use.

// A pre-activation failure ends reverted_clean: the hold is released, the
// main-tree lock is released, and an AddOn the place step already moved is
// restored from the job's AddonBackupDir. Mutant: drop the AddonRestore call
// (or release the wrong dir) → RED.
func TestRevertCleanReleasesHoldLockAndRestoresAddon(t *testing.T) {
	r := newRig(t)
	backup := "/mnt/c/Users/u/Documents/NinjaTrader 8/vl-addon-backups/" + boxJobID
	r.hookAt("drained_acked/started", func() {
		// DS-104's addon_place step would record this; here we set it directly
		// so the revert has something to restore.
		if _, err := r.w.update(boxJobID, updaterjob.StateDrainedAcked, updaterjob.PhaseStarted, func(k *updaterjob.Job) error {
			k.AddonBackupDir = backup
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	// The app names another job → the drain never passes → reverted_clean.
	r.set(func() { r.maintJob = "job-u4-9999abcd" })
	r.w.cfg.Budgets.Drain = 50 * time.Millisecond
	r.w.cfg.Budgets.Poll = 5 * time.Millisecond

	j := r.runToEnd(t)
	r.noViolations(t)

	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("state=%s, want reverted_clean", j.State)
	}
	if !strings.Contains(j.Error, "not held for this job") {
		t.Fatalf("error %q must keep the reason nothing changed", j.Error)
	}
	if r.hold().Present {
		t.Fatal("reverted_clean must release the hold")
	}
	if r.lockHolder != "" {
		t.Fatalf("reverted_clean must release the main-tree lock (still %q)", r.lockHolder)
	}
	if len(r.addonRestoreCalls) != 1 || r.addonRestoreCalls[0] != backup {
		t.Fatalf("addon restore calls = %v, want one call with %q", r.addonRestoreCalls, backup)
	}
}

// If the revert itself cannot restore the AddOn, the job ends recovery_needed —
// the one case a human must see. Mutant: swallow the AddonRestore error (treat
// it as success) → RED.
func TestRevertCleanAddonRestoreFailureIsRecoveryNeeded(t *testing.T) {
	r := newRig(t)
	r.hookAt("drained_acked/started", func() {
		if _, err := r.w.update(boxJobID, updaterjob.StateDrainedAcked, updaterjob.PhaseStarted, func(k *updaterjob.Job) error {
			k.AddonBackupDir = "/backup/" + boxJobID
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	r.set(func() { r.maintJob = "job-u4-9999abcd"; r.addonRestoreErr = errAddonRestore })
	r.w.cfg.Budgets.Drain = 50 * time.Millisecond
	r.w.cfg.Budgets.Poll = 5 * time.Millisecond

	j := r.runToEnd(t)
	if j.State != updaterjob.StateRecoveryNeeded || !strings.Contains(j.RecoveryReason, "AddOn could not be restored") {
		t.Fatalf("state=%s (reason %q), want recovery_needed: AddOn restore failed", j.State, j.RecoveryReason)
	}
}

var errAddonRestore = &addonRestoreErr{}

type addonRestoreErr struct{}

func (*addonRestoreErr) Error() string { return "addon restore: the backup is gone" }

// Wait-don't-refuse: while the blocker is a transitional connection or a stale
// census, pollPlanner re-checks for the Transitional budget (never less than
// the step's own) before giving up. Mutant: drop the transitional extension
// (always use the step budget) → RED.
func TestPollPlannerWaitsOutATransitionalBlocker(t *testing.T) {
	r := newRig(t)
	r.install()
	j := r.job()
	r.w.cfg.Budgets.Poll = 5 * time.Millisecond
	r.w.cfg.Budgets.Transitional = 150 * time.Millisecond

	start := r.w.host.Now()
	err := r.w.pollPlanner(t.Context(), j, 20*time.Millisecond, 0, func() (string, error) {
		return "addon_ack: the AddOn ack is 20 s old", nil
	})
	if err == nil || !strings.Contains(err.Error(), "not passed within 150ms") {
		t.Fatalf("err=%v, want a transitional timeout naming the Transitional budget", err)
	}
	if waited := r.w.host.Now().Sub(start); waited < 100*time.Millisecond {
		t.Fatalf("gave up after %s, want the full transitional wait", waited)
	}
}
