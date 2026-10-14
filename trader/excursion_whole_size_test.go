package trader

import (
	"path/filepath"
	"testing"
	"time"

	"vl/store"
)

// MATH-EXECUTOR P2-1 — the open hook stamps trade_excursions.size at the FIRST
// partial fill (1 of a 5-lot). The close hook now stamps the WHOLE position,
// so the stored column stops lying. Driven through excursionOnClose.
func TestExcursionCloseStampsTheWholePositionSize(t *testing.T) {
	at := mkTrader("ninjatrader", boolp(true), "5m")
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at.store = st
	at.id = "t1"
	pos := &store.TraderPosition{TraderID: "t1", Symbol: "MNQ", Side: "LONG", EntryPrice: 31237, EntryQuantity: 5,
		Quantity: 5, EntryTime: time.Now().Add(-40 * time.Minute).UnixMilli()}
	if err := st.Position().Create(pos); err != nil {
		t.Fatalf("create pos: %v", err)
	}
	if _, err := st.TradeExcursions().Open(store.TradeExcursion{PositionID: pos.ID, EntryPx: 31237, EntryTs: pos.EntryTime,
		Side: "LONG", StopPxInitial: 31223.25, Size: 1, Source: "live"}); err != nil {
		t.Fatalf("open excursion: %v", err)
	}
	if _, err := st.Position().ClosePosition(pos.ID, 31223.25, "", -137.5, 0, "stop"); err != nil {
		t.Fatalf("close pos: %v", err)
	}
	closed, _ := st.Position().GetClosedPositions("t1", 1)
	at.excursionOnClose(closed[0])
	row, err := st.TradeExcursions().GetByPosition(pos.ID)
	if err != nil || row == nil {
		t.Fatalf("excursion row: %v %v", row, err)
	}
	if row.Size != 5 {
		t.Fatalf("excursion size = %.0f after close, want 5 (the whole position, not the first partial)", row.Size)
	}
}
