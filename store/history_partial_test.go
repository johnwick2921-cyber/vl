package store

import (
	"path/filepath"
	"testing"
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
