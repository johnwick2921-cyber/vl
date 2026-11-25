package trader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── m244: sweep due expiries at the first open cycle BEFORE any new work ────
//
// Row 410 (mess): expiry 16:59:59.999 inside the 16:00–17:00 CME daily break,
// filled 17:00:40 on the reopen — the Go-side expiry was never swept (the
// armed pass is CME-open-gated), so the resting NT8 stop-limit survived the
// break and filled before the first open cycle could cancel it. The fix sweeps
// due expiries at the closed→open edge in cmeSessionClosedSkip, before any
// authoring/placement.

// seedM244Arm authors a mentor-origin arm with the given state/signal/expiry.
func seedM244Arm(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario, state, signalID string, expiryMs int64) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scenario, Side: "long", State: state,
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: "SWING4H", ExpiryMs: expiryMs,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1),
		SignalID: signalID,
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
}

// TestM244ReopenExpirySweepCancelsDueRestingOrder is the named RED: on the
// closed→open edge, the sweep cancels a resting (working) order whose expiry
// fell inside the break and expires a never-placed one, leaving a not-yet-due
// arm untouched. MUTANT: drop the `if wasClosed { sweepDueExpiriesAtReopen }`
// block in cmeSessionClosedSkip → the resting due arm stays working → RED.
func TestM244ReopenExpirySweepCancelsDueRestingOrder(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := b3Clock(17, 1) // 17:01 CT — just after the 16:00–17:00 daily reopen
	// The book must be fresh at the pinned clock (the loopback's real-wall-clock
	// seed would read "future" and refuse the cancel as a stale book).
	if srv := at.armedTrader().GetServer(); srv != nil {
		srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}
	// Simulate the previous cycle having seen the market CLOSED.
	closed := false
	at.cmePrevOpen = &closed
	prev := testNow
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = prev })

	// A WORKING (resting at the broker) arm whose expiry fell inside the break.
	seedM244Arm(t, at, ledger, "rest-due", store.StateWorking, "sig-rest-due", now.UnixMilli()-60_000)
	// A never-placed arm whose expiry also fell inside the break.
	seedM244Arm(t, at, ledger, "armed-due", store.StateArmed, "", now.UnixMilli()-60_000)
	// A working arm whose expiry is still in the future — untouched.
	seedM244Arm(t, at, ledger, "rest-future", store.StateWorking, "sig-rest-future", now.UnixMilli()+60_000)

	if at.cmeSessionClosedSkip() {
		t.Fatal("17:01 CT is open — cmeSessionClosedSkip must NOT skip")
	}

	if got := readArmRow(t, ledger, "rest-due"); got.State != store.StateCancelPending {
		t.Fatalf("the resting due arm must be cancelled by the reopen sweep, got state=%q", got.State)
	}
	if got := readArmRow(t, ledger, "armed-due"); got.State != "expired" {
		t.Fatalf("the never-placed due arm must be expired by the reopen sweep, got state=%q", got.State)
	}
	if got := readArmRow(t, ledger, "rest-future"); got.State != store.StateWorking {
		t.Fatalf("the not-yet-due arm must be untouched, got state=%q", got.State)
	}
}

// TestM244ReopenExpirySweepOnlyOnTheEdge pins that the sweep is EDGE-triggered:
// a fresh boot into an already-open market (cmePrevOpen nil) must NOT sweep —
// the inline armed pass owns expiries then. Prevents a sweep on every open
// cycle.
func TestM244ReopenExpirySweepOnlyOnTheEdge(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := b3Clock(10, 30) // mid-RTH, open
	prev := testNow
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = prev })
	// Fresh empty book like pin 1: without it cancelSafetyFor refuses
	// fail-closed and the row stays working EVEN under the mutant
	// (wasClosed := true), so the pin could not go RED.
	if srv := at.armedTrader().GetServer(); srv != nil {
		srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}

	seedM244Arm(t, at, ledger, "rest-due", store.StateWorking, "sig-rest-due", now.UnixMilli()-60_000)

	// cmePrevOpen is nil (fresh boot into an open market) — no edge, no sweep.
	if at.cmeSessionClosedSkip() {
		t.Fatal("10:30 CT is open — must not skip")
	}
	if got := readArmRow(t, ledger, "rest-due"); got.State != store.StateWorking {
		t.Fatalf("a fresh boot into an open market must NOT sweep (no edge), got state=%q", got.State)
	}
}

// m330 B2 — the one-shot reopen sweep runs once per closed->open edge. A cancel
// refused for a TRANSIENT reason (no fresh broker book) used to be dropped with
// `continue`, leaving the row working with no second chance from this path.
// It must go cancel_pending so the settlement pass re-requests it (the same
// retry the inline sweep gets). This pin asserts the QUEUE state only — the
// settlement pass's actual re-request is owned by its own pin, not here.
// MUTANT: restore the `continue` on the no-book refusal in
// sweepDueExpiriesAtReopen → row stays working → RED.
func TestM330B2ReopenSweepTransientRefusalGoesCancelPending(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := b3Clock(17, 1)
	// Deliberately NO fresh snapshot at the pinned clock: the verdict is the
	// transient "no broker book" refusal.
	closed := false
	at.cmePrevOpen = &closed
	prev := testNow
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = prev })
	seedM244Arm(t, at, ledger, "rest-due", store.StateWorking, "sig-rest-due", now.UnixMilli()-60_000)

	at.sweepDueExpiriesAtReopen(now)

	if got := readArmRow(t, ledger, "rest-due"); got.State != store.StateCancelPending {
		t.Fatalf("a transiently-refused cancel must be queued (cancel_pending) for the settlement retry, got state=%q", got.State)
	}
}

// A partially filled arm is never touched by the one-shot sweep, even when the
// verdict would allow the cancel (entry resting, fresh book). The inline pass
// owns it (remainder-only cancel + exit-drive hand-off).
func TestM330B2ReopenSweepNeverTouchesPartiallyFilledArm(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := b3Clock(17, 1)
	if srv := at.armedTrader().GetServer(); srv != nil {
		srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}
	prev := testNow
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = prev })
	seedM244Arm(t, at, ledger, "partial", store.StateWorking, "sig-partial", now.UnixMilli()-60_000)
	row := readArmRow(t, ledger, "partial")
	if err := ledger.SetFillQuantity(row.ID, 1); err != nil {
		t.Fatal(err)
	}

	at.sweepDueExpiriesAtReopen(now)

	if got := readArmRow(t, ledger, "partial"); got.State != store.StateWorking {
		t.Fatalf("a partially filled arm must never be cancelled by the one-shot sweep, got state=%q", got.State)
	}
}
