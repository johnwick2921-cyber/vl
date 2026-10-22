package store

import (
	"path/filepath"
	"testing"
)

// TestReducePositionQuantityAutoCloseResetsPnlCorrected pins M251-S2 #2: the
// auto-close branch changes realized_pnl, so a stale pnl_corrected must be reset
// to NULL (UNRESOLVED). MUTANT: drop pnl_corrected from the auto-close update map.
func TestReducePositionQuantityAutoCloseResetsPnlCorrected(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "autoclose.db"))
	if err != nil {
		t.Fatal(err)
	}
	row := openNT8Row(t, st, 2)
	if _, err := st.Position().ApplyNT8Exit(receiptFor(row, 1, 105, 2)); err != nil {
		t.Fatalf("partial: %v", err)
	}
	var mid TraderPosition
	if err := st.gdb.First(&mid, row.ID).Error; err != nil || mid.PnlCorrected == nil {
		t.Fatalf("precondition: partial must stamp pnl_corrected, got %v err=%v", mid.PnlCorrected, err)
	}
	if err := st.Position().ReducePositionQuantity(row.ID, 1, 95, 0, -10, "sl"); err != nil {
		t.Fatal(err)
	}
	var got TraderPosition
	if err := st.gdb.First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "CLOSED" {
		t.Fatalf("status = %s, want CLOSED", got.Status)
	}
	if got.PnlCorrected != nil {
		t.Fatalf("pnl_corrected = %v, want NULL after auto-close", *got.PnlCorrected)
	}
}
