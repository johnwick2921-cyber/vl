package ninjatrader

import (
	"path/filepath"
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// TestRecordClose_UnknownPointValueLeavesPnlUnresolved is the P3-1 CALL-SITE pin
// (CTO fold on #541, finding F2): the store method alone was pinned, so the
// mutant `pnlKnown := pv > 0 || pv <= 0` (always true) at close_sync.go:174
// stayed green — an unknown root went back to the fabricated price-delta P&L and
// a stamped pnl_corrected. This test drives the PRODUCTION recordClose with a
// root market.FuturesPointValue does not know ("XYZ") and asserts the row closes
// UNRESOLVED: pnl_corrected NULL, realized_pnl 0, and the close_pnl_unresolved
// event counted once.
func TestRecordClose_UnknownPointValueLeavesPnlUnresolved(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "unkpv.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	const ownerID = "trader-A"
	// Owner A holds an open SHORT on a root with no point value ("XYZ" is not in
	// market.futuresPointValues → market.FuturesPointValue returns 0).
	if err := st.Position().Create(&store.TraderPosition{
		TraderID: ownerID, Account: "Sim101", Symbol: "XYZ", Side: "SHORT",
		Quantity: 1, EntryQuantity: 1, EntryPrice: 29804.50, EntryTime: 1, Status: "OPEN",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "XYZ", "Sim101")
	pb := store.NewPositionBuilder(st.Position())
	before := counterValue(telemetry.TraderEventsTotal.WithLabelValues(ownerID, "close_pnl_unresolved"))

	tr.recordClose(ownerID, "ex", "ninjatrader", st, pb, ntwire.PositionClosePayload{
		SignalID: "z", Symbol: "XYZ", PositionSide: "short", ExitPrice: 29718.00,
		Quantity: 1, ExitReason: "sl", Account: "Sim101",
	})

	if open, _ := st.Position().GetOpenPositionByAccountSymbol("Sim101", "XYZ", "SHORT"); open != nil {
		t.Fatalf("row should be CLOSED after recordClose; still open id=%d", open.ID)
	}
	closed, err := st.Position().GetClosedPositions(ownerID, 10)
	if err != nil || len(closed) != 1 {
		t.Fatalf("want 1 closed row for owner, got %d (err=%v)", len(closed), err)
	}
	if closed[0].PnlCorrected != nil {
		t.Errorf("pnl_corrected must stay NULL (UNRESOLVED) for an unknown root, got %v", *closed[0].PnlCorrected)
	}
	if closed[0].RealizedPnL != 0 {
		t.Errorf("realized_pnl = %.2f, want 0 (no fabricated point value)", closed[0].RealizedPnL)
	}
	if delta := counterValue(telemetry.TraderEventsTotal.WithLabelValues(ownerID, "close_pnl_unresolved")) - before; delta != 1 {
		t.Errorf("close_pnl_unresolved event delta = %v, want 1", delta)
	}
}
