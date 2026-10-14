package trader

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── FULL-DAY-REPLAY ─────────────────────────────────────────────────────────
//
// A REAL recorded trading day (MNQ 12-26, 17:00 CT 10-12 → 16:00 CT 10-13, plus
// the 10-13 evening the fixture also carries) played through the PRODUCTION
// mentor path, bar by bar:
//
//	seed      mentorSeedAtStart over a store that holds exactly the bars the live
//	          store held at the seed instant (1m + native 1h, both contracts)
//	tick      mentorEventPassAt — the event loop's call site — then the 5 s
//	          placement cadence (mentorPlacementCadencePass) sampled at +5 s/+30 s
//	ledger    mentorDispatchIntent → mentorArmIntent → the armed pass
//	          (maybeManageArmedOrdersAt) → TCPTrader → a loopback TCP server
//	broker    a fake AddOn on the far side of that socket: it ACKS and RESTS the
//	          entries it receives, cancels them on cancel_order, moves stops on
//	          move_stop, records closes, and publishes the order book. It fills
//	          NOTHING on its own — the one fill of the day is the INJECTED swing
//	          (no swing ever filled live, so (c) needs one).
//	config    the LIVE hoang strategy (a5b7662e…, trader …_1781246265) through the
//	          production builder (store.Strategy.ParseConfig → AutoTraderConfig →
//	          mentorEvaluatorConfig), scrubbed of prose; no keys, no accounts.
//
// Two tests:
//
//	TestFullDayReplayFidelity (default run, ~15 s) — seeds at the LIVE seed
//	instant (00:59:15 CT 10-13) and must author the SAME 11 ISB LONG arms the
//	live bot authored 03:28–04:33 CT (armed_orders 329…340, the other trader's
//	335/341 excluded), minute, side, entry and stop.
//
//	TestFullDayReplay (FULL_DAY_REPLAY=1) — the whole day with TWO traders (one
//	account each), a restart at 21:30 CT and the invariants the 10-13 bugs broke,
//	each a named sub-test.

const (
	fdrFixturePath  = "testdata/mnq_1m_2026-10-12_full_day.json.gz"
	fdrStrategyPath = "testdata/full_day_replay_strategy_a5b7662e.json"
	fdrContract     = "MNQ 12-26"
	fdrAddonBuild   = "2026-10-13-a" // the AddOn the live bot ran from release #20 on

	fdrLiveSeedMs     int64 = 1791871155000 // 00:59:15 CT 10-13 — the live hoang reload (data/vl_2026-10-11.log)
	fdrFidelityStopMs int64 = 1791885600000 // 05:00 CT 10-13
	fdrDayStartMs     int64 = 1791842400000 // 17:00 CT 10-12 — the CME session open
	fdrRestartMs      int64 = 1791858600000 // 21:30 CT 10-12
	fdrDayEndMs       int64 = 1791925200000 // 16:00 CT 10-13
	fdrEveningEndMs   int64 = 1791950640000 // 23:04 CT 10-13 — the fixture's last bar
	fdrSwingBarMs     int64 = 1791858300000 // 21:25 CT 10-12 — the 5m bar the injected swing fills on
	fdrSwingRiskPts         = 55.0
	fdrSwingTargetPts       = 130.0
)

// fdrLiveArm is one row the LIVE bot authored for trader hoang between the
// 00:59:15 seed and 05:00 CT 10-13 (data.db armed_orders, created_at is UTC).
type fdrLiveArm struct {
	id      int64
	barCT   string // the bar-close minute the arm belongs to (CT)
	created string // the live created_at, in CT
	side    string
	entry   float64
	stop    float64
}

// fdrLiveArms — read with sqlite3 -readonly from data/data.db on 2026-10-14:
// SELECT id, created_at, side, entry_px, stop_px FROM armed_orders WHERE id
// BETWEEN 329 AND 341 AND trader_id LIKE '%1781246265'. Rows 335 and 341 are
// the OTHER trader's (…_1791870197) and are not part of hoang's set.
var fdrLiveArms = []fdrLiveArm{
	{329, "03:28", "03:28:00.007", "long", 30754.75, 30748.0},
	{330, "03:31", "03:30:59.960", "long", 30766.25, 30753.75},
	{331, "03:34", "03:34:00.875", "long", 30762.0, 30752.75},
	{332, "03:39", "03:39:00.494", "long", 30764.75, 30753.25},
	{333, "03:42", "03:42:00.002", "long", 30760.75, 30752.25},
	{334, "03:46", "03:46:00.077", "long", 30767.5, 30756.25},
	{336, "03:48", "03:48:00.075", "long", 30760.75, 30752.25},
	{337, "03:50", "03:50:00.134", "long", 30767.0, 30759.5},
	{338, "04:11", "04:11:00.029", "long", 30773.0, 30759.25},
	{339, "04:31", "04:31:00.629", "long", 30771.0, 30762.75},
	{340, "04:33", "04:33:00.316", "long", 30770.5, 30763.25},
}

// ── fixture ─────────────────────────────────────────────────────────────────

// fixtureBar is one recorded bar with its futures contract (the seed's
// ContractSpans + mentorFullKeyLevelHistory stitch across contracts).
type fixtureBar struct {
	kline    market.Kline
	contract string
}

// loadFullDayFixture loads the recorded MNQ fixture (warm-up + replay day) from
// the gzip testdata blob: the 1m bars and the native 1h bars (the 1h feeds ONLY
// the seed's roll-gap measurement — the live store's native 1h retention).
func loadFullDayFixture(t *testing.T) ([]fixtureBar, []fixtureBar) {
	t.Helper()
	raw, err := os.ReadFile(fdrFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	var doc struct {
		Bars map[string][][]json.RawMessage `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	parse := func(tf string, durMs int64) []fixtureBar {
		rows := doc.Bars[tf]
		out := make([]fixtureBar, 0, len(rows))
		for _, r := range rows {
			if len(r) < 6 {
				continue
			}
			var ot, o, h, l, c float64
			var contract string
			if json.Unmarshal(r[0], &ot) != nil || json.Unmarshal(r[1], &o) != nil ||
				json.Unmarshal(r[2], &h) != nil || json.Unmarshal(r[3], &l) != nil ||
				json.Unmarshal(r[4], &c) != nil || json.Unmarshal(r[5], &contract) != nil {
				continue
			}
			oo := int64(ot)
			out = append(out, fixtureBar{
				kline: market.Kline{
					Open: o, High: h, Low: l, Close: c,
					OpenTime: oo, CloseTime: oo + durMs - 1, Final: true,
				},
				contract: contract,
			})
		}
		return out
	}
	return parse("1m", 60_000), parse("1h", 3_600_000)
}

// loadReplayStrategy loads the live hoang strategy config through the
// PRODUCTION parser (store.Strategy.ParseConfig — the call manager's
// addTraderFromStore makes), so every knob, default and applyMissingDefaults
// backfill is the live one.
func loadReplayStrategy(t *testing.T) *store.StrategyConfig {
	t.Helper()
	raw, err := os.ReadFile(fdrStrategyPath)
	if err != nil {
		t.Fatalf("read strategy: %v", err)
	}
	cfg, err := (&store.Strategy{ID: "a5b7662e-7bf7-49bb-9f09-7efa48f95ac8", Config: string(raw)}).ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if !cfg.RiskControl.MentorMode || cfg.RiskControl.MentorWindowMinutes != -1 {
		t.Fatalf("strategy fixture is not the live mentor config: %+v", cfg.RiskControl)
	}
	return cfg
}

// ── the world: store, clock, bars, broker ───────────────────────────────────

type fdrWorld struct {
	t     *testing.T
	st    *store.Store
	srv   *ntwire.TCPServer
	addon *fdrAddon

	fix1m, fix1h []fixtureBar
	bars         []market.Kline // the CURRENT contract's 1m bars (the NT8 ring), ascending
	cur          atomic.Int64   // index into bars of the newest CLOSED bar the ring shows
	nowMs        atomic.Int64   // the replay clock (mentorNowSource)
	storedTo     int64          // store holds every fixture bar with OpenTime <= storedTo
}

func newFdrWorld(t *testing.T, seedMs int64, accounts []string, snapshotAccounts map[string]bool, build string) *fdrWorld {
	t.Helper()
	w := &fdrWorld{t: t}
	w.fix1m, w.fix1h = loadFullDayFixture(t)
	for _, b := range w.fix1m {
		if b.contract == fdrContract {
			w.bars = append(w.bars, b.kline)
		}
	}
	if len(w.bars) == 0 {
		t.Fatalf("fixture holds no %s bars", fdrContract)
	}
	w.cur.Store(-1)

	st, err := store.New(filepath.Join(t.TempDir(), "full-day-replay.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w.st = st
	if err := store.NewBarHistoryStore(st.GormDB()).Migrate(); err != nil {
		t.Fatalf("bars migrate: %v", err)
	}
	w.storedTo = math.MinInt64
	w.storeBarsUpTo(seedMs)

	// Package-level seams the replay pins; restored for the next test.
	prevProvider := market.FuturesBarsProvider
	prevNow := mentorNowSource
	market.FuturesBarsProvider = w.provide
	mentorNowSource = w.now
	t.Cleanup(func() {
		market.FuturesBarsProvider = prevProvider
		mentorNowSource = prevNow
		mentorLatestPriceSource = nil
		mentorConfluenceForIntent = nil
		mentorSetArmExpiryWire = nil
	})
	resetMentorLiveArms(t)
	t.Setenv("MENTOR_PLACE", "1")
	t.Setenv("STOP_ENTRY_SEAM", "on")
	withMaintenanceDir(t)

	// The NT8 link: a real TCP server on loopback and the fake AddOn dialled in.
	w.srv = ntwire.NewTCPServer(nil)
	w.srv.SetAddrForTest("127.0.0.1:0")
	infos := make([]ntwire.AccountInfo, 0, len(accounts))
	for _, a := range accounts {
		infos = append(infos, ntwire.AccountInfo{Name: a, IsSim: true})
	}
	w.srv.SetAccountsList(infos, accounts[0])
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = w.srv.Stop(); cancel() })
	conn, err := net.Dial("tcp", w.srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, w.srv)
	w.addon = newFdrAddon(t, w, conn, build, accounts, snapshotAccounts)
	for _, a := range accounts {
		w.srv.SeedPositionsForTest(a, []ntwire.OpenPosition{})
	}
	return w
}

// storeBarsUpTo appends every fixture bar (1m + native 1h) with OpenTime in
// (storedTo, toMs] to the bar store — what the live bar persistence had
// written by toMs. The seed is the only reader of the store; the evaluator's
// per-bar input is the ring (provide).
func (w *fdrWorld) storeBarsUpTo(toMs int64) {
	w.t.Helper()
	bh := store.NewBarHistoryStore(w.st.GormDB())
	add := func(tf string, bars []fixtureBar) {
		rows := make([]store.BarHistoryDB, 0, 1024)
		for _, b := range bars {
			if b.kline.OpenTime <= w.storedTo || b.kline.OpenTime > toMs {
				continue
			}
			rows = append(rows, store.BarHistoryDB{
				Symbol: "MNQ", TF: tf, Contract: b.contract,
				OpenTimeMs: b.kline.OpenTime, O: b.kline.Open, H: b.kline.High, L: b.kline.Low, C: b.kline.Close,
				Source: store.BarSourceHistorical,
			})
		}
		if err := bh.InsertBars(rows); err != nil {
			w.t.Fatalf("InsertBars(%s): %v", tf, err)
		}
	}
	add("1m", w.fix1m)
	add("1h", w.fix1h)
	w.storedTo = toMs
}

func (w *fdrWorld) now() time.Time { return time.UnixMilli(w.nowMs.Load()) }

func (w *fdrWorld) setNow(ms int64) { w.nowMs.Store(ms) }

// barIndexAtOrAfter returns the first ring index whose OpenTime >= ms.
func (w *fdrWorld) barIndexAtOrAfter(ms int64) int {
	return sort.Search(len(w.bars), func(i int) bool { return w.bars[i].OpenTime >= ms })
}

// provide is market.FuturesBarsProvider for the replay: the NT8 ring as it
// stood when bar `cur` closed. 1m is the ring itself; any higher timeframe is
// aggregated from it on epoch-aligned buckets (the native 5m/15m the AddOn
// streams are epoch-aligned), the bucket still forming marked !Final. Every
// slice is capped (len == cap) so no caller can append into the fixture.
func (w *fdrWorld) provide(symbol, tf string, n int) []market.Kline {
	cur := int(w.cur.Load())
	if cur < 0 || !strings.EqualFold(instrumentRoot(symbol), "MNQ") {
		return nil
	}
	vis := w.bars[: cur+1 : cur+1]
	if tf == "1m" {
		return fdrTail(vis, n)
	}
	durMs, ok := kernel.TFDurationMs(tf)
	if !ok || durMs <= 60_000 || durMs >= 4*3_600_000 {
		return nil // the mentor path asks 1m and 5m only; 4h needs the 17:00 anchor
	}
	k := int(durMs / 60_000)
	want := n
	if want <= 0 || want > 2000 {
		want = 2000
	}
	src := vis
	if lim := (want + 2) * k; len(src) > lim {
		src = src[len(src)-lim:]
	}
	return fdrTail(fdrAggregate(src, durMs, w.nowMs.Load()), n)
}

func fdrTail(bars []market.Kline, n int) []market.Kline {
	if n <= 0 || n > len(bars) {
		n = len(bars)
	}
	s := len(bars) - n
	return bars[s:len(bars):len(bars)]
}

// fdrAggregate buckets 1m bars into durMs buckets on epoch boundaries. A
// bucket whose last minute has not closed by nowMs is FORMING (!Final).
func fdrAggregate(src []market.Kline, durMs, nowMs int64) []market.Kline {
	out := make([]market.Kline, 0, len(src)/int(durMs/60_000)+1)
	for _, b := range src {
		start := b.OpenTime - b.OpenTime%durMs
		if n := len(out); n > 0 && out[n-1].OpenTime == start {
			c := &out[n-1]
			c.High = math.Max(c.High, b.High)
			c.Low = math.Min(c.Low, b.Low)
			c.Close = b.Close
			c.Volume += b.Volume
			continue
		}
		out = append(out, market.Kline{OpenTime: start, CloseTime: start + durMs - 1,
			Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume})
	}
	for i := range out {
		out[i].Final = out[i].CloseTime < nowMs
	}
	return out[:len(out):len(out)]
}

// ── the fake AddOn ──────────────────────────────────────────────────────────

// fdrWire is one frame the AddOn received, stamped with the replay clock.
type fdrWire struct {
	atMs     int64
	typ      ntwire.FrameType
	account  string
	traderID string
	signalID string
	side     string
	price    float64 // entry trigger / new stop / close limit
	stop     float64
	target   float64
	qty      int
}

type fdrAddon struct {
	t     *testing.T
	w     *fdrWorld
	conn  net.Conn
	build string

	writeMu sync.Mutex
	mu      sync.Mutex
	inbox   []ntwire.Envelope
	recv    int64
	done    chan struct{}

	// book state — touched only by the test goroutine (settle).
	books     map[string][]ntwire.NT8Order // account → orders resting at the broker
	snapshots map[string]bool              // accounts whose book the AddOn publishes
	signals   map[string]ntwire.SignalPayload
	log       []fdrWire
	orderSeq  int
}

func newFdrAddon(t *testing.T, w *fdrWorld, conn net.Conn, build string, accounts []string, snapshots map[string]bool) *fdrAddon {
	a := &fdrAddon{t: t, w: w, conn: conn, build: build, done: make(chan struct{}),
		books: map[string][]ntwire.NT8Order{}, snapshots: snapshots, signals: map[string]ntwire.SignalPayload{}}
	go func() {
		defer close(a.done)
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			a.mu.Lock()
			a.inbox = append(a.inbox, env)
			a.recv++
			a.mu.Unlock()
		}
	}()
	a.write(ntwire.FrameHello, ntwire.HelloPayload{ProtocolVersion: ntwire.ProtocolVersion, Source: "vltrader-addon", BuildID: build})
	a.write(ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: build})
	a.write(ntwire.FrameFeedStatus, ntwire.FeedStatusPayload{PriceStatus: "Connected", Time: time.Now().UTC().Format(time.RFC3339)})
	for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(w.srv.FarSideBuildID(), build) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if !ntwire.FarSideProven(w.srv.FarSideBuildID(), build) {
		t.Fatal("fixture: far-side build never proven")
	}
	// keep-alive: the real AddOn heartbeats; the wall-clock run lasts minutes.
	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				a.write(ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: build})
			}
		}
	}()
	t.Cleanup(func() { close(stop); _ = conn.Close() })
	return a
}

func (a *fdrAddon) write(ft ntwire.FrameType, p any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	_ = a.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = ntwire.WriteFrame(a.conn, ft, p)
}

// quiesce waits until no new frame has arrived for a few milliseconds — the
// loopback socket delivers in microseconds, so a quiet gap means every frame
// the production code wrote this step has been read.
func (a *fdrAddon) quiesce() {
	last := int64(-1)
	quiet := 0
	for i := 0; i < 400 && quiet < 3; i++ {
		time.Sleep(time.Millisecond)
		a.mu.Lock()
		n := a.recv
		a.mu.Unlock()
		if n == last {
			quiet++
		} else {
			quiet = 0
			last = n
		}
	}
}

// settle drains the frames production sent this step and plays the AddOn's
// part: rest/ack entries, cancel, move stops, record closes; then publishes the
// book of every snapshot account (receipt stamped on the WALL clock, exactly
// as the live server stamps it) and persists it the way main.go's sink does.
func (a *fdrAddon) settle() {
	a.quiesce()
	a.mu.Lock()
	frames := a.inbox
	a.inbox = nil
	a.mu.Unlock()
	nowMs := a.w.nowMs.Load()
	fed := false
	for _, env := range frames {
		switch env.Type {
		case ntwire.FrameSignal:
			var p ntwire.SignalPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.signals[p.SignalID] = p
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type, account: p.Account, traderID: p.TraderID,
				signalID: p.SignalID, side: p.Side, price: p.StopPrice, stop: p.StopLoss, target: p.TakeProfit, qty: p.Quantity})
			if !a.snapshots[p.Account] {
				continue // an account the AddOn does not serve never acks
			}
			a.orderSeq++
			action := "buy"
			if strings.EqualFold(p.Side, "short") {
				action = "sell"
			}
			typ := "stop"
			lim := 0.0
			if p.StopLimit {
				typ, lim = "stop_limit", p.StopPrice
			}
			a.books[p.Account] = append(a.books[p.Account], ntwire.NT8Order{
				OrderID: "fdr-" + strconv.Itoa(a.orderSeq), Symbol: "MNQ", Name: p.SignalID, Action: action,
				Type: typ, StopPrice: p.StopPrice, LimitPrice: lim, Quantity: p.Quantity, State: "Working", TimeMs: nowMs,
			})
			for _, s := range []string{"accepted", "working"} {
				a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: p.SignalID, State: s,
					Symbol: "MNQ", Account: p.Account, TraderID: p.TraderID, Quantity: p.Quantity, Seq: p.Seq})
			}
			fed = true
		case ntwire.FrameCancelOrder:
			var p ntwire.CancelOrderPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type, account: p.Account, traderID: p.TraderID, signalID: p.SignalID})
			if a.removeOrder(p.Account, p.SignalID) {
				a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: p.SignalID, State: "cancelled",
					Symbol: "MNQ", Account: p.Account, TraderID: p.TraderID, Seq: p.Seq})
				fed = true
			}
		case ntwire.FrameMoveStop:
			var p ntwire.MoveStopPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type, account: p.Account, traderID: p.TraderID, signalID: p.SignalID, price: p.NewStopLoss})
			book := a.books[p.Account]
			for i := range book {
				if strings.HasSuffix(book[i].Name, "-sl") && (p.SignalID == "" || strings.HasPrefix(book[i].Name, p.SignalID)) {
					book[i].StopPrice = p.NewStopLoss
				}
			}
		case ntwire.FrameClosePosition:
			var p ntwire.ClosePositionPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type, account: p.Account, traderID: p.TraderID, signalID: p.SignalID, side: p.Side, qty: p.Quantity, price: p.LimitPrice})
			// The account goes flat: the bracket children die with the position.
			kept := a.books[p.Account][:0]
			for _, o := range a.books[p.Account] {
				if !isBracketChild(o.Name) {
					kept = append(kept, o)
				}
			}
			a.books[p.Account] = kept
			a.w.srv.SeedPositionsForTest(p.Account, []ntwire.OpenPosition{})
		case ntwire.FrameModifyBracket, ntwire.FramePlaceProtectiveStop:
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type})
		}
	}
	a.publish()
	if fed {
		time.Sleep(2 * time.Millisecond) // the router hands the fed updates to the listeners
	}
}

func (a *fdrAddon) removeOrder(account, name string) bool {
	book := a.books[account]
	for i := range book {
		if book[i].Name == name {
			a.books[account] = append(book[:i:i], book[i+1:]...)
			return true
		}
	}
	return false
}

// publish puts every served account's book in the server cache (wall-clock
// receipt, like the live read loop) and the store (main.go's snapshot sink).
func (a *fdrAddon) publish() {
	for acct, on := range a.snapshots {
		if !on {
			continue
		}
		orders := append([]ntwire.NT8Order{}, a.books[acct]...)
		p := ntwire.OrderSnapshotPayload{Account: acct, BuildID: a.build, EmittedMs: a.w.nowMs.Load(), Reason: "periodic", Orders: orders}
		recv := time.Now()
		a.w.srv.OrderSnapshots().PutAt(p, recv)
		b, _ := json.Marshal(p.Orders)
		_ = a.w.st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{
			Account: acct, BuildID: a.build, Reason: p.Reason, OrdersJSON: string(b),
			OrderCount: len(p.Orders), WorkingCount: len(p.WorkingOrders()), EmittedMs: p.EmittedMs, ReceivedMs: recv.UnixMilli(),
		})
	}
}

// fill plays an NT8 fill of a resting entry: the entry leaves the book, its
// bracket children (-sl, -tp) start working, the account shows the position,
// and the order_update + fill frames go to the server's routers.
func (a *fdrAddon) fill(account, signalID string, price float64) {
	p, ok := a.signals[signalID]
	if !ok {
		a.t.Fatalf("fill of an unknown signal %s", signalID)
	}
	a.removeOrder(account, signalID)
	exit := "sell"
	if strings.EqualFold(p.Side, "short") {
		exit = "buy"
	}
	a.orderSeq++
	a.books[account] = append(a.books[account],
		ntwire.NT8Order{OrderID: "fdr-" + strconv.Itoa(a.orderSeq), Symbol: "MNQ", Name: signalID + "-sl", Action: exit, Type: "stop", StopPrice: p.StopLoss, Quantity: p.Quantity, State: "Working", TimeMs: a.w.nowMs.Load()},
		ntwire.NT8Order{OrderID: "fdr-" + strconv.Itoa(a.orderSeq+1), Symbol: "MNQ", Name: signalID + "-tp", Action: exit, Type: "limit", LimitPrice: p.TakeProfit, Quantity: p.Quantity, State: "Working", TimeMs: a.w.nowMs.Load()},
	)
	a.orderSeq++
	a.w.srv.SeedPositionsForTest(account, []ntwire.OpenPosition{{Symbol: "MNQ", Side: strings.ToLower(p.Side), Quantity: p.Quantity, AvgPrice: price}})
	a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: signalID, State: "filled", FillPrice: price,
		Quantity: p.Quantity, Symbol: "MNQ", Account: account, TraderID: p.TraderID, Seq: p.Seq})
	a.w.srv.FeedFillForTest(ntwire.FillPayload{SignalID: signalID, Symbol: "MNQ", Account: account, FillPrice: price,
		FillTime: time.UnixMilli(a.w.nowMs.Load()).UTC().Format(time.RFC3339), Side: p.Side, Quantity: p.Quantity, Status: "filled", TraderID: p.TraderID, Seq: p.Seq})
	a.publish()
	time.Sleep(2 * time.Millisecond)
}

func (a *fdrAddon) workingEntries(account string) []ntwire.NT8Order {
	var out []ntwire.NT8Order
	for _, o := range a.books[account] {
		if o.IsWorking() && !isBracketChild(o.Name) {
			out = append(out, o)
		}
	}
	return out
}

// ── the traders ─────────────────────────────────────────────────────────────

// fdrTick is one intent the evaluator emitted, stamped with its bar.
type fdrTick struct {
	barIdx int
	in     mentor.Intent
}

// fdrTrader is one mentor trader in the replay and what was observed of it.
type fdrTrader struct {
	id, account string
	at          *AutoTrader
	epochs      []int64
	intents     []fdrTick
	levelPx     map[string]float64 // lvl ArmID → level price (Intent.Anchor)
	curBar      int
}

// newFdrTrader builds one mentor AutoTrader the way NewAutoTrader's ninjatrader
// branch does — the TCP trader bound to its account, the NT8 wiring, then the
// production mentor seams and the seed — minus NewTraderFromEnv (that would
// start the process-singleton TCP listener) and minus Run (the replay drives the
// event-pass call site itself). The production `now` seam is time.Now; the
// replay re-pins it to the bar clock (the ONLY seam the replay overrides).
func (w *fdrWorld) newFdrTrader(ft *fdrTrader, cfg *store.StrategyConfig) {
	w.t.Helper()
	nt := ntTrader.NewTCPTrader(w.srv, "MNQ", ft.account)
	at := &AutoTrader{
		id: ft.id, name: ft.id, userID: "u", exchange: "ninjatrader", exchangeID: "ex",
		config: AutoTraderConfig{ID: ft.id, Name: ft.id, Exchange: "ninjatrader", ExchangeID: "ex",
			NinjaTraderSymbol: "MNQ", NinjaTraderAccount: ft.account, ScanInterval: time.Minute, StrategyConfig: cfg},
		trader: nt, store: w.st, mcpClient: &fakeDecisionClient{},
		positionFirstSeenTime: map[string]int64{}, positionMentorOwned: map[string]bool{},
		kickCh: make(chan string, 4), mentorFillCh: make(chan mentorFillReceipt, 256),
		peakPnLCache: map[string]float64{}, startTime: time.Now(), lastResetTime: time.Now(), lastBalanceSyncTime: time.Now(),
	}
	nt.SetOpenOrdersSource(at.ledgerOpenOrders)
	nt.SetRejectSink(at.recordBrokerRejection)
	wireNT8Maintenance(at, nt)
	wireNT8EntryLatch(at, nt)
	nt.StartCloseSync(at.id, at.exchangeID, at.exchange, w.st) // Run's first NT8 step: stamps the trader on every frame
	at.mentorIntentTap = func(in mentor.Intent) {
		ft.intents = append(ft.intents, fdrTick{barIdx: ft.curBar, in: in})
		if strings.HasPrefix(in.ArmID, "lvl-") && (in.Action == mentor.PlaceStopEntry || in.Action == mentor.PlaceStopLimitEntry) && in.Anchor != 0 {
			ft.levelPx[in.ArmID] = in.Anchor
		}
	}
	at.mentorWireProductionSeams()
	mentorNowSource = w.now
	at.mentorSeedAtStart()
	ft.at = at
	ft.epochs = append(ft.epochs, at.mentorArmEpoch)
}

// ── the ledger timeline ─────────────────────────────────────────────────────

// fdrRowLife is one armed_orders row's life as the replay saw it.
type fdrRowLife struct {
	row       store.ArmedOrderDB
	bornBar   int
	bornMs    int64 // the replay clock when the row was first seen
	endBar    int   // -1 while non-terminal
	endMs     int64
	endState  string
	endReason string
}

type fdrLedger struct {
	rows map[int64]*fdrRowLife
}

func (w *fdrWorld) scanLedger(l *fdrLedger, barIdx int) {
	var rows []store.ArmedOrderDB
	if err := w.st.GormDB().Order("id ASC").Find(&rows).Error; err != nil {
		w.t.Fatalf("ledger read: %v", err)
	}
	nowMs := w.nowMs.Load()
	for _, r := range rows {
		life, ok := l.rows[r.ID]
		if !ok {
			life = &fdrRowLife{row: r, bornBar: barIdx, bornMs: nowMs, endBar: -1}
			l.rows[r.ID] = life
		}
		life.row = r
		if life.endBar < 0 && store.IsTerminalArmState(strings.ToLower(r.State)) && !strings.EqualFold(r.State, store.StateFilled) {
			life.endBar, life.endMs, life.endState, life.endReason = barIdx, nowMs, r.State, r.StateReason
		}
	}
}

func (l *fdrLedger) sorted() []*fdrRowLife {
	out := make([]*fdrRowLife, 0, len(l.rows))
	for _, r := range l.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].row.ID < out[j].row.ID })
	return out
}

func fdrCT(ms int64) string {
	return time.UnixMilli(ms).In(kernel.CTLocation()).Format("01-02 15:04")
}

// ── the drive ───────────────────────────────────────────────────────────────

// step plays one closed 1m bar: the event pass at the bar's close, the AddOn's
// answers, then the 5 s placement cadence sampled at +5 s and +30 s.
func (w *fdrWorld) step(i int, traders []*fdrTrader, l *fdrLedger) {
	w.cur.Store(int64(i))
	closeMs := w.bars[i].CloseTime + 1
	w.setNow(closeMs)
	for _, ft := range traders {
		ft.curBar = i
		ft.at.mentorEventPassAt(w.now())
	}
	w.addon.settle()
	for _, off := range []int64{5_000, 30_000} {
		w.setNow(closeMs + off)
		for _, ft := range traders {
			ft.at.mentorPlacementCadencePass()
		}
		w.addon.settle()
	}
	w.scanLedger(l, i)
}

// ── TestFullDayReplayFidelity ───────────────────────────────────────────────

// TestFullDayReplayFidelity is the gate before any invariant: seeded at the
// LIVE seed instant with the LIVE config, the harness must author the SAME arms
// the live bot authored 00:59 → 05:00 CT 10-13. The live link that night had
// no order snapshot for hoang's account (the AddOn sent snapshots only for its
// active account), so every placement was refused "account unverifiable" — the
// fake AddOn serves no snapshot for the account here either.
func TestFullDayReplayFidelity(t *testing.T) {
	const acct = "SimReplayA"
	w := newFdrWorld(t, fdrLiveSeedMs, []string{acct}, map[string]bool{acct: false}, "2026-10-04-d1")
	cfg := loadReplayStrategy(t)
	w.setNow(fdrLiveSeedMs)
	w.cur.Store(int64(w.barIndexAtOrAfter(fdrLiveSeedMs) - 1))
	ft := &fdrTrader{id: "fdr-hoang", account: acct, levelPx: map[string]float64{}}
	w.newFdrTrader(ft, cfg)
	l := &fdrLedger{rows: map[int64]*fdrRowLife{}}
	start := w.barIndexAtOrAfter(fdrLiveSeedMs) // the first bar to CLOSE after the seed
	stop := w.barIndexAtOrAfter(fdrFidelityStopMs)
	for i := start; i < stop; i++ {
		w.step(i, []*fdrTrader{ft}, l)
	}
	var got []*fdrRowLife
	for _, r := range l.sorted() {
		if r.row.TraderID == ft.id && isMentorArmOrigin(r.row) {
			got = append(got, r)
		}
	}
	t.Logf("FIDELITY live vs harness (CT bar-close minute · side · entry · stop):")
	n := len(got)
	if len(fdrLiveArms) > n {
		n = len(fdrLiveArms)
	}
	mismatch := 0
	for i := 0; i < n; i++ {
		var lv, hv string
		var lvA *fdrLiveArm
		if i < len(fdrLiveArms) {
			lvA = &fdrLiveArms[i]
			lv = fmt.Sprintf("#%d %s (%s) %s %.2f/%.2f", lvA.id, lvA.barCT, lvA.created, strings.ToUpper(lvA.side), lvA.entry, lvA.stop)
		}
		ok := false
		if i < len(got) {
			r := got[i]
			ct := time.UnixMilli(r.bornMs).In(kernel.CTLocation()).Format("15:04")
			hv = fmt.Sprintf("row %d %s %s %.2f/%.2f [%s] %s", r.row.ID, ct, strings.ToUpper(r.row.Side), r.row.EntryPx, r.row.StopPx, r.row.Condition, r.row.Scenario)
			ok = lvA != nil && ct == lvA.barCT && strings.EqualFold(r.row.Side, lvA.side) && r.row.EntryPx == lvA.entry && r.row.StopPx == lvA.stop
		}
		mark := "MATCH"
		if !ok {
			mark = "DIFF"
			mismatch++
		}
		t.Logf("  %-5s live %-50s | harness %s", mark, lv, hv)
	}
	if mismatch != 0 {
		t.Fatalf("FIDELITY: %d of %d rows differ from the live 11 (see the table)", mismatch, n)
	}
}
