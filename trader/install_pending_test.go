package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
	"vl/kernel"
	"vl/store"
)

// ── release #22 OPS-SAFETY item 2 — freeze NEW entries during preflight ─────
//
// Verified at base from the live job file + log (read-only): job 30a8bbf6 was
// preflight_ok/started 01:39:53→01:49:53 CT; trader 12345 placed armed S1 at
// 01:44:00 (vl_2026-10-13.log 64591), four minutes into the preflight, and the
// job was refused on that order's ledger_exposure.

// writeJob writes a job file the way the updater does (tmp + rename, 0700 dirs).
func writeJob(t *testing.T, dataDir, id string, state updaterjob.State, phase updaterjob.Phase, created time.Time) {
	t.Helper()
	jobs, err := updaterjob.JobsDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"schema": 1, "job_id": id, "state": state, "phase": phase, "created_at": created.UTC()})
	tmp := filepath.Join(jobs, ".job-test.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(jobs, id+".json")); err != nil {
		t.Fatal(err)
	}
	resetInstallPendingForTest() // the 2 s cache must not hide the edit from the next read
}

func runDecisionGate(at *AutoTrader, action, prefix string) (rec *store.DecisionAction, err error, pastGate bool) {
	rec = &store.DecisionAction{Action: action, Symbol: "MNQ"}
	defer func() {
		if r := recover(); r != nil {
			pastGate = true
			err = fmt.Errorf("panicked past the gate: %v", r)
		}
	}()
	err = at.executeDecisionWithRecord(&kernel.Decision{Action: action, Symbol: "MNQ"}, rec)
	return rec, err, !strings.HasPrefix(rec.Error, prefix)
}

const pendingJob = "30a8bbf65c1f7185f9585844b2dbb039"

// TestEntryFrozenWhileAnUpdateJobIsInPreflight — THE named RED for the mutant
// "drop the freeze": a job in preflight_ok/started refuses NEW entries at the
// production call site, names the job, and counts install_pending.
func TestEntryFrozenWhileAnUpdateJobIsInPreflight(t *testing.T) {
	dir := withMaintenanceDir(t)
	t.Cleanup(resetInstallPendingForTest)
	writeJob(t, dir, pendingJob, updaterjob.StatePreflightOK, updaterjob.PhaseStarted, time.Now().Add(-4*time.Minute))
	at, _ := pauseTrader(t)
	at.id = "install-pending-1"
	for _, action := range []string{"open_long", "open_short"} {
		before := gateBlocks(at.id, "install_pending")
		rec, err, past := runDecisionGate(at, action, "install_pending:")
		if past {
			t.Fatalf("%s went past the install-pending freeze while job %s is in preflight (err=%v rec.Error=%q)", action, pendingJob, err, rec.Error)
		}
		if err != nil || rec.Success || !strings.Contains(rec.Error, pendingJob) {
			t.Fatalf("%s: the refusal must name the job; err=%v rec=%+v", action, err, rec)
		}
		if gateBlocks(at.id, "install_pending") != before+1 {
			t.Fatalf("%s: install_pending gate block not counted", action)
		}
	}
	// The arm path (planner + mentor rows) asks the same chain.
	if refusal, refused := at.admitEntry(admitIntent{Path: admitArm, Symbol: "MNQ", Action: "open_short", Now: time.Now(), Key: "arm-1"}); !refused || !strings.HasPrefix(refusal, "install_pending:") {
		t.Fatalf("an arm placement must be frozen too, got refused=%v %q", refused, refusal)
	}
	// Visible on GET /api/maintenance.
	if v := MaintenanceStatus(nil).InstallPending; !v.Pending || v.JobID != pendingJob || v.State != "preflight_ok" {
		t.Fatalf("/api/maintenance must show the freeze, got %+v", v)
	}
}

// Exits are never frozen.
func TestInstallPendingNeverFreezesACLose(t *testing.T) {
	dir := withMaintenanceDir(t)
	t.Cleanup(resetInstallPendingForTest)
	writeJob(t, dir, pendingJob, updaterjob.StatePreflightOK, updaterjob.PhaseStarted, time.Now())
	at, _ := pauseTrader(t)
	at.id = "install-pending-2"
	if rec, _, past := runDecisionGate(at, "close_long", "install_pending:"); !past {
		t.Fatalf("a close was refused by the install-pending freeze (rec.Error=%q)", rec.Error)
	}
}

// The freeze auto-clears: on the job's refusal (or any state past the pre-hold
// set), and on the updater's own 30-minute timeout for a job that never ends.
func TestInstallPendingClearsOnRefusalAndOnTimeout(t *testing.T) {
	dir := withMaintenanceDir(t)
	t.Cleanup(resetInstallPendingForTest)
	at, _ := pauseTrader(t)
	at.id = "install-pending-3"
	writeJob(t, dir, pendingJob, updaterjob.StatePreflightOK, updaterjob.PhaseStarted, time.Now().Add(-9*time.Minute))
	if _, pending := InstallPending(time.Now()); !pending {
		t.Fatal("precondition: preflight is pending")
	}
	writeJob(t, dir, pendingJob, updaterjob.StateRefused, updaterjob.PhaseDone, time.Now().Add(-10*time.Minute))
	if _, _, past := runDecisionGate(at, "open_long", "install_pending:"); !past {
		t.Fatal("a refused job must unfreeze entries")
	}
	writeJob(t, dir, pendingJob, updaterjob.StatePreflightOK, updaterjob.PhaseStarted, time.Now().Add(-updaterjob.MaxJobAge-time.Minute))
	if why, pending := InstallPending(time.Now()); pending {
		t.Fatalf("a job past the updater's 30-minute rule must not freeze entries forever, got %q", why)
	}
	// A job that reached the hold is the hold's business, not this gate's.
	writeJob(t, dir, pendingJob, updaterjob.StateMaintenanceHeld, updaterjob.PhaseStarted, time.Now())
	if _, pending := InstallPending(time.Now()); pending {
		t.Fatal("maintenance_held is past the pre-hold states: the hold refuses, not this gate")
	}
}

// An unreadable job file is skipped (and counted), never a freeze on garbage.
func TestInstallPendingSkipsAnUnreadableJobFile(t *testing.T) {
	dir := withMaintenanceDir(t)
	t.Cleanup(resetInstallPendingForTest)
	jobs, _ := updaterjob.JobsDir(dir)
	_ = os.MkdirAll(jobs, 0o700)
	if err := os.WriteFile(filepath.Join(jobs, "deadbeefdeadbeefdeadbeefdeadbeef.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetInstallPendingForTest()
	v := InstallPendingStatus(time.Now())
	if v.Pending || v.Unread != 1 {
		t.Fatalf("an unreadable file must be skipped and counted, got %+v", v)
	}
}
