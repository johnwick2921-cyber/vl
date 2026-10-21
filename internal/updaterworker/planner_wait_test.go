package updaterworker

import (
	"strings"
	"sync"
	"testing"
	"time"

	"vl/internal/updaterjob"
)

// F2 — the bounded planner wait (knob Budgets.PlannerWait, default 15 m).
// A live AI-plan read is a KNOWN BENIGN blocker: preflight/drain/gate wait for
// it (the job shows "waiting for the AI plan (started hh:mm:ss)" as the live
// blocker, never "not passed within"). On planner-wait EXPIRY: preflight
// refuses with NO hold (as today); drain/gate RELEASE the hold and end
// reverted_clean (self-recovery: the lock is released too, nothing was
// installed) — a benign blocker must not leave the desk held.

func shrinkPlanner(r *rig) {
	r.w.cfg.Budgets.Poll = 10 * time.Millisecond
	r.w.cfg.Budgets.PlannerWait = 100 * time.Millisecond
}

// watchBlocker observes the live blocker DETERMINISTICALLY, at the fake
// host's sleep boundary: every poll iteration persists a new blocker to the
// REAL job file and then calls host.Sleep — the hook reads the file in the
// SAME goroutine, immediately after the write, so no scheduling race can
// miss the waiting text (the injected clock makes the whole wait
// CPU-speed). The returned func reports whether the text ever appeared.
func watchBlocker(t *testing.T, r *rig) (saw func() bool) {
	t.Helper()
	var mu sync.Mutex
	sawIt := false
	old := r.pollHook
	r.pollHook = func() {
		if j, err := updaterjob.Read(r.data, boxJobID); err == nil &&
			strings.Contains(j.Blocker, "waiting for the AI plan (started") {
			mu.Lock()
			sawIt = true
			mu.Unlock()
		}
	}
	t.Cleanup(func() { r.pollHook = old })
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawIt
	}
}

// Preflight: the planner read outlives the wait → refused, NO hold, and the
// live blocker was persisted to the job file mid-wait.
func TestPreflightPlannerWaitExpiryRefusesNoHold(t *testing.T) {
	r := newRig(t, func(b *box) { b.plannerInFlight = true })
	shrinkPlanner(r)
	watch := watchBlocker(t, r)
	if r.runCrashing(t) {
		t.Fatal("preflight passed although the AI plan read outlived the planner wait")
	}
	j := r.job()
	if j.State != updaterjob.StateRefused {
		t.Fatalf("state=%s, want refused", j.State)
	}
	if r.hold().Present {
		t.Fatal("a planner-wait expiry at preflight must refuse with NO hold")
	}
	if !strings.Contains(j.Error, "AI plan still running after") {
		t.Fatalf("error %q must name the planner wait", j.Error)
	}
	if !watch() {
		t.Fatal("the live blocker must read the waiting text while the job waits")
	}
}

// Drain: the planner read appears AFTER the hold — expiry must RELEASE the
// hold and end reverted_clean (nothing installed; the desk must not stay held
// on a benign blocker).
func TestDrainPlannerWaitExpiryReleasesHoldAndRevertsClean(t *testing.T) {
	r := newRig(t)
	shrinkPlanner(r)
	r.w.crash = func(q string) {
		if q == "drained_acked/started" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("drain never started")
	}
	r.w.crash = nil
	r.set(func() { r.plannerInFlight = true })
	watch := watchBlocker(t, r)
	if r.runCrashing(t) {
		t.Fatal("drain passed although the AI plan read outlived the planner wait")
	}
	j := r.job()
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("state=%s, want reverted_clean (planner-wait expiry releases the hold)", j.State)
	}
	if r.hold().Present {
		t.Fatal("the hold must be RELEASED on planner-wait expiry — a benign blocker never keeps the desk held")
	}
	if !strings.Contains(j.Error, "AI plan still running after") {
		t.Fatalf("error %q must name the planner wait", j.Error)
	}
	rc := mustReceipt(t, j, "release_hold")
	if rc.Evidence["reason"] != "planner wait expired" {
		t.Fatalf("release_hold receipt must name the reason: %+v", rc.Evidence)
	}
	if !watch() {
		t.Fatal("the live blocker must read the waiting text while the drain waits")
	}
}

// Gate: same release-and-revert semantics at the gate step.
func TestGatePlannerWaitExpiryReleasesHoldAndRevertsClean(t *testing.T) {
	r, _ := atDrained(t)
	shrinkPlanner(r)
	r.set(func() { r.plannerInFlight = true })
	watch := watchBlocker(t, r)
	if r.runCrashing(t) {
		t.Fatal("gate passed although the AI plan read outlived the planner wait")
	}
	j := r.job()
	if j.State != updaterjob.StateRevertedClean {
		t.Fatalf("state=%s, want reverted_clean (planner-wait expiry releases the hold)", j.State)
	}
	if r.hold().Present {
		t.Fatal("the hold must be RELEASED on planner-wait expiry at the gate")
	}
	if !strings.Contains(j.Error, "AI plan still running after") {
		t.Fatalf("error %q must name the planner wait", j.Error)
	}
	if !watch() {
		t.Fatal("the live blocker must read the waiting text while the gate waits")
	}
}

// The planner wait is a WAIT, not a refusal: when the read ends inside the
// budget the step passes.
func TestPlannerWaitPassesWhenTheReadEnds(t *testing.T) {
	r := newRig(t)
	shrinkPlanner(r)
	r.w.crash = func(q string) {
		if q == "drained_acked/started" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		t.Fatal("drain never started")
	}
	r.w.crash = nil
	r.set(func() { r.plannerInFlight = true })
	// The read ends well inside the planner wait (100 ms).
	go func() {
		time.Sleep(40 * time.Millisecond)
		r.set(func() { r.plannerInFlight = false })
	}()
	r.w.crash = func(q string) {
		if q == "drained_acked/done" {
			panic(crashPanic{q})
		}
	}
	if !r.runCrashing(t) {
		j := r.job()
		t.Fatalf("the drain must pass once the planner read ends; state=%s error=%q", j.State, j.Error)
	}
}

// TestPreflightReportsThePlannerFirstSoItsGraceApplies — release #22: with a
// plan read in flight AND the bot not flat (an arm / working order), the
// planner is the REPORTED blocker, so the planner budget governs the wait —
// not the shorter flat budget naming the census/ledger leg (10-14 job
// 30a8bbf6 was refused at 10 min on ledger_exposure while its planner grace
// was owed).
func TestPreflightReportsThePlannerFirstSoItsGraceApplies(t *testing.T) {
	r := newRig(t, func(b *box) { b.plannerInFlight = true; b.flat = false })
	r.w.cfg.Budgets.Poll = 10 * time.Millisecond
	r.w.cfg.Budgets.PreflightFlat = 100 * time.Millisecond
	r.w.cfg.Budgets.PlannerWait = 400 * time.Millisecond
	watch := watchBlocker(t, r)
	if r.runCrashing(t) {
		t.Fatal("preflight passed although the bot was not flat")
	}
	j := r.job()
	if j.State != updaterjob.StateRefused || r.hold().Present {
		t.Fatalf("state=%s hold=%v, want refused with no hold", j.State, r.hold().Present)
	}
	if !strings.Contains(j.Error, "AI plan still running after") {
		t.Fatalf("while a plan read runs the planner must be the reported blocker and its budget must govern; got error %q", j.Error)
	}
	if !watch() {
		t.Fatal("the live blocker must read the planner's waiting text")
	}
}
