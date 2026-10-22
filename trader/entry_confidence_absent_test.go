package trader

import (
	"testing"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// R22 P2 — a plan-arm fill carries NO AI confidence. Before R22 the row stored
// a fabricated 0 (positions 649/650 and every one of the 49 armed_entry rows).
// Production call site: materializeArmedEntry (the armed fill's position
// writer). The column must read NULL; the decision path still stores its value.
func TestArmFillEntryConfidenceIsAbsentNotZero(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	at, st := resetTrader(t, cfg)
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "2026-10-13:ASIA:trader", Version: 2, Session: "ASIA",
		Scenario: "S2", Side: "short", EntryPx: 30843.5, StopPx: 30882.78, TargetPx: 30744.5,
		State: "filled", SignalID: "sig-r22-conf", FillPrice: 30843.5,
	}
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-r22-conf", Account: "Sim101", FillPrice: 30843.5})
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "SHORT")
	if err != nil || pos == nil {
		t.Fatalf("arm fill not materialized: %v", err)
	}
	var isNull int
	if err := st.DB().QueryRow(`SELECT entry_confidence IS NULL FROM trader_positions WHERE id = ?`, pos.ID).Scan(&isNull); err != nil {
		t.Fatal(err)
	}
	if isNull != 1 || pos.EntryConfidence != nil {
		t.Fatalf("a plan-arm entry has no AI confidence: the column must be NULL (absent), not a fabricated 0 — is_null=%d field=%v", isNull, pos.EntryConfidence)
	}
	// Control: a confidence that EXISTS is stored as itself.
	c := 72
	dec := &store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: "LONG", EntryPrice: 30800, EntryQuantity: 1, Quantity: 1, EntryConfidence: &c}
	if err := st.Position().Create(dec); err != nil {
		t.Fatal(err)
	}
	var got *int
	if err := st.DB().QueryRow(`SELECT entry_confidence FROM trader_positions WHERE id = ?`, dec.ID).Scan(&got); err != nil || got == nil || *got != 72 {
		t.Fatalf("a decision-path confidence must persist as itself: %v %v", got, err)
	}
	// The in-position observer is told n/a, never 0.
	p := kernel.NewStrategyEngine(&cfg).BuildObserverSystemPrompt(kernel.ObserverInput{Symbol: "MNQ", Side: "SHORT", EntryPrice: 30843.5, EntryConfidence: confidenceOrZero(pos.EntryConfidence)}, 0)
	if !containsAll(p, "entry confidence: n/a") || containsAll(p, "entry confidence: 0") {
		t.Fatalf("observer prompt must say n/a for an arm entry:\n%s", p)
	}
}
