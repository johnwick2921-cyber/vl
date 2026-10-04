package trader

import (
	"testing"
	"time"

	"vl/store"
)

// E1 — THE CALLER-LEVEL PIN, the one that matters.
//
// Wave B's own file says why this shape is required: its first cut proved a
// guard as a pure function and proved its wiring with a source grep, and three
// reviewers then mutated the call site with the whole suite staying green. So
// this drives the DISPATCH and asserts what reached the wire.
//
// The scenario is nt8_order_snapshots id 1664: nine orders live at the broker
// for ONE arm slot while every ledger row read 'cancelled'. With a well-formed
// trigger — which is exactly what Wave B just shipped — a tenth placement would
// have been a tenth live stop order on an account capped at two contracts.
func TestStopEntryRefusedWhenSlotIsLiveAtTheBroker(t *testing.T) {
	at := &AutoTrader{id: "hoang"}
	pl := &fakePlacer{}
	led := &fakeLedger{}
	r := store.ArmedOrderDB{
		ID: 1, PlanID: "2026-09-04:NY", Version: 3, Scenario: "S2", LegIndex: 0,
		Side: "SHORT", EntryPx: 29591.02, StopPx: 29650, TargetPx: 29500,
		SignalID: "7dd07a19-701e-4afa-9b15-655f65810e9e",
	}
	// The stop-side guard says PLACE — this arm is otherwise perfectly good.
	d := decideStopEntry("SHORT", r.EntryPx, testOffset(), testTick, 29650.00)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be an otherwise-placeable arm, got action %q", d.Action)
	}

	live := adjudicateSlot(snapshot1664(), true, 5*time.Second, bookBound, 1664, slot1664Signals())
	if live.Allowed() {
		t.Fatal("fixture precondition: the 1664 book must read LIVE")
	}

	at.placeOneStopEntry(pl, led, r, d, 29650.00, time.Now(), live, nil)

	if len(pl.calls) != 0 {
		t.Fatalf("E1 RED: %d order(s) reached the wire while nine were already live at the broker for this slot — the placement must be REFUSED (%s)",
			len(pl.calls), live.Refusal())
	}
	// And it must not have quietly cancelled the arm either: a refusal is not a
	// cancellation, and the slot may become placeable once the book clears.
	for _, w := range led.states {
		if w.state == "cancelled" {
			t.Fatalf("a slot-guard refusal must not cancel the arm; ledger got state %q reason %q", w.state, w.reason)
		}
	}
}

// The mirror: a FREE slot still places, so the guard cannot be satisfied by
// simply refusing everything.
func TestStopEntryStillPlacesWhenTheSlotIsFree(t *testing.T) {
	at := &AutoTrader{id: "hoang"}
	pl := &fakePlacer{}
	led := &fakeLedger{}
	r := store.ArmedOrderDB{
		ID: 1, PlanID: "2026-09-04:NY", Version: 3, Scenario: "S2",
		Side: "SHORT", EntryPx: 29591.02, StopPx: 29650, TargetPx: 29500,
	}
	d := decideStopEntry("SHORT", r.EntryPx, testOffset(), testTick, 29650.00)
	at.placeOneStopEntry(pl, led, r, d, 29650.00, time.Now(), freeSlot())
	if len(pl.calls) != 1 {
		t.Fatalf("a free slot must still place: got %d wire call(s)", len(pl.calls))
	}
}

// An UNVERIFIABLE book refuses too — an unverifiable slot is not an empty slot.
func TestStopEntryRefusedWhenTheBookCannotBeSeen(t *testing.T) {
	at := &AutoTrader{id: "hoang"}
	pl := &fakePlacer{}
	led := &fakeLedger{}
	r := store.ArmedOrderDB{ID: 1, Scenario: "S2", Side: "SHORT", EntryPx: 29591.02}
	d := decideStopEntry("SHORT", r.EntryPx, testOffset(), testTick, 29650.00)

	unseen := adjudicateSlot(nil, false, 0, bookBound, 0, []string{"sig-a"})
	at.placeOneStopEntry(pl, led, r, d, 29650.00, time.Now(), unseen)
	if len(pl.calls) != 0 {
		t.Fatalf("with no broker book the placement must be refused, %d reached the wire", len(pl.calls))
	}
}
