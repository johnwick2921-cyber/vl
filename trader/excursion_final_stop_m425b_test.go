package trader

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// m425b E1: trade_excursions.stop_px_final was ALWAYS the initial stop (689:
// 29058.25 stored vs a 29117.5 real BE exit). It is now the last stop move the
// process sent, the initial stop only when the process watched the whole trade
// and sent no move, and NULL otherwise. Driven through excursionOnClose with a
// real TCPTrader over a loopback server (the BE move goes out the production
// MoveStopForSignalLeg).

func m425bExcRig(t *testing.T) (*AutoTrader, *store.Store, *nttrader.TCPTrader) {
	t.Helper()
	at := mkTrader("ninjatrader", boolp(true), "5m")
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at.store, at.id = st, "t1"
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	c, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	waitAddonRegistered(t, s)
	go func() {
		for {
			if _, err := ntwire.ReadFrame(c); err != nil {
				return
			}
		}
	}()
	nt := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = nt
	return at, st, nt
}

// closeAndRead opens a LONG position + excursion row (entry at entryMs, initial
// stop 29058.25), closes it through excursionOnClose, returns the row.
func m425bCloseRow(t *testing.T, at *AutoTrader, st *store.Store, entryMs int64) *store.TradeExcursion {
	t.Helper()
	pos := &store.TraderPosition{TraderID: "t1", Symbol: "MNQ", Side: "LONG", EntryPrice: 29117.5, EntryQuantity: 1, Quantity: 1, EntryTime: entryMs}
	if err := st.Position().Create(pos); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TradeExcursions().Open(store.TradeExcursion{PositionID: pos.ID, EntryPx: 29117.5, EntryTs: entryMs,
		Side: "LONG", StopPxInitial: 29058.25, Size: 1, Source: "live"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Position().ClosePosition(pos.ID, 29117.5, "", 0, 0, "stop"); err != nil {
		t.Fatal(err)
	}
	closed, _ := st.Position().GetClosedPositions("t1", 1)
	at.excursionOnClose(closed[0])
	row, err := st.TradeExcursions().GetByPosition(pos.ID)
	if err != nil || row == nil {
		t.Fatalf("excursion row: %v %v", row, err)
	}
	return row
}

// MUTANT: restore `StopPxFinal: &row.StopPxInitial` → the BE'd trade reads 29058.25 → RED.
func TestM425bFinalStopIsTheBEMove(t *testing.T) {
	at, st, nt := m425bExcRig(t)
	entry := time.Now().Add(-time.Minute).UnixMilli()
	if err := nt.MoveStopForSignalLeg("sig-689", "LONG", 29117.5, 0); err != nil {
		t.Fatalf("BE move: %v", err)
	}
	row := m425bCloseRow(t, at, st, entry)
	if row.StopPxFinal == nil || *row.StopPxFinal != 29117.5 {
		t.Fatalf("stop_px_final = %v, want the BE move 29117.5 (initial was 29058.25)", row.StopPxFinal)
	}
}

func TestM425bFinalStopInitialWhenWatchedAndNeverMoved(t *testing.T) {
	at, st, _ := m425bExcRig(t)
	time.Sleep(2 * time.Millisecond)
	row := m425bCloseRow(t, at, st, time.Now().UnixMilli())
	if row.StopPxFinal == nil || *row.StopPxFinal != 29058.25 {
		t.Fatalf("a watched trade with no move keeps its initial stop, got %v", row.StopPxFinal)
	}
}

// A trade that opened BEFORE this process built its trader (restart mid-trade)
// cannot be known: NULL, not the initial stop.
func TestM425bFinalStopNullWhenUnknowable(t *testing.T) {
	at, st, _ := m425bExcRig(t)
	row := m425bCloseRow(t, at, st, time.Now().Add(-time.Hour).UnixMilli())
	if row.StopPxFinal != nil {
		t.Fatalf("an unwatched trade must leave stop_px_final NULL, got %v", *row.StopPxFinal)
	}
}
