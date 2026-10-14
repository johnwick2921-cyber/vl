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

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── FULL-DAY-REPLAY ─────────────────────────────────────────────────────────
//
// REAL recorded MNQ 12-26 sessions played through the PRODUCTION mentor path,
// bar by bar:
//
//	seed      mentorSeedAtStart over a store holding exactly the bars the live
//	          store held at the seed instant (1m + native 1h, both contracts)
//	tick      mentorEventPassAt — the event loop's call site — then the 5 s
//	          placement cadence (mentorPlacementCadencePass) sampled at +5 s/+30 s
//	ledger    mentorDispatchIntent → mentorArmIntent → the armed pass
//	          (maybeManageArmedOrdersAt) → TCPTrader → a loopback TCP server
//	broker    a fake AddOn on the far side of that socket: it ACKS and RESTS the
//	          entries it receives, cancels them on cancel_order, moves stops on
//	          move_stop, closes on close_position (and reports position_close),
//	          and publishes the order book every step. It fills NOTHING on its
//	          own — the one fill is the INJECTED swing (no swing ever filled
//	          live, so (c) needs one).
//	config    the LIVE hoang strategy (a5b7662e…, trader …_1781246265) through the
//	          production builder (store.Strategy.ParseConfig → AutoTraderConfig →
//	          mentorEvaluatorConfig), scrubbed of prose; no keys, no accounts.
//	clock     the bars are history, so the replay clock is pinned where
//	          production reads time.Now beside a replay-stamped book: the mentor
//	          `now` seam, the restart rebuild's book read and the entry latch's
//	          clock — the SAME evidence functions, only the clock differs. The
//	          position-reconcile C8 sweep (a wall-clock goroutine in production)
//	          is played at replay speed (c8Sweep).
//
// Two tests:
//
//	TestFullDayReplayFidelity (default run, ~5 s) — seeds at the LIVE seed
//	instant (00:59:15 CT 10-13) and must author the SAME ISB LONG arms the
//	live bot authored 03:28–04:33 CT (armed_orders 329…340, the other trader's
//	335/341 excluded): minute, side, entry and stop.
//
//	TestFullDayReplay (FULL_DAY_REPLAY=1, ~90 s) — 17:00 CT 10-11 (the Sunday
//	open) → 16:00 CT 10-13 with TWO traders (one SIM account each), a restart
//	at 21:30 CT 10-12, an injected swing fill at 21:25 CT 10-12, and the
//	invariants the 10-13 bugs broke, each a named sub-test. It starts at the
//	Sunday open because the 10-12 session emits ZERO level intents on the real
//	tape (the kernel refuses every level setup — see the (g) counters), while
//	the 10-11 evening emits two PHL Places — the same two the live bot sized at
//	19:49/19:54 CT 10-11 (NT8's ~10-min Sunday-reopen lag; the replay's ring
//	has no lag, so they land at 19:39/19:44).

const (
	fdrFixturePath  = "testdata/mnq_1m_2026-10-12_full_day.json.gz"
	fdrStrategyPath = "testdata/full_day_replay_strategy_a5b7662e.json"
	fdrContract     = "MNQ 12-26"
	fdrAddonBuild   = "2026-10-13-a" // the AddOn the live bot ran from release #20 on

	fdrLiveSeedMs     int64 = 1791871155000 // 00:59:15 CT 10-13 — the live hoang reload (data/vl_2026-10-11.log)
	fdrFidelityStopMs int64 = 1791885600000 // 05:00 CT 10-13
	fdrReplayFromMs   int64 = 1791756000000 // 17:00 CT 10-11 — the Sunday open the full replay starts at
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
	// wireShift (rel22 P2-1): the reverse ISB at EMA 9 now authors its WIRE
	// price. The live bot authored extreme ± 1.0 and the executor re-added the
	// planner's 2-tick stop-entry offset (wire ± 1.5); since rel22 the kernel
	// authors extreme ± 1.5 and the executor adds nothing to a mentor row, so
	// the harness entry is the live entry + this shift (side-aware). The WIRE
	// price is unchanged for these rows. 0 for every regular ISB.
	wireShift float64
}

// wantEntry is the entry the harness must author for this live row.
func (a fdrLiveArm) wantEntry() float64 {
	if strings.EqualFold(a.side, "short") {
		return a.entry - a.wireShift
	}
	return a.entry + a.wireShift
}

// fdrLiveArms — read with sqlite3 -readonly from data/data.db on 2026-10-14:
// SELECT id, created_at, side, entry_px, stop_px FROM armed_orders WHERE id
// BETWEEN 329 AND 341 AND trader_id LIKE '%1781246265'. Rows 335 and 341 are
// the OTHER trader's (…_1791870197) and are not part of hoang's set.
var fdrLiveArms = []fdrLiveArm{
	{329, "03:28", "03:28:00.007", "long", 30754.75, 30748.0, 0},
	{330, "03:31", "03:30:59.960", "long", 30766.25, 30753.75, 0},
	{331, "03:34", "03:34:00.875", "long", 30762.0, 30752.75, 0},
	{332, "03:39", "03:39:00.494", "long", 30764.75, 30753.25, 0},
	{333, "03:42", "03:42:00.002", "long", 30760.75, 30752.25, 0},
	{334, "03:46", "03:46:00.077", "long", 30767.5, 30756.25, 0.5},  // reverse ISB (scenario mentor-<ns>) — rel22 P2-1 wireShift
	{336, "03:48", "03:48:00.075", "long", 30760.75, 30752.25, 0.5}, // reverse ISB — rel22 P2-1 wireShift
	{337, "03:50", "03:50:00.134", "long", 30767.0, 30759.5, 0},
	{338, "04:11", "04:11:00.029", "long", 30773.0, 30759.25, 0},
	{339, "04:31", "04:31:00.629", "long", 30771.0, 30762.75, 0.5}, // reverse ISB — rel22 P2-1 wireShift
	{340, "04:33", "04:33:00.316", "long", 30770.5, 30763.25, 0.5}, // reverse ISB — rel22 P2-1 wireShift
}

// fdrR22Arms — arms the release #22 build authors on purpose that the live
// release #21 bot refused (id 0 = not a live row). r22 LEVELS: the bar closing
// 02:09 CT is an ISB short entry 30641.00 / stop 30651.00. Live (#21) refused
// it isb_target_below_floor: its target ladder read the BROKEN 5m/15m pivot low
// 30636.75 (4.25 pts — under the 10-pt stop). The old lows 30636.75 / 30632.75
// had been traded through; the nearest UNBROKEN old low is 30606.75, so the
// first level in the way is the key level 30611.25 (29.75 pts ≥ 2R) and the ISB
// is a valid trade. Pinned so the deliberate divergence from live is visible.
var fdrR22Arms = []fdrLiveArm{
	{0, "02:09", "r22: live refused isb_target_below_floor on a broken pivot low", "short", 30641.0, 30651.0, 0}, // regular ISB — no wire shift (#516 × #517 integration)
}

// fdrR23Dropped — live rows the release #23 build does NOT author, on purpose
// (MENTOR-SEED-STATE N7: the old-extreme pick tests EVERY pivot of the 12 h /
// 24 h lookback for unbroken, then takes the nearest — the live bot capped the
// pivots to the newest 3 per side first). At 03:30–03:42 CT 10-13 the nearest
// UNBROKEN old high is the 5m pivot 30769.50 (closed 10-12 20:15; first broken
// ~03:44), which the cap hid behind 3 broken newer highs, so live targeted
// 30905.50. With the old high 3.25–8.75 pts above these ISB longs' entries the
// kernel refuses them: isb_target_below_floor at 03:30/03:33/03:38, room at
// 03:41 (probed on the 10-13 session fixture). Independent oracle:
// ~/deck-fix-1005/rel22/fixwave/mentor-seed-state/py/oldext.py — "uncapped
// [30688.0, 30769.5] capped3 [30688.0, 30905.5]" at every one of those bars.
var fdrR23Dropped = map[int64]string{
	330: "N7: nearest unbroken old high 30769.50 is 3.25 pts above — isb_target_below_floor",
	331: "N7: nearest unbroken old high 30769.50 is 7.50 pts above — isb_target_below_floor",
	332: "N7: nearest unbroken old high 30769.50 is 4.75 pts above — isb_target_below_floor",
	333: "N7: nearest unbroken old high 30769.50 is 8.75 pts above — room",
}

// fdrExpectedArms is the live set plus the deliberate r22 divergences, minus
// the deliberate r23 drops, in bar-close order.
func fdrExpectedArms() []fdrLiveArm {
	out := append([]fdrLiveArm{}, fdrR22Arms...)
	for _, a := range fdrLiveArms {
		if _, dropped := fdrR23Dropped[a.id]; !dropped {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].barCT < out[j].barCT })
	return out
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
	// Release #25 planner-knobs: FDR_PLANNER_KNOBS_ON=1 replays the day with
	// all seven loss-fix switches ON (they ship OFF) — the mentor funnel must
	// not move (COMMON-25 PROOF: intents/placed with the knobs ON vs OFF).
	if os.Getenv("FDR_PLANNER_KNOBS_ON") == "1" {
		if cfg.DayPlan == nil {
			t.Fatal("FDR_PLANNER_KNOBS_ON: the fixture strategy carries no day_plan")
		}
		on, minR := true, 1.0
		cfg.DayPlan.PlannerRoomGate, cfg.DayPlan.PlannerRoomMinR = store.RoomGateRefuse, &minR
		cfg.DayPlan.PlannerExitOnThesisDeath, cfg.DayPlan.PlannerNoChaseEnforce = store.ThesisDeathAll, &on
		cfg.DayPlan.PlannerMomentumIntoZoneMaxATR, cfg.DayPlan.PlannerATRFloorCapAtInvalidation = 1.0, &on
		cfg.RiskControl.BreakevenTriggerR = 1
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
	traders      []*fdrTrader   // the replay's traders (the AddOn applies updates to them)
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
	// The restart rebuild reads the book at time.Now() (mentor_exit_drive.go —
	// the wall clock); the replay's book is stamped on the replay clock, so the
	// seam reads the SAME book (liveBook) at the replay clock instead.
	prevRebuildBook := mentorRebuildBook
	mentorRebuildBook = func(at *AutoTrader, _ time.Time) ([]ntwire.NT8Order, bool, time.Duration) {
		return at.liveBook(w.now())
	}
	t.Cleanup(func() {
		mentorRebuildBook = prevRebuildBook
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
	done    chan struct{}

	// book state — touched only by the test goroutine (settle).
	books     map[string][]ntwire.NT8Order // account → orders resting at the broker
	snapshots map[string]bool              // accounts whose book the AddOn publishes
	signals   map[string]ntwire.SignalPayload
	positions map[string]fdrAddonPos  // account → the open position
	unfilled  map[string]fdrAddonSend // entry signal → its send (the C8 sweep's input)
	listeners map[string]<-chan ntwire.OrderUpdatePayload
	sentinel  int
	log       []fdrWire
	orderSeq  int
}

type fdrAddonSend struct {
	account string
	atMs    int64
}

type fdrAddonPos struct {
	signalID, side string
	qty            int
	avg            float64
}

func newFdrAddon(t *testing.T, w *fdrWorld, conn net.Conn, build string, accounts []string, snapshots map[string]bool) *fdrAddon {
	a := &fdrAddon{t: t, w: w, conn: conn, build: build, done: make(chan struct{}),
		books: map[string][]ntwire.NT8Order{}, snapshots: snapshots, signals: map[string]ntwire.SignalPayload{}, positions: map[string]fdrAddonPos{}, unfilled: map[string]fdrAddonSend{}, listeners: map[string]<-chan ntwire.OrderUpdatePayload{}}
	go func() {
		defer close(a.done)
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			a.mu.Lock()
			a.inbox = append(a.inbox, env)
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
	// keep-alive: the real AddOn heartbeats AND acks the server's heartbeat
	// (FrameAck "heartbeat" refreshes the server's lastAckTime; without it the
	// server closes the link after TCPHeartbeatAckTimeout — 60 s — which a slow
	// run, e.g. under -race, outlives).
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
				a.write(ntwire.FrameAck, ntwire.AckPayload{Acks: "heartbeat"})
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

// quiesce makes the server→AddOn direction deterministic: every server
// write goes through ONE connection under the server's write lock, so a
// sentinel frame written now (a bars_history_request — a pure write with no
// server-side state) arrives after every frame production wrote before it.
func (a *fdrAddon) quiesce() {
	a.t.Helper()
	a.sentinel++
	id := "fdr-sentinel-" + strconv.Itoa(a.sentinel)
	if err := a.w.srv.SendBarsHistoryRequest(ntwire.BarsHistoryRequestPayload{RequestID: id, Symbol: "MNQ", Contract: fdrContract, Timeframe: "1m"}); err != nil {
		a.t.Fatalf("fixture: sentinel write: %v", err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Microsecond) {
		a.mu.Lock()
		for i, env := range a.inbox {
			if env.Type != ntwire.FrameBarsHistoryRequest {
				continue
			}
			var p ntwire.BarsHistoryRequestPayload
			if json.Unmarshal(env.Payload, &p) == nil && p.RequestID == id {
				a.inbox = append(a.inbox[:i:i], a.inbox[i+1:]...)
				a.mu.Unlock()
				return
			}
		}
		a.mu.Unlock()
	}
	a.t.Fatalf("fixture: the AddOn never read the sentinel %s", id)
}

// settle drains the frames production sent this step and plays the AddOn's
// part: rest/ack entries, cancel, move stops, record closes; then publishes the
// book of every snapshot account and persists it the way main.go's sink does.
func (a *fdrAddon) settle() {
	a.quiesce()
	a.mu.Lock()
	frames := a.inbox
	a.inbox = nil
	a.mu.Unlock()
	nowMs := a.w.nowMs.Load()
	fed := map[string]bool{} // accounts whose router got order_updates this step
	for _, env := range frames {
		switch env.Type {
		case ntwire.FrameSignal:
			var p ntwire.SignalPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.signals[p.SignalID] = p
			a.unfilled[p.SignalID] = fdrAddonSend{account: p.Account, atMs: nowMs}
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
			fed[p.Account] = true
		case ntwire.FrameCancelOrder:
			var p ntwire.CancelOrderPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type, account: p.Account, traderID: p.TraderID, signalID: p.SignalID})
			if a.removeOrder(p.Account, p.SignalID) {
				a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: p.SignalID, State: "cancelled",
					Symbol: "MNQ", Account: p.Account, TraderID: p.TraderID, Seq: p.Seq})
				fed[p.Account] = true
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
			// …and reports the close like NT8 does (position_close), so the
			// close-sync records the exit on the trader's position row.
			if pos, ok := a.positions[p.Account]; ok {
				delete(a.positions, p.Account)
				exitPx := a.w.bars[a.w.cur.Load()].Close
				a.write(ntwire.FramePositionClose, ntwire.PositionClosePayload{SignalID: pos.signalID, Symbol: "MNQ",
					PositionSide: pos.side, ExitPrice: exitPx, Quantity: pos.qty, ExitReason: "manual",
					ExitTime: time.UnixMilli(nowMs).UTC().Format(time.RFC3339), Account: p.Account, TraderID: p.TraderID, Seq: p.Seq})
				a.awaitClosed(pos.signalID)
			}
		case ntwire.FrameModifyBracket, ntwire.FramePlaceProtectiveStop:
			a.log = append(a.log, fdrWire{atMs: nowMs, typ: env.Type})
		}
	}
	a.publish()
	for acct := range fed {
		a.flushUpdates(acct)
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

// publish puts every served account's book in the server cache and the store
// (main.go's snapshot sink) — the AddOn's periodic order_snapshot.
func (a *fdrAddon) publish() {
	for acct, on := range a.snapshots {
		if !on {
			continue
		}
		orders := append([]ntwire.NT8Order{}, a.books[acct]...)
		p := ntwire.OrderSnapshotPayload{Account: acct, BuildID: a.build, EmittedMs: a.w.nowMs.Load(), Reason: "periodic", Orders: orders}
		// The receipt rides the REPLAY clock: every book reader on the mentor
		// path ages the book against the replay `now` it was handed (a wall
		// stamp would read as a future-dated receipt — F10 — i.e. no book).
		recv := time.UnixMilli(a.w.nowMs.Load())
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
	delete(a.unfilled, signalID)
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
	a.positions[account] = fdrAddonPos{signalID: signalID, side: strings.ToLower(p.Side), qty: p.Quantity, avg: price}
	a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: signalID, State: "filled", FillPrice: price,
		Quantity: p.Quantity, Symbol: "MNQ", Account: account, TraderID: p.TraderID, Seq: p.Seq})
	a.w.srv.FeedFillForTest(ntwire.FillPayload{SignalID: signalID, Symbol: "MNQ", Account: account, FillPrice: price,
		FillTime: time.UnixMilli(a.w.nowMs.Load()).UTC().Format(time.RFC3339), Side: p.Side, Quantity: p.Quantity, Status: "filled", TraderID: p.TraderID, Seq: p.Seq})
	a.publish()
	a.flushUpdates(account)
}

// flushUpdates makes the order_update delivery deterministic: a sentinel
// update goes through the SAME router behind the fed ones, and once the
// harness's own fan-out listener sees it, every earlier update is in every
// listener's channel. The trader(s) on the account then drain their stream
// (consumeArmedOrderUpdates — the armed pass's own drain), i.e. the updates
// are applied on arrival, as production's ordered-execution worker does.
func (a *fdrAddon) flushUpdates(account string) {
	a.t.Helper()
	a.sentinel++
	sid := "fdr-sentinel-" + strconv.Itoa(a.sentinel)
	ch := a.listener(account)
	a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: sid, State: "working", Symbol: "MNQ", Account: account})
	deadline := time.After(10 * time.Second)
	for seen := false; !seen; {
		select {
		case u, ok := <-ch:
			if !ok {
				delete(a.listeners, account) // the fan-out was rebuilt — listen again and re-send the sentinel
				ch = a.listener(account)
				a.w.srv.FeedOrderUpdateForTest(ntwire.OrderUpdatePayload{SignalID: sid, State: "working", Symbol: "MNQ", Account: account})
				continue
			}
			seen = u.SignalID == sid
		case <-deadline:
			a.t.Fatalf("fixture: the order_update router never delivered the sentinel for %s", account)
		}
	}
	for _, ft := range a.w.traders {
		if ft.account == account && ft.at != nil {
			if nt := ft.at.armedTrader(); nt != nil {
				ft.at.consumeArmedOrderUpdates(nt, a.w.st.ArmedOrders())
			}
		}
	}
}

func (a *fdrAddon) listener(account string) <-chan ntwire.OrderUpdatePayload {
	if ch, ok := a.listeners[account]; ok {
		return ch
	}
	ch, _ := a.w.srv.ListenOrderUpdates("MNQ", account)
	a.listeners[account] = ch
	return ch
}

// awaitClosed waits until the close-sync goroutine has recorded the
// position_close on the position row (it runs on its own goroutine).
func (a *fdrAddon) awaitClosed(signalID string) {
	a.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		var n int64
		a.w.st.GormDB().Model(&store.TraderPosition{}).Where("entry_order_id = ? AND status = ?", signalID, "OPEN").Count(&n)
		if n == 0 {
			return
		}
	}
	a.t.Logf("fixture: the close of %s was not recorded within 5 s", signalID)
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
	levelPx     map[string]float64 // lvl ledger scenario → level price (Intent.Anchor)
	swingBE     []fdrSwingBE       // every swing break-even the evaluator emitted, with its position
	curBar      int
}

// fdrSwingBE is one kernel swing break-even intent and the evaluator's swing
// position at that tick (entry and the stop the position carries).
type fdrSwingBE struct {
	barIdx      int
	armID       string
	side        mentor.Side
	entry, stop float64
	known       bool // the position was readable at the tick
}

// newFdrTrader builds one mentor AutoTrader the way NewAutoTrader's ninjatrader
// branch does — the TCP trader bound to its account, the NT8 wiring, then the
// production mentor seams and the seed — minus NewTraderFromEnv (that would
// start the process-singleton TCP listener) and minus Run (the replay drives the
// event-pass call site itself). Every mentor seam is the production binding
// (mentorWireProductionSeams) except `now`, re-pinned to the bar clock; the
// entry latch keeps its production evidence on the replay clock.
func (w *fdrWorld) newFdrTrader(ft *fdrTrader, cfg *store.StrategyConfig) {
	w.t.Helper()
	// The traders table row, as the live bot has it: the entry latch resolves
	// ANOTHER trader's account through it (traderScope) — without it a second
	// trader's placed row would read as this account's (fail-closed).
	if _, err := w.st.Trader().GetByID(ft.id); err != nil {
		if err := w.st.Trader().Create(&store.Trader{ID: ft.id, UserID: "u", Name: ft.id, AIModelID: "m",
			ExchangeID: "ex", StrategyID: "a5b7662e-7bf7-49bb-9f09-7efa48f95ac8", Account: ft.account, IsRunning: true}); err != nil {
			w.t.Fatalf("trader row %s: %v", ft.id, err)
		}
	}
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
	// The entry latch's evidence is the production wiring (entryLatchBook +
	// entryLatchLedgers); its clock is time.Now in production. The replay's book
	// is stamped on the replay clock, so the latch reads the SAME evidence at
	// the replay clock (a wall clock would age every replay book past ≤60 s).
	nt.SetEntryLatchSource(&ntTrader.EntryLatchSource{Book: at.entryLatchBook, Ledgers: at.entryLatchLedgers, Now: w.now})
	nt.StartCloseSync(at.id, at.exchangeID, at.exchange, w.st) // Run's first NT8 step: stamps the trader on every frame
	at.mentorIntentTap = func(in mentor.Intent) {
		ft.intents = append(ft.intents, fdrTick{barIdx: ft.curBar, in: in})
		if strings.HasPrefix(in.ArmID, "lvl-") && fdrIsPlace(in) && in.Anchor != 0 {
			ft.levelPx[at.mentorScenarioFor(in.ArmID)] = in.Anchor // keyed by the ledger scenario: ArmSeq restarts per construction
		}
		if in.Action == mentor.ActionMoveStopBE && strings.HasPrefix(in.ArmID, "swing-") {
			be := fdrSwingBE{barIdx: ft.curBar, armID: in.ArmID}
			if p := at.mentorEval.State.Swing.Pos; p != nil && p.ArmID == in.ArmID {
				be.side, be.entry, be.stop, be.known = p.Side, p.Entry, p.Stop, true
			}
			ft.swingBE = append(ft.swingBE, be)
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

// c8Sweep plays the position-reconcile goroutine's C8 sweep at REPLAY speed: an
// entry that has not filled within entryConfirmGraceMs (45 s) leaves the
// TCPTrader's pending map (it may still rest at the broker — the ledger_open
// latch leg keeps holding the account until its row is terminal). Production
// runs it on a wall-clock ticker (StartPositionReconcile, from Run); the whole
// replayed day lasts under two wall minutes, so it would never fire. The
// exported clear is ForgetPending, which also drops the latch's 60 s
// recent-send stamp — at the harness's +0/+5/+30 s pass cadence that can let a
// re-placement go ≤5 s earlier than production would.
func (w *fdrWorld) c8Sweep(traders []*fdrTrader) {
	now := w.nowMs.Load()
	for sig, snd := range w.addon.unfilled {
		if now-snd.atMs < 45_000 {
			continue
		}
		for _, ft := range traders {
			if ft.account == snd.account {
				if nt := ft.at.armedTrader(); nt != nil {
					nt.ForgetPending(sig)
				}
			}
		}
		delete(w.addon.unfilled, sig)
	}
}

// step plays one closed 1m bar: the event pass at the bar's close, the AddOn's
// answers, then the 5 s placement cadence sampled at +5 s and +30 s.
func (w *fdrWorld) step(i int, traders []*fdrTrader, l *fdrLedger) {
	w.cur.Store(int64(i))
	closeMs := w.bars[i].CloseTime + 1
	w.setNow(closeMs)
	w.addon.publish() // the AddOn's periodic order_snapshot
	w.c8Sweep(traders)
	for _, ft := range traders {
		ft.curBar = i
		ft.at.mentorEventPassAt(w.now())
	}
	w.addon.settle()
	for _, off := range []int64{5_000, 30_000} {
		w.setNow(closeMs + off)
		w.c8Sweep(traders)
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
	w.traders = []*fdrTrader{ft}
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
	want := fdrExpectedArms()
	n := len(got)
	if len(want) > n {
		n = len(want)
	}
	mismatch := 0
	for i := 0; i < n; i++ {
		var lv, hv string
		var lvA *fdrLiveArm
		if i < len(want) {
			lvA = &want[i]
			lv = fmt.Sprintf("#%d %s (%s) %s %.2f/%.2f", lvA.id, lvA.barCT, lvA.created, strings.ToUpper(lvA.side), lvA.entry, lvA.stop)
		}
		ok := false
		if i < len(got) {
			r := got[i]
			ct := time.UnixMilli(r.bornMs).In(kernel.CTLocation()).Format("15:04")
			hv = fmt.Sprintf("row %d %s %s %.2f/%.2f [%s] %s", r.row.ID, ct, strings.ToUpper(r.row.Side), r.row.EntryPx, r.row.StopPx, r.row.Condition, r.row.Scenario)
			ok = lvA != nil && ct == lvA.barCT && strings.EqualFold(r.row.Side, lvA.side) && r.row.EntryPx == lvA.wantEntry() && r.row.StopPx == lvA.stop
		}
		mark := "MATCH"
		if !ok {
			mark = "DIFF"
			mismatch++
		}
		t.Logf("  %-5s live %-50s | harness %s", mark, lv, hv)
	}
	if mismatch != 0 {
		t.Fatalf("FIDELITY: %d of %d rows differ from the live 11 + %d r22 arms − %d r23 drops (see the table)", mismatch, n, len(fdrR22Arms), len(fdrR23Dropped))
	}
}

// ── TestFullDayReplay (FULL_DAY_REPLAY=1) ───────────────────────────────────

// storeReplayCalendar writes the day's calendar slices. 10-12 is the live slice
// (no events). 10-13 is the live slice (one T2) PLUS an INJECTED T1 "CPI m/m"
// at 07:30 CT: the live 10-13 had no red-folder print, and without one the F11
// news gates (the 07:20–07:35 hold and the print −10m cancel) never run — the
// replay day is made a print day so invariant (f) is a real check.
func (w *fdrWorld) storeReplayCalendar() {
	w.t.Helper()
	ct := kernel.CTLocation()
	day13 := []calendar.Event{
		{Time: time.Date(2026, 10, 13, 21, 10, 0, 0, ct), Currency: "EUR", Title: "ECB President Lagarde Speaks", Impact: calendar.Impact("T2")},
		{Time: time.Date(2026, 10, 13, 7, 30, 0, 0, ct), Currency: "USD", Title: "CPI m/m (FULL-DAY-REPLAY injected print)", Impact: calendar.T1},
	}
	b, _ := json.Marshal(day13)
	for date, evs := range map[string]string{"2026-10-12": "[]", "2026-10-13": string(b)} {
		if _, err := w.st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{TradeDate: date, Source: "full-day-replay", EventsJSON: evs, CreatedAt: fdrDayStartMs}); err != nil {
			w.t.Fatalf("calendar slice %s: %v", date, err)
		}
	}
}

// fdrSwing is the injected swing fill.
type fdrSwing struct {
	traderID, account, armID, signalID string
	rowID                              int64
	entry, stop, target, risk          float64
	fillBar                            int
	entryBucket, holdMs                int64
}

// injectSwingFill — NO swing ever filled live (the ledger holds 5 SWING4H rows,
// 0 filled), so invariant (c) needs one. INJECTED at a real bar: the 21:25 CT
// 5m bar of 10-12, filled at that 1m bar's mid (a price the bar traded). The row
// is authored the way mentorArmIntent writes a swing (origin mentor, condition
// SWING4H, exit mode "swing", 1 contract, the swing expiry), placed with the
// ledger's own BeginPlacement, and FILLED by the fake AddOn — so the position
// row, the live-pos registration and the bracket (-sl/-tp) all come from the
// production fill path (onArmedOrderUpdate → materializeArmedEntry).
func (w *fdrWorld) injectSwingFill(ft *fdrTrader, i int) fdrSwing {
	w.t.Helper()
	b := w.bars[i]
	entry := math.Round((b.High+b.Low)/2*4) / 4
	sw := fdrSwing{traderID: ft.id, account: ft.account, entry: entry,
		stop: entry - fdrSwingRiskPts, target: entry + fdrSwingTargetPts, risk: fdrSwingRiskPts, fillBar: i,
		armID: "swing-" + strconv.FormatInt(b.OpenTime-b.OpenTime%300_000, 10), signalID: "fdr-injected-swing-" + ft.id}
	sw.entryBucket = kernel.FourHBucketStart(b.OpenTime-b.OpenTime%300_000, kernel.CTLocation())
	sw.holdMs = sw.entryBucket + int64(ft.at.mentorEval.Cfg.Swing.Hold4hBars)*4*3_600_000
	ledger := w.st.ArmedOrders()
	row := &store.ArmedOrderDB{
		TraderID: ft.id, PlanID: mentorPlanID(ft.id), Version: 1, Session: "MENTOR",
		Scenario: ft.at.mentorScenarioFor(sw.armID), Side: "long", State: store.StateArmed,
		EntryPx: sw.entry, StopPx: sw.stop, TargetPx: sw.target, Kind: "stop_entry", Condition: "SWING4H",
		ExpiryMs: mentorSwingExpiry(b.CloseTime + 1), Origin: store.ArmOriginMentor,
		ExitMode: mentorExitModePtr("swing"), Contracts: store.IntPtr(1),
	}
	if err := ledger.UpsertArm(row); err != nil || row.ID == 0 {
		w.t.Fatalf("inject swing row: id=%d err=%v", row.ID, err)
	}
	sw.rowID = row.ID
	if err := ledger.BeginPlacement(row.ID, sw.signalID); err != nil {
		w.t.Fatalf("inject swing placement: %v", err)
	}
	w.addon.signals[sw.signalID] = ntwire.SignalPayload{Symbol: "MNQ", Account: ft.account, TraderID: ft.id, Side: "long",
		Quantity: 1, Entry: sw.entry, StopLoss: sw.stop, TakeProfit: sw.target, SignalID: sw.signalID, OrderType: "stop_entry", StopPrice: sw.entry, StopLimit: true}
	w.addon.fill(ft.account, sw.signalID, sw.entry)
	w.t.Logf("INJECTED swing fill (c): trader %s %s LONG 1 @ %.2f stop %.2f target %.2f (R %.0f) on the %s CT bar — arm %s, 4h bucket %s, hold close at %s CT",
		ft.id, sw.signalID, sw.entry, sw.stop, sw.target, sw.risk, fdrCT(b.OpenTime), sw.armID, fdrCT(sw.entryBucket), fdrCT(sw.holdMs))
	return sw
}

// restart drops every trader's in-process state — the mentor registry entries,
// the runner targets, the AutoTrader itself — and builds fresh AutoTraders over
// the SAME store and the SAME NT8 link, re-seeded from the bars persisted by
// now. (The process-constant boot id means the class-33 boot sweep of placed
// pre-boot arms is NOT replayed: a reload, not a new binary.)
func (w *fdrWorld) restart(traders []*fdrTrader, cfg *store.StrategyConfig) {
	w.t.Helper()
	ids := map[string]bool{}
	for _, ft := range traders {
		ids[ft.id] = true
	}
	mentorLiveMu.Lock()
	for k, v := range mentorLiveArms {
		if ids[v.TraderID] {
			delete(mentorLiveArms, k)
		}
	}
	mentorLiveMu.Unlock()
	mentorRunnerTargets.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok {
			if i := strings.Index(s, "|"); i > 0 && ids[s[:i]] {
				mentorRunnerTargets.Delete(k)
			}
		}
		return true
	})
	w.storeBarsUpTo(w.nowMs.Load() - 1)
	for _, ft := range traders {
		w.newFdrTrader(ft, cfg)
	}
}

// fdrIsPlacementRefusal names the trader counters that record a refused ENTRY
// (the placement gates of mentorDispatchEntry / mentorPlaceIntent /
// mentorArmIntent). Cancel/extend/move/close refusals are not entry refusals.
func fdrIsPlacementRefusal(k string) bool {
	for _, p := range []string{"cancel_", "extend_", "move_be_", "close_", "swing_rebuild", "rebuild_"} {
		if strings.HasPrefix(k, p) {
			return false
		}
	}
	switch {
	case strings.HasPrefix(k, "refused_"), strings.HasPrefix(k, "placement_refused"), strings.HasSuffix(k, "_refused"),
		strings.HasPrefix(k, "news_hold"), strings.HasSuffix(k, "_no_data"), strings.HasSuffix(k, "_unresolved"):
		return true
	}
	switch k {
	case "no_chase_skip", "stale_intent", "stale_data", "mentor_sources_missing", "add_no_source", "placement_held", "expiry_missing_refused":
		return true
	}
	return false
}

func fdrIsPlace(in mentor.Intent) bool {
	return in.Action == mentor.PlaceStopEntry || in.Action == mentor.PlaceStopLimitEntry
}

// fdrPlaceOutcome is what became of one Place intent.
type fdrPlaceOutcome struct {
	trader  string
	barIdx  int
	in      mentor.Intent
	rowID   int64
	refusal string // the counters a refusal bumped this tick (several intents may share one)
	silent  bool   // no row and no named refusal
	birthCx bool   // the evaluator cancelled it in the SAME tick (the 15:00–23:59 bug)
}

func fdrCountersDiff(before, after map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range after {
		if d := v - before[k]; d > 0 {
			out[k] = d
		}
	}
	return out
}

func fdrSortedKV(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v != 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Itoa(m[k]))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

func fdrHHMM(ms int64) int {
	t := time.UnixMilli(ms).In(kernel.CTLocation())
	return t.Hour()*60 + t.Minute()
}

func fdrReplayEndMs() int64 {
	if os.Getenv("FULL_DAY_REPLAY_EVENING") == "1" {
		return fdrEveningEndMs
	}
	return fdrDayEndMs
}

// TestFullDayReplay plays 17:00 CT 10-11 → 16:00 CT 10-13 (FULL_DAY_REPLAY=1;
// FULL_DAY_REPLAY_EVENING=1 runs on through the 10-13 evening to the fixture's
// last bar) with TWO mentor traders on the live config, one SIM account each,
// a restart at 21:30 CT 10-12 and an injected swing fill on trader A at 21:25
// CT 10-12.
func TestFullDayReplay(t *testing.T) {
	if os.Getenv("FULL_DAY_REPLAY") != "1" {
		t.Skip("the full day (two traders, restart, minutes of wall time) runs with FULL_DAY_REPLAY=1; the default run is TestFullDayReplayFidelity")
	}
	const acctA, acctB = "SimReplayA", "SimReplayB"
	w := newFdrWorld(t, fdrReplayFromMs, []string{acctA, acctB}, map[string]bool{acctA: true, acctB: true}, fdrAddonBuild)
	w.storeReplayCalendar()
	cfg := loadReplayStrategy(t)
	start := w.barIndexAtOrAfter(fdrReplayFromMs)
	end := w.barIndexAtOrAfter(fdrReplayEndMs())
	w.setNow(fdrReplayFromMs)
	w.cur.Store(int64(start - 1))
	A := &fdrTrader{id: "fdr-a", account: acctA, levelPx: map[string]float64{}}
	B := &fdrTrader{id: "fdr-b", account: acctB, levelPx: map[string]float64{}}
	traders := []*fdrTrader{A, B}
	w.traders = traders
	for _, ft := range traders {
		w.newFdrTrader(ft, cfg)
	}
	l := &fdrLedger{rows: map[int64]*fdrRowLife{}}
	var outcomes []fdrPlaceOutcome
	kernelRefusals := map[string]map[string]int{A.id: {}, B.id: {}}
	traderCounters := map[string]map[string]int{A.id: {}, B.id: {}}
	foldInstance := func(ft *fdrTrader) {
		for k, v := range mentorKernelRefusals(ft.at) {
			kernelRefusals[ft.id][k] += v
		}
		for k, v := range ft.at.mentorCountSnapshot() {
			traderCounters[ft.id][k] += v
		}
	}
	var sw fdrSwing
	var orphanBook []string // (b) half 2: a working broker order with no live ledger row
	var registryMix []string
	wallStart := time.Now()
	for i := start; i < end; i++ {
		before := map[string]map[string]int{}
		for _, ft := range traders {
			before[ft.id] = ft.at.mentorCountSnapshot()
		}
		w.step(i, traders, l)
		// (b) half 1 — every Place intent this tick has a ledger row or a named refusal.
		for _, ft := range traders {
			diff := fdrCountersDiff(before[ft.id], ft.at.mentorCountSnapshot())
			refusals := map[string]int{}
			for k, v := range diff {
				if fdrIsPlacementRefusal(k) {
					refusals[k] = v
				}
			}
			born := map[string]*fdrRowLife{}
			for _, life := range l.rows {
				if life.bornBar == i && life.row.TraderID == ft.id && isMentorArmOrigin(life.row) {
					born[life.row.Scenario] = life
				}
			}
			cancelled := map[string]bool{}
			for _, tk := range ft.intents {
				if tk.barIdx == i && tk.in.Action == mentor.CancelArm {
					cancelled[tk.in.ArmID] = true
				}
			}
			epoch := ft.epochs[len(ft.epochs)-1]
			for _, tk := range ft.intents {
				if tk.barIdx != i || !fdrIsPlace(tk.in) {
					continue
				}
				o := fdrPlaceOutcome{trader: ft.id, barIdx: i, in: tk.in, birthCx: tk.in.ArmID != "" && cancelled[tk.in.ArmID]}
				if tk.in.ArmID != "" {
					if life, ok := born[fmt.Sprintf("%s-%d", tk.in.ArmID, epoch)]; ok {
						o.rowID = life.row.ID
					}
				} else {
					for sc, life := range born {
						if strings.HasPrefix(sc, "mentor-") && life.row.EntryPx == tk.in.Price && life.row.StopPx == tk.in.Stop {
							o.rowID = life.row.ID
							delete(born, sc)
							break
						}
					}
				}
				if o.rowID == 0 {
					o.refusal = fdrSortedKV(refusals)
					o.silent = len(refusals) == 0
				}
				outcomes = append(outcomes, o)
			}
		}
		// (b) half 2 — every working entry at the broker has a live ledger row.
		for _, ft := range traders {
			for _, o := range w.addon.workingEntries(ft.account) {
				live := false
				for _, life := range l.rows {
					if life.row.SignalID == o.Name && life.row.TraderID == ft.id && !store.IsTerminalArmState(strings.ToLower(life.row.State)) {
						live = true
						break
					}
				}
				if !live {
					orphanBook = append(orphanBook, fmt.Sprintf("%s %s %s order %s @%.2f has no live %s row", fdrCT(w.nowMs.Load()), ft.account, ft.id, o.Name, o.StopPrice, ft.id))
				}
			}
		}
		// (d) — the process registry: every entry is keyed by its own trader.
		mentorLiveMu.Lock()
		for k, v := range mentorLiveArms {
			if !strings.HasPrefix(k, v.TraderID+"|") {
				registryMix = append(registryMix, fmt.Sprintf("%s registry key %q holds trader %s", fdrCT(w.nowMs.Load()), k, v.TraderID))
			}
		}
		mentorLiveMu.Unlock()
		if w.bars[i].OpenTime == fdrSwingBarMs {
			sw = w.injectSwingFill(A, i)
		}
		if w.bars[i].CloseTime+1 == fdrRestartMs {
			for _, ft := range traders {
				foldInstance(ft)
			}
			t.Logf("RESTART at %s CT — fresh AutoTraders over the same store and NT8 link (pre-restart counters folded)", fdrCT(fdrRestartMs))
			w.restart(traders, cfg)
		}
	}
	for _, ft := range traders {
		foldInstance(ft)
	}
	t.Logf("replayed %d bars (%s → %s CT) × %d traders in %s wall", end-start, fdrCT(w.bars[start].OpenTime), fdrCT(w.bars[end-1].CloseTime+1), len(traders), time.Since(wallStart).Round(time.Second))

	lives := l.sorted()
	rowOf := map[string]*fdrRowLife{} // signal id → row
	for _, life := range lives {
		if life.row.SignalID != "" {
			rowOf[life.row.SignalID] = life
		}
	}
	accountOf := map[string]string{A.id: A.account, B.id: B.account}

	// ── (a) level orders after 15:00 are never cancelled at birth ──────────
	t.Run("a_level_order_after_1500_rests_until_its_own_1500", func(t *testing.T) {
		var bad []string
		lvlRows := 0
		for _, o := range outcomes {
			if !strings.HasPrefix(o.in.ArmID, "lvl-") || !o.birthCx {
				continue
			}
			bad = append(bad, fmt.Sprintf("%s %s %s %s @%.2f emitted at %s CT and CANCELLED BY THE EVALUATOR IN THE SAME TICK (row %d)",
				o.trader, o.in.ArmID, o.in.Setup, o.in.Side, o.in.Price, fdrCT(w.bars[o.barIdx].CloseTime+1), o.rowID))
		}
		for _, life := range lives {
			r := life.row
			armID := mentorArmIDFromScenario(r.Scenario)
			if !strings.HasPrefix(armID, "lvl-") {
				continue
			}
			lvlRows++
			end := "still resting at the end"
			if life.endBar >= 0 {
				end = fmt.Sprintf("%s at %s CT: %q", life.endState, fdrCT(life.endMs), life.endReason)
			}
			t.Logf("(a) row %d %s %s %s @%.2f authored %s CT, expiry %s CT — %s", r.ID, r.TraderID, armID, r.Side, r.EntryPx, fdrCT(life.bornMs), fdrCT(r.ExpiryMs), end)
			if fdrHHMM(life.bornMs) < 15*60 || life.endBar < 0 {
				continue
			}
			ownExpiry := mentor.LevelArmExpiry(life.bornMs)
			if r.ExpiryMs != ownExpiry {
				bad = append(bad, fmt.Sprintf("row %d %s authored %s CT carries expiry %s, not its own next 15:00 (%s)", r.ID, armID, fdrCT(life.bornMs), fdrCT(r.ExpiryMs), fdrCT(ownExpiry)))
			}
			if life.endBar == life.bornBar {
				bad = append(bad, fmt.Sprintf("row %d %s %s authored %s CT went %s IN ITS BIRTH BAR: %q", r.ID, r.TraderID, armID, fdrCT(life.bornMs), life.endState, life.endReason))
				continue
			}
			if life.endMs >= ownExpiry {
				continue
			}
			var level float64
			have := false
			for _, ft := range traders {
				if ft.id == r.TraderID {
					level, have = ft.levelPx[r.Scenario]
				}
			}
			through := false
			if have {
				for j := life.bornBar + 1; j <= life.endBar && j < len(w.bars); j++ {
					c := w.bars[j].Close
					if strings.EqualFold(r.Side, "long") && c < level || strings.EqualFold(r.Side, "short") && c > level {
						through = true
						break
					}
				}
			}
			// Named production ends that are not the bug: the day-stop sweeps, the
			// EOD flat, the 15m/5m conflict, the news hold, and the armed pass's
			// stop-side guard ("accepted through": the market traded through the
			// trigger before the order could rest).
			named := false
			for _, k := range []string{"day-stop", "session ended", "EOD", "15m/5m ISB conflict", "never-add", "news", "accepted through"} {
				if strings.Contains(life.endReason, k) {
					named = true
				}
			}
			if !through && !named {
				bad = append(bad, fmt.Sprintf("row %d %s %s authored %s CT ended %s at %s CT before its own 15:00 with no close-through (level %.2f known=%v): %q",
					r.ID, r.TraderID, armID, fdrCT(life.bornMs), life.endState, fdrCT(life.endMs), level, have, life.endReason))
			}
		}
		t.Logf("(a) level rows authored over the day: %d", lvlRows)
		if len(bad) > 0 {
			t.Fatalf("(a) %d level-order lifetime violation(s):\n  %s", len(bad), strings.Join(bad, "\n  "))
		}
	})

	// ── (b) every placement intent has a ledger row; every broker order a live row
	t.Run("b_intents_have_ledger_rows_and_book_orders_have_live_rows", func(t *testing.T) {
		var silent []string
		for _, o := range outcomes {
			if o.silent {
				silent = append(silent, fmt.Sprintf("%s %s %s %s @%.2f at %s CT — no ledger row and no named refusal",
					o.trader, o.in.ArmID, o.in.Setup, o.in.Side, o.in.Price, fdrCT(w.bars[o.barIdx].CloseTime+1)))
			}
		}
		if len(silent) > 0 || len(orphanBook) > 0 {
			t.Fatalf("(b) %d silent placement intent(s), %d orphan broker order(s):\n  %s\n  %s",
				len(silent), len(orphanBook), strings.Join(silent, "\n  "), strings.Join(orphanBook, "\n  "))
		}
	})

	// ── (c) an open swing across the restart: BE at +1R and the hold close ──
	t.Run("c_swing_across_restart_gets_be_at_1R_and_hold_close", func(t *testing.T) {
		if sw.signalID == "" {
			t.Fatal("the swing was never injected")
		}
		// The expected BE: the first 5m bar CLOSING after the restart at ≥ entry+1R.
		var beAt int64
		for _, b5 := range fdrAggregate(w.bars[sw.fillBar:end:end], 300_000, math.MaxInt64) {
			if b5.CloseTime+1 <= fdrRestartMs {
				continue
			}
			if b5.Close >= sw.entry+sw.risk {
				beAt = b5.CloseTime + 1
				break
			}
		}
		var moves, closes []fdrWire
		for _, f := range w.addon.log {
			switch {
			case f.typ == ntwire.FrameMoveStop && f.signalID == sw.signalID:
				moves = append(moves, f)
			case f.typ == ntwire.FrameClosePosition && f.account == sw.account:
				closes = append(closes, f)
			}
		}
		t.Logf("(c) injected swing %s: expected BE at %s CT (first 5m close ≥ %.2f after the restart), hold close at %s CT; move_stop frames %d, close frames %d",
			sw.signalID, fdrCT(beAt), sw.entry+sw.risk, fdrCT(sw.holdMs+300_000), len(moves), len(closes))
		for _, f := range moves {
			t.Logf("    move_stop %s CT → %.2f", fdrCT(f.atMs), f.price)
		}
		for _, f := range closes {
			t.Logf("    close_position %s CT %s qty %d", fdrCT(f.atMs), f.side, f.qty)
		}
		if beAt == 0 {
			t.Fatal("the fixture never reaches +1R for the injected swing — pick another injection")
		}
		if len(moves) == 0 {
			t.Fatalf("(c) RED: the restarted swing never got its break-even — no move_stop for %s (expected at %s CT)", sw.signalID, fdrCT(beAt))
		}
		if first := moves[0]; first.atMs != beAt || first.price != sw.entry {
			t.Fatalf("(c) the first stop move for the swing was %.2f at %s CT; want BE %.2f at %s CT (+1R)", first.price, fdrCT(first.atMs), sw.entry, fdrCT(beAt))
		}
		if end := w.barIndexAtOrAfter(sw.holdMs + 300_000); end < len(w.bars) && w.bars[end].OpenTime < fdrReplayEndMs() {
			if len(closes) == 0 {
				t.Fatalf("(c) RED: the restarted swing never got its hold close (2nd 4h close after entry, %s CT)", fdrCT(sw.holdMs+300_000))
			}
			if closes[0].atMs != sw.holdMs+300_000 {
				t.Fatalf("(c) the swing's hold close came at %s CT; want %s CT (the close of the first 5m bar of the hold boundary)", fdrCT(closes[0].atMs), fdrCT(sw.holdMs+300_000))
			}
		}
	})

	// ── (c2) every swing break-even comes at ≥ +1R, with and without a restart
	t.Run("c2_every_swing_break_even_at_or_beyond_1R", func(t *testing.T) {
		// The course rule is "+1R reached — stop to break-even" [§8]. Every swing
		// MoveStopBE the evaluator emitted is judged on the 5m close it was
		// emitted on, against the position's OWN risk |entry − stop| (measured
		// here independently of the kernel's sign convention). The injected swing
		// is the restart path (rebuilt from the records); every other swing was
		// filled — in the kernel's tape simulation — in the same process.
		var bad []string
		checked := map[string]int{}
		for _, ft := range traders {
			for _, be := range ft.swingBE {
				path := "no restart"
				entry, stop, side, known := be.entry, be.stop, be.side, be.known
				if be.armID == sw.armID && ft.id == sw.traderID {
					path = "after the restart"
					entry, stop, side, known = sw.entry, sw.stop, mentor.SideLong, true // the records, not the live (possibly moved) stop
				}
				when := fdrCT(w.bars[be.barIdx].CloseTime + 1)
				if !known || math.Abs(entry-stop) == 0 {
					bad = append(bad, fmt.Sprintf("%s %s break-even at %s CT with no readable position (entry %.2f stop %.2f)", ft.id, be.armID, when, entry, stop))
					continue
				}
				risk := math.Abs(entry - stop)
				c5 := fdrLast5mClose(w, be.barIdx)
				move := c5 - entry
				if side == mentor.SideShort {
					move = entry - c5
				}
				r := move / risk
				checked[path]++
				t.Logf("(c2) %s %s %s: %s entry %.2f stop %.2f (R %.2f), break-even at %s CT on a 5m close %.2f = %+.2fR",
					ft.id, be.armID, path, side, entry, stop, risk, when, c5, r)
				if r < 1-1e-9 {
					bad = append(bad, fmt.Sprintf("%s %s (%s) moved to break-even at %+.2fR (%s CT, close %.2f, entry %.2f, R %.2f) — the rule is +1R",
						ft.id, be.armID, path, r, when, c5, entry, risk))
				}
			}
		}
		t.Logf("(c2) swing break-evens checked: %s", fdrSortedKV(checked))
		if checked["after the restart"] == 0 {
			t.Fatal("(c2) the restarted swing never broke even — the restart half of the rule is untested")
		}
		if len(bad) > 0 {
			t.Fatalf("(c2) %d swing break-even(s) before +1R:\n  %s", len(bad), strings.Join(bad, "\n  "))
		}
	})

	// ── (d) the two traders never mix ──────────────────────────────────────
	t.Run("d_two_traders_never_mix", func(t *testing.T) {
		var bad []string
		epochOK := func(id, scenario string) bool {
			for _, ft := range traders {
				if ft.id != id {
					continue
				}
				for _, e := range ft.epochs {
					if strings.HasSuffix(scenario, "-"+strconv.FormatInt(e, 10)) {
						return true
					}
				}
			}
			return false
		}
		perTrader := map[string]int{}
		for _, life := range lives {
			r := life.row
			acct, known := accountOf[r.TraderID]
			if !known {
				bad = append(bad, fmt.Sprintf("row %d has a foreign trader id %q", r.ID, r.TraderID))
				continue
			}
			perTrader[r.TraderID]++
			if isMentorArmOrigin(r) && r.PlanID != mentorPlanID(r.TraderID) {
				bad = append(bad, fmt.Sprintf("row %d trader %s plan_id %q", r.ID, r.TraderID, r.PlanID))
			}
			if isMentorArmOrigin(r) && !epochOK(r.TraderID, r.Scenario) {
				bad = append(bad, fmt.Sprintf("row %d trader %s scenario %q carries another construction's epoch", r.ID, r.TraderID, r.Scenario))
			}
			_ = acct
		}
		for _, f := range w.addon.log {
			if f.signalID == "" {
				continue
			}
			life, ok := rowOf[f.signalID]
			if !ok {
				if f.typ == ntwire.FrameSignal {
					bad = append(bad, fmt.Sprintf("%s entry frame %s on %s has no ledger row", fdrCT(f.atMs), f.signalID, f.account))
				}
				continue
			}
			if f.account != accountOf[life.row.TraderID] || (f.traderID != "" && f.traderID != life.row.TraderID) {
				bad = append(bad, fmt.Sprintf("%s %s frame for row %d (trader %s) went to account %q trader %q", fdrCT(f.atMs), f.typ, life.row.ID, life.row.TraderID, f.account, f.traderID))
			}
		}
		var positions []store.TraderPosition
		if err := w.st.GormDB().Find(&positions).Error; err != nil {
			t.Fatalf("positions read: %v", err)
		}
		for _, p := range positions {
			if acct, ok := accountOf[p.TraderID]; !ok || (p.Account != "" && p.Account != acct) {
				bad = append(bad, fmt.Sprintf("position %d trader %q account %q", p.ID, p.TraderID, p.Account))
			}
		}
		bad = append(bad, registryMix...)
		t.Logf("(d) rows per trader %s · positions %d · wire frames %d", fdrSortedKV(perTrader), len(positions), len(w.addon.log))
		if len(bad) > 0 {
			t.Fatalf("(d) %d mix(es):\n  %s", len(bad), strings.Join(bad, "\n  "))
		}
	})

	// ── (e) is_running stays 1 through the restart's start path ─────────────
	t.Run("e_is_running_stays_1_after_the_restart_start_path", func(t *testing.T) {
		flags := fdrStartPathFlags(t, []string{A.id, B.id}, []string{A.account, B.account})
		t.Logf("(e) DB is_running after boot auto-start + API start of the same trader: %v", flags)
		for id, on := range flags {
			if !on {
				t.Errorf("(e) RED: trader %s reads is_running=0 after a duplicate start of the running instance", id)
			}
		}
	})

	// ── (f) no entry inside the 14:45 EOD or 07:20–07:35 news windows ───────
	t.Run("f_no_entry_in_eod_or_news_windows", func(t *testing.T) {
		var bad []string
		authoredIn := 0
		for _, f := range w.addon.log {
			if f.typ != ntwire.FrameSignal || strings.HasPrefix(f.signalID, "fdr-injected") {
				continue
			}
			if hm := fdrHHMM(f.atMs); hm >= 14*60+45 && hm < 17*60 || hm >= 7*60+20 && hm < 7*60+35 {
				bad = append(bad, fmt.Sprintf("entry %s %s @%.2f SENT at %s CT on %s", f.signalID, f.side, f.price, fdrCT(f.atMs), f.account))
			}
		}
		for _, life := range lives {
			if hm := fdrHHMM(life.bornMs); hm >= 7*60+20 && hm < 7*60+35 {
				authoredIn++
				bad = append(bad, fmt.Sprintf("row %d %s %s AUTHORED at %s CT inside the news hold", life.row.ID, life.row.TraderID, life.row.Scenario, fdrCT(life.bornMs)))
			}
		}
		if len(bad) > 0 {
			t.Fatalf("(f) %d entr(ies) inside a closed window:\n  %s", len(bad), strings.Join(bad, "\n  "))
		}
	})

	// ── (g) the daily counters, printed so a future change shows as a diff ───
	t.Run("g_daily_counters", func(t *testing.T) {
		for _, ft := range traders {
			intents := map[string]int{}
			for _, tk := range ft.intents {
				k := string(tk.in.Action)
				if fdrIsPlace(tk.in) {
					k = "place_" + tk.in.Setup
				}
				intents[k]++
			}
			authored, placed, filled, working := 0, 0, 0, 0
			endedBy := map[string]int{} // "<terminal state> — <reason>"
			for _, life := range lives {
				r := life.row
				if r.TraderID != ft.id {
					continue
				}
				authored++
				if r.SignalID != "" {
					placed++
				}
				state := strings.ToLower(strings.TrimSpace(r.State))
				switch {
				case state == store.StateFilled:
					filled++
				case store.IsTerminalArmState(state): // the ONE terminal predicate (arm_state guard)
					reason := strings.TrimPrefix(r.StateReason, "mentor: ")
					if i := strings.Index(reason, " ["); i > 0 {
						reason = reason[:i]
					}
					if i := strings.Index(reason, " — "); i > 0 {
						reason = reason[:i]
					}
					endedBy[state+" — "+reason]++
				default:
					working++
				}
			}
			sent := 0
			for _, f := range w.addon.log {
				if f.typ == ntwire.FrameSignal && f.account == ft.account {
					sent++
				}
			}
			t.Logf("(g) %s: authored %d · placed %d (entry frames %d) · filled %d · non-terminal at end %d", ft.id, authored, placed, sent, filled, working)
			t.Logf("(g) %s intents: %s", ft.id, fdrSortedKV(intents))
			for _, k := range fdrSortedKeys(endedBy) {
				t.Logf("(g) %s ended ×%d — %s", ft.id, endedBy[k], k)
			}
			t.Logf("(g) %s trader counters: %s", ft.id, fdrSortedKV(traderCounters[ft.id]))
			t.Logf("(g) %s kernel refusals: %s", ft.id, fdrSortedKV(kernelRefusals[ft.id]))
		}
		// Level (lvl-) intents over the day — the live ledger has ZERO level rows.
		lvl := map[string]int{}
		var lines []string
		for _, o := range outcomes {
			if !strings.HasPrefix(o.in.ArmID, "lvl-") {
				continue
			}
			state := "authored"
			if o.rowID == 0 {
				state = "refused " + o.refusal
			}
			if o.birthCx {
				state += " + CANCELLED IN THE SAME TICK"
			}
			lvl[o.trader+" "+o.in.Setup]++
			lines = append(lines, fmt.Sprintf("%s %s %s %s %s @%.2f stop %.2f — %s", fdrCT(w.bars[o.barIdx].CloseTime+1), o.trader, o.in.ArmID, o.in.Setup, o.in.Side, o.in.Price, o.in.Stop, state))
		}
		t.Logf("(g) level (lvl-) Place intents emitted: %d — %s", len(lines), fdrSortedKV(lvl))
		for _, s := range lines {
			t.Logf("    %s", s)
		}
		for _, ft := range traders {
			lv := map[string]int{}
			for k, v := range kernelRefusals[ft.id] {
				for _, p := range []string{"phl", "plh", "level", "lvl", "trigger_retest", "near_box", "ema"} {
					if strings.HasPrefix(k, p) {
						lv[k] += v
						break
					}
				}
			}
			t.Logf("(g) %s level-family kernel refusals: %s", ft.id, fdrSortedKV(lv))
		}
	})
}

// fdrLast5mClose is the close of the last 5m bar that had CLOSED when ring bar
// i closed — the bar manageSwingPosition judges the swing on at that tick.
func fdrLast5mClose(w *fdrWorld, i int) float64 {
	from := i - 10
	if from < 0 {
		from = 0
	}
	agg := fdrAggregate(w.bars[from:i+1:i+1], 300_000, w.bars[i].CloseTime+1)
	for j := len(agg) - 1; j >= 0; j-- {
		if agg[j].Final {
			return agg[j].Close
		}
	}
	return 0
}

func fdrSortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fdrStartPathFlags drives the PRODUCTION start path a restart takes, for each
// trader id/account: Run() as the boot auto-start (manager.addTraderFromStore
// auto-starts an is_running=1 row), then Run() AGAIN on the SAME object as the
// owner's API start (api.handleStartTrader loads, then starts) — the duplicate
// start of 10-13. It returns each trader's DB is_running afterwards. Isolated
// store and NT8 link: the Run loops must not touch the replay's ledger.
func fdrStartPathFlags(t *testing.T, ids, accounts []string) map[string]bool {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "start-path.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	infos := make([]ntwire.AccountInfo, 0, len(accounts))
	for i, id := range ids {
		if err := st.Trader().Create(&store.Trader{ID: id, UserID: "u", Name: id, AIModelID: "m", ExchangeID: "ex", StrategyID: "s", Account: accounts[i], IsRunning: true}); err != nil {
			t.Fatal(err)
		}
		infos = append(infos, ntwire.AccountInfo{Name: accounts[i], IsSim: true})
	}
	srv := ntwire.NewTCPServer(nil)
	srv.SetAddrForTest("127.0.0.1:0")
	srv.SetAccountsList(infos, accounts[0])
	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop(); cancel() })
	c, err := net.Dial("tcp", srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	waitAddonRegistered(t, srv)
	t.Cleanup(func() { _ = c.Close() })
	if err := ntwire.WriteFrame(c, ntwire.FrameHello, ntwire.HelloPayload{ProtocolVersion: ntwire.ProtocolVersion, Source: "vltrader-addon"}); err != nil {
		t.Fatal(err)
	}
	var running []*AutoTrader
	var exits []chan struct{}
	for i, id := range ids {
		at := &AutoTrader{id: id, name: id, userID: "u", store: st, exchange: "ninjatrader", exchangeID: "ex",
			trader: ntTrader.NewTCPTrader(srv, "MNQ", accounts[i]), mcpClient: &fakeDecisionClient{}}
		at.config.StrategyConfig = &store.StrategyConfig{}
		at.config.ScanInterval = 200 * time.Millisecond
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() { _ = recover() }() // a bare-fixture cycle panic must not fail the flag check
			_ = at.Run()                     // the boot auto-start
		}()
		deadline := time.Now().Add(10 * time.Second)
		for !fdrOrderedInstalled(at) && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if !fdrOrderedInstalled(at) {
			t.Fatalf("(e) the boot start of %s never installed its ordered execution", id)
		}
		_ = at.Run() // the API start of the SAME running object
		running = append(running, at)
		exits = append(exits, done)
	}
	flags := map[string]bool{}
	for _, id := range ids {
		row, err := st.Trader().GetByID(id)
		flags[id] = err == nil && row != nil && row.IsRunning
	}
	// Stop every instance; a Stop that cannot return (the duplicate start
	// overwrote the first Run's stop context, orphaning its monitor) is
	// reported, never waited on forever.
	stopped := make(chan string, len(running))
	for _, at := range running {
		at := at
		go func() { at.Stop(); stopped <- at.id }()
	}
	deadline := time.After(5 * time.Second)
	for range running {
		select {
		case <-stopped:
		case <-deadline:
			t.Errorf("(e) Stop() of a duplicate-started trader did not return within 5 s (the second Run replaced the first Run's stop context)")
			return flags
		}
	}
	for i, at := range running {
		select {
		case <-exits[i]:
		case <-time.After(5 * time.Second):
			t.Errorf("(e) the running instance of %s did not exit after Stop", at.id)
		}
	}
	return flags
}

func fdrOrderedInstalled(at *AutoTrader) bool {
	at.orderedExecMu.Lock()
	defer at.orderedExecMu.Unlock()
	return at.orderedExecUnreg != nil
}
