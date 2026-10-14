package trader

import (
	"testing"
	"time"

	"vl/store"
)

// T6 (UI-GUIDE-TRUTH, class 28) — /api/open-orders flipped every SHORT arm to
// BUY/LONG: ledgerOpenOrders compared r.Side == "short" while UpsertArm
// canonicalizes the side to UPPER at the write (data.db 10-14: SHORT 138 /
// short 18 / LONG 151 / long 19; row 363, a SHORT limit, was served BUY).
// Driven through the PRODUCTION renderer behind /api/open-orders.
func TestLedgerOpenOrdersKeepsShortShort(t *testing.T) {
	at := class33Trader(t)
	now := time.Now()
	for _, r := range []struct{ scenario, side string }{{"S1", "short"}, {"S2", "long"}} {
		row := &store.ArmedOrderDB{TraderID: at.id, PlanID: "p1", Version: 1, Session: "ASIA", Scenario: r.scenario,
			Side: r.side, EntryPx: 30815, StopPx: 30840, TargetPx: 30760, State: "working", SignalID: "sig-" + r.scenario,
			CreatedAt: now, UpdatedAt: now}
		if err := at.store.ArmedOrders().UpsertArm(row); err != nil {
			t.Fatal(err)
		}
	}
	// A legacy pre-canonical row (the 18 lowercase "short" rows) written raw.
	legacy := &store.ArmedOrderDB{TraderID: at.id, PlanID: "p0", Version: 1, Session: "NY", Scenario: "S9",
		Side: "short", EntryPx: 30900, StopPx: 30920, TargetPx: 30850, State: "working", SignalID: "sig-legacy",
		CreatedAt: now, UpdatedAt: now}
	if err := at.store.ArmedOrders().DB().Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	orders, err := at.ledgerOpenOrders("MNQ")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 3 {
		t.Fatalf("want 3 open orders, got %d", len(orders))
	}
	for _, o := range orders {
		switch o.OrderID {
		case "sig-S1", "sig-legacy":
			if o.Side != "SELL" || o.PositionSide != "SHORT" {
				t.Errorf("%s is a SHORT arm but rendered side=%s position_side=%s", o.OrderID, o.Side, o.PositionSide)
			}
		case "sig-S2":
			if o.Side != "BUY" || o.PositionSide != "LONG" {
				t.Errorf("%s is a LONG arm but rendered side=%s position_side=%s", o.OrderID, o.Side, o.PositionSide)
			}
		}
	}
}
