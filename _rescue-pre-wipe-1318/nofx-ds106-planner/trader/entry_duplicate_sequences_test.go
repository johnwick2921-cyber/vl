package trader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nofx/hook"
	"nofx/kernel"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	ntTrader "nofx/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W0 (b), dispatch D10 — THE FIVE DUPLICATE SEQUENCES ────────
//
// D10 (the defect): cross-path duplicates were POSSIBLE. Picture read only
// GetPositions and its own pending rows; the armed oneContractGuard read the
// book and positions, never the Picture ledger; the AI path read positions; B3
// keys never collide across paths; nothing serialized the sends; and the AI's
// reconcileBeforeOpenNT flattened a B/C fill not yet in trader_positions and
// then opened its own.
//
// Every sequence below runs over ONE real in-process TCPTrader — a started
// ntwire.TCPServer on 127.0.0.1:0, a dialed conn that reads every frame the
// server writes (the AddOn's side of the wire), ntTrader.NewTCPTrader(s, "MNQ",
// "Sim101") bound as at.trader, and wireNT8Maintenance + wireNT8EntryLatch
// exactly as NewAutoTrader wires them. The PRODUCERS are driven, never the
// latch (canon 53):
//
//	B  the armed placement pass   at.maybeManageArmedOrdersAt(nil, armNow)
//	C  Picture HTF                eval.Evaluate("MNQ", picNow) → claim → the Day Plan
//	                              hand-off (records scenario P1) → the armed pass at
//	                              picNow+1s places P1 (a market_in_zone LIMIT)
//	A  the AI open (send half)    at.executeOpenLongWithRecord / ...ShortWithRecord
//
// and the FrameSignal / FrameClosePosition frames reaching the far side are
// counted after each step, up to an ordered stream marker (expectFrames).
//
//	S1 B→C  an armed entry is working → Picture records P1 → P1's pass is refused
//	S2 C→B  Picture's P1 was placed → the armed pass for S1 runs → refused
//	S3 B→A  an armed entry is working (resting, then filled at NT8 but not yet
//	        in trader_positions) → an AI open → refused, never flattened
//	S4 C→A  Picture's P1 is working (resting, then filled) → an AI open →
//	        refused, never flattened
//	S5 A→B  an AI entry was just sent → the armed pass → refused; and two
//	        producers released at the same instant (AI ‖ armed, Picture ‖ armed)
//	        → exactly one entry reaches the wire
//
// W5 — PICTURE HAS NO SEND OF ITS OWN. Its order is the P1 scenario the
// hand-off records in the Day Plan, placed by the SAME armed executor as S1
// (the retired market-entry send is gone). With no plan row for the Picture
// instant's session, the hand-off writes a machine plan v1 holding P1; the
// armed pass reads it on the Picture clock (the plan provider and the 1m tape
// are the fixture's CONTEXT: the Picture context for P1's pass, the arm
// context — the defaults, exactly the W0 fixture — for everything else).
//
// Every sequence ends with EXACTLY ONE FrameSignal on the wire and no
// FrameClosePosition. S1r/S2r repeat S1/S2 across a process restart (a fresh
// server + TCPTrader + AutoTrader over the same store), where the in-memory
// queued/recent evidence is gone and the ledgers are the only evidence left.
//
// CLOCKS. Each producer runs on its own pinned clock: Picture on its tape's
// picNow (2026-09-14 19:00:00.5Z), the armed pass on armNow (2026-09-15
// 15:00Z). armNow is placed AFTER picNow so the arm's 1m tape is in Picture's
// future and Picture's newest-1m-close read (latestClose) stays "unknown" — the
// two fixtures share one bar provider without bending each other's geometry.
// Picture's P1 is placed by the armed pass at picPassNow (picNow+1s, inside
// its 10 s eligibility window) on the Picture context.
// The AI open's send half takes no clock. The latch itself reads the wall clock
// (wireNT8EntryLatch passes time.Now — production), so the book is seeded at
// the wall instant: fresh for the latch, and a negative (fresh) age at both
// pinned clocks.
//
// WHY THE AI's SEND HALF. executeDecisionWithRecord stamps time.Now() into the
// admission chain (no clock seam), and that chain's date-dependent gates
// (session window, last-entry, contract roll, EntryGate at the live price) are
// not the D10 surface; driving it would make these tests depend on the wall
// clock. The send half (reconcileBeforeOpenNT → positions → OpenLong/Short) is
// exactly the part D10 names.
//
// NO NETWORK. Until W-NO-BINANCE A the send half's market read,
// market.GetWithExchange, made two outbound HTTPS calls on the futures branch
// (getOpenInterestData and getFundingRate, each to fapi.binance.com with a 30 s
// client timeout). Those calls are GONE: the futures branch now takes
// market.futuresOIFunding (absent, no network), pinned by
// TestAIOpenSendHalfMakesNoBinanceCall and the market source guard. The stub
// stays, belt and braces: newDupWire still routes every client
// market.NewAPIClient builds through its own seam (hook.SET_HTTP_CLIENT) to a
// RoundTripper that fails at once — so a future outbound call on this path
// costs microseconds here instead of the fixture's 60 s budgets (the latch's
// book-age bound and the server's TCPHeartbeatAckTimeout) — and restores the
// previous hook in t.Cleanup.
//
// THE RACES. Picture runs on the live-bar goroutine, concurrently with the
// cycle (armed pass, AI decision); the agent-chat and debug doors are other
// goroutines. raceAtPermit holds each producer at the broker's entry permit
// (the seam just before the latch) until both have arrived, so they reach the
// latch at the same instant — a test of the per-key send mutex, not of which
// goroutine happens to run first. Since W5 Picture's half of a race is its
// HAND-OFF (on the live-bar goroutine) — its order is placed by an armed pass,
// and armed passes are one at a time per trader (armedPassMu) — so Picture ‖
// armed races the hand-off against the armed pass, then runs P1's pass.
//
// RED MAP (mutations of production code, dispatch D10's proof):
//
//	latch allows everything (unwired in effect)    → every sequence: 2 signals
//	ledger clause returns free                     → S1r, S2r (after a restart the
//	                                                 ledgers are the only evidence)
//	queued clause free / recent clause free, alone → nothing (each covers the
//	                                                 other in-process)
//	queued AND recent free                         → S5 A→B (the AI path has no
//	                                                 ledger); the AI‖armed race
//	                                                 when the AI wins the mutex
//	no per-key send mutex                          → the AI‖armed race
//	                                                 (probabilistic per round)
//	a Picture claim in flight latches (ledger       → S5 Picture‖armed: the arm is
//	  clause ignores the send stamp)                  refused behind an unsent claim
//	reconcile_owned off                            → S3/S4 filled halves: a
//	                                                 close_position frame (the flatten)

// dupFrames reads every frame the server writes to one dialed AddOn conn.
type dupFrames struct {
	mu      sync.Mutex
	signals []ntwire.SignalPayload
	closes  int
	markers map[string]bool // account_select frames the test wrote as stream markers
	err     error           // why the reader stopped (diagnostics)
}

func (f *dupFrames) read(conn net.Conn) {
	for {
		env, err := ntwire.ReadFrame(conn)
		if err != nil {
			f.mu.Lock()
			f.err = err
			f.mu.Unlock()
			return
		}
		switch env.Type {
		case ntwire.FrameSignal:
			var p ntwire.SignalPayload
			_ = json.Unmarshal(env.Payload, &p)
			f.mu.Lock()
			f.signals = append(f.signals, p)
			f.mu.Unlock()
		case ntwire.FrameClosePosition:
			f.mu.Lock()
			f.closes++
			f.mu.Unlock()
		case ntwire.FrameAccountSelect:
			var p ntwire.AccountSelectPayload
			_ = json.Unmarshal(env.Payload, &p)
			f.mu.Lock()
			if f.markers == nil {
				f.markers = map[string]bool{}
			}
			f.markers[p.Account] = true
			f.mu.Unlock()
		}
	}
}

func (f *dupFrames) sawMarker(tag string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.markers[tag]
}

func (f *dupFrames) readErr() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *dupFrames) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.signals), f.closes
}

func (f *dupFrames) signalIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.signals))
	for _, s := range f.signals {
		out = append(out, s.SignalID)
	}
	return out
}

// dupLink is one "process": a server, its AddOn conn, the TCPTrader and the
// AutoTrader over the shared store.
type dupLink struct {
	s      *ntwire.TCPServer
	nt     *ntTrader.TCPTrader
	at     *AutoTrader
	eval   *PictureHtfEvaluator
	frames *dupFrames
	born   time.Time // wall instant the link came up (its book seed); diagnostics
}

type dupWire struct {
	t      *testing.T
	st     *store.Store
	cfg    *store.StrategyConfig
	pcfg   store.PictureHtfConfig
	armNow time.Time
	picNow time.Time
	links  []*dupLink
	net    *dupOffline

	// The CONTEXT the bar provider and the plan provider serve (W5): the arm
	// context by default (the arm's 1m tape, the plan clock at armNow — the
	// W0 fixture exactly); Picture's P1 pass switches to the Picture context.
	ctxMu   sync.Mutex
	fiveM   []market.Kline
	oneMin  []market.Kline
	planAt  time.Time
	armBars []market.Kline
}

// planClock is the plan provider's clock: the context's plan instant.
func (w *dupWire) planClock() time.Time {
	w.ctxMu.Lock()
	defer w.ctxMu.Unlock()
	return w.planAt
}

const dupTraderID = "dup-seq-trader"

var (
	dupPicNow = time.UnixMilli(t4h0 + 42*3600*1000 + 500)                  // 2026-09-14 19:00:00.5Z (the Picture tape's instant)
	dupArmNow = time.Date(2026, time.September, 15, 15, 0, 0, 0, time.UTC) // Tue 10:00 CT, after picNow
)

// errDupOffline is what every outbound HTTP request made under the fixture
// gets back, at once.
var errDupOffline = errors.New("dup fixture: outbound HTTP is stubbed offline")

// dupOffline is the RoundTripper behind every client market.NewAPIClient
// builds while a dup fixture is up. It never dials: it records the host and
// fails immediately.
type dupOffline struct {
	mu    sync.Mutex
	hosts []string
}

func (o *dupOffline) RoundTrip(r *http.Request) (*http.Response, error) {
	o.mu.Lock()
	o.hosts = append(o.hosts, r.URL.Host)
	o.mu.Unlock()
	return nil, errDupOffline
}

func (o *dupOffline) seen() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.hosts...)
}

// stubOutboundHTTP installs dupOffline through market's own client seam
// (hook.SET_HTTP_CLIENT, consulted by market.NewAPIClient on every call) for
// the test's lifetime, and restores whatever was registered before — nested
// fixtures (S5's rounds) unwind in t.Cleanup's LIFO order. It is registered
// FIRST in newDupWire so it is restored LAST, after every server and producer
// of the fixture has stopped.
func stubOutboundHTTP(t *testing.T) *dupOffline {
	t.Helper()
	o := &dupOffline{}
	prev, had := hook.Hooks[hook.SET_HTTP_CLIENT]
	prevEnabled := hook.EnableHooks
	hook.EnableHooks = true
	hook.RegisterHook(hook.SET_HTTP_CLIENT, func(args ...any) any {
		return &hook.SetHttpClientResult{Client: &http.Client{Transport: o, Timeout: time.Second}}
	})
	t.Cleanup(func() {
		if had {
			hook.Hooks[hook.SET_HTTP_CLIENT] = prev
		} else {
			delete(hook.Hooks, hook.SET_HTTP_CLIENT)
		}
		hook.EnableHooks = prevEnabled
		if hosts := o.seen(); len(hosts) > 0 {
			t.Logf("dup fixture: %d outbound HTTP request(s) answered offline by the stub: %v", len(hosts), hosts)
		}
	})
	return o
}

func newDupWire(t *testing.T) *dupWire {
	t.Helper()
	offline := stubOutboundHTTP(t) // first: restored last
	withMaintenanceDir(t)
	pcfg := store.PictureHtfConfig{Enabled: true, MinRR: 2.5}
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, PictureHtf: &pcfg}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2
	at, st := resetTrader(t, cfg)
	w := &dupWire{t: t, st: st, cfg: at.config.StrategyConfig, pcfg: pcfg, armNow: dupArmNow, picNow: dupPicNow, net: offline}

	// ONE bar provider for every producer: the Picture tape on 4h/1h/5m (the AI
	// open's market read uses 5m/1h too) and the arm's 1m tape near 100 at armNow.
	w.armBars = shadowBarsNearAt(100, w.armNow)
	w.fiveM, w.oneMin, w.planAt = pictureBars5M(true), w.armBars, w.armNow
	p4h, p1h := pictureBars4H(), pictureBarsH1()
	prevProvider := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		w.ctxMu.Lock()
		p5m, p1m := w.fiveM, w.oneMin
		w.ctxMu.Unlock()
		switch tf {
		case "4h":
			return tailOf(p4h, count)
		case "1h":
			return tailOf(p1h, count)
		case "5m":
			return tailOf(p5m, count)
		case "1m":
			return p1m
		}
		return nil
	}
	// The submit seam stays the PRODUCTION binding (the Day Plan hand-off, its
	// init binding) — Picture has no send of its own since W5.
	prevSeam, prevCap := pictureHtfSubmitSeam, pictureHtfCapabilityProven
	t.Cleanup(func() {
		market.FuturesBarsProvider = prevProvider
		pictureHtfSubmitSeam, pictureHtfCapabilityProven = prevSeam, prevCap
	})

	w.links = append(w.links, w.link(at))

	// The armed plan: S1 long @100, stop 95, target 110 (the liveArmFixture plan).
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
	shadowPlanAtTime(t, at, st, string(blob), w.armNow)
	installActivePlanProviderAt(at, st, w.planClock) // the context's clock (armNow unless a Picture pass runs)
	return w
}

// link builds one process's wire around at: a started server, a dialed AddOn
// conn that proves the Picture build on a heartbeat, the TCPTrader bound to
// Sim101, and NewAutoTrader's production wiring.
func (w *dupWire) link(at *AutoTrader) *dupLink {
	t := w.t
	t.Helper()
	at.id = dupTraderID
	at.positionFirstSeenTime = map[string]int64{}
	at.isRunningMutex.Lock()
	at.isRunning = true // Picture is admitted only while its trader runs (D26)
	at.isRunningMutex.Unlock()

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
	frames := &dupFrames{}
	go frames.read(conn)
	top := ntwire.MinAddonBuildPictureHtf
	if err := ntwire.WriteFrame(conn, ntwire.FrameHeartbeat, ntwire.HeartbeatPayload{BuildID: top}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); !ntwire.FarSideProven(s.FarSideBuildID(), top); {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the AddOn build was never proven on the wire")
		}
		time.Sleep(2 * time.Millisecond)
	}

	nt := ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	at.trader = nt
	at.config.NinjaTraderSymbol = "MNQ"
	// NewAutoTrader's TCPTrader block, verbatim in effect (both calls pinned there).
	wireNT8Maintenance(at, nt)
	wireNT8EntryLatch(at, nt)
	if !nt.EntryLatchWired() {
		t.Fatal("fixture: the entry latch must be wired as production wires it")
	}
	// The AddOn's periodic order snapshot: a flat, empty book on Sim101. Seeded
	// once: every sequence must finish inside the latch's book-age bound
	// (snapshotMaxAge) and the server's heartbeat-ack timeout (the fake AddOn
	// never acks) — both 60 s at the defaults, which is why outbound HTTP is
	// stubbed.
	born := time.Now()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, born)

	return &dupLink{s: s, nt: nt, at: at, eval: NewPictureHtfEvaluator(at, pictureTestResolved(&w.pcfg)), frames: frames, born: born}
}

// restart is a new process over the same store: a fresh AutoTrader (same id,
// same strategy), server, TCPTrader and AddOn conn. The old link stays up so a
// frame it (wrongly) wrote would still be counted.
func (w *dupWire) restart() *dupLink {
	w.t.Helper()
	at := &AutoTrader{id: dupTraderID, store: w.st, exchange: "ninjatrader"}
	at.config.StrategyConfig = w.cfg
	at.mcpClient = &fakeDecisionClient{}
	l := w.link(at)
	installActivePlanProviderAt(at, w.st, w.planClock)
	w.links = append(w.links, l)
	return l
}

func (w *dupWire) main() *dupLink { return w.links[0] }

// totals sums the frames every link's AddOn received.
func (w *dupWire) totals() (signals, closes int) {
	for _, l := range w.links {
		s, c := l.frames.counts()
		signals += s
		closes += c
	}
	return
}

var dupMarkerSeq atomic.Int64

// expectFrames pins the exact totals the far side received: `want` signal
// frames and no close_position. Every producer send is synchronous (SendSignal
// / SendClosePosition write before they return), so once the step returned,
// the test writes a MARKER frame (an account_select, which touches no server
// state) on each link and waits — bounded — until the AddOn side reads it. A
// TCP stream is ordered, so every frame written before the marker has then
// been read: no sleep-and-hope quiet window, and a loaded machine only makes
// the wait longer, never the count wrong.
func (w *dupWire) expectFrames(step string, want int) {
	w.t.Helper()
	diag := func() string {
		var links []string
		for i, l := range w.links {
			links = append(links, fmt.Sprintf("link%d age=%s connected=%v queued=%d reader_err=%v", i, time.Since(l.born).Round(time.Millisecond), l.s.IsConnected(), l.s.PendingSignalCount(), l.frames.readErr()))
		}
		return strings.Join(links, "; ") + fmt.Sprintf("; outbound HTTP stubbed: %v", w.net.seen())
	}
	for i, l := range w.links {
		tag := fmt.Sprintf("dup-marker-%d", dupMarkerSeq.Add(1))
		if err := l.s.SendAccountSelect(ntwire.AccountSelectPayload{Account: tag}); err != nil {
			w.t.Fatalf("%s: link%d marker not written: %v (%s)", step, i, err, diag())
		}
		for deadline := time.Now().Add(10 * time.Second); !l.frames.sawMarker(tag); {
			if time.Now().After(deadline) {
				w.t.Fatalf("%s: link%d marker never read by the AddOn side (%s)", step, i, diag())
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	s, c := w.totals()
	if s != want || c != 0 {
		var ids []string
		for _, l := range w.links {
			ids = append(ids, l.frames.signalIDs()...)
		}
		w.t.Fatalf("%s: the wire carried %d signal frame(s) and %d close_position frame(s), want exactly %d signal(s) and no close (signals %v; %s)", step, s, c, want, ids, diag())
	}
}

// armPass drives the armed producer once on its pinned clock.
func (w *dupWire) armPass(l *dupLink) { l.at.maybeManageArmedOrdersAt(nil, w.armNow) }

// picture drives Picture once at its tape's instant, with its 5m frame fresh.
func (w *dupWire) picture(l *dupLink) EvaluateResult {
	l.eval.mu.Lock()
	l.eval.markFresh5mReceivedAt(w.picNow) // the 5m frame of the interval was received at picNow
	l.eval.mu.Unlock()
	return l.eval.Evaluate("MNQ", w.picNow)
}

// aiOpen drives the AI decision's send half for action.
func (w *dupWire) aiOpen(l *dupLink, action string) (*store.DecisionAction, error) {
	d := &kernel.Decision{Action: action, Symbol: "MNQ", Leverage: 1, Confidence: 70}
	if action == "open_long" {
		d.StopLoss, d.TakeProfit = 99, 106
	} else {
		d.StopLoss, d.TakeProfit = 104, 97
	}
	rec := &store.DecisionAction{Action: action, Symbol: "MNQ"}
	var err error
	if action == "open_long" {
		err = l.at.executeOpenLongWithRecord(d, rec)
	} else {
		err = l.at.executeOpenShortWithRecord(d, rec)
	}
	return rec, err
}

// armRow is the plan's one armed row.
func (w *dupWire) armRow() store.ArmedOrderDB {
	w.t.Helper()
	rows, err := w.st.ArmedOrders().ListNonTerminal(dupTraderID)
	if err != nil {
		w.t.Fatal(err)
	}
	if len(rows) != 1 {
		w.t.Fatalf("fixture: want exactly one non-terminal armed row, got %+v", rows)
	}
	return rows[0]
}

// placeArm runs the armed pass and pins that it put the arm on the wire.
func (w *dupWire) placeArm(l *dupLink) store.ArmedOrderDB {
	w.t.Helper()
	w.armPass(l)
	w.expectFrames("the armed placement", 1)
	r := w.armRow()
	if r.SignalID == "" || r.State != store.StatePlacePending {
		w.t.Fatalf("fixture: the armed pass must have stamped its row place_pending with the broker signal: %+v", r)
	}
	if ids := l.frames.signalIDs(); len(ids) != 1 || ids[0] != r.SignalID {
		w.t.Fatalf("fixture: the wire's signal %v must be the armed row's %s", ids, r.SignalID)
	}
	return r
}

// ── the Picture producer since W5: hand-off → the armed pass ───────────────

// picPassNow is P1's pass instant: 1 s into Picture's 10 s window.
func (w *dupWire) picPassNow() time.Time { return w.picNow.Add(time.Second) }

// newDupPictureWire is newDupWire with Picture's 5m ladder on the tick grid —
// a hand-off judges its zone after inward rounding to the tick, so an
// off-grid close is an empty zone (pictureOnGrid5M). The arm context is
// untouched.
func newDupPictureWire(t *testing.T) *dupWire {
	t.Helper()
	w := newDupWire(t)
	w.ctxMu.Lock()
	w.fiveM = pictureOnGrid5M()
	w.ctxMu.Unlock()
	return w
}

// startRun marks a link's run epoch, as the production Run does: a Picture
// scenario is recorded under — and placed only by — the live run. Each link
// (process) gets its own epoch.
func (w *dupWire) startRun(l *dupLink) {
	w.t.Helper()
	l.at.markPictureRunEpoch(w.picNow.Add(time.Duration(len(w.links)) * time.Nanosecond))
	at := l.at
	w.t.Cleanup(at.clearPictureRunEpoch)
}

// recordPicture drives Picture once (Evaluate at picNow → claim → the
// production hand-off) and pins that it RECORDED its opportunity and sent
// nothing: the evaluator reports the seam's word and the row settles planned.
func (w *dupWire) recordPicture(l *dupLink) EvaluateResult {
	w.t.Helper()
	res := w.picture(l)
	if res.Stage != store.PictureStagePlanned || res.Reason != "" || res.OppKey == "" {
		w.t.Fatalf("fixture: Picture must hand its opportunity to the Day Plan: %+v", res)
	}
	if row, ok, err := w.st.PictureHtfGet(res.OppKey); err != nil || !ok || row.Stage != store.PictureStagePlanned || store.PictureSendStarted(*row) {
		w.t.Fatalf("fixture: the Picture row must settle planned, never a send: %+v ok=%v err=%v", row, ok, err)
	}
	return res
}

// picturePass runs the armed pass that places Picture's P1: on the Picture
// context (the plan clock and a 1m tape at picPassNow whose newest close,
// 101.50, is P1's one-price zone), then restores the arm context.
func (w *dupWire) picturePass(l *dupLink) {
	w.t.Helper()
	w.ctxMu.Lock()
	w.oneMin, w.planAt = zoneTape(101.5, w.picNow, 0), w.picPassNow()
	w.ctxMu.Unlock()
	defer func() {
		w.ctxMu.Lock()
		w.oneMin, w.planAt = w.armBars, w.armNow
		w.ctxMu.Unlock()
	}()
	l.at.maybeManageArmedOrdersAt(nil, w.picPassNow())
}

// placePicture records Picture's opportunity and places P1, and pins that its
// ONE frame is P1's LIMIT under the P1 row's signal.
func (w *dupWire) placePicture(l *dupLink) store.ArmedOrderDB {
	w.t.Helper()
	res := w.recordPicture(l)
	w.expectFrames("the Picture hand-off (records, never sends)", 0)
	w.picturePass(l)
	w.expectFrames("P1's placement", 1)
	p1 := w.rowFor("P1")
	if p1.Source != store.ArmSourcePicture || p1.SourceRef != res.OppKey || p1.SignalID == "" || p1.State != store.StatePlacePending {
		w.t.Fatalf("fixture: P1's row must be the Picture scenario's, placed under its signal: %+v", p1)
	}
	ids := l.frames.signalIDs()
	if len(ids) != 1 || ids[0] != p1.SignalID {
		w.t.Fatalf("fixture: the wire's signal %v must be P1's %s", ids, p1.SignalID)
	}
	l.frames.mu.Lock()
	sig := l.frames.signals[0]
	l.frames.mu.Unlock()
	if sig.OrderType != "limit" || sig.LimitPrice != 101.5 {
		w.t.Fatalf("fixture: P1 is a LIMIT at 101.50 (Picture has no market entry): %+v", sig)
	}
	return p1
}

// rowFor is the newest non-terminal armed row of scenario (S1 or P1) — the
// two live in two plans, so armRow's "exactly one row" does not apply.
func (w *dupWire) rowFor(scenario string) store.ArmedOrderDB {
	w.t.Helper()
	rows, err := w.st.ArmedOrders().ListNonTerminal(dupTraderID)
	if err != nil {
		w.t.Fatal(err)
	}
	var out store.ArmedOrderDB
	found := false
	for _, r := range rows {
		if r.Scenario == scenario && (!found || r.ID > out.ID) {
			out, found = r, true
		}
	}
	if !found {
		w.t.Fatalf("fixture: no non-terminal armed row for %s: %+v", scenario, rows)
	}
	return out
}

// latched reports a refusal by the entry latch in err or a result reason.
func latched(s string) bool { return strings.Contains(s, "one_entry_latch") }

// latchRefusals is the entry latch's refusal count. In these fixtures the
// TCPTrader has no trader id (StartCloseSync never ran), so the latch counts
// under the process-wide "" bucket; the package's tests run serially.
func latchRefusals(reason string) int {
	if reason != "" {
		return gateBlocks("", "one_entry_latch:"+reason)
	}
	n := 0
	for _, r := range []string{"book_unverifiable", "working_entry_or_position", "ledger_unreadable", "ledger_open", "queued_entry", "recent_send"} {
		n += gateBlocks("", "one_entry_latch:"+r)
	}
	return n
}

// fillAtNT8 is the AddOn reporting the entry FILLED: NT8's positions frame for
// Sim101 carries a 1-lot long on MNQ, and trader_positions has nothing yet (the
// reconciler's untracked grace).
func fillAtNT8(l *dupLink) {
	l.s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 100}})
}

// ── S1 B→C ──────────────────────────────────────────────────────────────────

// S1: an armed entry is working at NT8 (placed by the armed producer) →
// Picture evaluates and RECORDS its opportunity as P1 (it sends nothing) →
// P1's armed pass is REFUSED by the entry latch (S1's placed row / queued /
// recent send): P1's row stays armed and unstamped — refused, never
// cancelled. One signal on the wire.
func TestDupS1ArmedWorkingThenPictureIsRefused(t *testing.T) {
	w := newDupPictureWire(t)
	l := w.main()
	w.startRun(l)
	w.placeArm(l)

	w.recordPicture(l)
	w.expectFrames("S1 the Picture hand-off", 1)
	before := latchRefusals("")
	w.picturePass(l)
	w.expectFrames("S1 B→C", 1)
	if p1 := w.rowFor("P1"); p1.State != store.StateArmed || p1.SignalID != "" || p1.Source != store.ArmSourcePicture {
		t.Fatalf("S1: P1 must be refused at its send point and kept armed, unstamped: %+v", p1)
	}
	if latchRefusals("") <= before {
		t.Fatal("S1: P1's placement must have been refused by the entry latch (counted)")
	}
}

// S1r: S1 across a process restart. The new process has no queued entry and no
// recent send in memory; the armed ledger's placed S1 row is the evidence.
func TestDupS1rArmedWorkingThenPictureAfterRestartIsRefused(t *testing.T) {
	w := newDupPictureWire(t)
	w.placeArm(w.main())

	l2 := w.restart()
	w.startRun(l2)
	w.recordPicture(l2)
	before := latchRefusals("ledger_open")
	w.picturePass(l2)
	w.expectFrames("S1r B→C across a restart", 1)
	if p1 := w.rowFor("P1"); p1.State != store.StateArmed || p1.SignalID != "" {
		t.Fatalf("S1r: P1 must be refused and kept armed, unstamped: %+v", p1)
	}
	if latchRefusals("ledger_open") <= before {
		t.Fatal("S1r: after a restart the armed ledger row must refuse P1 (ledger_open), counted")
	}
}

// ── S2 C→B ──────────────────────────────────────────────────────────────────

// S2: Picture's P1 was placed (its armed row stamped) → the armed pass for S1
// runs → S1 is REFUSED at the latch and stays armed. One signal.
func TestDupS2PictureSentThenArmedIsRefused(t *testing.T) {
	w := newDupPictureWire(t)
	l := w.main()
	w.startRun(l)
	w.placePicture(l)

	before := latchRefusals("")
	w.armPass(l)
	w.expectFrames("S2 C→B", 1)
	if r := w.rowFor("S1"); r.State != store.StateArmed || r.SignalID != "" {
		t.Fatalf("S2: the refused arm must stay armed and unstamped: %+v", r)
	}
	if latchRefusals("") <= before {
		t.Fatal("S2: the arm must have been refused by the entry latch (counted)")
	}
}

// S2r: S2 across a process restart: P1's placed armed row is the evidence.
func TestDupS2rPictureSentThenArmedAfterRestartIsRefused(t *testing.T) {
	w := newDupPictureWire(t)
	l := w.main()
	w.startRun(l)
	w.placePicture(l)

	l2 := w.restart()
	before := latchRefusals("ledger_open")
	w.armPass(l2)
	w.expectFrames("S2r C→B across a restart", 1)
	if r := w.rowFor("S1"); r.State != store.StateArmed || r.SignalID != "" {
		t.Fatalf("S2r: the refused arm must stay armed and unstamped: %+v", r)
	}
	if latchRefusals("ledger_open") <= before {
		t.Fatal("S2r: the refusal must be the latch's ledger_open (P1's placed row), counted")
	}
}

// ── S3 B→A ──────────────────────────────────────────────────────────────────

// S3: an armed entry is working (resting) → an AI open → REFUSED by the latch.
// Then NT8 reports the arm FILLED (not yet in trader_positions) → an AI open
// (a flip) → REFUSED by reconcile_owned, and the arm's position is NEVER
// flattened. One signal, no close_position, throughout.
func TestDupS3ArmedWorkingThenAIOpenIsRefusedNeverFlattened(t *testing.T) {
	w := newDupWire(t)
	l := w.main()
	arm := w.placeArm(l)

	_, err := w.aiOpen(l, "open_long")
	w.expectFrames("S3 B→A (resting arm)", 1)
	if err == nil || !ntTrader.IsEntryLatched(err) {
		t.Fatalf("S3 (resting): the AI open must be refused by the entry latch: %v", err)
	}

	fillAtNT8(l)
	before := gateBlocks(dupTraderID, "reconcile_owned")
	rec, err := w.aiOpen(l, "open_short")
	w.expectFrames("S3 B→A (filled arm)", 1)
	if err != nil || rec.Success || !strings.HasPrefix(rec.Error, "reconcile_owned: ") || !strings.Contains(rec.Error, "armed #"+latchItoa(arm.ID)) {
		t.Fatalf("S3 (filled): the AI open must be refused as reconcile_owned, naming armed #%d: err=%v rec=%+v", arm.ID, err, rec)
	}
	if gateBlocks(dupTraderID, "reconcile_owned") != before+1 {
		t.Fatal("S3 (filled): the refusal must be counted as reconcile_owned")
	}
}

// ── S4 C→A ──────────────────────────────────────────────────────────────────

// S4: Picture's P1 is working → an AI open → REFUSED by the latch; then NT8
// reports it FILLED → an AI open (a flip) → REFUSED by reconcile_owned, naming
// P1's armed row, never flattened. One signal, no close_position.
func TestDupS4PictureWorkingThenAIOpenIsRefusedNeverFlattened(t *testing.T) {
	w := newDupPictureWire(t)
	l := w.main()
	w.startRun(l)
	p1 := w.placePicture(l)

	_, err := w.aiOpen(l, "open_long")
	w.expectFrames("S4 C→A (resting P1)", 1)
	if err == nil || !ntTrader.IsEntryLatched(err) {
		t.Fatalf("S4 (resting): the AI open must be refused by the entry latch: %v", err)
	}

	fillAtNT8(l)
	before := gateBlocks(dupTraderID, "reconcile_owned")
	rec, err := w.aiOpen(l, "open_short")
	w.expectFrames("S4 C→A (filled P1)", 1)
	if err != nil || rec.Success || !strings.HasPrefix(rec.Error, "reconcile_owned: ") || !strings.Contains(rec.Error, "armed #"+latchItoa(p1.ID)+" P1 ") {
		t.Fatalf("S4 (filled): the AI open must be refused as reconcile_owned, naming P1's armed #%d: err=%v rec=%+v", p1.ID, err, rec)
	}
	if gateBlocks(dupTraderID, "reconcile_owned") != before+1 {
		t.Fatal("S4 (filled): the refusal must be counted as reconcile_owned")
	}
}

// ── S5 A→B ──────────────────────────────────────────────────────────────────

// S5: an AI entry was just sent (queued at the broker, not filled; the AI path
// has NO ledger) → the armed pass → REFUSED at the latch; the arm stays armed.
func TestDupS5AISentThenArmedIsRefused(t *testing.T) {
	w := newDupWire(t)
	l := w.main()
	rec, err := w.aiOpen(l, "open_long")
	if err != nil || rec.Error != "" {
		t.Fatalf("fixture: the AI open must send: err=%v rec=%+v", err, rec)
	}
	w.expectFrames("the AI send", 1)

	before := latchRefusals("")
	w.armPass(l)
	w.expectFrames("S5 A→B", 1)
	if r := w.armRow(); r.State != store.StateArmed || r.SignalID != "" {
		t.Fatalf("S5: the refused arm must stay armed and unstamped: %+v", r)
	}
	if latchRefusals("") <= before {
		t.Fatal("S5: the arm must have been refused by the entry latch (counted)")
	}
}

// raceAtPermit replaces the broker's entry permit with one that holds each
// caller until BOTH producers have reached it, then releases them together —
// so the two entries arrive at the latch at the same instant. The production
// permit (the maintenance hold) still decides after the barrier.
func raceAtPermit(t *testing.T, nt *ntTrader.TCPTrader) *atomic.Int32 {
	t.Helper()
	var arrived atomic.Int32
	release := make(chan struct{})
	nt.SetEntryPermit(func() (func(), bool) {
		if arrived.Add(1) == 2 {
			close(release)
		}
		select {
		case <-release:
		case <-time.After(20 * time.Second): // bound: a producer that never arrives cannot hang the test
		}
		return MaintenanceEntryPermit()
	})
	return &arrived
}

// S5 (race): the AI open and the armed placement pass run on two goroutines
// and meet at the broker at the same instant → exactly ONE entry reaches the
// wire; the other is refused by the latch.
func TestDupS5AIAndArmedRaceYieldOneEntry(t *testing.T) {
	w := newDupWire(t)
	l := w.main()
	arrived := raceAtPermit(t, l.nt)
	before := latchRefusals("")

	var wg sync.WaitGroup
	var aiErr error
	var aiRec *store.DecisionAction
	wg.Add(2)
	go func() { defer wg.Done(); aiRec, aiErr = w.aiOpen(l, "open_long") }()
	go func() { defer wg.Done(); w.armPass(l) }()
	waitGroupBounded(t, &wg, 30*time.Second)

	if n := arrived.Load(); n != 2 {
		t.Fatalf("fixture: both producers must reach the broker's entry permit (the race is at the latch), got %d", n)
	}
	w.expectFrames("S5 AI ‖ armed", 1)
	aiSent := aiErr == nil && aiRec.Error == ""
	armSent := w.armRow().SignalID != ""
	if aiSent == armSent {
		t.Fatalf("S5: exactly one producer may send (ai sent=%v err=%v, arm sent=%v)", aiSent, aiErr, armSent)
	}
	if !aiSent && !ntTrader.IsEntryLatched(aiErr) {
		t.Fatalf("S5: the losing AI open must be refused by the entry latch: %v", aiErr)
	}
	if latchRefusals("") <= before {
		t.Fatal("S5: the losing producer must have been refused by the entry latch (counted)")
	}
}

// S5 (race): Picture on the live-bar goroutine and the armed placement pass on
// the cycle goroutine, released at the same instant. Since W5 Picture's half
// is its HAND-OFF: the claim (place_pending, unstamped) and the record race
// the armed pass's latch read of the Picture ledger — an unsent claim must
// NEVER latch the arm — and then P1's own pass runs (the poke's pass; armed
// passes are one at a time, armedPassMu). Exactly ONE entry: S1's; P1 is
// recorded and refused at its send point, kept armed. Run several rounds (the
// claim-in-flight window lands at a different point of the arm's pass each
// time). The Picture row's stage is not asserted in the free rounds: on the
// fixture's clocks the arm pass runs a day after P1's window, so its
// interrupted-hand-off sweep may settle an in-flight claim before the hand-off
// does (the sweep's own test pins that). The last round is DETERMINISTIC: the
// claim is in flight exactly when the arm reads the latch.
func TestDupS5PictureAndArmedRaceYieldOneEntry(t *testing.T) {
	for i := 0; i < 6; i++ {
		w := newDupPictureWire(t)
		l := w.main()
		w.startRun(l)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var res EvaluateResult
		wg.Add(2)
		go func() { defer wg.Done(); <-start; res = w.picture(l) }()
		go func() { defer wg.Done(); <-start; w.armPass(l) }()
		close(start)
		waitGroupBounded(t, &wg, 30*time.Second)
		if res.Stage != store.PictureStagePlanned {
			t.Fatalf("round %d fixture: Picture must hand off its opportunity while the arm pass runs: %+v", i, res)
		}
		w.expectFrames("S5 Picture ‖ armed (hand-off ‖ pass)", 1)
		s1 := w.rowFor("S1")
		if s1.SignalID == "" || s1.State != store.StatePlacePending {
			t.Fatalf("round %d S5: an unsent Picture claim must never latch the armed entry — S1 must place: %+v", i, s1)
		}
		before := latchRefusals("")
		w.picturePass(l)
		w.expectFrames("S5 P1's pass", 1)
		if p1 := w.rowFor("P1"); p1.State != store.StateArmed || p1.SignalID != "" {
			t.Fatalf("round %d S5: P1 must be refused at its send point and kept armed: %+v", i, p1)
		}
		if latchRefusals("") <= before {
			t.Fatalf("round %d S5: P1's refusal must be the entry latch's (counted)", i)
		}
	}

	// The deterministic round: the armed pass reaches the broker's entry
	// permit (after its pass head and the interrupted-hand-off sweep), Picture
	// claims on its own goroutine, and only once the claim is IN FLIGHT
	// (place_pending, unstamped, the hand-off not yet run) does the arm go on
	// to the latch. The seam is the PRODUCTION hand-off, held until the arm
	// pass returns — it records and sends nothing before that.
	t.Run("claim in flight at the arm's latch", func(t *testing.T) {
		w := newDupPictureWire(t)
		l := w.main()
		w.startRun(l)
		prod := pictureHtfSubmitSeam
		claimed, release, picDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var claimOnce, permitOnce sync.Once
		pictureHtfSubmitSeam = func(e *PictureHtfEvaluator, row *store.PictureHtfOpportunityDB, stopPx, targetPx, qty float64, now time.Time) error {
			claimOnce.Do(func() { close(claimed) })
			<-release
			return prod(e, row, stopPx, targetPx, qty, now)
		}
		t.Cleanup(func() { pictureHtfSubmitSeam = prod })
		var res EvaluateResult
		var inFlight []store.PictureHtfOpportunityDB
		l.nt.SetEntryPermit(func() (func(), bool) {
			permitOnce.Do(func() {
				go func() { defer close(picDone); res = w.picture(l) }()
				select {
				case <-claimed:
					inFlight, _ = w.st.PictureHtfPendingByTrader(dupTraderID)
				case <-time.After(20 * time.Second):
				}
			})
			return MaintenanceEntryPermit()
		})
		w.armPass(l)
		close(release)
		select {
		case <-picDone:
		case <-time.After(30 * time.Second):
			t.Fatal("Picture never returned after the hand-off was released")
		}
		if len(inFlight) != 1 || inFlight[0].Stage != store.StatePlacePending || inFlight[0].SubmittedAt != 0 {
			t.Fatalf("fixture: exactly one unsent claim must be in flight at the arm's latch, got %+v", inFlight)
		}
		w.expectFrames("S5 claim in flight ‖ the arm's latch", 1)
		if s1 := w.rowFor("S1"); s1.SignalID == "" || s1.State != store.StatePlacePending {
			t.Fatalf("S5: an unsent Picture claim must never latch the armed entry — S1 must place: %+v", s1)
		}
		if res.Stage != store.PictureStagePlanned {
			t.Fatalf("S5: the held hand-off must still record the opportunity: %+v", res)
		}
		if row, _, _ := w.st.PictureHtfGet(res.OppKey); row == nil || row.Stage != store.PictureStagePlanned {
			t.Fatalf("S5: the claim settles planned once its hand-off runs: %+v", row)
		}
		w.picturePass(l)
		w.expectFrames("S5 P1's pass", 1)
		if p1 := w.rowFor("P1"); p1.State != store.StateArmed || p1.SignalID != "" {
			t.Fatalf("S5: P1 must be refused at its send point and kept armed: %+v", p1)
		}
	})
}

func waitGroupBounded(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("the racing producers did not return within %s", d)
	}
}
