package expectancy

import (
	"testing"
	"time"

	"vl/store"
)

// MATH-EXECUTOR P2-1 — realized R used trade_excursions.size, which the open
// hook stamps at the FIRST partial fill (20 of 21 multi-contract rows stored 1
// or 2 instead of 3 or 5), inflating R 1.5–5×. Row #627 (data.db): 5 MNQ long
// 31237.00, stop 31223.25, pnl_corrected −137.50, excursion size 1 → the code
// read −5.00R; the truth is −137.50 / (13.75 × 5 × $2) = −1.00R.
// Driven through LoadAndBuildAt (the /api/expectancy builder).
func TestRealizedRUsesTheWholePosition(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-10-05:P", 1, "NY")
	entry := msAt(t, 2026, time.October, 5, 9, 0)
	pnl := -137.50
	pos := &store.TraderPosition{
		TraderID: "t1", Symbol: "MNQ", Side: "LONG", Quantity: 5, EntryQuantity: 5,
		EntryPrice: 31237.00, ExitPrice: 31223.25, EntryTime: entry, ExitTime: entry + 600000,
		Status: "CLOSED", CloseReason: "stop", Source: "armed_entry", PnlCorrected: &pnl,
		PlanID: "2026-10-05:P", PlanVersion: 1, PlanSession: "NY", CitedScenarioID: "S2",
		CreatedAt: entry, UpdatedAt: entry,
	}
	if err := db.Create(pos).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.TradeExcursion{PositionID: pos.ID, PlanID: "2026-10-05:P", Version: 1, Session: "NY",
		Scenario: "S2", Side: "LONG", EntryPx: 31237.00, EntryTs: entry, StopPxInitial: 31223.25, Size: 1, Source: "live"}).Error; err != nil {
		t.Fatal(err)
	}
	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.October, 6, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range tbl.Cells {
		if c.N == 1 && c.AvgRealizedR != nil {
			found = true
			closeTo(t, "realized R of a 5-lot −137.50 over a 13.75-pt stop", *c.AvgRealizedR, -1.0)
		}
	}
	if !found {
		t.Fatalf("no cell carried the row's realized R: %+v", tbl.Cells)
	}
}

// R22 R1 — a manual NT8 adoption is not a bot trade: excluded from the model
// and COUNTED in the honesty ledger under its own reason.
func TestManualOriginRowsAreExcludedAndCounted(t *testing.T) {
	db := newFixtureDB(t)
	seedPlan(t, db, "2026-10-05:P", 1, "NY")
	entry := msAt(t, 2026, time.October, 5, 9, 0)
	pnl := -1127.50
	if err := db.Create(&store.TraderPosition{
		TraderID: "t1", Symbol: "MNQ", Side: "SHORT", Quantity: 20, EntryQuantity: 20, EntryPrice: 31208.775,
		EntryTime: entry, ExitTime: entry + 600000, Status: "CLOSED", CloseReason: "manual", Source: "reconcile",
		PnlCorrected: &pnl, PlanID: "2026-10-05:P", PlanVersion: 1, PlanSession: "NY", CitedScenarioID: "S1",
		Origin: store.OriginManual, CreatedAt: entry, UpdatedAt: entry,
	}).Error; err != nil {
		t.Fatal(err)
	}
	tbl, err := LoadAndBuildAt(db, time.Date(2026, time.October, 6, 12, 0, 0, 0, ct(t)))
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Excluded.Manual != 1 {
		t.Fatalf("Excluded.Manual = %d, want 1", tbl.Excluded.Manual)
	}
	for _, c := range tbl.Cells {
		if c.N > 0 {
			t.Fatalf("a manual row reached a cell: %+v", c)
		}
	}
}
