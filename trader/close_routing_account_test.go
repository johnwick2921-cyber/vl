package trader

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
)

// ── r24 AC-5 — EVERY GO CLOSE PATH STAMPS THE TRADER'S BOUND ACCOUNT ─────────
//
// One NT8 AddOn serves two MNQ traders on two SIM accounts (hoang → Sim101,
// 12345 → SimAccount1). From build 2026-10-14-a the AddOn closes ONLY the
// account a close_position frame names (fail-closed when it cannot resolve
// it), so the frame's `account` field is now the whole routing decision. These
// tests drive each Go close path through its production entry point over a
// REAL in-process TCP wire with BOTH traders bound, and read the frame the
// fake AddOn received: Account must be the closing trader's bound account and
// TraderID the closing trader — never the other trader's.
//
// The API paths (Emergency Flat, Positions → Close) are pinned in
// api/close_routing_account_test.go.
//
// MUTANT (L4, quoted in the PR): drop `Account: t.boundAccount` from
// tcp_trader.go sendCloseAt — the one place a close frame is stamped — and
// every sub-test here goes RED.

const (
	crAcctA = "Sim101"      // trader A (hoang's live binding)
	crAcctB = "SimAccount1" // trader B (12345's live binding)
)

// crWire is the fake AddOn's end of the socket: it decodes every
// close_position frame the bot writes, in order.
type crWire struct {
	srv    *ntwire.TCPServer
	conn   net.Conn
	mu     sync.Mutex
	closes []ntwire.ClosePositionPayload
	notify chan struct{}
}

func (w *crWire) read() {
	for {
		env, err := ntwire.ReadFrame(w.conn)
		if err != nil {
			return
		}
		if env.Type != ntwire.FrameClosePosition {
			continue
		}
		var p ntwire.ClosePositionPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			continue
		}
		w.mu.Lock()
		w.closes = append(w.closes, p)
		w.mu.Unlock()
		select {
		case w.notify <- struct{}{}:
		default:
		}
	}
}

func (w *crWire) all() []ntwire.ClosePositionPayload {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]ntwire.ClosePositionPayload(nil), w.closes...)
}

// waitCloses blocks (bounded) until n close frames arrived, then returns them.
func (w *crWire) waitCloses(t *testing.T, n int) []ntwire.ClosePositionPayload {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		if got := w.all(); len(got) >= n {
			return got
		}
		select {
		case <-w.notify:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %d close_position frame(s); got %+v", n, w.all())
		}
	}
}

// settle proves nothing else is in flight: a sentinel frame down the SAME
// ordered socket, then the close count is read — a "no further close" assert
// after it is a fact, not a sleep.
func (w *crWire) settle(t *testing.T) []ntwire.ClosePositionPayload {
	t.Helper()
	sid := "cr-settle-" + time.Now().Format("150405.000000000")
	if err := w.srv.SendClosePosition(ntwire.ClosePositionPayload{Symbol: "SETTLE", SignalID: sid}); err != nil {
		t.Fatalf("settle send: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := w.all()
		for i, p := range got {
			if p.SignalID == sid {
				return got[:i]
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("settle sentinel never arrived")
		}
		time.Sleep(time.Millisecond)
	}
}

type crRig struct {
	w      *crWire
	st     *store.Store
	a, b   *AutoTrader
	now    time.Time
	stopAt []func()
}

// crRigFor builds ONE server + ONE fake AddOn link with TWO bound MNQ traders
// on two SIM accounts — the live 10-14 topology. tune adjusts each trader's
// strategy (both get the same).
func crRigFor(t *testing.T, now time.Time, tune func(*store.StrategyConfig)) *crRig {
	t.Helper()
	withMaintenanceDir(t) // configured, no hold file: not held
	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: crAcctA, IsSim: true}, {Name: crAcctB, IsSim: true}}, crAcctA)
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, s)
	t.Cleanup(func() { _ = conn.Close() })
	w := &crWire{srv: s, conn: conn, notify: make(chan struct{}, 1)}
	go w.read()

	st, err := store.New(filepath.Join(t.TempDir(), "close-routing.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, acct := range []string{crAcctA, crAcctB} {
		s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: acct, Orders: []ntwire.NT8Order{}}, now)
		s.SeedPositionsForTest(acct, []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 3, AvgPrice: 30000}})
	}
	mk := func(id, acct string) *AutoTrader {
		cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
		if tune != nil {
			tune(&cfg)
		}
		broker := ntTrader.NewTCPTrader(s, "MNQ", acct)
		broker.StartCloseSync(id, "fixture", "ninjatrader", st) // stamps TraderID on outbound frames
		at := &AutoTrader{id: id, name: id, exchange: "ninjatrader", store: st, trader: broker}
		at.config.StrategyConfig = &cfg
		at.config.NinjaTraderSymbol = "MNQ"
		at.mcpClient = &fakeDecisionClient{}
		kernel.ClearDailyForceFlat(id)
		crForgetDailyLossSends(id)
		t.Cleanup(func() { kernel.ClearDailyForceFlat(id); crForgetDailyLossSends(id) })
		return at
	}
	r := &crRig{w: w, st: st, now: now}
	r.a = mk("acr-trader-A", crAcctA)
	r.b = mk("acr-trader-B", crAcctB)
	return r
}

// crForgetDailyLossSends drops the process-wide resend memory for one trader
// id, so a re-run (-count=N) is not suppressed by the previous run's send.
func crForgetDailyLossSends(id string) {
	dailyLossMu.Lock()
	defer dailyLossMu.Unlock()
	for k := range dailyLossFlatSent {
		if strings.HasPrefix(k, id+"|") {
			delete(dailyLossFlatSent, k)
		}
	}
}

// open writes one OPEN bot LONG for at on acct.
func (r *crRig) open(t *testing.T, at *AutoTrader, acct string, qty float64) *store.TraderPosition {
	t.Helper()
	p := &store.TraderPosition{TraderID: at.id, Account: acct, Symbol: "MNQ", Side: "LONG",
		ExchangeType: "ninjatrader", Quantity: qty, EntryQuantity: qty, EntryPrice: 30000,
		EntryTime: r.now.Add(-time.Hour).UnixMilli(), Status: "OPEN",
		CreatedAt: r.now.Add(-time.Hour).UnixMilli(), UpdatedAt: r.now.Add(-time.Hour).UnixMilli()}
	if err := r.st.Position().Create(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// mustOnlyAccount asserts every close frame names want (account AND trader) —
// and that at least one arrived.
func mustOnlyAccount(t *testing.T, path string, got []ntwire.ClosePositionPayload, want *AutoTrader, wantAcct string) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("%s: no close_position frame reached the AddOn", path)
	}
	for _, p := range got {
		if p.Account != wantAcct {
			t.Fatalf("%s: close_position carries account %q, want the closing trader's bound account %q — the AddOn would flatten the wrong account (or refuse): %+v", path, p.Account, wantAcct, p)
		}
		if p.TraderID != want.id {
			t.Fatalf("%s: close_position carries trader_id %q, want %q: %+v", path, p.TraderID, want.id, p)
		}
	}
}

// 15:30 CT on a weekday: between sessions in the default registry, so the EOD
// flat is past-flat by definition (TestEODFlatBetweenSessions).
func crEODNow(t *testing.T) time.Time { return ctTimeAt(t, "2026-10-14", "15:30") }

// EOD FLAT (auto_trader_clock.go enforceEODFlatAt → flattenPosition →
// CloseLong). Scenario (b) of the bug: both traders hold MNQ and both flatten
// at the close — each frame must name ITS OWN account, so the AddOn flattens
// two accounts, not the last-filled one twice.
func TestCloseRoutingEODFlatStampsEachTradersBoundAccount(t *testing.T) {
	r := crRigFor(t, crEODNow(t), nil)
	r.open(t, r.a, crAcctA, 3)
	r.open(t, r.b, crAcctB, 3)

	if !r.b.enforceEODFlatAt(r.now) {
		t.Fatal("fixture: trader B's EOD flat did not act")
	}
	mustOnlyAccount(t, "EOD flat (trader B)", r.w.waitCloses(t, 1), r.b, crAcctB)

	if !r.a.enforceEODFlatAt(r.now) {
		t.Fatal("fixture: trader A's EOD flat did not act")
	}
	got := r.w.waitCloses(t, 2)
	mustOnlyAccount(t, "EOD flat (trader A)", got[1:], r.a, crAcctA)
	if got[0].Account == got[1].Account {
		t.Fatalf("both EOD flats name account %q — one account flattened twice, the other left open past the close", got[0].Account)
	}
}

// DAILY-LOSS FLATTEN (daily_loss_gate.go dailyLossFlattenAt — the last line of
// the 60s monitorTick beat — → flattenPosition). Scenario (a) shape: only
// trader B is latched; trader A's position must see no close at all, and B's
// close names B's account.
func TestCloseRoutingDailyLossFlattenStampsTheLatchedTradersAccount(t *testing.T) {
	r := crRigFor(t, crEODNow(t).Add(-4*time.Hour), nil) // 11:30 CT, mid-session
	r.open(t, r.a, crAcctA, 3)
	r.open(t, r.b, crAcctB, 3)
	kernel.SetDailyForceFlatAt(r.b.id, "test: B's daily loss limit hit", r.now)

	r.a.dailyLossFlattenAt(r.now) // not latched: must send nothing
	r.b.dailyLossFlattenAt(r.now)

	r.w.waitCloses(t, 1)
	got := r.w.settle(t)
	if len(got) != 1 {
		t.Fatalf("exactly ONE close (trader B's) must reach the AddOn; got %d: %+v", len(got), got)
	}
	mustOnlyAccount(t, "daily-loss flatten", got, r.b, crAcctB)
}

// 4.3 LIMIT EXIT (flattenPosition's limit-then-market branch, live with
// EOD_FLAT_LIMIT_TICKS > 0 → CloseWithLimit). The limit frame names the bound
// account; the AddOn sizes it to that account's whole position.
func TestCloseRoutingLimitExitStampsTheBoundAccount(t *testing.T) {
	r := crRigFor(t, crEODNow(t), nil)
	r.b.config.LimitCloseTicks = 2
	r.b.config.LimitCloseMarketAfterS = 600 // the fallback never fires inside the test
	t.Cleanup(r.b.stopLimitFlattens)
	r.open(t, r.b, crAcctB, 3)

	if !r.b.enforceEODFlatAt(r.now) {
		t.Fatal("fixture: trader B's EOD flat did not act")
	}
	got := r.w.waitCloses(t, 1)
	mustOnlyAccount(t, "4.3 limit exit", got[:1], r.b, crAcctB)
	if got[0].LimitPrice <= 0 {
		t.Fatalf("the 4.3 path must send a LIMIT exit (limit_price > 0), got %+v", got[0])
	}
}

// SWING HOLD-CLOSE (mentor_actions.go mentorClosePosition, reached through the
// evaluator's dispatch). The mentor's own sized close names B's account.
func TestCloseRoutingSwingHoldCloseStampsTheBoundAccount(t *testing.T) {
	r := crRigFor(t, time.Now(), func(c *store.StrategyConfig) {
		c.RiskControl.MentorMode = true
		c.RiskControl.MentorWindowStart = "00:00"
		c.RiskControl.MentorWindowMinutes = 1440
	})
	row := store.ArmedOrderDB{TraderID: r.b.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "swing-acr",
		Side: "long", EntryPx: 30000, StopPx: 29950, TargetPx: 30150, Kind: "stop_entry", Condition: "SWING4H",
		State: store.StateFilled, SignalID: "swing-acr-sig", FillPrice: 30000, FillQuantity: 3}
	if err := r.st.ArmedOrders().DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	swingOwnedFixture(t, r.b, row) // m441: the arm's own position must be provable
	mentorRegisterLiveArmForTest("swing-acr", r.b.id, row.ID, "long", 30000)
	resetMentorCounters(r.b)

	r.b.mentorDispatchIntent(mentor.Intent{Action: mentor.ActionClosePosition, ArmID: "swing-acr", Reason: "hold to the 2nd 4h close"}, mentorTierInputs{}, 1000, 1100)

	got := r.w.waitCloses(t, 1)
	mustOnlyAccount(t, "swing hold-close", got, r.b, crAcctB)
	if got[0].Quantity != 3 {
		t.Fatalf("the swing close is sized to its own fill (3), got %d", got[0].Quantity)
	}
}

// DRAWDOWN EMERGENCY CLOSE (auto_trader_risk.go emergencyClosePosition — the
// close the dispatch cites at :213-219; its caller checkPositionDrawdown needs
// a peak-drawdown scene, so the close function itself is driven).
func TestCloseRoutingDrawdownCloseStampsTheBoundAccount(t *testing.T) {
	r := crRigFor(t, time.Now(), nil)
	if err := r.b.emergencyClosePosition("MNQ", "LONG"); err != nil {
		t.Fatal(err)
	}
	mustOnlyAccount(t, "drawdown emergency close", r.w.waitCloses(t, 1), r.b, crAcctB)
}

// A REFUSED CLOSE IS COUNTED (L7). From build 2026-10-14-a the AddOn answers
// a close it will not route (named account not found / not SIM, ambiguous
// legacy close, unknown instrument) with position_close_rejected naming the
// account the close named. The frame reaches THAT trader's close-sync over
// the real wire, which logs the 🚨 line and counts it. Mutant: drop
// telemetry.IncNT8CloseRejected() from the reject watcher → RED.
func TestCloseRoutingRefusedCloseIsCountedForTheNamedTrader(t *testing.T) {
	r := crRigFor(t, time.Now(), nil)
	before := telemetry.NT8CloseRejectedCount()
	if err := ntwire.WriteFrame(r.w.conn, ntwire.FramePositionCloseRejected, ntwire.PositionCloseRejectedPayload{
		SignalID: "cr-refused", Symbol: "MNQ", PositionSide: "long", Account: crAcctB,
		Reason: "the named account 'SimAccount1' was not found in Account.All (fail-closed: no other account is used when one is named)",
	}); err != nil {
		t.Fatalf("reject write: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for telemetry.NT8CloseRejectedCount() < before+1 {
		if time.Now().After(deadline) {
			t.Fatalf("a position_close_rejected for the bound account must be counted (vl_nt8_close_rejected_total); count stayed %d", telemetry.NT8CloseRejectedCount())
		}
		time.Sleep(time.Millisecond)
	}
}
