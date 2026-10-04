// B2 — MENTOR SIZE REACHES THE ORDER (2026-10-04, release #3b P0 blocker).
//
// WHY THIS FILE EXISTS. mentorArmIntent always logged the size-table choice
// (choice.Contracts) but the armed pass sent quantity 1 for EVERY stop entry,
// so a 5/10/20-contract mentor decision never reached the broker — the
// course's minimum-2-contract partial (split legs) was impossible. This file
// pins the PRODUCTION CALL SITE (placeOneStopEntry): the mentor arm's signed
// contract count rides the row and is what goes on the wire; non-mentor rows
// stay 1; a mentor row WITHOUT a count is REFUSED, never sent as 1.

package trader

import (
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// mentorSizeRow builds a placeable mentor stop-entry row with the given count.
func mentorSizeRow(at *AutoTrader, contracts *int) store.ArmedOrderDB {
	return store.ArmedOrderDB{
		ID: 8, TraderID: at.id, PlanID: "mentor", Version: 1,
		Session: "MENTOR", Scenario: "isb-1", Side: "long",
		EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		Origin: store.ArmOriginMentor, ExpiryMs: 90_000, Contracts: contracts,
	}
}

// TestMentorArmSendsStoredContractCount: a 5-contract mentor intent wires
// quantity 5 (never 1). Mutant: send 1 again → RED.
func TestMentorArmSendsStoredContractCount(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-size"
	r := mentorSizeRow(at, store.IntPtr(5))
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 {
		t.Fatalf("wire calls = %d, want 1", len(pl.calls))
	}
	if pl.calls[0].qty != 5 {
		t.Fatalf("mentor arm wired qty %.0f, want 5 (the size must reach the order)", pl.calls[0].qty)
	}
}

// TestMentorArmCountClampedToTraderMax: a 20-contract arm is clamped to the
// strategy's mentor_max_contracts (here 12), never over it.
func TestMentorArmCountClampedToTraderMax(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorMaxContracts: 12}})
	at.id = "mentor-clamp"
	r := mentorSizeRow(at, store.IntPtr(20))
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 || pl.calls[0].qty != 12 {
		t.Fatalf("mentor arm wired qty %.0f (%d calls), want 12 (clamped to mentor_max_contracts)",
			pl.calls[0].qty, len(pl.calls))
	}
}

// TestNonMentorArmStaysOneContract: a non-mentor stop entry is untouched —
// quantity 1, whatever the mentor knobs say.
func TestNonMentorArmStaysOneContract(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "non-mentor"
	r := store.ArmedOrderDB{
		ID: 8, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
		Session: "NY", Scenario: "S1", Side: "long",
		EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		// no Origin → not a mentor arm; no Contracts.
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 || pl.calls[0].qty != 1 {
		t.Fatalf("non-mentor arm wired qty %.0f (%d calls), want 1 (unchanged)",
			pl.calls[0].qty, len(pl.calls))
	}
}

// TestMentorArmWithoutCountRefused: a mentor row with NO count is REFUSED at
// placement — counted + logged, zero wire calls — never sent as 1 (absent ≠ 0).
func TestMentorArmWithoutCountRefused(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-nocount"
	r := mentorSizeRow(at, nil) // the mentor injector must stamp it; here it did not
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be otherwise placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceNotSent {
		t.Fatalf("outcome = %d, want NOT_SENT (a mentor arm without a count must never be sent as 1)", got)
	}
	if len(pl.calls) != 0 {
		t.Fatalf("a mentor arm without a count must never reach the wire, got %d calls", len(pl.calls))
	}
}

// TestMaterializeArmedEntryUsesFillQuantity (N2, must ship with B2): a 5-lot
// mentor fill materializes an OPEN position of quantity 5 — the broker holds 5
// and the ledger must agree, or reconcile freezes on the 1-vs-N mismatch.
// Mutant: build the position with 1 again → RED.
func TestMaterializeArmedEntryUsesFillQuantity(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-fill"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-1", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "filled", SignalID: "sig-fill5", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	u := ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-fill5", Account: "Sim101", FillPrice: 29645, Quantity: 5}
	at.materializeArmedEntry(row, u)
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.Quantity != 5 || pos.EntryQuantity != 5 || pos.EntryPrice != 29645 {
		t.Fatalf("materialized position qty = %.0f / entry %.0f @ %.2f, want 5/5 @ 29645 (N2: the fill frame's cumulative quantity + average price are the truth)", pos.Quantity, pos.EntryQuantity, pos.EntryPrice)
	}
}

// TestMaterializeArmedEntrySetsCumulativeOnFill (N2): the AddOn emits ONE
// partfilled frame then ONE filled frame with the CUMULATIVE quantity, so the
// ledger SETS the position — partfilled 2 → 2, then filled 5 → 5 at the new
// average. Mutant: skip the filled-frame update → RED.
func TestMaterializeArmedEntrySetsCumulativeOnFill(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-partfill"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-2", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "partfilled", SignalID: "sig-part", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partfilled", SignalID: "sig-part", Account: "Sim101", FillPrice: 29644, Quantity: 2})
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-part", Account: "Sim101", FillPrice: 29646, Quantity: 5})
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.Quantity != 5 || pos.EntryQuantity != 5 || pos.EntryPrice != 29646 {
		t.Fatalf("position = %.0f / entry %.0f @ %.2f, want 5/5 @ 29646 (the filled frame's cumulative qty + average price must win)", pos.Quantity, pos.EntryQuantity, pos.EntryPrice)
	}
}

// TestMaterializeArmedEntryDuplicatePartFillIdempotent (N2): a duplicate
// partfilled frame keeps the position unchanged (never shrinks, never adds).
func TestMaterializeArmedEntryDuplicatePartFillIdempotent(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-dup"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-3", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "partfilled", SignalID: "sig-dup", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	u := ntwire.OrderUpdatePayload{State: "partfilled", SignalID: "sig-dup", Account: "Sim101", FillPrice: 29644, Quantity: 2}
	at.materializeArmedEntry(row, u)
	at.materializeArmedEntry(row, u) // duplicate — same cumulative qty
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.Quantity != 2 {
		t.Fatalf("position qty = %.0f after a duplicate frame, want 2 (idempotent)", pos.Quantity)
	}
}

// TestMaterializeArmedEntryPartFillSetsNotAdds (N2): two partfilled frames with
// cumulative quantities 2 then 4 set the position to 4, not 6 — the frame
// quantity is cumulative, never an increment. Mutant: add instead of set → RED.
func TestMaterializeArmedEntryPartFillSetsNotAdds(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "mentor-set"
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-4", Side: "long", EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		State: "partfilled", SignalID: "sig-set", FillPrice: 29645,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
	}
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partfilled", SignalID: "sig-set", Account: "Sim101", FillPrice: 29644, Quantity: 2})
	at.materializeArmedEntry(row, ntwire.OrderUpdatePayload{State: "partfilled", SignalID: "sig-set", Account: "Sim101", FillPrice: 29645, Quantity: 4})
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.Quantity != 4 {
		t.Fatalf("position qty = %.0f after partfills 2 then 4, want 4 (SET, not add)", pos.Quantity)
	}
}
