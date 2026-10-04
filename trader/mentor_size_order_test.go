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
