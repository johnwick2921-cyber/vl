package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestGetHistoryPositionsIncludesOpenPartial pins m232: the trade-history list
// must INCLUDE an open row that booked a partial leg (realized_pnl != 0), while
// GetClosedPositions stays CLOSED-only. MUTANT: filter GetHistoryPositions to
// status=CLOSED → the open partial vanishes → RED.
func TestGetHistoryPositionsIncludesOpenPartial(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "hist-partial.db"))
	if err != nil {
		t.Fatal(err)
	}
	row := openNT8Row(t, st, 2)
	if _, err := st.Position().ApplyNT8Exit(receiptFor(row, 1, 105, 2)); err != nil {
		t.Fatalf("partial exit: %v", err)
	}

	history, err := st.Position().GetHistoryPositions("t1", 10)
	if err != nil {
		t.Fatalf("GetHistoryPositions: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history must include the open partial row, got %d", len(history))
	}
	h := history[0]
	if h.Status != "OPEN" || h.PnlCorrected == nil || *h.PnlCorrected != 10 {
		t.Fatalf("the open partial must be stamped: status=%s pnl=%v", h.Status, h.PnlCorrected)
	}

	closed, err := st.Position().GetClosedPositions("t1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 0 {
		t.Fatalf("GetClosedPositions must stay CLOSED-only, got %d row(s)", len(closed))
	}
}

// TestClosePositionResetsStalePartialStamp pins m240 fix 1: a partial leg stamps
// pnl_corrected=+10, then a reconcile close of the residual overwrites the row
// (realized=-20) — pnl_corrected must be NULL (UNRESOLVED), never the stale +10.
// MUTANT: drop the pnl_corrected reset in ClosePosition → the stale +10 survives
// → RED.
func TestClosePositionResetsStalePartialStamp(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "close-reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	row := openNT8Row(t, st, 2)
	if _, err := st.Position().ApplyNT8Exit(receiptFor(row, 1, 105, 2)); err != nil {
		t.Fatalf("partial exit: %v", err)
	}
	// reconcile close of the residual at a loss
	if _, err := st.Position().ClosePosition(row.ID, 95, "reconcile", -20, 0, "reconcile_priced"); err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
	var after TraderPosition
	if err := st.GormDB().First(&after, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.PnlCorrected != nil {
		t.Fatalf("a reconcile close must reset pnl_corrected to NULL (stale partial), got %v", *after.PnlCorrected)
	}
}

// TestClosePositionUnresolvedResetsPartialStamp pins m240 fix 1 (unresolved path).
func TestClosePositionUnresolvedResetsPartialStamp(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "unresolved-reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	row := openNT8Row(t, st, 2)
	if _, err := st.Position().ApplyNT8Exit(receiptFor(row, 1, 105, 2)); err != nil {
		t.Fatalf("partial exit: %v", err)
	}
	if _, err := st.Position().ClosePositionUnresolved(row.ID, "no frame"); err != nil {
		t.Fatalf("ClosePositionUnresolved: %v", err)
	}
	var after TraderPosition
	if err := st.GormDB().First(&after, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.PnlCorrected != nil {
		t.Fatalf("an unresolved close must reset pnl_corrected to NULL, got %v", *after.PnlCorrected)
	}
}

// TestGetHistoryPositionsOpenPartialSortsByActivity pins m240 fix 2: an open
// partial (exit_time=0) must sort by updated_at, not fall to the end. Seed more
// closed rows than the limit so the partial would vanish under the old ORDER BY.
func TestGetHistoryPositionsOpenPartialSortsByActivity(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "hist-order.db"))
	if err != nil {
		t.Fatal(err)
	}
	// One open partial, updated recently.
	row := openNT8Row(t, st, 2)
	if _, err := st.Position().ApplyNT8Exit(receiptFor(row, 1, 105, 2)); err != nil {
		t.Fatalf("partial exit: %v", err)
	}
	// Seed 5 closed rows, older (exit_time in the past).
	for i := 0; i < 5; i++ {
		closed := &TraderPosition{
			TraderID: "t1", Account: "Sim101", Symbol: "MNQ", Side: "LONG",
			Quantity: 1, EntryQuantity: 1, EntryPrice: 100, Status: "CLOSED",
			RealizedPnL: 1, ExitPrice: 101, ExitTime: time.Now().UTC().Add(-time.Duration(10+i) * time.Minute).UnixMilli(),
			EntryTime: time.Now().UTC().Add(-2 * time.Hour).UnixMilli(),
			CreatedAt: time.Now().UTC().Add(-2 * time.Hour).UnixMilli(),
		}
		pc := 1.0
		closed.PnlCorrected = &pc
		if err := st.GormDB().Create(closed).Error; err != nil {
			t.Fatal(err)
		}
	}
	history, err := st.Position().GetHistoryPositions("t1", 3) // limit < closed count
	if err != nil {
		t.Fatalf("GetHistoryPositions: %v", err)
	}
	found := false
	for _, h := range history {
		if h.Status == "OPEN" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the open partial must sort by its recent activity and appear within the limit, got %d rows", len(history))
	}
}
