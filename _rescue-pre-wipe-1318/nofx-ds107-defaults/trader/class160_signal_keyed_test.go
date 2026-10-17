package trader

import (
	"context"
	"net"
	"testing"
	"time"

	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	nttrader "nofx/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0 (d) — CLASS 160: A QUEUED SEND IS NOT A FILL ─────────────
//
// recordAndConfirmOrder polled GetOrderStatus for ~3 s and, with no match,
// recorded an OPEN trader_positions row at the MARK plus a P0 "Filled" alert —
// for an entry that was only queued. The probe work for W0 found it wider:
//   - a REJECTED AI entry was recorded OPEN too (the REJECTED branch of
//     GetOrderStatus is unreachable for NT8);
//   - GetOrderStatus correlates on the ONE shared lastEntrySignalID, so an
//     armed/Picture fill landing inside the AI's poll became the AI's fill;
//   - placeEntry returns no "orderId", so every AI order row was keyed "<nil>"
//     and CreateOrder's dedupe collapsed them all onto one trader_orders row.
//
// The fix keys everything on the signal id the entry itself returns: the order
// row, and the fill/reject evidence. A position is recorded ONLY on a received
// fill for THAT signal.

type aiEntryWire struct {
	at   *AutoTrader
	st   *store.Store
	s    *ntwire.TCPServer
	nt   *nttrader.TCPTrader
	conn net.Conn
}

func newAIEntryWire(t *testing.T) *aiEntryWire {
	t.Helper()
	withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	at.id = "class160-trader"
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	waitAddonRegistered(t, s) // CTO M7: the producer must not race the accept
	t.Cleanup(func() { _ = conn.Close() })
	go func() { // drain everything the server writes
		for {
			if _, err := ntwire.ReadFrame(conn); err != nil {
				return
			}
		}
	}()
	top := ntwire.MinAddonBuildPictureHtf
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(s.FarSideBuildID(), top) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	nt := nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = nt
	at.config.NinjaTraderSymbol = "MNQ"
	_ = nt.SetStopLoss("MNQ", "LONG", 1, 28950)
	_ = nt.SetTakeProfit("MNQ", "LONG", 1, 29100)
	_ = nt.SetStopLoss("MNQ", "LONG", 2, 28950)
	_ = nt.SetTakeProfit("MNQ", "LONG", 2, 29100)
	return &aiEntryWire{at: at, st: st, s: s, nt: nt, conn: conn}
}

// fill writes a fill frame for signal sid as the AddOn would, and waits until
// the TCPTrader has consumed it.
func (w *aiEntryWire) fill(t *testing.T, sid, status string, px float64) {
	t.Helper()
	if err := ntwire.WriteFrame(w.conn, ntwire.FrameFill, ntwire.FillPayload{
		SignalID: sid, Symbol: "MNQ", Account: "Sim101", Side: "long", Quantity: 1,
		FillPrice: px, Status: status, FillTime: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
}

func (w *aiEntryWire) openAI(t *testing.T, qty float64) string {
	t.Helper()
	order, err := w.nt.OpenLong("MNQ", qty, 1)
	if err != nil {
		t.Fatalf("fixture: AI entry refused: %v", err)
	}
	sid, _ := order["signal_id"].(string)
	if sid == "" {
		t.Fatalf("fixture: the entry must return its signal id: %+v", order)
	}
	return sid
}

func (w *aiEntryWire) record(t *testing.T, sid string, qty float64) {
	t.Helper()
	// The production caller, as executeOpenLongWithRecord runs it.
	w.at.recordAndConfirmOrder(map[string]interface{}{"status": "submitted", "signal_id": sid, "symbol": "MNQ", "side": "long", "quantity": qty},
		"MNQ", "open_long", qty, 29000, 1, 0, 70)
}

func (w *aiEntryWire) openPositions(t *testing.T) []*store.TraderPosition {
	t.Helper()
	open, err := w.st.Position().GetOpenPositions(w.at.id)
	if err != nil {
		t.Fatal(err)
	}
	return open
}

func (w *aiEntryWire) orderFor(t *testing.T, sid string) *store.TraderOrder {
	t.Helper()
	o, err := w.st.Order().GetOrderByExchangeID(w.at.exchangeID, sid)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func (w *aiEntryWire) fillAlerts(t *testing.T) int {
	t.Helper()
	rows, err := w.st.Alert().List(w.at.id, 50)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.Kind == "fill" {
			n++
		}
	}
	return n
}

// A queued/unfilled AI entry records NO position at the mark and NO fill alert.
// Its order row is keyed by the signal and stays unresolved (NEW).
func TestQueuedAIEntryWithNoFillRecordsNoPosition(t *testing.T) {
	w := newAIEntryWire(t)
	sid := w.openAI(t, 1)
	w.record(t, sid, 1)
	if open := w.openPositions(t); len(open) != 0 {
		t.Fatalf("CLASS 160: an unfilled entry must record no position, got %+v", open[0])
	}
	if n := w.fillAlerts(t); n != 0 {
		t.Fatalf("CLASS 160: no fill evidence → no 'Filled' alert, got %d", n)
	}
	o := w.orderFor(t, sid)
	if o == nil || o.Status != "NEW" {
		t.Fatalf("the order row must be keyed by signal %s and stay unresolved (NEW): %+v", sid, o)
	}
}

// A REJECTED AI entry records no position, no fill alert, and a REJECTED
// order row.
func TestRejectedAIEntryRecordsNoPosition(t *testing.T) {
	w := newAIEntryWire(t)
	sid := w.openAI(t, 1)
	w.fill(t, sid, "rejected", 0)
	w.record(t, sid, 1)
	if open := w.openPositions(t); len(open) != 0 {
		t.Fatalf("a rejected entry must record no position, got %+v", open[0])
	}
	if n := w.fillAlerts(t); n != 0 {
		t.Fatalf("a rejected entry must raise no 'Filled' alert, got %d", n)
	}
	if o := w.orderFor(t, sid); o == nil || o.Status != "REJECTED" {
		t.Fatalf("the order row must read REJECTED: %+v", o)
	}
}

// A filled AI entry records the position at the REAL fill price, linked to
// its own signal.
func TestFilledAIEntryRecordsThePositionAtItsFill(t *testing.T) {
	w := newAIEntryWire(t)
	sid := w.openAI(t, 1)
	w.fill(t, sid, "filled", 29012.25)
	w.record(t, sid, 1)
	open := w.openPositions(t)
	if len(open) != 1 {
		t.Fatalf("a filled entry records exactly one position, got %d", len(open))
	}
	if open[0].EntryPrice != 29012.25 || open[0].EntryOrderID != sid {
		t.Fatalf("the position must carry the real fill (29012.25) and its own signal %s: entry=%.2f order=%q", sid, open[0].EntryPrice, open[0].EntryOrderID)
	}
	if o := w.orderFor(t, sid); o == nil || o.Status != "FILLED" {
		t.Fatalf("the order row must read FILLED: %+v", o)
	}
}

// Another path's fill landing inside the AI's poll is NOT the AI's fill.
func TestAnotherPathsFillIsNeverTheAIsFill(t *testing.T) {
	w := newAIEntryWire(t)
	sid := w.openAI(t, 1)
	other, err := w.nt.PlaceLimitEntry("MNQ", "long", 1, 29070, 29050, 29120)
	if err != nil {
		t.Fatalf("fixture: the armed limit must send: %v", err)
	}
	w.fill(t, other, "filled", 29077.25)
	w.record(t, sid, 1)
	for _, p := range w.openPositions(t) {
		if p.EntryOrderID == sid || p.EntryPrice == 29077.25 {
			t.Fatalf("the AI recorded another path's fill as its own: %+v", p)
		}
	}
}

// Two AI entries are two order rows (no "<nil>" collapse).
func TestTwoAIEntriesAreTwoOrderRows(t *testing.T) {
	w := newAIEntryWire(t)
	a := w.openAI(t, 1)
	w.record(t, a, 1)
	b := w.openAI(t, 2)
	w.record(t, b, 2)
	if a == b {
		t.Fatal("fixture: two entries must carry two signals")
	}
	oa, ob := w.orderFor(t, a), w.orderFor(t, b)
	if oa == nil || ob == nil || oa.ID == ob.ID {
		t.Fatalf("each AI entry must have its own order row: %+v / %+v", oa, ob)
	}
}
