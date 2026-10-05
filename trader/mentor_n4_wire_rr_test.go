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
	// A LEVEL target just above 1:1: risk 10 (100 → 90), reward 10.5 (100 →
	// 110.5). The level cannot move; the 0.5-pt offset makes the wire
	// 10.0 / 10.5 < 1:1. (A target at EXACTLY 1R is the floor and is re-based
	// instead — TestMentorWireOneRFloorRebasedNotRefused.)
	r := store.ArmedOrderDB{
		ID: 9, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
		Session: "TEST-N4", Scenario: "TEST-N4", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 110.5,
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

// N4's twin for the split (CTO 2026-10-04): the leg-1 TP keeps its R-multiple
// at the WIRE trigger. Authored 1R (entry 100, stop 90 → leg 1 at 110); the
// 0.5-pt offset puts the trigger at 100.5, so the true 1:1 from the fill is
// 100.5 + 10.5 = 111. Mutant: send the authored leg1_tp → 110 (0.905R) → RED.
func TestMentorWireLeg1TPKeepsOneToOneAtTheTrigger(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.id = "n4-wire-leg1"
	r := store.ArmedOrderDB{
		ID: 11, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
		Session: "TEST-N4L", Scenario: "TEST-N4L", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 125,
		Origin: store.ArmOriginMentor, ExpiryMs: 90_000, Contracts: store.IntPtr(5),
		Leg1Qty: store.IntPtr(3), Leg1TP: 110,
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(pl.calls))
	}
	c := pl.calls[0]
	want := d.Trigger + (d.Trigger - r.StopPx) // 1R measured from the wire trigger
	if c.leg1Qty != 3 || c.leg1TP != want {
		t.Fatalf("leg 1 on the wire = (%d, %.2f), want (3, %.2f) — 1:1 from the trigger %.2f", c.leg1Qty, c.leg1TP, want, d.Trigger)
	}
}

// mentorWireLeg1TP is a pure R-multiple re-base: 1R stays 1R, 2R stays 2R,
// both sides; degenerate input returns the authored TP.
func TestMentorWireLeg1TPRebase(t *testing.T) {
	for _, c := range []struct {
		name                        string
		entry, trigger, stop, tp, w float64
	}{
		{"long 1R", 100, 100.5, 90, 110, 111},
		{"long 2R (mode C)", 100, 100.5, 90, 120, 121.5},
		{"short 1R", 100, 99.5, 110, 90, 89},
		{"no offset", 100, 100, 90, 110, 110},
		{"no TP", 100, 100.5, 90, 0, 0},
		{"loss-side TP left for the wire check", 100, 100.5, 90, 95, 95},
	} {
		if got := mentorWireLeg1TP(c.entry, c.trigger, c.stop, c.tp); got != c.w {
			t.Errorf("%s: mentorWireLeg1TP = %.2f, want %.2f", c.name, got, c.w)
		}
	}
}

// The 1R floor (CTO 2026-10-04, DS-105 replay b5: 48/461 entries, all SWING4H,
// refused by N4): a target authored at EXACTLY 1:1 is derived from the entry,
// so it moves to the 1:1 point at the wire trigger and the entry is PLACED.
// Mutant: drop the re-base → refused (outcome not-sent) → RED.
func TestMentorWireOneRFloorRebasedNotRefused(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{})
	at.id = "n4-wire-floor"
	r := store.ArmedOrderDB{
		ID: 12, TraderID: at.id, PlanID: "2026-09-23:NY", Version: 1,
		Session: "TEST-N4F", Scenario: "TEST-N4F", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 110,
		Origin: store.ArmOriginMentor, ExpiryMs: 90_000, Contracts: store.IntPtr(1),
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed (the 1R floor is re-based, not refused)", got)
	}
	want := d.Trigger + (d.Trigger - r.StopPx)
	if len(pl.calls) != 1 || pl.calls[0].tp != want {
		t.Fatalf("target on the wire = %+v, want %.2f (1:1 from the trigger %.2f)", pl.calls, want, d.Trigger)
	}
}

// mentorWireOneRFloor moves ONLY a target on the 1R floor; a level target
// (anything off the floor by more than half a tick) never moves.
func TestMentorWireOneRFloorOnlyMovesTheFloor(t *testing.T) {
	for _, c := range []struct {
		name                        string
		entry, trigger, stop, tp, w float64
	}{
		{"long floor", 100, 100.5, 90, 110, 111},
		{"short floor", 100, 99.5, 110, 90, 89},
		{"long level 1.05R stays", 100, 100.5, 90, 110.5, 110.5},
		{"long 2R stays", 100, 100.5, 90, 120, 120},
		{"no target", 100, 100.5, 90, 0, 0},
	} {
		if got := mentorWireOneRFloor(c.entry, c.trigger, c.stop, c.tp, 0.25); got != c.w {
			t.Errorf("%s: mentorWireOneRFloor = %.2f, want %.2f", c.name, got, c.w)
		}
	}
}
