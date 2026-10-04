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

// Pin: the CUMULATIVE fill quantity grows the position to the final total —
// fills 3 then 2 (partfill 3, then filled 5) → position 5, never double-count.
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
