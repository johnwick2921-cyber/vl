package trader

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── EXEC-FOLLOWUPS P2-1 (release #22 rescan) ─────────────────────────────────
//
// The protection net re-placed the ORIGINAL plan stop after an auto-BE move.
// Live shape, position 654: SHORT filled 30456.50 (row 367), plan stop
// 30551.25, auto-BE moved the live stop to 30456.50 at 10:08:27 (snapshot
// 112688). Had its -sl vanished, the net would have placed 30551.25 —
// 94.75 pts / $189.50 of re-opened risk.

// execWire is a real TCPServer with a dialed AddOn socket that RECORDS every
// frame the server writes, so a test asserts on the wire, not on state.
type execWire struct {
	at   *AutoTrader
	st   *store.Store
	s    *ntwire.TCPServer
	nt   *nttrader.TCPTrader
	conn net.Conn

	mu     sync.Mutex
	frames []ntwire.Envelope
}

func newExecWire(t *testing.T, traderID string) *execWire {
	t.Helper()
	withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	at.id = traderID
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	s.SeedAccountBalanceForTest("Sim101", ntwire.AccountBalancePayload{Account: "Sim101", NetLiquidation: 100000, CashValue: 100000, BuyingPower: 100000})
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	waitAddonRegistered(t, s)
	t.Cleanup(func() { _ = conn.Close() })
	w := &execWire{at: at, st: st, s: s, conn: conn}
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			w.mu.Lock()
			w.frames = append(w.frames, env)
			w.mu.Unlock()
		}
	}()
	top := ntwire.MinAddonBuildPictureHtf
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(s.FarSideBuildID(), top) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	w.nt = nttrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = w.nt
	at.config.NinjaTraderSymbol = "MNQ"
	return w
}

// framesOf returns the payloads of every recorded frame of type ft, waiting up
// to d for at least min of them.
func (w *execWire) framesOf(ft ntwire.FrameType, min int, d time.Duration) []json.RawMessage {
	deadline := time.Now().Add(d)
	for {
		w.mu.Lock()
		var out []json.RawMessage
		for _, e := range w.frames {
			if e.Type == ft {
				out = append(out, e.Payload)
			}
		}
		w.mu.Unlock()
		if len(out) >= min || time.Now().After(deadline) {
			return out
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// open654 builds the 654 shape at the PRODUCTION call sites: the armed SHORT
// limit is sent through PlaceLimitEntry (plan stop 30551.25), the row fills, the
// broker holds the short, and the OPEN position row carries the entry signal.
func (w *execWire) open654(t *testing.T) string {
	t.Helper()
	led := w.st.ArmedOrders()
	r := &store.ArmedOrderDB{TraderID: w.at.id, PlanID: "2026-10-14:NY:" + w.at.id, Session: "NY", Scenario: "S1", Version: 1,
		State: "armed", Side: "SHORT", EntryPx: 30456.50, StopPx: 30551.25, TargetPx: 30257.50}
	if err := led.UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	sid, err := w.nt.PlaceLimitEntry("MNQ", "short", 1, r.EntryPx, r.StopPx, r.TargetPx, func(sid string) error {
		return led.BeginPlacement(r.ID, sid)
	})
	if err != nil {
		t.Fatalf("fixture: the 654 limit entry was refused: %v", err)
	}
	if err := led.SetState(r.ID, store.StateFilled, "fill@30456.50"); err != nil {
		t.Fatal(err)
	}
	if err := w.st.Position().Create(&store.TraderPosition{
		TraderID: w.at.id, Symbol: "MNQ", Side: "SHORT", Quantity: 1, EntryPrice: 30456.50,
		EntryTime: time.Now().UTC().UnixMilli(), Status: "OPEN", EntryOrderID: sid, Account: "Sim101",
	}); err != nil {
		t.Fatal(err)
	}
	w.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "short", Quantity: 1, AvgPrice: 30456.50}})
	return sid
}

// bookWithout puts a FRESH broker book carrying only the -tp: the -sl is gone.
func (w *execWire) bookWithout(sid string, now time.Time) {
	w.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{{
		OrderID: "tp1", Symbol: "MNQ", Name: sid + "-tp", Action: "buy", Type: "limit",
		LimitPrice: 30257.50, Quantity: 1, State: "Working",
	}}}, now)
}

func (w *execWire) placedStops(t *testing.T, want int) []ntwire.PlaceProtectiveStopPayload {
	t.Helper()
	var out []ntwire.PlaceProtectiveStopPayload
	for _, raw := range w.framesOf(ntwire.FramePlaceProtectiveStop, want, 2*time.Second) {
		var p ntwire.PlaceProtectiveStopPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func noFreshPrice(t *testing.T) {
	t.Helper()
	orig := market.FuturesBarsProvider
	market.FuturesBarsProvider = nil // no fresh last price: the net places, never flattens
	t.Cleanup(func() { market.FuturesBarsProvider = orig })
}

// TestProtectionNetNeverReWidensAMovedStop654 — the production chain:
// PlaceLimitEntry → planner auto-BE (maybeMoveStopToBreakeven → moveStopWire →
// MoveStopToBreakeven, a real move_stop frame) → the book loses -sl →
// reconcileProtectionAt → PlaceProtectiveStop (a real place_protective_stop
// frame). The frame must carry the BE 30456.50, never the plan's 30551.25.
func TestProtectionNetNeverReWidensAMovedStop654(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	noFreshPrice(t)
	w := newExecWire(t, "p21-654")
	on := bePtr(true)
	w.at.config.StrategyConfig.RiskControl = store.RiskControlConfig{BreakevenEnabled: on, BreakevenTriggerPoints: 40}
	sid := w.open654(t)

	// Auto-BE at the production call site: SHORT 30456.50, mark 30400 (+56.5).
	w.at.maybeMoveStopToBreakeven("MNQ", "SHORT", 30456.50, 30400)
	moves := w.framesOf(ntwire.FrameMoveStop, 1, 2*time.Second)
	if len(moves) != 1 {
		t.Fatalf("fixture: the auto-BE must send ONE move_stop frame, got %d", len(moves))
	}
	var mv ntwire.MoveStopPayload
	_ = json.Unmarshal(moves[0], &mv)
	if mv.SignalID != sid || mv.NewStopLoss != 30456.50 {
		t.Fatalf("fixture: move_stop = %+v, want signal %s stop 30456.50", mv, sid)
	}

	now := time.Now()
	w.bookWithout(sid, now) // the -sl vanished (manual cancel / NT8 restart / OCO edge)
	w.at.reconcileProtectionAt(now, "monitor")

	stops := w.placedStops(t, 1)
	if len(stops) != 1 {
		t.Fatalf("the net must place exactly ONE protective stop, got %d", len(stops))
	}
	if stops[0].StopPrice != 30456.50 {
		t.Fatalf("P2-1: the net placed %.2f — it must place the BE stop 30456.50 the broker held, never the plan's 30551.25 (re-opens $189.50)", stops[0].StopPrice)
	}
	if stops[0].SignalID != sid || stops[0].PositionSide != "SHORT" || stops[0].Quantity != 1 {
		t.Fatalf("protective stop frame shape: %+v", stops[0])
	}
}

// TestProtectionNetUsesBookHistoryAfterRestart654 — after a restart the
// TCPTrader has no moved-stop record. The persisted F12 history still shows the
// -sl at the BE price (snapshot 112688's shape), and that is the floor.
func TestProtectionNetUsesBookHistoryAfterRestart654(t *testing.T) {
	noFreshPrice(t)
	w := newExecWire(t, "p21-654-hist")
	sid := w.open654(t)
	snap := w.st.NT8OrderSnapshots()
	base := time.Now().Add(-40 * time.Minute).UnixMilli()
	for i, stop := range []float64{30551.25, 30456.50} { // the original, then the BE move
		b, _ := json.Marshal([]ntwire.NT8Order{
			{OrderID: "sl1", Symbol: "MNQ", Name: sid + "-sl", Action: "buy", Type: "stop", StopPrice: stop, Quantity: 1, State: "Accepted"},
			{OrderID: "tp1", Symbol: "MNQ", Name: sid + "-tp", Action: "buy", Type: "limit", LimitPrice: 30257.50, Quantity: 1, State: "Working"},
		})
		if err := snap.Insert(&store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: string(b), OrderCount: 2,
			ReceivedMs: base + int64(i)*60_000}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	w.bookWithout(sid, now)
	w.at.reconcileProtectionAt(now, "monitor")
	stops := w.placedStops(t, 1)
	if len(stops) != 1 || stops[0].StopPrice != 30456.50 {
		t.Fatalf("P2-1 restart: placed %+v — want ONE stop at the book-history BE 30456.50, not the plan 30551.25", stops)
	}
}

// TestProtectionKnownStopThroughPriceFlattens654 — the pure decision on the
// known broker stop: a SHORT whose BE 30456.50 is already through the FRESH
// last price 30470 is flattened, never re-protected at a wider price.
func TestProtectionKnownStopThroughPriceFlattens654(t *testing.T) {
	be := knownBrokerStop{Px: 30456.50, From: "moved this process"}
	plan := 30551.25
	fresh := 30470.0
	v := adjudicateProtectionWith("MNQ", "SHORT", 1, nil, true, nil, plan, nil, &be, &fresh)
	if v.Action != protectionFlatten || v.StopPx != 30456.50 {
		t.Fatalf("through the BE: got %s @ %.2f (%s), want flatten @ 30456.50", v.Action, v.StopPx, v.Why)
	}
	fresh = 30440.0 // on the protective side: place the BE, not the plan
	v = adjudicateProtectionWith("MNQ", "SHORT", 1, nil, true, nil, plan, nil, &be, &fresh)
	if v.Action != protectionPlace || v.StopPx != 30456.50 || !strings.Contains(v.Source, "broker_stop") {
		t.Fatalf("protective side: got %s @ %.2f src=%s, want place @ 30456.50 from broker_stop", v.Action, v.StopPx, v.Source)
	}
	// A LONG mirror: never wider than the known stop even when accepted_risk
	// carries the wider original.
	acc := 30400.0
	beL := knownBrokerStop{Px: 30500, From: "book snapshot 1"}
	v = adjudicateProtectionWith("MNQ", "LONG", 1, nil, true, &acc, 30399.75, nil, &beL, nil)
	if v.Action != protectionPlace || v.StopPx != 30500 {
		t.Fatalf("LONG: got %s @ %.2f, want place @ 30500 (never the wider accepted 30400)", v.Action, v.StopPx)
	}
}

// TestPlaceProtectiveStopRoundsAwayFromEntry — P3-1 at :1454: the wire stop is
// WireStop (away from the entry), never the nearest tick.
func TestPlaceProtectiveStopRoundsAwayFromEntry(t *testing.T) {
	w := newExecWire(t, "p31-wirestop")
	if err := w.nt.PlaceProtectiveStop("MNQ", "SHORT", 1, 30456.30, "sig-x", "t"); err != nil {
		t.Fatal(err)
	}
	if err := w.nt.PlaceProtectiveStop("MNQ", "LONG", 1, 30427.47, "sig-y", "t"); err != nil {
		t.Fatal(err)
	}
	stops := w.placedStops(t, 2)
	if len(stops) != 2 {
		t.Fatalf("want 2 frames, got %d", len(stops))
	}
	if stops[0].StopPrice != 30456.50 || stops[1].StopPrice != 30427.25 {
		t.Fatalf("WireStop: SHORT 30456.30→%.2f (want 30456.50), LONG 30427.47→%.2f (want 30427.25)", stops[0].StopPrice, stops[1].StopPrice)
	}
}

// ── N-G — the planner auto-BE gets an NT8 verdict ────────────────────────────

func (w *execWire) bookWithStop(sid string, stop float64, recv time.Time) {
	w.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "sl1", Symbol: "MNQ", Name: sid + "-sl", Action: "buy", Type: "stop", StopPrice: stop, Quantity: 1, State: "Accepted"},
		{OrderID: "tp1", Symbol: "MNQ", Name: sid + "-tp", Action: "buy", Type: "limit", LimitPrice: 30257.50, Quantity: 1, State: "Working"},
	}}, recv)
}

// TestPlannerAutoBENotAppliedIsJudgedAndResent — the move_stop was SENT but
// the next fresh book still shows the plan stop 30551.25: NOT APPLIED is
// logged with the trader tag, counted, breakevenDone is cleared and the SAME
// monitor beat re-sends the move.
func TestPlannerAutoBENotAppliedIsJudgedAndResent(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	noFreshPrice(t)
	w := newExecWire(t, "ng-not-applied")
	on := bePtr(true)
	w.at.config.StrategyConfig.RiskControl = store.RiskControlConfig{BreakevenEnabled: on, BreakevenTriggerPoints: 40}
	sid := w.open654(t)
	get := warnPlusCapture(t)

	w.at.maybeMoveStopToBreakeven("MNQ", "SHORT", 30456.50, 30400) // beat 1: SENT
	if n := len(w.framesOf(ntwire.FrameMoveStop, 1, 2*time.Second)); n != 1 {
		t.Fatalf("fixture: beat 1 must send one move_stop, got %d", n)
	}
	if !hasLine(get(), "[trader_id=ng-not-applied]", "auto-breakeven", "SENT") {
		t.Fatalf("N-G: the SENT line must carry the trader tag; got %v", get())
	}
	// NT8 refused the Change: the fresh book (received > send+2s) still holds 30551.25.
	w.bookWithStop(sid, 30551.25, time.Now().Add(3*time.Second))
	w.at.maybeMoveStopToBreakeven("MNQ", "SHORT", 30456.50, 30400) // beat 2: verdict + re-send

	if c := PlannerBECount("ng-not-applied", "not_applied"); c != 1 {
		t.Fatalf("N-G: not_applied counter = %d, want 1", c)
	}
	if !hasLine(get(), "[trader_id=ng-not-applied]", "auto-breakeven NOT APPLIED at NT8", "want 30456.50 broker 30551.25") {
		t.Fatalf("N-G: the NOT APPLIED line is missing; got %v", get())
	}
	if n := len(w.framesOf(ntwire.FrameMoveStop, 2, 2*time.Second)); n != 2 {
		t.Fatalf("N-G: NOT APPLIED must clear breakevenDone so the beat re-sends — move_stop frames = %d, want 2", n)
	}
}

// TestPlannerAutoBEConfirmedNoResend — the book shows the BE: CONFIRMED is
// counted and nothing is re-sent.
func TestPlannerAutoBEConfirmedNoResend(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	noFreshPrice(t)
	w := newExecWire(t, "ng-confirmed")
	on := bePtr(true)
	w.at.config.StrategyConfig.RiskControl = store.RiskControlConfig{BreakevenEnabled: on, BreakevenTriggerPoints: 40}
	sid := w.open654(t)
	w.at.maybeMoveStopToBreakeven("MNQ", "SHORT", 30456.50, 30400)
	w.bookWithStop(sid, 30456.50, time.Now().Add(3*time.Second)) // snapshot 112688's shape
	w.at.maybeMoveStopToBreakeven("MNQ", "SHORT", 30456.50, 30400)
	if c := PlannerBECount("ng-confirmed", "confirmed"); c != 1 {
		t.Fatalf("N-G: confirmed counter = %d, want 1", c)
	}
	if c := PlannerBECount("ng-confirmed", "not_applied"); c != 0 {
		t.Fatalf("N-G: not_applied = %d on a confirmed move", c)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(w.framesOf(ntwire.FrameMoveStop, 2, 0)); n != 1 {
		t.Fatalf("N-G: a CONFIRMED move must not be re-sent — move_stop frames = %d, want 1", n)
	}
}

// ── N-F — accepted_risk once per signal, quantity from the book order ───────

// TestAcceptedRiskOncePerSignalWithBookQuantity — the AddOn's Accepted then
// Working frames for one entry (rows 228/229's shape) write ONE row, and its
// quantity is the broker order's 1, never the cumulative filled count 0.
func TestAcceptedRiskOncePerSignalWithBookQuantity(t *testing.T) {
	w := newExecWire(t, "nf-once")
	led := w.st.ArmedOrders()
	r := &store.ArmedOrderDB{TraderID: w.at.id, PlanID: "2026-10-14:NY:" + w.at.id, Session: "NY", Scenario: "S1", Version: 1,
		State: "armed", Side: "SHORT", EntryPx: 30456.50, StopPx: 30551.25, TargetPx: 30257.50}
	if err := led.UpsertArm(r); err != nil {
		t.Fatal(err)
	}
	sid := "4401cd88-nf"
	if err := led.BeginPlacement(r.ID, sid); err != nil {
		t.Fatal(err)
	}
	w.s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "e1", Symbol: "MNQ", Name: sid, Action: "sell", Type: "limit", LimitPrice: 30456.5, Quantity: 1, State: "Working"},
	}}, time.Now())
	for _, state := range []string{"Accepted", "Working"} {
		w.at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sid, OrderName: sid, State: state, Symbol: "MNQ", Account: "Sim101", Quantity: 0}, led)
	}
	rows, err := w.st.AcceptedRisk().ForSignal(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("N-F: one acceptance must write ONE accepted_risk row, got %d", len(rows))
	}
	if rows[0].Quantity != 1 {
		t.Fatalf("N-F: quantity = %d — it must be the broker order's 1, not the filled count 0", rows[0].Quantity)
	}
	if rows[0].AcceptedEntryPx == nil || *rows[0].AcceptedEntryPx != 30456.5 {
		t.Fatalf("N-F: the broker's entry price must still be recorded: %v", rows[0].AcceptedEntryPx)
	}
}
