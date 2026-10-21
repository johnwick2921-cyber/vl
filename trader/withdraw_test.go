package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"vl/internal/updaterjob"
	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0 (f) — WITHDRAW RESTING ENTRIES: THE ACCEPTANCE TESTS ─────
//
// Every test drives the PRODUCTION call site: at.monitorTick(now), the
// per-trader wall-clock beat whose last line is withdrawEntriesIfDue (the
// concurrency test calls withdrawEntriesIfDue directly — it IS the monitorTick
// hook — because it has to gate one specific read inside it). The arm is
// placed by the producer (maybeManageArmedOrdersAt → runArmedPlacementAt) over
// a REAL in-process TCP wire, so it carries the signal the broker knows it by;
// the fake AddOn on the other end of that socket records every frame the bot
// sends and answers with real order_update frames. Clocks are pinned.

// wdFrame is one frame the bot sent to the fake AddOn.
type wdFrame struct {
	Type   ntwire.FrameType
	Signal string
}

// wdWire is the fake AddOn's end of the socket: it records every frame the
// bot writes, in order.
type wdWire struct {
	srv    *ntwire.TCPServer
	conn   net.Conn
	mu     sync.Mutex
	frames []wdFrame
	notify chan struct{}
	flushN atomic.Int64
}

func (w *wdWire) read() {
	for {
		env, err := ntwire.ReadFrame(w.conn)
		if err != nil {
			return
		}
		var p struct {
			SignalID string `json:"signal_id"`
		}
		_ = json.Unmarshal(env.Payload, &p)
		w.mu.Lock()
		w.frames = append(w.frames, wdFrame{Type: env.Type, Signal: p.SignalID})
		w.mu.Unlock()
		select {
		case w.notify <- struct{}{}:
		default:
		}
	}
}

func (w *wdWire) all() []wdFrame {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]wdFrame(nil), w.frames...)
}

// waitFor blocks (bounded) until a recorded frame satisfies ok.
func (w *wdWire) waitFor(t *testing.T, what string, ok func(wdFrame) bool) wdFrame {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		for _, f := range w.all() {
			if ok(f) {
				return f
			}
		}
		select {
		case <-w.notify:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s; frames the fake AddOn received: %+v", what, w.all())
		}
	}
}

// flush sends a sentinel frame down the SAME connection and waits for it: the
// socket is ordered, so every frame the bot wrote before the flush has been
// recorded once it returns. A "nothing was sent" assertion after a flush is a
// fact, not a sleep that happened to be long enough.
func (w *wdWire) flush(t *testing.T) {
	t.Helper()
	sid := fmt.Sprintf("wd-flush-%d", w.flushN.Add(1))
	if err := w.srv.SendCancelOrder(ntwire.CancelOrderPayload{Symbol: "MNQ", Account: "Sim101", SignalID: sid}); err != nil {
		t.Fatalf("flush send: %v", err)
	}
	w.waitFor(t, "flush sentinel "+sid, func(f wdFrame) bool { return f.Type == ntwire.FrameCancelOrder && f.Signal == sid })
}

// count counts frames of one type for one signal (flush sentinels excluded by
// their own ids).
func (w *wdWire) count(typ ntwire.FrameType, sid string) int {
	n := 0
	for _, f := range w.all() {
		if f.Type == typ && f.Signal == sid {
			n++
		}
	}
	return n
}

// touching lists every frame after the entry's own placement that names the
// signal or one of its bracket children ("<signal>-sl" / "<signal>-tp").
func (w *wdWire) touching(sid string) []wdFrame {
	var out []wdFrame
	for _, f := range w.all() {
		if f.Type == ntwire.FrameSignal {
			continue
		}
		if f.Signal == sid || strings.HasPrefix(f.Signal, sid+"-") {
			out = append(out, f)
		}
	}
	return out
}

func isCancelFor(sid string) func(wdFrame) bool {
	return func(f wdFrame) bool { return f.Type == ntwire.FrameCancelOrder && f.Signal == sid }
}

type wdFix struct {
	at  *AutoTrader
	st  *store.Store
	w   *wdWire
	dir string
	now time.Time
}

// withdrawFixture is liveArmFixture's arm (S1 long limit @100, stop 95,
// target 110, on a whole-day TEST session) over a wire that keeps EVERY frame
// and exposes the connection, so the fake AddOn can answer. A per-test trader
// id keeps the process-wide per-trader state (armedSubs, the plan provider,
// the force-flat map) from leaking between tests.
func withdrawFixture(t *testing.T, id string, tune func(*store.StrategyConfig)) *wdFix {
	t.Helper()
	dir := withMaintenanceDir(t) // configured, no hold file: not held
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC)
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2
	if tune != nil {
		tune(&cfg)
	}

	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, s) // CTO M7: the producer must not race the accept
	t.Cleanup(func() { _ = conn.Close() })
	w := &wdWire{srv: s, conn: conn, notify: make(chan struct{}, 1)}
	go w.read()
	deadline := time.Now().Add(3 * time.Second)
	for !s.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the fake AddOn never registered as connected")
		}
		time.Sleep(time.Millisecond)
	}

	st, err := store.New(filepath.Join(t.TempDir(), "withdraw.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// A real AddOn emits a book whether or not it holds anything; an absent
	// book is a dark AddOn (see shadowWireHarnessAt).
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	// W117 F4 — GetPositions is UNKNOWN until a snapshot or a confirmed fill;
	// the AddOn emits a positions frame on connect. Seed the known-flat book so
	// the one-contract guard reads empty (pre-F4 contract) rather than the A24
	// fail-safe refusal.
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})

	at := &AutoTrader{id: id, exchange: "ninjatrader", store: st, trader: ntTrader.NewTCPTrader(s, "MNQ", "Sim101")}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	armedSubs.Delete(id)
	kernel.ClearDailyForceFlat(id)
	t.Cleanup(func() { armedSubs.Delete(id); kernel.ClearDailyForceFlat(id) })

	live := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", Trigger: "t", Condition: "reject", Direction: "long",
			TargetChain: []float64{110}, Invalid: "i", Quality: "B",
			Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "above"},
			Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}},
		},
		NoTrade: []string{}, DeathCondition: "n/a",
	}
	structuralTestMap(&live, structuralTestZone{100, 95.5, 100, "PDH"}, structuralTestZone{110, 110, 111, "target"})
	blob, _ := json.Marshal(live)
	shadowPlanAtTime(t, at, st, string(blob), now)
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return shadowBarsNearAt(100, now) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	return &wdFix{at: at, st: st, w: w, dir: dir, now: now}
}

// hold writes the installation hold the way the updater / CLI does.
func (f *wdFix) hold(t *testing.T, job string, withdraw bool) {
	t.Helper()
	if err := store.WriteMaintenanceHold(f.dir, store.MaintenanceHold{Held: true, JobID: job,
		Since: f.now.UTC().Format(time.RFC3339), Owner: "updater", WithdrawEntries: withdraw}); err != nil {
		t.Fatal(err)
	}
	if _, held := MaintenanceHeld(); !held {
		t.Fatal("fixture: the hold file was written but the gate does not read it as held")
	}
}

func wdEntryOrder(sid string) ntwire.NT8Order {
	return ntwire.NT8Order{OrderID: "ord-" + shortID(sid), Symbol: "MNQ", Name: sid, Action: "buy", Type: "limit",
		LimitPrice: 100, Quantity: 1, State: "Working"}
}

// setLiveBook is the AddOn's order_snapshot as the in-memory cache holds it —
// what cancelSafetyFor and the slot guard read.
func (f *wdFix) setLiveBook(at time.Time, orders ...ntwire.NT8Order) {
	if orders == nil {
		orders = []ntwire.NT8Order{}
	}
	f.w.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: orders}, at)
}

// persist writes an order snapshot the way main.go's sink does (empty symbol)
// and returns its id — the only evidence confirmPendingCancels accepts.
func (f *wdFix) persist(t *testing.T, at time.Time, orders ...ntwire.NT8Order) int64 {
	t.Helper()
	if orders == nil {
		orders = []ntwire.NT8Order{}
	}
	b, _ := json.Marshal(orders)
	row := &store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: string(b), OrderCount: len(orders),
		WorkingCount: len(orders), ReceivedMs: at.UnixMilli(), EmittedMs: at.UnixMilli()}
	if err := f.st.NT8OrderSnapshots().Insert(row); err != nil {
		t.Fatal(err)
	}
	if row.ID <= 0 {
		t.Fatal("fixture: a persisted snapshot must carry an id")
	}
	return row.ID
}

func (f *wdFix) rowBySignal(t *testing.T, sid string) store.ArmedOrderDB {
	t.Helper()
	var r store.ArmedOrderDB
	if err := f.st.ArmedOrders().DB().Where("trader_id = ? AND signal_id = ?", f.at.id, sid).First(&r).Error; err != nil {
		t.Fatalf("ledger row for signal %s: %v", sid, err)
	}
	return r
}

func (f *wdFix) rowByID(t *testing.T, id int64) store.ArmedOrderDB {
	t.Helper()
	var r store.ArmedOrderDB
	if err := f.st.ArmedOrders().DB().First(&r, id).Error; err != nil {
		t.Fatalf("ledger row %d: %v", id, err)
	}
	return r
}

// waitRowState pumps the given production pass (bounded) until the row for sid
// reads state. The pump is how a received frame reaches the ledger: the armed
// pass drains the order_update stream.
func (f *wdFix) waitRowState(t *testing.T, sid, state string, pump func()) store.ArmedOrderDB {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if pump != nil {
			pump()
		}
		r := f.rowBySignal(t, sid)
		if r.State == state {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("row for signal %s never reached %q; last: state=%q reason=%q", sid, state, r.State, r.StateReason)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sendOrderUpdate is the fake AddOn answering on the wire.
func (f *wdFix) sendOrderUpdate(t *testing.T, sid, orderName, state string) {
	t.Helper()
	if err := ntwire.WriteFrame(f.w.conn, ntwire.FrameOrderUpdate, ntwire.OrderUpdatePayload{
		SignalID: sid, OrderName: orderName, State: state, Symbol: "MNQ", Account: "Sim101"}); err != nil {
		t.Fatalf("order_update write: %v", err)
	}
}

// placeResting places S1 through the producer, lets the broker accept it (a
// received Working order_update, drained by the armed pass) and shows the
// entry resting in the live book. Returns the ledger row and its signal.
func (f *wdFix) placeResting(t *testing.T) (store.ArmedOrderDB, string) {
	t.Helper()
	f.at.maybeManageArmedOrdersAt(nil, f.now)
	sig := f.w.waitFor(t, "the arm's entry signal", func(fr wdFrame) bool { return fr.Type == ntwire.FrameSignal })
	if sig.Signal == "" {
		t.Fatal("fixture: the placed entry carries no signal id")
	}
	f.sendOrderUpdate(t, sig.Signal, sig.Signal, "Working")
	row := f.waitRowState(t, sig.Signal, store.StateWorking, func() { f.at.maybeManageArmedOrdersAt(nil, f.now) })
	f.setLiveBook(f.now, wdEntryOrder(sig.Signal))
	return row, sig.Signal
}

// seedLoss writes one CLOSED losing trade for this trader (the shape the
// breaker's own store test uses: corrected P&L present).
func (f *wdFix) seedLoss(t *testing.T, exit time.Time) {
	t.Helper()
	pnl := -12.5
	p := &store.TraderPosition{TraderID: f.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 93.75, RealizedPnL: pnl, PnlCorrected: &pnl,
		Status: "CLOSED", CloseReason: "sync", EntryTime: exit.Add(-5 * time.Minute).UnixMilli(),
		ExitTime: exit.UnixMilli(), CreatedAt: exit.UnixMilli(), UpdatedAt: exit.UnixMilli()}
	if err := f.st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
}

// assertWithdrawnThenConfirmed is the shared lifecycle every withdraw trigger
// must follow: one cancel_order for the signal, the row cancel_pending with the
// withdraw reason, NOT settled by a beat without evidence, NOT settled by a
// stale book or by a fresh book that still lists the entry, and settled by a
// fresh persisted book without it — citing that snapshot.
func assertWithdrawnThenConfirmed(t *testing.T, f *wdFix, row store.ArmedOrderDB, sid, wantReason string, t1 time.Time) {
	t.Helper()
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order for "+sid, isCancelFor(sid))
	got := f.rowByID(t, row.ID)
	if got.State != store.StateCancelPending {
		t.Fatalf("a withdrawn entry must read cancel_pending (a send is not a settlement), got %q (%s)", got.State, got.StateReason)
	}
	if got.StateReason != wantReason {
		t.Fatalf("withdraw reason = %q, want %q", got.StateReason, wantReason)
	}
	if got.CancelAttempts != 1 || got.CancelRequestedAtMs != t1.UnixMilli() || got.CancelSettledSnapshotID != 0 {
		t.Fatalf("cancel bookkeeping: attempts=%d requested_at=%d settled_snapshot=%d, want 1/%d/0",
			got.CancelAttempts, got.CancelRequestedAtMs, got.CancelSettledSnapshotID, t1.UnixMilli())
	}

	// Beat 2 — no evidence of any kind: still pending, and not re-sent inside
	// its confirmation window.
	f.at.monitorTick(t1.Add(10 * time.Second))
	f.w.flush(t)
	if s := f.rowByID(t, row.ID).State; s != store.StateCancelPending {
		t.Fatalf("a beat with no evidence settled the withdraw: state %q", s)
	}
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
		t.Fatalf("cancel_order frames for %s = %d, want exactly 1 inside the confirmation window", sid, n)
	}

	// Beat 3 — the only persisted book is empty but STALE: settles nothing.
	f.persist(t, t1.Add(-10*time.Minute))
	f.at.monitorTick(t1.Add(20 * time.Second))
	if s := f.rowByID(t, row.ID).State; s != store.StateCancelPending {
		t.Fatalf("a stale book settled the withdraw: state %q", s)
	}

	// Beat 4 — a FRESH persisted book that still lists the entry: settles nothing.
	f.persist(t, t1.Add(30*time.Second), wdEntryOrder(sid))
	f.at.monitorTick(t1.Add(30 * time.Second))
	if s := f.rowByID(t, row.ID).State; s != store.StateCancelPending {
		t.Fatalf("a book that still lists the entry settled the withdraw: state %q", s)
	}

	// Beat 5 — a fresh persisted book without it: cancelled, citing it.
	snap := f.persist(t, t1.Add(40*time.Second))
	f.at.monitorTick(t1.Add(40 * time.Second))
	got = f.rowByID(t, row.ID)
	if got.State != store.StateCancelled || got.CancelSettledSnapshotID != snap {
		t.Fatalf("absence from a fresh persisted book must confirm the cancel citing snapshot %d; got state=%q snapshot=%d (%s)",
			snap, got.State, got.CancelSettledSnapshotID, got.StateReason)
	}
	if !strings.HasPrefix(got.StateReason, wantReason) || !strings.Contains(got.StateReason, "confirmed: absent from a fresh book") {
		t.Fatalf("the confirmation must keep the withdraw's reason and name its evidence: %q", got.StateReason)
	}
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
		t.Fatalf("cancel_order frames for %s = %d over the whole lifecycle, want exactly 1", sid, n)
	}
}

// 1 — a hold WITH withdraw_entries withdraws the resting entry: cancel_order
// sent for its signal, cancel_pending, never confirmed without evidence,
// cancelled on its absence from a fresh PERSISTED snapshot.
func TestWithdrawHoldCancelsARestingEntryAndConfirmsOnlyOnEvidence(t *testing.T) {
	f := withdrawFixture(t, "wd-hold-snapshot", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-wd-1", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	assertWithdrawnThenConfirmed(t, f, row, sid, WithdrawReasonPrefix+"maintenance job job-wd-1", t1)
}

// 1 (second evidence) — the same withdraw settles on a RECEIVED order_update
// 'Cancelled' for that signal (the armed pass is its production consumer). A
// dying state is not a cancellation, and another signal's Cancelled is not
// this one's.
func TestWithdrawPendingCancelSettlesOnAReceivedCancelledOrderUpdate(t *testing.T) {
	f := withdrawFixture(t, "wd-hold-orderupdate", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-wd-ou", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))

	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	if s := f.rowByID(t, row.ID).State; s != store.StateCancelPending {
		t.Fatalf("withdrawn entry must be cancel_pending, got %q", s)
	}

	// Another placed entry of this trader: its Cancelled update is the
	// sentinel. The wire → router → fan-out → consumer path is ordered, so once
	// the sentinel's row reads cancelled every frame sent before it has been
	// applied.
	ledger := f.st.ArmedOrders()
	sentinel := store.ArmedOrderDB{TraderID: f.at.id, PlanID: "wd-ou-sentinel-plan", Version: 1, Session: "TEST", Scenario: "S9",
		State: store.StateArmed, Side: "long", EntryPx: 90, StopPx: 85, TargetPx: 100, Kind: "limit", Condition: "reject"}
	if err := ledger.UpsertArm(&sentinel); err != nil {
		t.Fatal(err)
	}
	const sentinelSid = "wd-ou-sentinel-signal"
	if err := ledger.BeginPlacement(sentinel.ID, sentinelSid); err != nil {
		t.Fatal(err)
	}

	pump := func() { f.at.maybeManageArmedOrdersAt(nil, t1.Add(time.Second)) }
	// NT8 is still withdrawing it, and its bracket child was cancelled: neither
	// is evidence that THIS entry is gone.
	f.sendOrderUpdate(t, sid, sid, "CancelPending")
	f.sendOrderUpdate(t, sid, sid+"-sl", "Cancelled")
	f.sendOrderUpdate(t, sentinelSid, sentinelSid, "Cancelled")
	f.waitRowState(t, sentinelSid, store.StateCancelled, pump)
	if r := f.rowByID(t, row.ID); r.State != store.StateCancelPending {
		t.Fatalf("a non-evidence frame (or another signal's Cancelled) moved the pending withdraw to %q (%s)", r.State, r.StateReason)
	}

	f.sendOrderUpdate(t, sid, sid, "Cancelled")
	got := f.waitRowState(t, sid, store.StateCancelled, pump)
	if got.ID != row.ID {
		t.Fatalf("the Cancelled update settled row %d, want %d", got.ID, row.ID)
	}
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
		t.Fatalf("cancel_order frames for %s = %d, want exactly 1", sid, n)
	}
}

// 2 — the consecutive-loss breaker trips (N losing closes this CME
// session-day): the same withdraw, with no maintenance hold at all.
func TestWithdrawOnConsecutiveLossBreakerTrip(t *testing.T) {
	f := withdrawFixture(t, "wd-breaker", func(c *store.StrategyConfig) { c.RiskControl.ConsecutiveLossHalt = store.IntPtr(2) })
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	if _, halted := f.at.consecutiveLossHaltedAt(t1); halted {
		t.Fatal("fixture: halted before any loss was seeded")
	}
	if _, held := MaintenanceHeld(); held {
		t.Fatal("fixture: this test must run with NO maintenance hold")
	}
	f.seedLoss(t, f.now.Add(-30*time.Minute))
	f.seedLoss(t, f.now.Add(-20*time.Minute))
	if reason, halted := f.at.consecutiveLossHaltedAt(t1); !halted {
		t.Fatalf("fixture: two losing closes this session-day must trip a limit-2 breaker (%q)", reason)
	}
	f.setLiveBook(t1, wdEntryOrder(sid))
	assertWithdrawnThenConfirmed(t, f, row, sid, WithdrawReasonPrefix+"consecutive_loss", t1)
}

// 3 — the daily force-flat trips: the same withdraw.
func TestWithdrawOnDailyForceFlatTrip(t *testing.T) {
	f := withdrawFixture(t, "wd-forceflat", nil)
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	if _, held := MaintenanceHeld(); held {
		t.Fatal("fixture: this test must run with NO maintenance hold")
	}
	kernel.SetDailyForceFlat(f.at.id, "daily loss limit hit (withdraw test)")
	if kernel.DailyForceFlatReason(f.at.id) == "" {
		t.Fatal("fixture: force-flat did not arm")
	}
	f.setLiveBook(t1, wdEntryOrder(sid))
	assertWithdrawnThenConfirmed(t, f, row, sid, WithdrawReasonPrefix+"daily_force_flat", t1)
}

// 4 — a row the filled-arm guard refuses (the book shows its bracket
// children and no entry: it may have FILLED) is NOT cancelled: no cancel_order
// for its signal, no frame touching its bracket, the row untouched. A second,
// genuinely resting entry in the same beat IS withdrawn — the refusal is per
// row, not a stall.
func TestWithdrawNeverCancelsAnArmTheBookSaysMayHaveFilled(t *testing.T) {
	f := withdrawFixture(t, "wd-filled-guard", nil)
	row, sid := f.placeResting(t)

	// A second placed entry from another plan (store producer path: an armed
	// row that has been handed its broker identity).
	ledger := f.st.ArmedOrders()
	other := store.ArmedOrderDB{TraderID: f.at.id, PlanID: "wd-other-plan", Version: 1, Session: "TEST", Scenario: "S9",
		State: store.StateArmed, Side: "long", EntryPx: 90, StopPx: 85, TargetPx: 100, Kind: "limit", Condition: "reject"}
	if err := ledger.UpsertArm(&other); err != nil {
		t.Fatal(err)
	}
	const otherSid = "wd-other-entry-signal"
	if err := ledger.BeginPlacement(other.ID, otherSid); err != nil {
		t.Fatal(err)
	}

	f.hold(t, "job-wd-guard", true)
	t1 := f.now.Add(time.Minute)
	// The entry is GONE from the book and its protection rests: it filled, and
	// the ledger has not heard yet. The other entry still rests.
	f.setLiveBook(t1,
		ntwire.NT8Order{OrderID: "ord-sl", Symbol: "MNQ", Name: sid + "-sl", Action: "sell", Type: "stop", StopPrice: 95, Quantity: 1, State: "Accepted"},
		ntwire.NT8Order{OrderID: "ord-tp", Symbol: "MNQ", Name: sid + "-tp", Action: "sell", Type: "limit", LimitPrice: 110, Quantity: 1, State: "Working"},
		ntwire.NT8Order{OrderID: "ord-other", Symbol: "MNQ", Name: otherSid, Action: "buy", Type: "limit", LimitPrice: 90, Quantity: 1, State: "Working"},
	)
	if v := f.at.cancelSafetyFor(f.rowByID(t, row.ID), t1); v.Allow {
		t.Fatalf("fixture: the guard must refuse this row, said %q", v.Why)
	}
	logs := captureTraderLog(t)

	f.at.monitorTick(t1)
	f.at.monitorTick(t1.Add(10 * time.Second))
	f.w.waitFor(t, "the other entry's withdraw", isCancelFor(otherSid))
	f.w.flush(t)

	if fr := f.w.touching(sid); len(fr) != 0 {
		t.Fatalf("the may-have-filled entry's signal or bracket was touched on the wire: %+v", fr)
	}
	got := f.rowByID(t, row.ID)
	if got.State != row.State || got.StateReason != row.StateReason || got.CancelAttempts != 0 || got.CancelRequestedAtMs != 0 {
		t.Fatalf("the refused row must be untouched: before state=%q reason=%q, after state=%q reason=%q attempts=%d requested=%d",
			row.State, row.StateReason, got.State, got.StateReason, got.CancelAttempts, got.CancelRequestedAtMs)
	}
	if !strings.Contains(logs.String(), "withdraw cancel REFUSED") {
		t.Fatalf("the refusal must be said; log:\n%s", logs.String())
	}
	if s := f.rowByID(t, other.ID).State; s != store.StateCancelPending {
		t.Fatalf("the resting entry beside it must still be withdrawn, got %q", s)
	}
	if n := f.w.count(ntwire.FrameCancelOrder, otherSid); n != 1 {
		t.Fatalf("cancel_order frames for the resting entry = %d, want 1", n)
	}
}

// 5 — an arm that was never sent (no signal) is untouched: it stays armed and
// nothing is sent for it. With no wire bound at all, the no-signal skip is the
// only thing standing between that row and a cancel_pending it never earned.
func TestWithdrawLeavesANeverSentArmArmed(t *testing.T) {
	f := withdrawFixture(t, "wd-unsent", nil)
	f.hold(t, "job-wd-unsent", true) // BEFORE the pass: the arm authors but is refused at its send point
	f.at.maybeManageArmedOrdersAt(nil, f.now)
	f.w.flush(t)
	for _, fr := range f.w.all() {
		if fr.Type == ntwire.FrameSignal {
			t.Fatalf("fixture: a held arm reached the wire (%+v)", fr)
		}
	}
	rows, err := f.st.ArmedOrders().ListNonTerminal(f.at.id)
	if err != nil {
		t.Fatal(err)
	}
	var unsent []store.ArmedOrderDB
	for _, r := range rows {
		if r.State == store.StateArmed && strings.TrimSpace(r.SignalID) == "" {
			unsent = append(unsent, r)
		}
	}
	if len(unsent) == 0 {
		t.Fatalf("fixture: expected an authored, never-sent arm; rows: %+v", rows)
	}

	t1 := f.now.Add(time.Minute)
	f.at.monitorTick(t1)
	f.at.monitorTick(t1.Add(10 * time.Second))
	f.w.flush(t)
	for _, fr := range f.w.all() {
		if fr.Type == ntwire.FrameCancelOrder && !strings.HasPrefix(fr.Signal, "wd-flush-") {
			t.Fatalf("a cancel_order was sent while the only arm was never placed: %+v", fr)
		}
	}
	check := func(when string) {
		t.Helper()
		for _, r := range unsent {
			got := f.rowByID(t, r.ID)
			if got.State != store.StateArmed || got.SignalID != "" || got.CancelAttempts != 0 || got.StateReason != r.StateReason {
				t.Fatalf("%s: a never-sent arm must stay armed and untouched; before %q/%q, after state=%q reason=%q attempts=%d",
					when, r.State, r.StateReason, got.State, got.StateReason, got.CancelAttempts)
			}
		}
	}
	check("wire bound")

	// No wire bound: the guard cannot run, only the no-signal skip can.
	f.at.trader = nil
	f.at.monitorTick(t1.Add(20 * time.Second))
	check("no wire bound")
}

// 6 — a hold WITHOUT withdraw_entries refuses new entries only: nothing
// resting is cancelled, and the /api/maintenance withdraw view is null.
func TestHoldWithoutWithdrawEntriesCancelsNothing(t *testing.T) {
	f := withdrawFixture(t, "wd-hold-plain", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-plain", false)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))

	f.at.monitorTick(t1)
	f.at.monitorTick(t1.Add(10 * time.Second))
	f.w.flush(t)
	if fr := f.w.touching(sid); len(fr) != 0 {
		t.Fatalf("a plain hold touched a resting entry on the wire: %+v", fr)
	}
	got := f.rowByID(t, row.ID)
	if got.State != store.StateWorking || got.CancelAttempts != 0 {
		t.Fatalf("a plain hold must leave the resting entry working, got state=%q attempts=%d (%s)", got.State, got.CancelAttempts, got.StateReason)
	}
	if v := maintenanceWithdrawView(f.st); v != nil {
		t.Fatalf("a hold without withdraw_entries must show no withdraw, got %+v", *v)
	}
}

// 7 — GET /api/maintenance's withdraw half, through the real
// MaintenanceStatus reader: null with no hold; requested with READ empty
// lists before any row is asked; the row pending after the withdraw; the row
// confirmed after the evidence; scoped to the current job.
func TestMaintenanceWithdrawViewReportsRequestedPendingThenConfirmed(t *testing.T) {
	f := withdrawFixture(t, "wd-view", nil)
	loaded := map[string]*AutoTrader{f.at.id: f.at}
	if v := MaintenanceStatus(loaded).Withdraw; v != nil {
		t.Fatalf("no hold → withdraw must be null, got %+v", *v)
	}
	row, sid := f.placeResting(t)
	f.hold(t, "job-view", true)

	body := func() string {
		b, err := json.Marshal(MaintenanceStatus(loaded))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := body(); !strings.Contains(got, `"withdraw":{"requested":true,"pending":[],"confirmed":[],"filled":[],"ended":[]}`) {
		t.Fatalf("requested, nothing asked yet: pending/confirmed must be READ empty lists; body %s", got)
	}

	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	want := fmt.Sprintf(`"withdraw":{"requested":true,"pending":[%d],"confirmed":[],"filled":[],"ended":[]}`, row.ID)
	if got := body(); !strings.Contains(got, want) {
		t.Fatalf("after the withdraw the row must be pending: want %s in %s", want, got)
	}
	if v := maintenanceWithdrawView(f.st); v == nil || len(v.Pending) != 1 || v.Pending[0] != row.ID || len(v.Confirmed) != 0 {
		t.Fatalf("maintenanceWithdrawView: %+v", v)
	}

	f.at.monitorTick(t1.Add(10 * time.Second)) // no evidence
	if got := body(); !strings.Contains(got, want) {
		t.Fatalf("no evidence: the row must still be pending: want %s in %s", want, got)
	}

	f.persist(t, t1.Add(20*time.Second))
	f.at.monitorTick(t1.Add(20 * time.Second))
	want = fmt.Sprintf(`"withdraw":{"requested":true,"pending":[],"confirmed":[%d],"filled":[],"ended":[]}`, row.ID)
	if got := body(); !strings.Contains(got, want) {
		t.Fatalf("after a fresh book without it the row must be confirmed: want %s in %s", want, got)
	}

	// Scoped to the CURRENT job: a new job's view does not inherit job-view's rows.
	if err := store.ClearMaintenanceHold(f.dir, "job-view"); err != nil {
		t.Fatal(err)
	}
	if v := MaintenanceStatus(loaded).Withdraw; v != nil {
		t.Fatalf("hold cleared → withdraw must be null, got %+v", *v)
	}
	f.hold(t, "job-view-2", true)
	if got := body(); !strings.Contains(got, `"withdraw":{"requested":true,"pending":[],"confirmed":[],"filled":[],"ended":[]}`) {
		t.Fatalf("a new job's view must not list the previous job's rows: %s", got)
	}
}

// 7 (a defect the builder found; fixed in W0b) — a withdraw row confirmed by a RECEIVED order_update
// 'Cancelled' must still be listed as confirmed for its job.
func TestMaintenanceWithdrawViewKeepsARowConfirmedByOrderUpdate(t *testing.T) {
	f := withdrawFixture(t, "wd-view-ou", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-view-ou", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	f.sendOrderUpdate(t, sid, sid, "Cancelled")
	got := f.waitRowState(t, sid, store.StateCancelled, func() { f.at.maybeManageArmedOrdersAt(nil, t1.Add(time.Second)) })

	v := maintenanceWithdrawView(f.st)
	if v == nil || !v.Requested {
		t.Fatalf("the hold asks for a withdraw: %+v", v)
	}
	for _, id := range v.Pending {
		if id == row.ID {
			t.Fatalf("a cancelled row must not read pending: %+v", *v)
		}
	}
	listed := false
	for _, id := range v.Confirmed {
		listed = listed || id == row.ID
	}
	if !listed {
		t.Fatalf("row %d was withdrawn for job-view-ou and confirmed by a received order_update "+
			"'Cancelled', but GET /api/maintenance lists it neither pending nor confirmed (view %+v, reason %q): "+
			"the withdraw head must survive the order_update's reason write (store.reasonKeepingWithdraw)",
			row.ID, *v, got.StateReason)
	}
}

// 7 (a defect the builder found; fixed in W0b) — a withdraw row that went unconfirmed past its window and
// was re-requested is still this job's pending withdraw.
func TestMaintenanceWithdrawViewKeepsARowThroughAReRequest(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_TIMEOUT_S", "1")
	f := withdrawFixture(t, "wd-view-rereq", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-view-rr", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	if v := maintenanceWithdrawView(f.st); v == nil || len(v.Pending) != 1 || v.Pending[0] != row.ID {
		t.Fatalf("fixture: the row must first read pending: %+v", v)
	}
	// Past the 1s window, no book: the beat re-requests.
	f.at.monitorTick(t1.Add(5 * time.Second))
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 2 {
		t.Fatalf("fixture: expected the initial cancel plus one re-request, got %d", n)
	}
	got := f.rowByID(t, row.ID)
	if got.State != store.StateCancelPending || got.CancelAttempts != 2 {
		t.Fatalf("fixture: re-requested row must be cancel_pending with 2 attempts: %+v", got)
	}
	v := maintenanceWithdrawView(f.st)
	pending := false
	for _, id := range v.Pending {
		pending = pending || id == row.ID
	}
	if !pending {
		t.Fatalf("row %d is still cancel_pending for job-view-rr after one re-request, but GET "+
			"/api/maintenance no longer lists it (view %+v, reason %q): the withdraw head must survive the "+
			"re-request's reason write (store.reasonKeepingWithdraw)",
			row.ID, *v, got.StateReason)
	}
}

// 7 (a defect the builder found; fixed in W0b) — L7 "absent ≠ []": a view that READ nothing must not
// report "nothing pending".
func TestMaintenanceWithdrawViewDoesNotFabricateEmptyListsItNeverRead(t *testing.T) {
	dir := withMaintenanceDir(t)
	if err := store.WriteMaintenanceHold(dir, store.MaintenanceHold{Held: true, JobID: "job-nostore",
		Since: "2026-09-11T15:00:00Z", Owner: "updater", WithdrawEntries: true}); err != nil {
		t.Fatal(err)
	}
	// No trader loaded → MaintenanceStatus has no store to read the ledger with.
	v := MaintenanceStatus(map[string]*AutoTrader{}).Withdraw
	if v == nil || !v.Requested {
		t.Fatalf("the hold asks for a withdraw: %+v", v)
	}
	if v.Pending != nil || v.Confirmed != nil || v.Filled != nil || v.Ended != nil || v.Unread == "" {
		b, _ := json.Marshal(v)
		t.Fatalf("with no store to read (no trader loaded) the withdraw view must report its lists ABSENT and say why — "+
			"never READ-looking empty lists for a ledger it never read (L7: absent ≠ []); got %s", string(b))
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"pending":null`) || !strings.Contains(string(b), `"unread":"no trader loaded`) {
		t.Fatalf("the API form must carry null lists and the reason: %s", string(b))
	}
}

// 8 — the armed pass and the withdraw beat settling cancels at the SAME TIME
// (two goroutines) send exactly ONE re-request for the signal: the beat is
// held INSIDE its settlement (at its persisted-book read, after it has listed
// the pending rows) while the armed pass runs to completion; the armed pass
// must skip the settlement rather than re-request beside it.
func TestWithdrawBeatAndArmedPassReRequestOnceWhenConcurrent(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_TIMEOUT_S", "1")
	f := withdrawFixture(t, "wd-concurrent", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-wd-conc", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
		t.Fatalf("fixture: initial withdraw frames = %d", n)
	}

	// The gate: the FIRST order-snapshot read after arming blocks until
	// released. In the withdraw beat that read is persistedBook, inside
	// confirmPendingCancels, under cancelConfirmMu.
	var armed atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	if err := f.st.GormDB().Callback().Query().Before("gorm:query").Register("wd_test_gate", func(db *gorm.DB) {
		if db.Statement != nil && db.Statement.Table == "nt8_order_snapshots" && armed.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	armed.Store(true)

	t2 := t1.Add(5 * time.Second) // past the 1s confirmation window
	beatDone, passDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(beatDone); f.at.withdrawEntriesIfDue(t2) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the withdraw beat never reached its settlement's book read")
	}
	go func() { defer close(passDone); f.at.maybeManageArmedOrdersAt(nil, t2) }()
	select {
	case <-passDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the armed pass blocked behind the withdraw beat's settlement (it must skip, never wait)")
	}
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
		t.Fatalf("the armed pass re-requested while the withdraw beat was settling: cancel frames = %d, want 1", n)
	}
	close(release)
	released = true
	select {
	case <-beatDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the withdraw beat never finished")
	}
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 2 {
		t.Fatalf("cancel_order frames for %s = %d, want 2 (the withdraw + exactly ONE re-request)", sid, n)
	}
	if got := f.rowByID(t, row.ID); got.CancelAttempts != 2 || got.State != store.StateCancelPending {
		t.Fatalf("exactly one re-request must be COUNTED: attempts=%d state=%q (%s)", got.CancelAttempts, got.State, got.StateReason)
	}
}

// 8 (lock semantics) — while another pass holds the settlement, the withdraw
// beat neither waits for it nor re-requests; once it is free the beat
// re-requests exactly once.
func TestWithdrawBeatSkipsSettlementHeldByTheArmedPass(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_TIMEOUT_S", "1")
	f := withdrawFixture(t, "wd-trylock", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-wd-lock", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))

	t2 := t1.Add(5 * time.Second)
	f.at.cancelConfirmMu.Lock() // the armed pass is mid-settlement
	done := make(chan struct{})
	go func() { defer close(done); f.at.withdrawEntriesIfDue(t2) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		f.at.cancelConfirmMu.Unlock()
		<-done
		t.Fatal("the withdraw beat waited on the armed pass's settlement (it must skip)")
	}
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
		f.at.cancelConfirmMu.Unlock()
		t.Fatalf("the beat re-requested while the settlement was held elsewhere: frames=%d", n)
	}
	f.at.cancelConfirmMu.Unlock()

	f.at.withdrawEntriesIfDue(t2)
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 2 {
		t.Fatalf("once free, the beat must re-request exactly once: frames=%d", n)
	}
	if got := f.rowByID(t, row.ID); got.CancelAttempts != 2 {
		t.Fatalf("attempts=%d, want 2", got.CancelAttempts)
	}
}

// ── fixes found by this suite (W-EXEC-TRUTH W0b) ────────────────────────────

// canon 35 — a re-request the filled-arm guard REFUSES sends nothing, and is
// neither recorded nor counted as a re-request: attempts and reason stay as
// the withdraw left them. Both settlement callers — the withdraw beat and the
// armed pass — hand the refusal back the same way.
func TestWithdrawReRequestRefusedByTheGuardIsNotRecorded(t *testing.T) {
	t.Setenv("CANCEL_CONFIRM_TIMEOUT_S", "1")
	f := withdrawFixture(t, "wd-refused-rr", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-refused-rr", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	before := f.rowByID(t, row.ID)
	if before.State != store.StateCancelPending || before.CancelAttempts != 1 {
		t.Fatalf("fixture: withdrawn row must be cancel_pending with 1 attempt: %+v", before)
	}
	// The entry is gone and its protection rests: it filled while the cancel
	// was in flight. The guard must refuse any re-request.
	filledBook := func(at time.Time) {
		f.setLiveBook(at,
			ntwire.NT8Order{OrderID: "ord-sl", Symbol: "MNQ", Name: sid + "-sl", Action: "sell", Type: "stop", StopPrice: 95, Quantity: 1, State: "Accepted"},
			ntwire.NT8Order{OrderID: "ord-tp", Symbol: "MNQ", Name: sid + "-tp", Action: "sell", Type: "limit", LimitPrice: 110, Quantity: 1, State: "Working"})
	}
	check := func(who string) {
		t.Helper()
		f.w.flush(t)
		if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 1 {
			t.Fatalf("%s: a refused re-request reached the wire: cancel frames=%d, want 1", who, n)
		}
		got := f.rowByID(t, row.ID)
		if got.State != store.StateCancelPending || got.CancelAttempts != before.CancelAttempts || got.StateReason != before.StateReason {
			t.Fatalf("%s: a refused re-request was recorded: before %d/%q, after %s %d/%q",
				who, before.CancelAttempts, before.StateReason, got.State, got.CancelAttempts, got.StateReason)
		}
	}
	t2 := t1.Add(5 * time.Second) // past the 1s window
	filledBook(t2)
	f.at.monitorTick(t2)
	check("withdraw beat")

	if err := store.ClearMaintenanceHold(f.dir, "job-refused-rr"); err != nil {
		t.Fatal(err)
	}
	t3 := t2.Add(5 * time.Second)
	filledBook(t3)
	f.at.maybeManageArmedOrdersAt(nil, t3)
	check("armed pass")
}

// A withdrawn entry that FILLED before its cancel landed is a position, not a
// withdraw: the view lists it under filled, never confirmed.
func TestMaintenanceWithdrawViewListsAFillAsFilledNeverConfirmed(t *testing.T) {
	f := withdrawFixture(t, "wd-view-fill", nil)
	row, sid := f.placeResting(t)
	f.hold(t, "job-view-fill", true)
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.waitFor(t, "the withdraw's cancel_order", isCancelFor(sid))
	f.sendOrderUpdate(t, sid, sid, "Filled")
	f.waitRowState(t, sid, store.StateFilled, func() { f.at.maybeManageArmedOrdersAt(nil, t1.Add(time.Second)) })
	v := maintenanceWithdrawView(f.st)
	if v == nil || len(v.Confirmed) != 0 || len(v.Filled) != 1 || v.Filled[0] != row.ID {
		t.Fatalf("a withdrawn entry that filled must read filled, never confirmed: %+v", v)
	}
}

// With no NinjaTrader wire no cancel can be sent, so no row may be marked
// cancel_pending (a pending cancel with nothing in flight is fabricated).
// withdrawEntriesIfDue is the monitorTick hook, called directly here because
// monitorTick's other beats read the broker.
func TestWithdrawWithNoWireMarksNothing(t *testing.T) {
	f := withdrawFixture(t, "wd-no-wire", nil)
	row, _ := f.placeResting(t)
	before := f.rowByID(t, row.ID)
	f.hold(t, "job-no-wire", true)
	f.at.trader = nil
	f.at.withdrawEntriesIfDue(f.now.Add(time.Minute))
	if got := f.rowByID(t, row.ID); got.State != before.State || got.CancelAttempts != 0 || got.StateReason != before.StateReason {
		t.Fatalf("no wire: the row must be untouched, was %s/%q now %s/%d/%q", before.State, before.StateReason, got.State, got.CancelAttempts, got.StateReason)
	}
}

// A hold file that cannot be read counts as HELD (entries refused) but it
// cannot say withdraw_entries — only the explicit flag withdraws.
func TestWithdrawNeverOnACorruptHold(t *testing.T) {
	f := withdrawFixture(t, "wd-corrupt", nil)
	row, sid := f.placeResting(t)
	if err := os.MkdirAll(filepath.Dir(store.MaintenanceHoldPath(f.dir)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.MaintenanceHoldPath(f.dir), []byte(`{"held":true,"withdraw_entries":true,`), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, ok := maintenanceState(); !ok || !st.Corrupt {
		t.Fatalf("fixture: the hold must read corrupt: %+v %v", st, ok)
	}
	t1 := f.now.Add(time.Minute)
	f.setLiveBook(t1, wdEntryOrder(sid))
	f.at.monitorTick(t1)
	f.w.flush(t)
	if n := f.w.count(ntwire.FrameCancelOrder, sid); n != 0 {
		t.Fatalf("a corrupt hold withdrew an entry: cancel frames=%d", n)
	}
	if got := f.rowByID(t, row.ID); got.State == store.StateCancelPending {
		t.Fatalf("a corrupt hold marked the row cancel_pending: %+v", got)
	}
	if v := maintenanceWithdrawView(f.st); v != nil {
		t.Fatalf("a corrupt hold asks for no withdraw view: %+v", *v)
	}
}

// EXECUTION P2-2 (10-14) — an update job in PREFLIGHT froze new entries
// (install_pending) but left a planner limit resting at NT8, and the
// preflight's ledger_exposure leg refused the install. The freeze now
// withdraws resting entries through the same entries-only path: the cancel is
// sent for the entry's signal, the row goes cancel_pending and settles only on
// evidence, and nothing touches the stop or the target.
func TestWithdrawOnInstallPendingPreflight(t *testing.T) {
	f := withdrawFixture(t, "wd-install-pending", nil)
	t.Cleanup(resetInstallPendingForTest)
	row, sid := f.placeResting(t)
	t1 := f.now.Add(time.Minute)
	if _, held := MaintenanceHeld(); held {
		t.Fatal("fixture: this test must run with NO maintenance hold")
	}
	writeJob(t, f.dir, pendingJob, updaterjob.StatePreflightOK, updaterjob.PhaseStarted, t1.Add(-4*time.Minute))
	if _, pending := InstallPending(t1); !pending {
		t.Fatal("fixture: the job in preflight must read as install pending")
	}
	f.setLiveBook(t1, wdEntryOrder(sid))
	assertWithdrawnThenConfirmed(t, f, row, sid, WithdrawReasonPrefix+"install_pending job "+pendingJob, t1)
	for _, fr := range f.w.touching(sid) {
		if fr.Signal != sid || fr.Type != ntwire.FrameCancelOrder {
			t.Fatalf("the withdraw must cancel the ENTRY only; a frame touched its bracket: %+v", fr)
		}
	}
}
