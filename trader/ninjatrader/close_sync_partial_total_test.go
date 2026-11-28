package ninjatrader

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// TestRecordClose_StampsTrueTotalAfterBookedPartial pins M251-S2 #1 at the
// production call site: +10 partial via ApplyNT8Exit, then the legacy recordClose
// closes the last lot at -20. The builder writes realized = -10; pnl_corrected
// must be the same TRUE total (-10), not the leg (-20).
// MUTANT: stamp realizedPnL (the leg) instead of owner.RealizedPnL+realizedPnL.
func TestRecordClose_StampsTrueTotalAfterBookedPartial(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "partial.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	nowMs := time.Now().UTC().UnixMilli()
	row := &store.TraderPosition{
		TraderID: "trader-A", ExchangeID: "ex", ExchangeType: "ninjatrader",
		ExchangePositionID: "pos-1", Symbol: "MNQ", Side: "LONG",
		Quantity: 2, EntryQuantity: 2, EntryPrice: 100, EntryTime: nowMs - 400_000,
		EntryOrderID: "entry-1", Leverage: 1, Status: "OPEN", Source: "armed_entry",
		Account: "Sim101", CreatedAt: nowMs - 400_000, UpdatedAt: nowMs - 400_000,
	}
	if err := st.Position().CreateOpenPosition(row); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Position().ApplyNT8Exit(store.NT8ExitReceipt{
		ID: "r1", Account: "Sim101", Symbol: "MNQ", Side: "LONG", SignalID: "entry-1",
		TraderID: "trader-A", ExchangeID: "ex", ExchangeType: "ninjatrader", Reason: "tp",
		Quantity: 1, Price: 105, PointValue: 2,
		ExitMs: row.EntryTime + 300_000, ReceivedMs: row.EntryTime + 301_000,
	}); err != nil {
		t.Fatalf("partial: %v", err)
	}

	s := ntwire.NewTCPServer(nil)
	tr := NewTCPTrader(s, "MNQ", "Sim101")
	pb := store.NewPositionBuilder(st.Position())
	tr.recordClose("trader-A", "ex", "ninjatrader", st, pb, ntwire.PositionClosePayload{
		SignalID: "z", Symbol: "MNQ", PositionSide: "long", ExitPrice: 90,
		Quantity: 1, ExitReason: "sl", Account: "Sim101",
	})

	closed, err := st.Position().GetClosedPositions("trader-A", 10)
	if err != nil || len(closed) != 1 {
		t.Fatalf("want 1 closed row, got %d (err=%v)", len(closed), err)
	}
	if math.Abs(closed[0].RealizedPnL-(-10)) > 0.01 {
		t.Fatalf("realized_pnl = %.2f, want -10", closed[0].RealizedPnL)
	}
	if closed[0].PnlCorrected == nil || math.Abs(*closed[0].PnlCorrected-(-10)) > 0.01 {
		t.Fatalf("pnl_corrected = %v, want -10 (true total), not the -20 leg", closed[0].PnlCorrected)
	}
}
