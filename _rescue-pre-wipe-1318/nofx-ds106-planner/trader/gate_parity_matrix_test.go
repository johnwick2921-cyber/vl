package trader

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"nofx/discipline"
	"nofx/kernel"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	ntTrader "nofx/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0b — THE GATE-PARITY MATRIX ───────────────────────────────
//
// Every gate in admitChain (entry_admission.go) is tripped against EACH entry
// producer, and the producer — never admitEntry alone — is driven:
//
//	A_decision        executeDecisionWithRecord            → admitEntry(decision)
//	B_arm_pass        maybeManageArmedOrdersAt             → runArmedPlacementAt → armAdmitted → admitEntry(arm)
//	B_arm_send        runArmedPlacementAt (the send point) → armAdmitted → admitEntry(arm)
//	C_picture         PictureHtfEvaluator.Evaluate          → admitEntry(picture), before the claim
//	D_picture_scenario  a Picture scenario (source picture, P1) in the plan
//	                  → maybeManageArmedOrdersAt → runArmedPlacementAt → armAdmitted → admitEntry(arm, Source picture)
//	D_picture_send    runArmedPlacementAt (the send point) for the P1 row → armAdmitted → admitEntry(arm, Source picture)
//
// W5 retired C_picture_send: Picture has no send of its own. Its order is the
// P scenario the hand-off records, placed by the SHARED armed executor, so the
// D paths run every gate row the arm paths run, on a Picture-sourced scenario
// — plus the two admitChain steps a source=picture row answers at its send
// point (trader_stopped, day_plan_off).
//
// One row per (gate, path). A row either expects a refusal, names the layer
// that refuses (via), and checks the refusal NAMES the gate (the gate-block
// class counted and, where the path stamps one, the refusal text) — or ADMITS
// (the trip must not refuse: strict admits a Picture scenario, W5) — or is N/A
// with its reason. Every path's POSITIVE CONTROL runs first: nothing tripped →
// the producer gets past the whole chain (A: the decision reaches
// executeOpenLongWithRecord's price read; B: a signal frame reaches the wire;
// C: a hand-off is recorded; D: P1's LIMIT frame reaches the wire).
//
// The RED proof is external: each gate's refusal is removed from
// entry_admission.go by a scripted mutation and the rows the table marks
// via=admit for that gate must fail.

const (
	parityA     = "A_decision"
	parityB     = "B_arm_pass"
	parityBSend = "B_arm_send"
	parityC     = "C_picture"
	parityD     = "D_picture_scenario"
	parityDSend = "D_picture_send"
)

// parityVia names the layer expected to refuse the cell.
type parityVia string

const (
	// parityViaAdmit — admitEntry refuses (the gate under test).
	parityViaAdmit parityVia = "admit"
	// parityViaPassHead — maybeManageArmedOrdersAt's once-per-pass sessionRiskGateAt
	// (armed_executor.go:336) refuses before any row is authored.
	parityViaPassHead parityVia = "pass_head"
	// parityViaHoldPrecheck — an earlier MaintenanceHeld() on the path refuses first
	// (runArmedPlacementAt's hold check, the picture evaluator's pre-claim check).
	parityViaHoldPrecheck parityVia = "hold_precheck"
	// parityViaG1 — the arm path's authoring gates (EntryGate at authoring) plus G1
	// at the send point (arm_admission.go:41).
	parityViaG1 parityVia = "g1"
	// parityViaProducerHead — the producer's own head refuses (maybeManageArmedOrdersAt
	// returns when the Day Plan master is off).
	parityViaProducerHead parityVia = "producer_head"
)

type parityRow struct {
	gate string
	path string
	na   string // non-empty → N/A, with the reason
	// admits: the trip must NOT refuse — the producer still gets past the
	// whole chain (W5: plan_mode=strict admits a Picture scenario).
	admits bool
	via    parityVia
	// class is the gate-block counter class (telemetry) the refusal must count;
	// for parityViaPassHead it is the arm-refusal class (store counter).
	class string
	// text is a substring the path's refusal text must carry (A: rec.Error; C:
	// the evaluation reason). "" = the path stamps no text (B).
	text string
	trip func(r *parityRig)
	// offset: B_arm_pass / D_picture_scenario → the pass clock is base+offset;
	// B_arm_send / D_picture_send → the send point's clock is base+offset
	// (authoring stays at base); C → the whole bar tape (and the clock) shift
	// by offset (whole days only). A: unused (wall).
	offset time.Duration
}

// ── the wire ────────────────────────────────────────────────────────────────

// parityWire is one real in-process NT8 link: a started TCPServer, the
// AddOn-side conn the test writes frames on (feed_status, subscribed), and the
// TCPTrader on it. frames carries, in wire order, every signal frame and every
// sentinel the server wrote to the "AddOn".
type parityWire struct {
	srv    *ntwire.TCPServer
	conn   net.Conn
	nt     *ntTrader.TCPTrader
	frames chan parityFrame
	seq    int
}

type parityFrame struct {
	signal   bool
	sentinel string
}

// paritySentinel marks the barrier frame: a bars_history_request is a pure
// write under the server's writeMu with no server-side state
// (SendBarsHistoryRequest), so it is ordered on the one conn after anything
// the producer already wrote.
const paritySentinel = "PARITY-SENTINEL"

func newParityWire(t *testing.T, heartbeat bool) *parityWire {
	t.Helper()
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
	frames := make(chan parityFrame, 64)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			var f parityFrame
			switch env.Type {
			case ntwire.FrameSignal:
				f.signal = true
			case ntwire.FrameBarsHistoryRequest:
				var p ntwire.BarsHistoryRequestPayload
				if json.Unmarshal(env.Payload, &p) != nil || p.Symbol != paritySentinel {
					continue
				}
				f.sentinel = p.RequestID
			default:
				continue
			}
			select {
			case frames <- f:
			case <-done:
				return
			}
		}
	}()
	parityWaitFor(t, "server accepted the AddOn connection", s.IsConnected)
	if heartbeat {
		top := ntwire.MinAddonBuildPictureHtf
		if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
			t.Fatal(err)
		}
		parityWaitFor(t, "far side proven", func() bool { return ntwire.FarSideProven(s.FarSideBuildID(), top) })
	}
	return &parityWire{srv: s, conn: conn, nt: ntTrader.NewTCPTrader(s, "MNQ", "Sim101"), frames: frames}
}

// signalArrives waits (bounded: 2s) for a signal frame — a send we expect.
func (w *parityWire) signalArrives(t *testing.T) bool {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f := <-w.frames:
			if f.signal {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// signalSent reports whether ANY signal frame reached the wire before now,
// without a timing guess: every producer writes its signal synchronously
// (SendSignal → flushPendingReportFor, under writeMu) before it returns, so a
// sentinel written after it returns is read after that signal on the same conn.
func (w *parityWire) signalSent(t *testing.T) bool {
	t.Helper()
	w.seq++
	id := paritySentinel + "-" + strconv.Itoa(w.seq)
	if err := w.srv.SendBarsHistoryRequest(ntwire.BarsHistoryRequestPayload{RequestID: id, Symbol: paritySentinel}); err != nil {
		t.Fatalf("sentinel: %v", err)
	}
	sent := false
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f := <-w.frames:
			if f.signal {
				sent = true
			} else if f.sentinel == id {
				return sent
			}
		case <-deadline:
			t.Fatal("sentinel frame never came back")
		}
	}
}

// parityWaitFor polls cond (bounded: 2s) — frames are consumed asynchronously.
func parityWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ── the rigs (one fresh fixture per cell) ───────────────────────────────────

// A migrated store costs ~1s to build (store.New runs every migration) and a
// copy of a migrated file ~0.07s, so each cell opens its own COPY of one
// template migrated once per matrix run: the same schema store.New builds,
// with every cell still on a private database.
func parityTemplate(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "template.db")
	st, err := store.New(path)
	if err != nil {
		t.Fatalf("template store: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("template close: %v", err)
	}
	return path
}

func parityStore(t *testing.T, template string) *store.Store {
	t.Helper()
	raw, err := os.ReadFile(template)
	if err != nil {
		t.Fatalf("template read: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cell.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("template copy: %v", err)
	}
	st, err := store.New(path)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

type parityRig struct {
	t      *testing.T
	path   string
	at     *AutoTrader
	st     *store.Store
	w      *parityWire
	dir    string    // maintenance data dir
	now    time.Time // the producer's pinned clock (A: unused — wall clock)
	sendAt time.Time // B_arm_send: the send point's clock
	env    *pictureHtfTestEnv
}

// clock is the instant the producer will judge at. The decision path reads the
// wall clock itself (executeDecisionWithRecord passes time.Now()), so A's trips
// are built to hold at ANY wall time.
func (r *parityRig) clock() time.Time {
	switch r.path {
	case parityA:
		return time.Now()
	case parityBSend, parityDSend:
		return r.sendAt
	}
	return r.now
}

// parityBaseB is the arm fixture's clock: Friday 2026-09-11 10:00 CT (open,
// inside the whole-day TEST session, outside lunch and first-5m).
var parityBaseB = time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC)

// parityDecisionRig is resetTrader's trader (same construction) on a template
// copy, with a real wire.
func parityDecisionRig(t *testing.T, id, template string) *parityRig {
	t.Helper()
	dir := withMaintenanceDir(t)
	st := parityStore(t, template)
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: false}}
	at := &AutoTrader{id: id, store: st, exchange: "ninjatrader"}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	at.config.Exchange = "ninjatrader"
	at.config.NinjaTraderSymbol = "MNQ"
	w := newParityWire(t, false)
	at.trader = w.nt
	// No bar provider: the entry gate's live price reads 0 (legs 5/6 abstain)
	// and the order step stops at its price read. (The futures market read
	// makes no Binance call since W-NO-BINANCE A — TestAIOpenSendHalfMakesNoBinanceCall.)
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = nil
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	t.Cleanup(func() { kernel.SetTraderPlanProviders(id, kernel.TraderPlanProviders{}) })
	return &parityRig{t: t, path: parityA, at: at, st: st, w: w, dir: dir}
}

// parityArmRig is liveArmFixture with the AddOn conn exposed (feed_status and
// subscribed frames must be written by the test) and a per-cell trader id and
// clock (the plan provider is registered under the id with no cleanup of its
// own — removed here).
func parityArmRig(t *testing.T, id, path, template string, now time.Time) *parityRig {
	t.Helper()
	dir := withMaintenanceDir(t)
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2
	w := newParityWire(t, false)
	if snaps := w.srv.OrderSnapshots(); snaps != nil {
		snaps.PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}
	st := parityStore(t, template)
	at := &AutoTrader{id: id, exchange: "ninjatrader", store: st, trader: w.nt}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	t.Cleanup(func() { kernel.SetTraderPlanProviders(id, kernel.TraderPlanProviders{}) })
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
	return &parityRig{t: t, path: path, at: at, st: st, w: w, dir: dir, now: now, sendAt: now.Add(time.Second)}
}

// parityShift moves a bar tape by d (whole days keep every CT time of day).
func parityShift(bars []market.Kline, d time.Duration) []market.Kline {
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		b.OpenTime += d.Milliseconds()
		b.CloseTime += d.Milliseconds()
		out[i] = b
	}
	return out
}

// parityPictureRig is newPictureHtfEnv's harness (same construction: trader
// RUNNING, Day Plan ON, R:R floor 2.5, capability proven, the submit seam
// recording; the seam and capability are restored on cleanup) on a template
// copy and a REAL wire — the Picture send path needs the concrete TCPTrader,
// and the feed/roll trips need the AddOn conn. shift moves the whole tape (the
// seeded setup is 14:00:00.5 CT Monday).
func parityPictureRig(t *testing.T, id, path, template string, shift time.Duration) *parityRig {
	t.Helper()
	dir := withMaintenanceDir(t)
	cfg := store.PictureHtfConfig{Enabled: true, MinRR: 2.5}
	sc := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PictureHtf: &cfg, PlanEnabled: true}}
	sc.RiskControl.MinRiskRewardRatio = 2.5
	st := parityStore(t, template)
	at := &AutoTrader{id: id, store: st, exchange: "ninjatrader"}
	at.config.StrategyConfig = &sc
	at.mcpClient = &fakeDecisionClient{}
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()
	env := &pictureHtfTestEnv{t: t, at: at, st: st, eval: NewPictureHtfEvaluator(at, pictureTestResolved(&cfg))}
	origSeam, origCap, origBars := pictureHtfSubmitSeam, pictureHtfCapabilityProven, market.FuturesBarsProvider
	pictureHtfSubmitSeam = func(e *PictureHtfEvaluator, row *store.PictureHtfOpportunityDB, stopPx, targetPx, qty float64, _ time.Time) error {
		env.submits = append(env.submits, row.OppKey)
		return nil
	}
	pictureHtfCapabilityProven = func(*AutoTrader) bool { return true }
	t.Cleanup(func() {
		pictureHtfSubmitSeam = origSeam
		pictureHtfCapabilityProven = origCap
		market.FuturesBarsProvider = origBars
	})
	w := newParityWire(t, true)
	env.at.trader = w.nt
	env.at.config.NinjaTraderSymbol = "MNQ"
	wireNT8EntryLatch(env.at, w.nt)
	w.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, time.Now())
	env.seed(parityShift(pictureBars4H(), shift), parityShift(pictureBarsH1(), shift), parityShift(pictureBars5M(true), shift))
	env.now = time.UnixMilli(t4h0 + 42*3600*1000 + 500).Add(shift)
	return &parityRig{t: t, path: path, at: env.at, st: env.st, w: w, dir: dir, now: env.now, env: env}
}

// parityPictureScenarioRig is the D rig: the arm rig's trader, strategy and
// wire (parityArmRig) with the plan holding ONE Picture scenario — P1, source
// picture, recorded by the live run (the run epoch is marked and the trader
// RUNS, as a Picture row is admitted only then) — instead of the planner's S1.
// The geometry is the Picture executor fixture's (picDefault: zone
// 100.00–100.50, stop 97, target 110) on a 1m tape whose newest close 100.25
// is inside the zone. The eligibility window is wide (8 h) so every row's
// clock offset — the lunch band, the Friday close — sits inside it and a
// refusal is the gate's, never the deadline's.
func parityPictureScenarioRig(t *testing.T, id, path, template string, now time.Time) *parityRig {
	t.Helper()
	dir := withMaintenanceDir(t)
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2
	w := newParityWire(t, false)
	if snaps := w.srv.OrderSnapshots(); snaps != nil {
		snaps.PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	}
	st := parityStore(t, template)
	at := &AutoTrader{id: id, exchange: "ninjatrader", store: st, trader: w.nt}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()
	t.Cleanup(func() { kernel.SetTraderPlanProviders(id, kernel.TraderPlanProviders{}) })
	epoch := at.markPictureRunEpoch(now)
	t.Cleanup(at.clearPictureRunEpoch)
	g := picDefault
	g.window = 8 * time.Hour
	blob, _ := json.Marshal(zoneDoc(picScenario("P1", "opp-"+id, now, epoch, g)))
	shadowPlanAtTime(t, at, st, string(blob), now)
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return zoneTape(100.25, now, 0) }
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	return &parityRig{t: t, path: path, at: at, st: st, w: w, dir: dir, now: now, sendAt: now.Add(time.Second)}
}

// ── registry helpers ────────────────────────────────────────────────────────

func parityResetRegistryCache(at *AutoTrader) {
	at.regMu.Lock()
	at.regCacheDay = ""
	at.regCache = kernel.SessionRegistry{}
	at.regMu.Unlock()
}

func parityWriteRegistry(r *parityRig, sessions ...kernel.SessionDef) {
	r.t.Helper()
	blob, err := json.Marshal(kernel.SessionRegistry{Sessions: sessions})
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.st.SetSystemConfig(kernel.SessionRegistryConfigKey, string(blob)); err != nil {
		r.t.Fatal(err)
	}
	parityResetRegistryCache(r.at)
}

// parityNoSession is a registry whose one window is empty (start == end —
// InBlackoutWindow is never true), so NO session is active at any instant.
var parityNoSession = kernel.SessionDef{Name: "NEVER", WindowStartCT: "03:00", WindowEndCT: "03:00", ReadCT: "03:00", FlatCT: "03:00", Enabled: true}

func paritySessionOffset(r *parityRig, session string, minutes int) {
	m := minutes
	dp := r.at.config.StrategyConfig.DayPlan
	dp.Sessions = append(dp.Sessions, store.DayPlanSessionOverride{Session: session, LastEntryOffsetMin: &m})
}

// ── trips ───────────────────────────────────────────────────────────────────

func parityTripStopped(r *parityRig) {
	r.at.isRunningMutex.Lock()
	r.at.isRunning = false
	r.at.isRunningMutex.Unlock()
}

func parityTripDayPlanOff(r *parityRig) { r.at.config.StrategyConfig.DayPlan.PlanEnabled = false }

func parityTripFeedDown(r *parityRig) {
	if err := ntwire.WriteFrame(r.w.conn, ntwire.FrameFeedStatus, ntwire.FeedStatusPayload{PriceStatus: "ConnectionLost", Time: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		r.t.Fatal(err)
	}
	parityWaitFor(r.t, "feed_status ConnectionLost consumed", func() bool { return !r.w.nt.IsFeedConnected() })
}

func parityTripDeadMan(r *parityRig) { r.at.deadMan.set(dmDisconnected) }

func parityTripFrozen(r *parityRig) {
	discipline.FreezeTrader(r.at.id, "parity: reconcile divergence", r.clock().UnixMilli())
	id := r.at.id
	r.t.Cleanup(func() { discipline.ClearFreeze(id) })
}

func parityTripBootIntegrity(r *parityRig) {
	kernel.SetTradingRefusedForTest(true, "parity: prompt goldens drifted")
	r.t.Cleanup(func() { kernel.SetTradingRefusedForTest(false, "") })
}

func parityTripPause(r *parityRig) { r.at.pauseUntilMs.Store(r.clock().Add(time.Hour).UnixMilli()) }

func parityTripHold(r *parityRig) { setHold(r.t, r.dir, "parity-job") }

// parityTripRoll: the AddOn ACKs a resolved contract, and the window is wide enough
// that it is inside the block window on ANY date before 2099 (the decision
// path judges at the wall clock).
func parityTripRoll(r *parityRig) {
	r.t.Setenv("ROLL_BLOCK_DAYS_BEFORE_EXPIRY", "100000")
	r.at.config.Exchange = "ninjatrader" // the rigs set at.exchange (resetTrader's construction); the roll gate reads config.Exchange
	if err := ntwire.WriteFrame(r.w.conn, ntwire.FrameSubscribed, ntwire.SubscribedPayload{Symbol: "MNQ", ResolvedContract: "MNQ 12-99"}); err != nil {
		r.t.Fatal(err)
	}
	parityWaitFor(r.t, "subscribed ACK consumed", func() bool {
		return r.w.nt.BarsSubscriptionStates()["MNQ"].Contract == "MNQ 12-99"
	})
}

// parityTripBreaker: N=1 and one resolved LOSING close this CME session-day. On the
// wall-clock path the exit is a minute in the FUTURE, so it sits inside
// whichever session-day the gate's own time.Now() lands in (the query is
// exit_time >= session start, no upper bound).
func parityTripBreaker(r *parityRig) {
	r.at.config.StrategyConfig.RiskControl.ConsecutiveLossHalt = store.IntPtr(1)
	exit := r.clock().Add(-10 * time.Minute)
	if r.path == parityA {
		exit = time.Now().Add(time.Minute)
	}
	neg := -52.0
	row := &store.TraderPosition{TraderID: r.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG", Quantity: 1,
		EntryPrice: 100, ExitPrice: 99, RealizedPnL: neg, PnlCorrected: &neg, Status: "CLOSED", CloseReason: "sync",
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}
	if err := r.st.GormDB().Create(row).Error; err != nil {
		r.t.Fatal(err)
	}
}

// parityTripNoTradeBand (C paths): no session window is active → the session-risk
// verdict's band leg ("outside all session windows"). The arm paths take the
// lunch band from their clock instead (row offset).
func parityTripNoTradeBand(r *parityRig) { parityWriteRegistry(r, parityNoSession) }

func parityTripLastEntry(r *parityRig) {
	switch r.path {
	case parityA:
		// Wall clock: two sessions tile the whole day and each cutoff sits at
		// its session START, so every instant is inside a session past its
		// last entry.
		r.at.config.StrategyConfig.DayPlan.PlanEnabled = true
		parityWriteRegistry(r,
			kernel.SessionDef{Name: "AM", WindowStartCT: "00:00", WindowEndCT: "12:00", ReadCT: "00:00", FlatCT: "12:00", Enabled: true},
			kernel.SessionDef{Name: "PM", WindowStartCT: "12:00", WindowEndCT: "00:00", ReadCT: "12:00", FlatCT: "00:00", Enabled: true})
		paritySessionOffset(r, "AM", 720)
		paritySessionOffset(r, "PM", 720)
	case parityB, parityBSend, parityD, parityDSend:
		paritySessionOffset(r, "TEST", 900) // TEST ends 23:59 → cutoff 08:59 CT; the clock is 10:00 CT
	default:
		paritySessionOffset(r, kernel.SessionNY, 60) // NY ends 14:45 → cutoff 13:45 CT; the clock is 14:00 CT
	}
}

// parityTripSessionGate (A): Day Plan on and no session active at any wall time.
func parityTripSessionGate(r *parityRig) {
	r.at.config.StrategyConfig.DayPlan.PlanEnabled = true
	parityWriteRegistry(r, parityNoSession)
}

// parityTripPlanMode (A): strict, and the trader's plan provider has no plan.
func parityTripPlanMode(r *parityRig) {
	r.at.config.StrategyConfig.DayPlan.PlanMode = "strict"
	kernel.SetTraderPlanProviders(r.at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return nil }})
}

// parityTripStrict (C, D): plan_mode=strict, the plan left as it is. Since W5
// strict does not refuse Picture — its opportunity enters as a Day Plan
// scenario (C), and the scenario is a cited, armed plan scenario (D).
func parityTripStrict(r *parityRig) { r.at.config.StrategyConfig.DayPlan.PlanMode = "strict" }

func parityTripApproval(r *parityRig) { r.at.config.StrategyConfig.DayPlan.ApprovalRequired = true }

func parityTripReentry(r *parityRig) {
	r.at.config.StrategyConfig.RiskControl.ReentryCooldownMinutes = 20
	discipline.NoteStopLossExit(r.at.id, "MNQ", "long", 99, r.clock().Add(-time.Minute).UnixMilli())
	r.t.Cleanup(discipline.ResetReentryForTest)
}

// parityTripForceFlat trips EntryGate leg D (the daily force-flat), the leg every
// path's entry gate runs first.
func parityTripForceFlat(r *parityRig) {
	kernel.SetDailyForceFlat(r.at.id, "parity: daily loss limit hit")
	id := r.at.id
	r.t.Cleanup(func() { kernel.ClearDailyForceFlat(id) })
}

// ── the table ───────────────────────────────────────────────────────────────

const (
	parityNAStopped       = "the decision and planner-arm producers run only inside runCycle, which runs only while the trader runs (entry_admission.go:166-169)"
	parityNADayPlanA      = "Day Plan off is a legitimate decision-path mode (the AI path trades with the master off); admitChain's day_plan_off step is Picture-only"
	parityNABandA         = "the decision path's band is its session_gate step (sessionEntryBlockedAt); the session-risk verdict is arm/picture only"
	parityNACMEA          = "the decision path's runCycle skips the whole cycle while CME is closed; admitChain's cme_closed step is arm/picture only"
	parityNAReentryA      = "the decision path's cooldown is the kernel's destructive ReentryBlocked (it turns the decision into wait) — admitChain's reentry step is arm/picture only"
	parityNASessionGateBC = "session_gate is decision/agent only; arm and picture get the same sessionEntryBlockedAt verdict as the no_trade_band row (sessionRiskGateAt) and the calendar as the cme_closed row"
	parityNAPlanModeB     = "plan_mode is decision/agent only; the arm path's strict leg is EntryGate leg 0 at authoring (entry_gate row, G1)"
	parityNASendViaPass   = "runArmedPlacementAt is reached only from maybeManageArmedOrdersAt (armed_executor.go:898), whose head and runCycle own this gate — the B_arm_pass row"
	parityNASendEntryGate = "the arm send point's entry gate is G1 — the admitted set only an authoring pass produces; a hand-built set would be a re-built input (the B_arm_pass G1 row drives it)"
	parityNAPlanModeDSend = "the arm send point never re-reads plan_mode — strict is EntryGate leg 0 at authoring; the D_picture_scenario row drives strict through the pass (and admits)"
)

func parityTable() []parityRow {
	var rows []parityRow
	add := func(r parityRow) { rows = append(rows, r) }
	for _, p := range []string{parityA, parityB, parityBSend, parityC, parityD, parityDSend} {
		add(parityRow{gate: "positive", path: p})
	}

	// trader_stopped / day_plan_off — Picture only in admitChain: the Picture
	// path, and a source=picture arm row at its send point (W5 D21).
	add(parityRow{gate: "trader_stopped", path: parityA, na: parityNAStopped})
	add(parityRow{gate: "trader_stopped", path: parityB, na: parityNAStopped})
	add(parityRow{gate: "trader_stopped", path: parityBSend, na: parityNAStopped})
	add(parityRow{gate: "trader_stopped", path: parityC, via: parityViaAdmit, class: "trader_stopped", text: "picture: the trader is not running", trip: parityTripStopped})
	for _, p := range []string{parityD, parityDSend} {
		add(parityRow{gate: "trader_stopped", path: p, via: parityViaAdmit, class: "trader_stopped", trip: parityTripStopped})
	}
	add(parityRow{gate: "day_plan_off", path: parityA, na: parityNADayPlanA})
	add(parityRow{gate: "day_plan_off", path: parityB, via: parityViaProducerHead, trip: parityTripDayPlanOff})
	add(parityRow{gate: "day_plan_off", path: parityBSend, na: parityNASendViaPass})
	add(parityRow{gate: "day_plan_off", path: parityC, via: parityViaAdmit, class: "day_plan_off", text: "picture: the Day Plan master is off", trip: parityTripDayPlanOff})
	add(parityRow{gate: "day_plan_off", path: parityD, via: parityViaProducerHead, trip: parityTripDayPlanOff})
	add(parityRow{gate: "day_plan_off", path: parityDSend, via: parityViaAdmit, class: "day_plan_off", trip: parityTripDayPlanOff})

	// The shared system/owner gates — every path, through admitEntry.
	shared := []struct {
		gate, class, text string
		trip              func(*parityRig)
	}{
		{"feed_down", "feed_down", "feed_down: NT8 price feed not Connected", parityTripFeedDown},
		{"dead_man", "dead_man", "dead_man_watchdog: awaiting reconciliation", parityTripDeadMan},
		{"frozen", "frozen", "frozen: parity: reconcile divergence", parityTripFrozen},
		{"boot_integrity", "boot_integrity", "boot_integrity_refused: parity: prompt goldens drifted", parityTripBootIntegrity},
		{"stop_until", "stop_until", "stop_until: paused until", parityTripPause},
		{"contract_roll", "contract_roll_resolved", "contract_roll: MNQ DEC99 expires 2099-12-18", parityTripRoll},
		{"approval_required", "approval_required", "approval_required", parityTripApproval},
	}
	for _, g := range shared {
		for _, p := range []string{parityA, parityB, parityBSend, parityC, parityD, parityDSend} {
			text := g.text
			if p == parityB || p == parityBSend || p == parityD || p == parityDSend {
				text = "" // armAdmitted discards the refusal text; the arm path is judged by the wire and the counter
			}
			add(parityRow{gate: g.gate, path: p, via: parityViaAdmit, class: g.class, text: text, trip: g.trip})
		}
	}

	// maintenance_hold — admitEntry on A; an earlier hold check on B and C.
	add(parityRow{gate: "maintenance_hold", path: parityA, via: parityViaAdmit, class: "maintenance_hold", text: "maintenance_hold: ", trip: parityTripHold})
	for _, p := range []string{parityB, parityBSend, parityD, parityDSend} {
		add(parityRow{gate: "maintenance_hold", path: p, via: parityViaHoldPrecheck, class: "maintenance_hold", trip: parityTripHold})
	}
	add(parityRow{gate: "maintenance_hold", path: parityC, via: parityViaHoldPrecheck, class: "maintenance_hold", text: "maintenance hold", trip: parityTripHold})

	// consecutive_loss — A: consecutiveLossHaltedAt; B/C: sessionRiskGateAt.
	add(parityRow{gate: "consecutive_loss", path: parityA, via: parityViaAdmit, class: "consecutive_loss", text: "consecutive_loss_halt: 1 consecutive losing trades", trip: parityTripBreaker})
	for _, p := range []string{parityB, parityD} {
		add(parityRow{gate: "consecutive_loss", path: p, via: parityViaPassHead, class: "consecutive_loss", trip: parityTripBreaker})
	}
	for _, p := range []string{parityBSend, parityDSend} {
		add(parityRow{gate: "consecutive_loss", path: p, via: parityViaAdmit, class: "consecutive_loss", trip: parityTripBreaker})
	}
	add(parityRow{gate: "consecutive_loss", path: parityC, via: parityViaAdmit, class: "consecutive_loss", text: "consecutive_loss_halt: 1 consecutive losing trades", trip: parityTripBreaker})

	// no_trade_band — the session-risk band (arm/picture).
	lunch := 2*time.Hour + 15*time.Minute // 12:15 CT
	add(parityRow{gate: "no_trade_band", path: parityA, na: parityNABandA})
	for _, p := range []string{parityB, parityD} {
		add(parityRow{gate: "no_trade_band", path: p, via: parityViaPassHead, class: "no_trade_band", offset: lunch})
	}
	for _, p := range []string{parityBSend, parityDSend} {
		add(parityRow{gate: "no_trade_band", path: p, via: parityViaAdmit, class: "no_trade_band", offset: lunch})
	}
	add(parityRow{gate: "no_trade_band", path: parityC, via: parityViaAdmit, class: "no_trade_band", text: "no_trade_band: outside all session windows", trip: parityTripNoTradeBand})

	// last_entry — every path.
	for _, p := range []string{parityA, parityC} {
		add(parityRow{gate: "last_entry", path: p, via: parityViaAdmit, class: "last_entry", text: "last_entry_cutoff: past last-entry", trip: parityTripLastEntry})
	}
	for _, p := range []string{parityB, parityBSend, parityD, parityDSend} {
		add(parityRow{gate: "last_entry", path: p, via: parityViaAdmit, class: "last_entry", trip: parityTripLastEntry})
	}

	// session_gate — decision only.
	add(parityRow{gate: "session_gate", path: parityA, via: parityViaAdmit, class: "session_gate", text: "session_gate: outside all session windows", trip: parityTripSessionGate})
	for _, p := range []string{parityB, parityBSend, parityC, parityD, parityDSend} {
		add(parityRow{gate: "session_gate", path: p, na: parityNASessionGateBC})
	}

	// cme_closed — arm/picture only.
	fridayClose := 6*time.Hour + 30*time.Minute // Friday 16:30 CT
	saturday := 5 * 24 * time.Hour              // Monday 14:00 CT → Saturday 14:00 CT
	add(parityRow{gate: "cme_closed", path: parityA, na: parityNACMEA})
	for _, p := range []string{parityB, parityBSend, parityD, parityDSend} {
		add(parityRow{gate: "cme_closed", path: p, via: parityViaAdmit, class: "cme_closed", offset: fridayClose})
	}
	add(parityRow{gate: "cme_closed", path: parityC, via: parityViaAdmit, class: "cme_closed", text: "cme_closed: weekend", offset: saturday})

	// plan_mode — refuses on the decision path only. W5: strict no longer
	// refuses Picture — its opportunity enters as a Day Plan scenario (C
	// admits), and that scenario is a cited, armed plan scenario (D admits).
	add(parityRow{gate: "plan_mode", path: parityA, via: parityViaAdmit, class: "plan_mode", text: "plan_mode: no active plan (strict mode restricts to the plan)", trip: parityTripPlanMode})
	add(parityRow{gate: "plan_mode", path: parityB, na: parityNAPlanModeB})
	add(parityRow{gate: "plan_mode", path: parityBSend, na: parityNAPlanModeB})
	add(parityRow{gate: "plan_mode", path: parityC, admits: true, trip: parityTripStrict})
	add(parityRow{gate: "plan_mode", path: parityD, admits: true, trip: parityTripStrict})
	add(parityRow{gate: "plan_mode", path: parityDSend, na: parityNAPlanModeDSend})

	// reentry_cooldown — arm/picture only.
	add(parityRow{gate: "reentry_cooldown", path: parityA, na: parityNAReentryA})
	for _, p := range []string{parityB, parityBSend, parityD, parityDSend} {
		add(parityRow{gate: "reentry_cooldown", path: p, via: parityViaAdmit, class: "reentry_cooldown", trip: parityTripReentry})
	}
	add(parityRow{gate: "reentry_cooldown", path: parityC, via: parityViaAdmit, class: "reentry_cooldown", text: "reentry_cooldown: stop-loss long exit", trip: parityTripReentry})

	// entry_gate — A: entryGateForDecisionAt; B, D: at authoring + G1; C: pictureEntryGate.
	add(parityRow{gate: "entry_gate", path: parityA, via: parityViaAdmit, class: "entry_gate", text: "entry_gate: refused: daily_force_flat — parity: daily loss limit hit (new entries blocked on the decision path", trip: parityTripForceFlat})
	for _, p := range []string{parityB, parityD} {
		add(parityRow{gate: "entry_gate", path: p, via: parityViaG1, class: "arm_not_admitted", trip: parityTripForceFlat})
	}
	for _, p := range []string{parityBSend, parityDSend} {
		add(parityRow{gate: "entry_gate", path: p, na: parityNASendEntryGate})
	}
	add(parityRow{gate: "entry_gate", path: parityC, via: parityViaAdmit, class: "entry_gate", text: "entry_gate: refused: daily_force_flat — parity: daily loss limit hit (new entries blocked on the picture path", trip: parityTripForceFlat})
	return rows
}

// ── the drive ───────────────────────────────────────────────────────────────

type parityOutcome struct {
	// passed: the producer got PAST the whole chain (see the header for each
	// path's observable) — judged on the side effect alone, never on whether
	// a refusal was also stamped: a chain that stamps a refusal and carries on
	// anyway must read as passed.
	passed bool
	clean  bool   // positive control only: no refusal stamped, the path's own success verdict
	text   string // the refusal text the path stamped ("" for B)
	detail string // for failure messages
}

func newParityRig(t *testing.T, row parityRow, template string) *parityRig {
	t.Helper()
	id := "parity-" + row.path + "-" + row.gate
	switch row.path {
	case parityA:
		return parityDecisionRig(t, id, template)
	case parityB:
		return parityArmRig(t, id, parityB, template, parityBaseB.Add(row.offset))
	case parityBSend:
		r := parityArmRig(t, id, parityBSend, template, parityBaseB)
		if row.offset > 0 {
			r.sendAt = parityBaseB.Add(row.offset)
		}
		return r
	case parityC:
		return parityPictureRig(t, id, row.path, template, row.offset)
	case parityD:
		return parityPictureScenarioRig(t, id, parityD, template, parityBaseB.Add(row.offset))
	case parityDSend:
		r := parityPictureScenarioRig(t, id, parityDSend, template, parityBaseB)
		if row.offset > 0 {
			r.sendAt = parityBaseB.Add(row.offset)
		}
		return r
	}
	t.Fatalf("unknown path %q", row.path)
	return nil
}

// parityAuthorUnderHold runs one full production pass with the maintenance hold
// set: the pass AUTHORS the arm (every authoring gate passed) and the send
// point refuses it on the hold, so the row stays armed and unsent. The hold is
// then cleared. Returns the armed row.
func parityAuthorUnderHold(r *parityRig) store.ArmedOrderDB {
	r.t.Helper()
	setHold(r.t, r.dir, "parity-author")
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if err := store.ClearMaintenanceHold(r.dir, "parity-author"); err != nil {
		r.t.Fatal(err)
	}
	if r.w.signalSent(r.t) {
		r.t.Fatal("fixture: the authoring pass under hold must not send")
	}
	rows, err := r.st.ArmedOrders().ListNonTerminal(r.at.id)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, a := range rows {
		if a.State == store.StateArmed {
			return a
		}
	}
	r.t.Fatalf("fixture: the authoring pass must leave an armed row, got %+v", rows)
	return store.ArmedOrderDB{}
}

func parityDrive(r *parityRig, row parityRow, expectPass bool) parityOutcome {
	r.t.Helper()
	// A send we expect is waited for (bounded); a send we refuse is ruled out
	// by the sentinel barrier, never by a timing guess.
	sent := func() bool {
		if expectPass {
			return r.w.signalArrives(r.t)
		}
		return r.w.signalSent(r.t)
	}
	switch r.path {
	case parityA:
		if row.trip != nil {
			row.trip(r)
		}
		rec := &store.DecisionAction{Action: "open_long", Symbol: "MNQ"}
		err := r.at.executeDecisionWithRecord(&kernel.Decision{Action: "open_long", Symbol: "MNQ"}, rec)
		// PAST THE CHAIN: executeOpenLongWithRecord ran reconcileBeforeOpenNT,
		// the positions read and the max-positions check, and stopped at its
		// price read (no bar provider).
		reached := err != nil && strings.Contains(err.Error(), "no NT8 bar provider wired")
		return parityOutcome{passed: reached, clean: rec.Error == "", text: rec.Error, detail: "err=" + parityErr(err) + " rec.Error=" + rec.Error}

	case parityB, parityD:
		if row.via == parityViaG1 {
			// G1: an arm authored and admitted by an EARLIER pass; THIS pass's
			// authoring refuses it (the trip), so the send point must not
			// place it.
			parityAuthorUnderHold(r)
			r.now = r.now.Add(time.Second)
		}
		if row.trip != nil {
			row.trip(r)
		}
		r.at.maybeManageArmedOrdersAt(nil, r.now)
		return parityOutcome{passed: sent(), clean: true}

	case parityBSend, parityDSend:
		arm := parityAuthorUnderHold(r)
		if row.trip != nil {
			row.trip(r)
		}
		if snaps := r.w.srv.OrderSnapshots(); snaps != nil {
			snaps.PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, r.sendAt)
		}
		// The admitted set the authoring pass produced for this row (pass 1
		// passed every authoring gate); the send point is then driven at its
		// own clock, as maybeManageArmedOrdersAt:898 calls it.
		admitted := armAdmission{}
		admitted.admit(arm.PlanID, arm.Scenario, arm.LegIndex)
		bars := shadowBarsNearAt(100, r.sendAt)
		if r.path == parityDSend {
			if arm.Source != store.ArmSourcePicture || arm.Scenario != "P1" {
				r.t.Fatalf("fixture: the D send point must drive the Picture scenario's row, got %+v", arm)
			}
			bars = zoneTape(100.25, r.sendAt, 0)
		}
		r.at.runArmedPlacementAt(bars, r.sendAt.Add(-time.Hour).UnixMilli(), r.sendAt, admitted)
		return parityOutcome{passed: sent(), clean: true}

	case parityC:
		if row.trip != nil {
			row.trip(r)
		}
		before := len(r.env.submits)
		r.env.eval.markFresh5mReceivedAt(r.env.now)
		res := r.env.eval.Evaluate("MNQ", r.env.now)
		return parityOutcome{passed: len(r.env.submits) > before, clean: res.Stage == store.PictureStagePlanned, text: res.Reason, detail: "stage=" + res.Stage + " reason=" + res.Reason}
	}
	r.t.Fatalf("unknown path %q", r.path)
	return parityOutcome{}
}

func parityErr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// armRefusals reads the arm-refusal store counter for the rig's active plan.
func (r *parityRig) armRefusals(class string) int {
	plan := kernel.ActivePlanFor(r.at.id, r.at.futuresSymbol())
	if plan == nil {
		r.t.Fatal("fixture: no active plan to key the arm-refusal counter")
	}
	return store.ArmRefusalCount(r.st, r.at.id, kernel.PlanTradeDateFor(plan), plan.Session, class)
}

func TestGateParityMatrix(t *testing.T) {
	template := parityTemplate(t)
	for _, row := range parityTable() {
		row := row
		t.Run(row.gate+"@"+row.path, func(t *testing.T) {
			if row.na != "" {
				t.Logf("N/A — %s", row.na)
				return
			}
			r := newParityRig(t, row, template)
			if row.gate == "positive" {
				if out := parityDrive(r, row, true); !out.passed || !out.clean {
					t.Fatalf("POSITIVE CONTROL: nothing tripped, but %s did not get past the chain (%s)", row.path, out.detail)
				}
				return
			}
			if row.admits {
				if out := parityDrive(r, row, true); !out.passed || !out.clean {
					t.Fatalf("%s: tripped %s, which must ADMIT this producer, but it did not get past the chain (%s)", row.path, row.gate, out.detail)
				}
				return
			}
			before := 0
			if row.via != parityViaPassHead && row.class != "" {
				before = gateBlocks(r.at.id, row.class)
			}
			out := parityDrive(r, row, false)
			if out.passed {
				t.Fatalf("%s: tripped %s, but the producer got past the chain (%s)", row.path, row.gate, out.detail)
			}
			if row.text != "" && !strings.Contains(out.text, row.text) {
				t.Fatalf("%s: the refusal must name %s — want %q in %q (%s)", row.path, row.gate, row.text, out.text, out.detail)
			}
			switch row.via {
			case parityViaAdmit, parityViaHoldPrecheck, parityViaG1:
				if got := gateBlocks(r.at.id, row.class); got < before+1 {
					t.Fatalf("%s: the refusal must count gate-block class %q (before=%d after=%d; %s)", row.path, row.class, before, got, out.detail)
				}
			case parityViaPassHead:
				if n := r.armRefusals(row.class); n < 1 {
					t.Fatalf("%s: the pass head must record the arm refusal class %q (got %d)", row.path, row.class, n)
				}
			case parityViaProducerHead:
				// no counter: the producer returns at its head.
			}
		})
	}
}
