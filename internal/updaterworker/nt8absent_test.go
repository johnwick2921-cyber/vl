package updaterworker

// UPDATER-NT8-CLOSED — the nt8_absent drain path, at the production call
// sites: the REAL runner, job file, hold file and loopback app (the harness).
// NT8 closed = the safest moment to update. The drain then passes on the
// gate's nt8_absent verdict (every ledger leg on its own evidence + the SIM
// predicate), records the path and each leg's evidence, and the job file says
// "nt8_absent (link down since <ts>)". A reconnect revokes the verdict —
// the normal ack/census legs apply again from that moment, never
// grandfathered.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/updaterjob"
	"vl/store"
)

const absentDown = 90 * time.Second

func withNT8Down(d time.Duration) rigOpt {
	return func(b *box) {
		b.addonDownSince = d
		b.addonConnected = false
	}
}

// atDrained drives to drained_acked/done (held + drained).
func atDrained(t *testing.T, opts ...rigOpt) (*rig, updaterjob.Job) {
	t.Helper()
	r := newRig(t, opts...)
	r.w.crash = func(q string) {
		if q == "drained_acked/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("never reached drained_acked/done")
	}
	r.w.crash = nil
	j := r.job()
	if j.State != updaterjob.StateDrainedAcked || j.Phase != updaterjob.PhaseDone {
		t.Fatalf("at %s/%s", j.State, j.Phase)
	}
	return r, j
}

func mustReceipt(t *testing.T, j updaterjob.Job, step string) Receipt {
	t.Helper()
	for _, rc := range j.Receipts {
		if rc.Step == step {
			return rc
		}
	}
	t.Fatalf("no %q receipt", step)
	return Receipt{}
}

func TestDrainNt8AbsentPassesAndRecordsPathAndLegs(t *testing.T) {
	r, j := atDrained(t, withNT8Down(absentDown))
	if j.DrainPath != drainPathNT8Absent {
		t.Fatalf("drain_path=%q", j.DrainPath)
	}
	if j.LinkDownSince == "" {
		t.Fatal("link_down_since not recorded")
	}
	rc := mustReceipt(t, j, "drain")
	if !rc.OK {
		t.Fatalf("drain receipt failed: %s", rc.Err)
	}
	if rc.Evidence["drain_path"] != drainPathNT8Absent {
		t.Fatalf("receipt drain_path=%q", rc.Evidence["drain_path"])
	}
	for _, leg := range []string{"hold", "go_drained", "in_flight_sends", "queued_signals", "planner_in_flight", "ledger_exposure", "sim_accounts"} {
		if v := rc.Evidence["leg_"+leg]; !strings.HasPrefix(v, "PASS: ") {
			t.Fatalf("leg_%s evidence=%q — the receipt records each leg's evidence", leg, v)
		}
	}
	r.noViolations(t)
}

func TestDrainNt8AbsentLinkDownBelowWindowRefuses(t *testing.T) {
	r := newRig(t, withNT8Down(30*time.Second))
	if r.runCrashing(t) {
		t.Fatal("drain passed with the link down only 30 s")
	}
	j := r.job()
	// the absent verdict is not eligible → the normal path applies → the ack
	// blocker names the real reason and the drain ends reverted_clean (self-
	// recovery: the hold is released, the lock is released, nothing installed)
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("state=%s, want reverted_clean", j.State)
	}
	if !strings.Contains(j.Error, "addon_ack") {
		t.Fatalf("error %q must name the missing ack", j.Error)
	}
}

func TestDrainNt8AbsentNonSIMAccountRefuses(t *testing.T) {
	// P-D ruling item 3: with the absent verdict eligible, PREFLIGHT runs the
	// absent legs — the refusal is pre-hold (stronger: no hold ever lands).
	r := newRig(t, withNT8Down(absentDown), func(b *box) { b.absentSim = false })
	if r.runCrashing(t) {
		t.Fatal("the job passed although the bound account is not SIM-tradeable")
	}
	j := r.job()
	if j.State != updaterjob.StateRefused {
		t.Fatalf("state=%s, want refused pre-hold", j.State)
	}
	if r.hold().Present {
		t.Fatal("a non-SIM account must refuse BEFORE the hold is written")
	}
	// the named failing leg must be the blocker text
	if !strings.Contains(j.Error, "sim_accounts: ") || !strings.Contains(j.Error, "tradeable") {
		t.Fatalf("error %q must carry the named leg's blocker text", j.Error)
	}
}

// The db_open_positions leg failing (the CTO P1 leg) must refuse pre-hold the
// same way, with its own name in the blocker text.
func TestDrainNt8AbsentOpenPositionLegRefuses(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown), func(b *box) { b.absentDbOpen = true })
	if r.runCrashing(t) {
		t.Fatal("the job passed although an OPEN trader_positions row exists")
	}
	j := r.job()
	if j.State != updaterjob.StateRefused {
		t.Fatalf("state=%s, want refused pre-hold", j.State)
	}
	if r.hold().Present {
		t.Fatal("an open position must refuse BEFORE the hold is written")
	}
	if !strings.Contains(j.Error, "db_open_positions: ") {
		t.Fatalf("error %q must carry the db_open_positions blocker text", j.Error)
	}
}

// CTO fold (b) — the worker's ELIGIBILITY check is the revoke: the drain's
// read is eligible+ready (the drain stamps the absent path), then the verdict
// flaps to eligible=false but, adversarially, ready=true with every leg
// passing — the gate must NOT advance on the absent path. K_elig
// ('if a == nil || !a.Eligible' → 'if a == nil') would let the gate pass and
// the run completes; this pin goes RED.
func TestDrainNt8AbsentNotEligibleNeverPassesEvenIfReady(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown), func(b *box) {
		// reads: 1=preflight's flat check, 2=the drain (passes, stamps the
		// path), 3=the gate — flapped there
		b.absentOverride, b.absentFlapAt, b.absentEligible, b.absentReady = true, 3, true, true
	})
	if r.runCrashing(t) {
		t.Fatal("drain passed although the gate says the absent path is NOT eligible")
	}
	j := r.job()
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("state=%s — the drain must fail on the normal path", j.State)
	}
	// the normal path's own blocker (no AddOn ack) is the real reason
	if !strings.Contains(j.Error, "addon_ack") {
		t.Fatalf("error %q must be the normal-path ack blocker, not the absent path", j.Error)
	}
}

// CTO fold (c) — the hold on disk not ours refuses, even with the absent
// verdict ready.
func TestDrainNt8AbsentRefusesWhenTheHoldIsNotOurs(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown))
	r.w.crash = func(q string) {
		if q == "drained_acked/started" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("drain never started")
	}
	r.w.crash = nil
	// someone else's hold lands on disk mid-job (the REAL store writer — the
	// hold_write seam is for the worker's own step)
	if err := store.WriteMaintenanceHold(r.data, store.MaintenanceHold{Held: true, JobID: "job-u4-9999zzzz", Since: time.Now().UTC().Format(time.RFC3339), Owner: "operator"}); err != nil {
		t.Fatal(err)
	}
	if r.runCrashing(t) {
		t.Fatal("drain passed although the hold on disk is another job's")
	}
	j := r.job()
	if j.State != updaterjob.StateRecoveryNeeded {
		t.Fatalf("state=%s", j.State)
	}
	// the foreign hold is NAMED — either by the gate's job-id check (first) or
	// by the absent hold-ours check; both are refusals of the same fact
	if !strings.Contains(j.Error, "job-u4-9999zzzz") && !strings.Contains(j.Error, "hold on disk is not this job's") {
		t.Fatalf("error %q must name the foreign hold", j.Error)
	}
}

// P-D ruling item 3: the queued_signals absent leg now runs at PREFLIGHT too,
// so a queued signal refuses pre-hold (stronger than the old drain-time
// not-ready).
func TestNT8AbsentQueuedSignalRefusesPreflight(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown), func(b *box) { b.absentQueued = 1 })
	if r.runCrashing(t) {
		t.Fatal("the job passed although a signal is queued for the AddOn")
	}
	j := r.job()
	if j.State != updaterjob.StateRefused {
		t.Fatalf("state=%s, want refused pre-hold on queued_signals", j.State)
	}
	if r.hold().Present {
		t.Fatal("a queued signal must refuse BEFORE the hold is written")
	}
	if !strings.Contains(j.Error, "queued_signals: ") {
		t.Fatalf("error %q must carry the queued_signals blocker text", j.Error)
	}
}

// A reconnect mid-DRAIN revokes the verdict: the normal ack/census path
// applies from that moment, never grandfathered.
func TestDrainNt8AbsentReconnectMidDrainRevokes(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown))
	// crash the instant the drain STARTS, then the AddOn reconnects
	r.w.crash = func(q string) {
		if q == "drained_acked/started" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("drain never started")
	}
	r.w.crash = nil
	r.set(func() { r.addonDownSince, r.addonConnected = 0, true })
	r.w.crash = func(q string) {
		if q == "drained_acked/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("the normal drain never passed after the reconnect")
	}
	j := r.job()
	rc := mustReceipt(t, j, "drain")
	if rc.Evidence["drain_path"] != "normal" {
		t.Fatalf("the reconnect must revoke the absent path; drain_path=%q", rc.Evidence["drain_path"])
	}
	if j.DrainPath != "" {
		t.Fatalf("a normal drain must not stamp drain_path (got %q)", j.DrainPath)
	}
}

// A reconnect AFTER the drain (at the gate) revokes too: the gate runs the
// normal two-ack path instead of the absent fast path.
func TestGateAfterAbsentDrainReconnectRunsTheNormalAckPath(t *testing.T) {
	r, j := atDrained(t, withNT8Down(absentDown))
	if j.DrainPath != drainPathNT8Absent {
		t.Fatalf("drain_path=%q", j.DrainPath)
	}
	r.set(func() { r.addonDownSince, r.addonConnected = 0, true })
	r.w.crash = func(q string) {
		if q == "gate_ok/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("the gate never passed on the normal ack path")
	}
	j = r.job()
	rc := mustReceipt(t, j, "gate")
	if _, ok := rc.Evidence["gate_path"]; ok {
		t.Fatalf("the gate must not take the absent fast path after a reconnect: %+v", rc.Evidence)
	}
	if _, ok := rc.Evidence["ack_1_age_ms"]; !ok {
		t.Fatalf("the normal gate must record ack ages: %+v", rc.Evidence)
	}
}

func TestDecideNT8AbsentNoCSChangeSkips(t *testing.T) {
	r, j := atBackupDone(t, withNT8Down(absentDown))
	r.w.crash = func(q string) {
		if q == "nt8_skipped/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("never reached nt8_skipped/done")
	}
	j = r.job()
	if j.NT8 == nil || j.NT8.Decision != updaterjob.NT8Skipped || !j.NT8.Absent {
		t.Fatalf("nt8 decision %+v", j.NT8)
	}
	if j.NT8.F5Owed {
		t.Fatalf("C# unchanged — no F5 is owed: %+v", j.NT8)
	}
}

// With a .cs change the job does NOT park (NT8 is closed — nobody to F5 now):
// the Go side completes and the owed F5 is RECORDED.
func TestDecideNT8AbsentCSChangeRecordsF5OwedAndDoesNotPark(t *testing.T) {
	r, _ := atBackupDone(t, withNT8Down(absentDown))
	writeFile(r.t, filepath.Join(r.relDir, "ninjascript", "VLTrader.cs"), "// C# v2\n")
	r.w.crash = func(q string) {
		if q == "nt8_skipped/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("never reached nt8_skipped/done (must not park)")
	}
	j := r.job()
	if j.NT8 == nil || j.NT8.Decision != updaterjob.NT8Skipped {
		t.Fatalf("nt8 decision %+v — the absent path must not park", j.NT8)
	}
	if !j.NT8.Absent || !j.NT8.F5Owed {
		t.Fatalf("nt8 decision %+v — the owed F5 must be recorded", j.NT8)
	}
	if !strings.Contains(j.NT8.Reason, "AddOn F5 owed at next NT8 start") {
		t.Fatalf("reason %q", j.NT8.Reason)
	}
}

// A reconnect before the decision revokes the absent mode: the normal
// decision (which reads the ack) applies again.
func TestDecideNT8AbsentReconnectedUsesTheNormalDecision(t *testing.T) {
	r, _ := atBackupDone(t, withNT8Down(absentDown))
	r.set(func() { r.addonDownSince, r.addonConnected = 0, true })
	r.w.crash = func(q string) {
		if q == "nt8_skipped/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("never reached nt8_skipped/done")
	}
	j := r.job()
	if j.NT8 == nil || j.NT8.Absent {
		t.Fatalf("the reconnect must revoke the absent decision: %+v", j.NT8)
	}
	if j.NT8.Decision != updaterjob.NT8Skipped {
		t.Fatalf("nt8 decision %+v", j.NT8)
	}
}

// The full absent run completes, and boot_verify SKIPS the AddOn-ack wait
// with that fact recorded (never faked).
func TestNt8AbsentFullRunCompletesAndBootVerifySkipsTheAckWait(t *testing.T) {
	r := newRig(t, withNT8Down(absentDown))
	j := r.runToEnd(t)
	r.noViolations(t)
	if j.State != updaterjob.StateComplete {
		t.Fatalf("job %s\n%v", j.State, states(j))
	}
	if j.DrainPath != drainPathNT8Absent {
		t.Fatalf("drain_path=%q", j.DrainPath)
	}
	rc := mustReceipt(t, j, "boot_verify")
	if rc.Evidence["addon_ack_wait"] != "skipped (drain path nt8_absent)" {
		t.Fatalf("boot_verify must record the skipped ack wait: %+v", rc.Evidence)
	}
	if _, ok := rc.Evidence["acked_build_id"]; ok {
		t.Fatalf("no ack comparison may be faked in absent mode: %+v", rc.Evidence)
	}
}
