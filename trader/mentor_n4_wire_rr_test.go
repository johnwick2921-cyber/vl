package trader

import (
	"testing"

	"vl/store"
)

// TestMentorWireRRRefusesSubOneToOne — N4 (DAY-1 #15/#42, CTO ruling 17:46):
// the +2-tick wire offset moves the entry 0.5 pt against the trade, so a mentor
// intent that passed the 1:1 floor at the authored price can land UNDER 1:1 at
// the wire. The place re-checks R at the WIRE trigger and refuses (counted,
// nothing sent). Mutant: drop the re-check → RED (the sub-1:1 order is placed).
func TestMentorWireRRRefusesSubOneToOne(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "n4-wire-rr"
	// Authored exactly 1:1: risk 10 (100 → 90), reward 10 (100 → 110). The
	// 0.5-pt offset makes the wire 9.5 / 10.5 < 1:1.
	r := store.ArmedOrderDB{
		ID: 9, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
		Session: "TEST-N4", Scenario: "TEST-N4", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 110,
		Origin: store.ArmOriginMentor, ExpiryMs: 90_000, Contracts: store.IntPtr(5),
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be an otherwise-placeable arm, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceNotSent {
		t.Fatalf("outcome = %d, want not-sent (sub-1:1 at the wire)", got)
	}
	if len(pl.calls) != 0 {
		t.Fatalf("the sub-1:1 mentor order must never reach the wire, got %d calls", len(pl.calls))
	}
}

// TestMentorWireRRPassesAtOrAboveOneToOne — the same re-check must NOT refuse a
// trade whose wire R:R is still ≥ 1:1.
func TestMentorWireRRPassesAtOrAboveOneToOne(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "n4-wire-rr-pass"
	// Authored 2:1: risk 10, reward 20. Wire: 19.5 / 10.5 ≥ 1 → placed.
	r := store.ArmedOrderDB{
		ID: 10, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
		Session: "TEST-N4P", Scenario: "TEST-N4P", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 120,
		Origin: store.ArmOriginMentor, ExpiryMs: 90_000, Contracts: store.IntPtr(5),
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed (wire R:R ≥ 1:1)", got)
	}
	if pl.stopLimitCalls != 1 {
		t.Fatalf("the mentor order must route through the limit variant once, got %d", pl.stopLimitCalls)
	}
}
