package store

import (
	"testing"
	"time"
)

// P3-1 (release #26) — a close whose futures point value is UNKNOWN must not
// fabricate a P&L. The old close_sync.go wrote `if pv <= 0 { pv = 1 }`, so an
// unknown root closed with price-delta × qty × 1 as if it were dollars.
// ProcessTradeCloseUnresolvedPnl records the close with realized_pnl 0 and
// pnl_corrected NULL — the corrected-column law (canon 40) reads NULL as
// UNRESOLVED (excluded from every sum, counted).
func TestProcessTradeCloseUnresolvedPnlLeavesPnlUnknown(t *testing.T) {
	st := exitCauseStore(t)
	pb := NewPositionBuilder(st.Position())
	now := time.Now().UnixMilli()

	if err := pb.ProcessTrade("hoang", "nt8", "futures", "MNQ", "SHORT", "open_short",
		1, 29285, 0, 0, now, "sig-open"); err != nil {
		t.Fatal(err)
	}
	// SHORT close at 29355: a price-delta recompute would be (29285−29355)×1 =
	// −70 — the exact figure the old pv=1 fallback would have fabricated.
	if err := pb.ProcessTradeCloseUnresolvedPnl("hoang", "nt8", "futures", "MNQ", "SHORT", "close_short",
		1, 29355, 0, now+1000, "sig-exit", "sl"); err != nil {
		t.Fatal(err)
	}

	rows, err := st.Position().GetClosedPositions("hoang", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected exactly one closed row, got %d (%v)", len(rows), err)
	}
	r := rows[0]
	if r.RealizedPnL != 0 {
		t.Errorf("unresolved close must not fabricate a P&L: realized_pnl = %.2f, want 0", r.RealizedPnL)
	}
	if r.PnlCorrected != nil {
		t.Errorf("unresolved close must leave pnl_corrected NULL (UNRESOLVED), got %v", *r.PnlCorrected)
	}
	if r.Status != "CLOSED" {
		t.Errorf("status = %q, want CLOSED", r.Status)
	}
}
