package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// N7 part 2 — the armed pass settles cancels BEFORE placing, so a cancel_pending
// row left by a previous pass's cancel does not hold the next entry's one-entry
// latch as "ledger_open". Without the pre-loop settlement the placement loop
// runs first and the new arm is refused on the still-pending cancel, then the
// cancel settles AFTER the placement opportunity has passed.
//
// The fixture drives the real call site (maybeManageArmedOrdersAt over the real
// TCP wire, canon 53): one prior entry is cancel_pending with its signal id
// still on file, a fresh persisted flat book proves the order is gone, and a new
// plan arm must place in the SAME pass.
func TestArmedPassSettlesCancelsBeforePlacing(t *testing.T) {
	r := newZoneRig(t, "n7-settle-first", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	ntTCP := r.at.trader.(*ntTrader.TCPTrader)
	wireNT8EntryLatch(r.at, ntTCP)
	// The rig runs on a frozen clock (2026-09-11) while production wires the
	// latch's Now to the wall clock. Re-wire the same sources with the rig's
	// clock so the ≤60s book-age bound judges against the same clock the
	// snapshots are written on.
	ntTCP.SetEntryLatchSource(&ntTrader.EntryLatchSource{
		Book:    r.at.entryLatchBook,
		Ledgers: r.at.entryLatchLedgers,
		Now:     func() time.Time { return r.now },
	})

	// The prior pass's entry, now cancel_pending with its signal id still on
	// file (a cancel never clears the broker's pending map on its own).
	ledger := r.st.ArmedOrders()
	rowA := &store.ArmedOrderDB{
		TraderID: r.at.id, PlanID: "n7-prior-plan", Version: 1, Session: "NY",
		Scenario: "S0-prior", Side: "long", EntryPx: 100, StopPx: 98, TargetPx: 110,
		State: store.StateArmed,
	}
	if err := ledger.UpsertArm(rowA); err != nil {
		t.Fatal(err)
	}
	_ = ledger.SetState(rowA.ID, store.StateWorking, "")
	_ = ledger.SetSignal(rowA.ID, "sig-prior")
	if err := ledger.RequestCancel(rowA.ID, "gate changed", r.now.Add(-10*time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}

	// A fresh persisted flat book proves the prior order is gone (settlement
	// evidence), and a flat live book admits the contract guard.
	r.persistFlat(r.now)
	r.flatBook(r.now)
	r.setTape(zoneTape(101.95, r.now, 0))

	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("the new arm must place in the SAME pass that settles the prior cancel; got %d signal(s): %+v", len(sigs), sigs)
	}
	limitOnly(t, sigs)
}
