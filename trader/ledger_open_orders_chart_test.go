package trader

import (
	"testing"
	"time"

	"vl/store"
)

// T5 (release #22, UI-GUIDE-TRUTH): /api/open-orders → ledgerOpenOrders served
// every ledger row with Quantity 1, so a working 3-lot mentor stop-entry (row
// 362: entry 30742.75 / stop 30760.25 / contracts 3) reached the chart as a
// 1-lot. The row's own signed size is the truth; Price is the ENTRY, StopPrice
// the bracket stop, and the target / planner zone ride along for the chart.
// Mutant: restore `Quantity: 1` → RED.
func TestLedgerOpenOrdersCarryTheRowsOwnSizeAndEntry(t *testing.T) {
	at := class33Trader(t)
	now := time.Now()
	three := 3
	lo, hi := 30740.0, 30745.0
	mentorRow := &store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "NY", Scenario: "mentor-362",
		Side: "short", Kind: "stop_entry", EntryPx: 30742.75, StopPx: 30760.25, TargetPx: 30700,
		State: store.StateWorking, SignalID: "sig-362", Contracts: &three, CreatedAt: now, UpdatedAt: now,
	}
	plannerRow := &store.ArmedOrderDB{
		TraderID: at.id, PlanID: "p1", Version: 1, Session: "NY", Scenario: "S1",
		Side: "long", Kind: "limit", EntryPx: 30800, StopPx: 30780, TargetPx: 30850, ZoneLo: &lo, ZoneHi: &hi,
		State: store.StateWorking, SignalID: "sig-s1", CreatedAt: now, UpdatedAt: now,
	}
	for _, r := range []*store.ArmedOrderDB{mentorRow, plannerRow} {
		if err := at.store.ArmedOrders().UpsertArm(r); err != nil {
			t.Fatalf("seed %s: %v", r.Scenario, err)
		}
	}
	orders, err := at.ledgerOpenOrders("MNQ")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("want 2 ledger orders, got %+v", orders)
	}
	for _, o := range orders {
		switch o.OrderID {
		case "sig-362":
			if o.Quantity != 3 || o.Price != 30742.75 || o.StopPrice != 30760.25 || o.TargetPrice != 30700 || o.Type != "STOP_MARKET" {
				t.Errorf("mentor stop-entry = %+v, want qty 3 entry 30742.75 stop 30760.25 target 30700", o)
			}
		case "sig-s1":
			if o.Quantity != 1 {
				t.Errorf("planner row with NULL contracts = qty %v, want the 1 it always rendered", o.Quantity)
			}
			if o.ZoneLo == nil || o.ZoneHi == nil || *o.ZoneLo != lo || *o.ZoneHi != hi {
				t.Errorf("planner zone = %v/%v, want %v/%v", o.ZoneLo, o.ZoneHi, lo, hi)
			}
		default:
			t.Errorf("unexpected order %+v", o)
		}
	}
}
