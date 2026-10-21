package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vl/telemetry"
)

// RELEASE #22 CANCEL-SETTLE (F-1, review-1014 EXECUTION.md / PLANNER.md F2).
//
// Row 363 (release #21 preflight, 10-14 01:47:29 CT) was written 'cancelled'
// by NT8's own report and then moved BACK to cancel_pending by the sync-cancel
// timeout branch's RequestCancel, because RequestCancel guarded only 'filled'.
// The ledger then said an order was live that NT8 had confirmed dead, the
// installation gate's ledger_exposure leg refused the update for 10 minutes,
// and a cancel was re-sent every pass for an order NT8 no longer had
// (cancel_attempts reached 8).
//
// The pin, at the production store method every cancel site calls: a cancel
// request on ANY terminal row is refused — typed, the row untouched (state,
// reason, request stamp, attempt count), and counted. The terminal set is read
// from the canonical table (ArmStateNames + IsTerminalArmState), never re-typed
// here, so a state added to the table is covered without editing this test.
func TestRequestCancelRefusesEveryTerminalState(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	telemetry.ResetGateBlocksForTest()
	t.Cleanup(telemetry.ResetGateBlocksForTest)

	var terminal []string
	for _, s := range ArmStateNames() {
		if IsTerminalArmState(s) {
			terminal = append(terminal, s)
		}
	}
	// The table today: filled, cancelled, canceled, rejected, expired,
	// superseded, shadowed. A shrink is a reclassification someone must see.
	if len(terminal) < 7 {
		t.Fatalf("terminal arm states shrank to %v — re-read store/arm_state.go before trusting this pin", terminal)
	}
	for i, state := range terminal {
		t.Run(state, func(t *testing.T) {
			trader := "t-r22-" + state
			row := &ArmedOrderDB{
				TraderID: trader, PlanID: "2026-10-13:ASIA:" + trader, Version: 5, Session: "ASIA",
				Scenario: "S1", Side: "SHORT", EntryPx: 30815, StopPx: 30855, TargetPx: 30725.75,
				State: state, StateReason: "settled: " + state + " in NT8", SignalID: "7e614fac-r22-" + state,
				CancelSettledSnapshotID: int64(110695 + i),
			}
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			err := st.RequestCancel(row.ID, "no-trade band opened (ack timeout — unconfirmed, settlement pass owns it)", time.Now().UnixMilli())
			var got ArmedOrderDB
			if err := db.First(&got, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if got.State != state {
				t.Fatalf("a %q row was moved to %q by a cancel request — a settled outcome was re-opened (row 363's resurrection)", state, got.State)
			}
			if got.StateReason != row.StateReason {
				t.Fatalf("the settled reason was overwritten: %q → %q", row.StateReason, got.StateReason)
			}
			if got.CancelAttempts != 0 || got.CancelRequestedAtMs != 0 {
				t.Fatalf("a refused request must record nothing: attempts=%d requested_at=%d", got.CancelAttempts, got.CancelRequestedAtMs)
			}
			if got.CancelSettledSnapshotID != row.CancelSettledSnapshotID {
				t.Fatalf("the settlement evidence was touched: %d → %d", row.CancelSettledSnapshotID, got.CancelSettledSnapshotID)
			}
			if !errors.Is(err, ErrArmCancelTerminal) {
				t.Fatalf("RequestCancel on a %q row returned %v — want the typed terminal refusal (ErrArmCancelTerminal), so no caller reads it as a request", state, err)
			}
			_, table := telemetry.GateBlockSnapshot()
			if n := table[trader][GateRequestCancelTerminal]; n != 1 {
				t.Fatalf("the refusal must be COUNTED once for %s, gate-block table says %d (%v)", trader, n, table[trader])
			}
		})
	}
}

// The guard must not over-reach: every NON-terminal state still accepts a
// cancel request (cancel_pending included — that is the re-request path).
func TestRequestCancelStillMovesEveryNonTerminalState(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	for _, state := range ArmStateNames() {
		if IsTerminalArmState(state) {
			continue
		}
		t.Run(state, func(t *testing.T) {
			row := &ArmedOrderDB{TraderID: "t-r22-live", PlanID: "2026-10-14:LONDON:" + state, Version: 1, Session: "LONDON",
				Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, State: state, SignalID: "sig-live-" + state}
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			if err := st.RequestCancel(row.ID, "gate changed", time.Now().UnixMilli()); err != nil {
				t.Fatalf("a %q row must accept a cancel request: %v", state, err)
			}
			got, ok := st.StateOf(row.ID)
			if !ok || got != StateCancelPending {
				t.Fatalf("a %q row must move to cancel_pending, got %q", state, got)
			}
		})
	}
}

// The CAS half: a row that turns terminal BETWEEN RequestCancel's read and its
// write (the ordered worker applying NT8's report on its own goroutine) must
// not be overwritten either. The race is forced through the store's own seam
// that runs between the read and the write.
func TestRequestCancelCASRefusesARowSettledMidRequest(t *testing.T) {
	db := newArmedTestDB(t)
	st := NewArmedOrderStore(db)
	row := &ArmedOrderDB{TraderID: "t-r22-cas", PlanID: "2026-10-13:ASIA:cas", Version: 5, Session: "ASIA",
		Scenario: "S1", Side: "SHORT", EntryPx: 30815, StopPx: 30855, TargetPx: 30725.75, State: StateWorking, SignalID: "sig-cas"}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	requestCancelBeforeWriteHook = func(id int64) {
		// The worker writes NT8's positive report while the request is in flight.
		if err := st.SetState(id, StateCancelled, "cancelled in NT8"); err != nil {
			t.Errorf("worker write: %v", err)
		}
	}
	t.Cleanup(func() { requestCancelBeforeWriteHook = nil })
	err := st.RequestCancel(row.ID, "ack timeout", time.Now().UnixMilli())
	if !errors.Is(err, ErrArmCancelTerminal) {
		t.Fatalf("the CAS must refuse a row settled mid-request, got %v", err)
	}
	var got ArmedOrderDB
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.State != StateCancelled || !strings.Contains(got.StateReason, "cancelled in NT8") {
		t.Fatalf("the worker's settlement was overwritten: state=%q reason=%q", got.State, got.StateReason)
	}
}
