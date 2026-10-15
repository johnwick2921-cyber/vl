// B5 — dead-man watchdog: disconnect blocks entries; a reconnect alone does NOT
// resume (must reconcile first); a clean reconcile resumes.
package trader

import "testing"

func TestDeadManWatchdog_BothPaths(t *testing.T) {
	w := &deadManWatchdog{}
	if w.entriesBlocked() {
		t.Fatal("fresh watchdog (live) must allow entries")
	}

	// Path 1 — disconnect blocks entries.
	if st, changed := w.observe(false); !changed || st != dmDisconnected || !w.entriesBlocked() {
		t.Fatalf("disconnect must block entries; state=%v changed=%v", st, changed)
	}

	// Reconnect ALONE must NOT resume — the link must reconcile first.
	if st, changed := w.observe(true); !changed || st != dmAwaitingReconcile || !w.entriesBlocked() {
		t.Fatalf("reconnect alone must stay blocked (awaiting reconcile); state=%v", st)
	}

	// Path 2 — a clean reconcile resumes.
	if !w.reconciled() || w.entriesBlocked() {
		t.Fatal("clean reconcile must resume entries")
	}
	// Reconcile while already live → no-op.
	if w.reconciled() {
		t.Fatal("reconcile while live must be a no-op")
	}
}

func TestDeadManWatchdog_FlapAndStrayReconcile(t *testing.T) {
	w := &deadManWatchdog{}
	w.observe(false) // disconnected
	w.observe(true)  // awaiting reconcile

	// Flap back down before reconciling → returns to disconnected, still blocked.
	if st, _ := w.observe(false); st != dmDisconnected || !w.entriesBlocked() {
		t.Fatalf("flap back down must return to disconnected; state=%v", st)
	}
	// A stray reconcile while disconnected must NOT resume.
	if w.reconciled() || !w.entriesBlocked() {
		t.Fatal("reconcile while disconnected must not resume")
	}
	// Reconnect → awaiting → reconcile → live.
	w.observe(true)
	if !w.reconciled() || w.entriesBlocked() {
		t.Fatal("reconnect then reconcile must resume")
	}
}

// TestDeadManWatchdog_StepDriver exercises the per-cycle step() driver end-to-end
// over a simulated disconnect → reconnect → dirty-then-clean reconciliation, which
// is exactly how runCycle drives it against the live NT8 link. Both the block path
// (disconnect) and the resume path (clean reconcile) are covered.
func TestDeadManWatchdog_StepDriver(t *testing.T) {
	w := &deadManWatchdog{}
	reconcileClean := true
	probe := func() bool { return reconcileClean }

	// Steady live → no event, entries allowed.
	if ev := w.step(true, 0, probe); ev != wdNone || w.entriesBlocked() {
		t.Fatalf("steady live must be a no-op; ev=%v blocked=%v", ev, w.entriesBlocked())
	}
	// Disconnect → wentDown + block.
	if ev := w.step(false, 0, probe); ev != wdWentDown || !w.entriesBlocked() {
		t.Fatalf("disconnect must emit wentDown + block; ev=%v", ev)
	}
	// Still down → no repeat event, still blocked; reconcile probe must NOT run.
	if ev := w.step(false, 0, func() bool { t.Fatal("probe must not run while link down"); return true }); ev != wdNone || !w.entriesBlocked() {
		t.Fatalf("still-down must not re-emit; ev=%v", ev)
	}
	// Reconnect → reconnected (sweep unfilled), STILL blocked; no reconcile this step.
	if ev := w.step(true, 0, func() bool { t.Fatal("probe must not run on the reconnect step"); return true }); ev != wdReconnected || !w.entriesBlocked() {
		t.Fatalf("reconnect must emit reconnected + stay blocked; ev=%v", ev)
	}
	// Next cycle, dirty reconcile → stays blocked, no resume.
	reconcileClean = false
	if ev := w.step(true, 0, probe); ev != wdNone || !w.entriesBlocked() {
		t.Fatalf("dirty reconcile must keep entries blocked; ev=%v", ev)
	}
	// Clean reconcile → resumed, entries allowed.
	reconcileClean = true
	if ev := w.step(true, 0, probe); ev != wdResumed || w.entriesBlocked() {
		t.Fatalf("clean reconcile must resume; ev=%v", ev)
	}
	// Steady live again → no event, probe not invoked (entries already allowed).
	if ev := w.step(true, 0, func() bool { t.Fatal("probe must not run while live"); return true }); ev != wdNone {
		t.Fatalf("steady live after resume must be a no-op; ev=%v", ev)
	}
}

// TestDeadManWatchdog_StepFlapDownBeforeReconcile: a reconnect that flaps back down
// before the clean reconcile must re-block (re-emit wentDown), never leak a resume.
func TestDeadManWatchdog_StepFlapDownBeforeReconcile(t *testing.T) {
	w := &deadManWatchdog{}
	probe := func() bool { return true }
	w.step(false, 0, probe) // down
	w.step(true, 0, probe)  // reconnected (awaiting reconcile)
	if ev := w.step(false, 0, probe); ev != wdWentDown || !w.entriesBlocked() {
		t.Fatalf("flap down before reconcile must re-emit wentDown + block; ev=%v", ev)
	}
	// And a clean reconcile only counts once the link is back up and awaiting again.
	w.step(true, 0, func() bool { return false }) // reconnected
	if ev := w.step(true, 0, probe); ev != wdResumed || w.entriesBlocked() {
		t.Fatalf("clean reconcile after re-reconnect must resume; ev=%v", ev)
	}
}
