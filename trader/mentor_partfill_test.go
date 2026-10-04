package trader

import (
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── N3 P0/P1 partial-fill pins (DS-107) ───────────────────────────────────

// Pin: the pure full-fill predicate splits the AddOn's "partial" state from a
// completing "filled" state.
func TestMentorFillIsFull(t *testing.T) {
	if mentorFillIsFull("partial", 3, 5) {
		t.Fatal("partial 3/5 must not be full")
	}
	if mentorFillIsFull("partfilled", 3, 5) {
		t.Fatal("partfilled 3/5 must not be full")
	}
	if !mentorFillIsFull("filled", 5, 5) {
		t.Fatal("filled must be full")
	}
	if !mentorFillIsFull("partial", 5, 5) {
		t.Fatal("partial at/above the total must be full (defensive)")
	}
}

// Pin: the remainder to cancel at expiry (filled vs signed count).
func TestMentorRemainderToCancel(t *testing.T) {
	if got := mentorRemainderToCancel(store.ArmedOrderDB{Contracts: store.IntPtr(5), FillQuantity: 3}); got != 2 {
		t.Fatalf("remainder = %d, want 2", got)
	}
	if got := mentorRemainderToCancel(store.ArmedOrderDB{Contracts: store.IntPtr(5), FillQuantity: 0}); got != 0 {
		t.Fatalf("unfilled remainder = %d, want 0", got)
	}
	if got := mentorRemainderToCancel(store.ArmedOrderDB{Contracts: store.IntPtr(5), FillQuantity: 5}); got != 0 {
		t.Fatalf("full remainder = %d, want 0", got)
	}
	if got := mentorRemainderToCancel(store.ArmedOrderDB{}); got != 0 {
		t.Fatalf("no-count remainder = %d, want 0", got)
	}
}

// Pin (CTO 2026-10-04): fills 3 then 2 (partfill 3, then the completing filled
// frame carries cumulative 5) → position 5 — the delta (5 − 3 = 2) is added,
// never the cumulative value re-added.
func TestMaterializeArmedEntryCumulativePartFill(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-n3"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-n3", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "working", SignalID: "sig-n3", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	// part-fill 3 (cumulative 3).
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partial", SignalID: "sig-n3", Account: "Sim101", FillPrice: 29645, Quantity: 3})
	// production re-reads the row each frame; advance the recorded fill.
	row.FillQuantity = 3
	// completing fill (cumulative 5) — the remaining 2.
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-n3", Account: "Sim101", FillPrice: 29646, Quantity: 5})
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.Quantity != 5 || pos.EntryQuantity != 5 {
		t.Fatalf("position qty = %.0f / entry %.0f, want 5/5 (cumulative fill, not additive)", pos.Quantity, pos.EntryQuantity)
	}
}

// Pin: a duplicate / out-of-order frame (delta ≤ 0) is a no-op — the position
// neither shrinks nor double-counts.
func TestMaterializeArmedEntryDuplicateFrameNoop(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-dup"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-dup", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "working", SignalID: "sig-dup", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partial", SignalID: "sig-dup", Account: "Sim101", FillPrice: 29645, Quantity: 3})
	row.FillQuantity = 3
	// the same cumulative frame re-delivered → delta 0.
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partial", SignalID: "sig-dup", Account: "Sim101", FillPrice: 29645, Quantity: 3})
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.Quantity != 3 {
		t.Fatalf("position qty = %.0f, want 3 (a duplicate frame must be a no-op)", pos.Quantity)
	}
}

// Pin (CTO 2026-10-04, the exit case): 3 filled → the filled part exits to 0 →
// a late cumulative frame (5) must land on the REMAINDER — position 2, not 5.
// The delta is u.Quantity − r.FillQuantity, NEVER qty − pos.Quantity: measuring
// against the current position (0 after the exit) would re-materialize all 5.
func TestMaterializeArmedEntryDeltaAfterExit(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-exit"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-exit", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "working", SignalID: "sig-exit", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partial", SignalID: "sig-exit", Account: "Sim101", FillPrice: 29645, Quantity: 3})
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil || pos.Quantity != 3 {
		t.Fatalf("after the 3-fill: pos qty = %.0f (err %v), want 3", pos.Quantity, err)
	}
	// the far-side stop takes the filled part out: the row stays OPEN at 0 while
	// the arm row is still working its remaining 2 contracts.
	if err := st.Position().SetPositionQuantityAndPrice(pos.ID, 0, 29645); err != nil {
		t.Fatalf("exit to 0 failed: %v", err)
	}
	row.FillQuantity = 3 // the row's last recorded cumulative fill
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-exit", Account: "Sim101", FillPrice: 29646, Quantity: 5})
	pos, err = st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not found after the late frame: %v", err)
	}
	if pos.Quantity != 2 {
		t.Fatalf("position qty = %.0f, want 2 (delta = u.Quantity − r.FillQuantity, not qty − pos.Quantity)", pos.Quantity)
	}
}
