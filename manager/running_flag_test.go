package manager

import (
	"errors"
	"path/filepath"
	"testing"

	"vl/store"
	"vl/trader"
)

// TestAutoStartRunErrorDoesNotClearRunningOnDuplicate is the M3-killer: the
// auto-start must NOT write is_running=0 when Run returns
// ErrTraderAlreadyRunning (a duplicate self-start over a live instance); any
// other Run error still clears it. M3 (write 0 on the sentinel anyway) is RED.
func TestAutoStartRunErrorDoesNotClearRunningOnDuplicate(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "auto-start.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Trader().Create(&store.Trader{ID: "t2", UserID: "u", Name: "trader-2", AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: "Sim101", IsRunning: true}); err != nil {
		t.Fatal(err)
	}

	tm := NewTraderManager()

	// Duplicate self-start: the flag must stay true.
	tm.handleAutoStartRunError(st, "t2", "u", "trader-2", trader.ErrTraderAlreadyRunning)
	row, _ := st.Trader().GetByID("t2")
	if row == nil || !row.IsRunning {
		t.Fatalf("ErrTraderAlreadyRunning must NOT clear is_running, got %+v", row)
	}

	// A real Run error: the flag must clear, as before.
	tm.handleAutoStartRunError(st, "t2", "u", "trader-2", errors.New("grid initialization failed"))
	row, _ = st.Trader().GetByID("t2")
	if row == nil || row.IsRunning {
		t.Fatalf("a real Run error must clear is_running, got %+v", row)
	}
}
