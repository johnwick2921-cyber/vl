package trader

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	ntTrader "nofx/trader/ninjatrader"
)

// ── W-EXEC-TRUTH W5 (Builder B) — a Picture scenario on the SHARED executor ─
//
// Every test drives a production call site: maybeManageArmedOrdersAt (the scan
// pass), AutoTrader.Stop, onArmedOrderUpdate, the entry latch's ledger source.
// The rig is W3's zone rig (a REAL TCP server + AddOn conn read frame by frame,
// a real store, the plan provider on a fixed clock over a whole-day TEST
// session), built here WITHOUT its plan: a Picture scenario carries the run
// epoch it was recorded under, so the run is started (the epoch marked) first
// and the plan written after — exactly the production order (a hand-off reads
// the live epoch). The machine scenario is written the way Builder A's hand-off
// records it (D3/D4/D5): source picture, P<n>, condition acceptance, no
// confirm{}, arm{market_in_zone}, economics.entry_zone, the machine record with
// its frozen PictureEvidence.

// picGeo is one Picture scenario's geometry (long unless dir says short).
type picGeo struct {
	lo, hi, entry, stop, target float64
	bodyTop, bodyBot            float64
	dir                         string
	window                      time.Duration // eligibility window from now−1s
	rrFloor                     float64
}

var picDefault = picGeo{lo: 100, hi: 100.5, entry: 100, stop: 97, target: 110, bodyTop: 99, bodyBot: 97.5, dir: "long", window: 30 * time.Minute, rrFloor: 2}

// picScenario is the machine scenario a Picture hand-off records.
func picScenario(id, ref string, now time.Time, epoch int64, g picGeo) kernel.PlanScenario {
	if g.dir == "" {
		g.dir = "long"
	}
	from, until := now.Add(-time.Second).UnixMilli(), now.Add(g.window).UnixMilli()
	ev := PictureEvidence{
		OppKey: ref, ClaimID: "picture-htf-" + ref, Direction: g.dir,
		Rule: kernel.MachineRulePictureH1CloseBreak, RuleVer: 1,
		LevelRole: "resistance", BodyTop: g.bodyTop, BodyBot: g.bodyBot, WickHi: g.bodyTop + 0.5, WickLo: g.bodyBot - 0.5,
		H1PrevClose: g.bodyTop - 0.5, H1NewClose: g.entry, H1Boundary: g.bodyTop,
		H1OpenMs: now.Add(-70 * time.Minute).UnixMilli(), H1CloseMs: now.Add(-10 * time.Minute).UnixMilli(),
		EntryRef: g.entry, LatestClose: g.hi, Stop: g.stop, Target: g.target, ATR5m: 1.2,
		StopSource: "5m swing", TargetZone: "4H supply", RREstimate: 3, RRFloor: g.rrFloor,
		WindowOpenMs: from, WindowCloseMs: until, EvalAtMs: now.UnixMilli(),
	}
	blob, _ := json.Marshal(ev)
	return kernel.PlanScenario{
		ID: id, Source: kernel.ScenarioSourcePicture, Condition: "acceptance", Direction: g.dir, Quality: "B",
		Trigger: "H1 close beyond the 4H body (h1_close_break v1)", Invalid: "back inside the body or window closed",
		TargetChain: []float64{g.target},
		Arm:         &kernel.PlanArmSpec{Enabled: true, Entry: g.entry, Stop: g.stop, Target: g.target, Policy: kernel.EntryPolicyMarketInZone},
		Economics:   &kernel.ScenarioEconomics{Version: 1, EntryZone: []float64{g.lo, g.hi}},
		Machine: &kernel.PlanMachineSource{Rule: kernel.MachineRulePictureH1CloseBreak, RuleVer: 1, Ref: ref,
			EligibleFromMs: from, EligibleUntilMs: until, RunEpoch: epoch, Evidence: blob},
	}
}

// plannerZone is W3's zone scenario (a planner reject long) moved to [lo,hi]
// with its authored entry and touch confirm at entry.
func plannerZone(id string, lo, hi, entry float64) kernel.PlanScenario {
	sc := zoneScenario(id, kernel.EntryPolicyMarketInZone, []float64{lo, hi}, false)
	sc.Arm.Entry = entry
	sc.Confirm.RefPrice = entry
	return sc
}

// newPicRig is newZoneRig with the run started and no plan yet (see above).
// tune edits the strategy before the trader is built.
func newPicRig(t *testing.T, id string, tune func(*store.StrategyConfig)) (*zoneRig, int64) {
	t.Helper()
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC) // Thu 10:00 CT
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
	waitAddonRegistered(t, s)
	t.Cleanup(func() { _ = conn.Close() })
	ev := make(chan zoneFrame, 64)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go readZoneFrames(conn, ev, done)
	st, err := store.New(filepath.Join(t.TempDir(), "pic.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at := &AutoTrader{id: id, exchange: "ninjatrader", store: st, trader: ntTrader.NewTCPTrader(s, "MNQ", "Sim101")}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	at.isRunning = true // a Picture row is admitted only while its trader runs (D21)
	t.Cleanup(func() { kernel.SetTraderPlanProviders(id, kernel.TraderPlanProviders{}) })
	epoch := at.markPictureRunEpoch(now)
	t.Cleanup(at.clearPictureRunEpoch)
	r := &zoneRig{t: t, at: at, st: st, srv: s, conn: conn, ev: ev, now: now}
	r.setTape(zoneTape(100.25, now, 0))
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]market.Kline(nil), r.bars...)
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	return r, epoch
}

// readZoneFrames is newZoneRig's AddOn-side reader.
func readZoneFrames(conn net.Conn, ev chan zoneFrame, done chan struct{}) {
	for {
		env, err := ntwire.ReadFrame(conn)
		if err != nil {
			return
		}
		var f zoneFrame
		switch env.Type {
		case ntwire.FrameSignal:
			var p ntwire.SignalPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			f.sig = &p
		case ntwire.FrameCancelOrder:
			var p ntwire.CancelOrderPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			f.cancel = &p
		case ntwire.FrameBarsHistoryRequest:
			var p ntwire.BarsHistoryRequestPayload
			if json.Unmarshal(env.Payload, &p) != nil || p.Symbol != zoneSentinel {
				continue
			}
			f.sentinel = p.RequestID
		default:
			continue
		}
		select {
		case ev <- f:
		case <-done:
			return
		}
	}
}

// picPlan writes the plan (a new version on each call) and installs the
// provider on the rig's clock.
func picPlan(r *zoneRig, scs ...kernel.PlanScenario) {
	r.t.Helper()
	blob, _ := json.Marshal(zoneDoc(scs...))
	r.pid = shadowPlanAtTime(r.t, r.at, r.st, string(blob), r.now)
}

// pass runs the scan pass at now+d with the tape's last close at last.
func picPass(r *zoneRig, d time.Duration, last float64) {
	at := r.now.Add(d)
	r.setTape(zoneTape(last, at, 0))
	r.at.maybeManageArmedOrdersAt(nil, at)
}

// limitOnly is D22 at the wire: every entry frame the Picture path wrote is a
// LIMIT (a market frame's order_type is "" or "market").
func limitOnly(t *testing.T, sigs []ntwire.SignalPayload) {
	t.Helper()
	for _, s := range sigs {
		if s.OrderType != "limit" || s.LimitPrice <= 0 || s.Quantity != 1 {
			t.Fatalf("D22: every entry on the Picture path is a 1-lot LIMIT frame, got order_type=%q limit=%.2f qty=%d (%+v)", s.OrderType, s.LimitPrice, s.Quantity, s)
		}
	}
}

// workingBook is the AddOn's snapshot at `at` with one working buy limit.
func workingBook(r *zoneRig, at time.Time, sid string, px float64) {
	r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "o-" + sid, Name: sid, Symbol: "MNQ", Action: "buy", Type: "limit", LimitPrice: px, Quantity: 1, State: "Working"},
	}}, at)
}

func picSysCount(t *testing.T, st *store.Store, event string) int {
	t.Helper()
	n, err := store.SystemCounter(st, pictureClassSharedPrefix+event)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ── the fixture is the shape the machine writes ────────────────────────────

func TestPictureScenarioFixtureIsAValidMachineScenario(t *testing.T) {
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC)
	sc := picScenario("P1", "opp-valid", now, 1, picDefault)
	if err := kernel.ValidateMachineScenario(zoneDoc(), sc); err != nil {
		t.Fatalf("the fixture must be a machine scenario the plan validator accepts: %v", err)
	}
	// The no-plan door's machine plan v1 (D1 b): reasoning, a neutral bias,
	// the death condition, no levels, the one machine scenario.
	doc := kernel.PlanDoc{Reasoning: "MACHINE-AUTHORED (Picture HTF rule v1) — fixture",
		Bias: kernel.PlanBias{Direction: "neutral"}, DeathCondition: "machine plan: ends when an AI plan is written",
		Levels: []kernel.PlanLevel{}, Scenarios: []kernel.PlanScenario{sc}, NoTrade: []string{}}
	if err := kernel.ValidatePlanDocWithCaps(&doc, kernel.PlanHardMaxLevels, kernel.PlanHardMaxScenarios); err != nil {
		t.Fatalf("a machine plan carrying the scenario must validate: %v", err)
	}
}

// ── placement ───────────────────────────────────────────────────────────────

// A P-scenario → ONE policy row (Kind limit, Policy market_in_zone, Source
// picture, SourceRef, SourceRule, deadline, run epoch, qty 1, the zone from
// economics) placed inside the window: one LIMIT frame at the FAR edge, the
// authored stop (never widened) and target (never substituted).
func TestPictureScenarioPlacesOneMarketInZoneLimit(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-place", nil)
	sc := picScenario("P1", "opp-place", r.now, epoch, picDefault)
	picPlan(r, sc)
	picPass(r, 0, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("want exactly one frame, got %d: %+v", len(sigs), sigs)
	}
	limitOnly(t, sigs)
	s := sigs[0]
	if s.LimitPrice != 100.5 || !strings.EqualFold(s.Side, "long") || s.StopLoss != 97 || s.TakeProfit != 110 {
		t.Fatalf("the limit sits at the FAR edge 100.50 with the authored bracket 97/110: %+v", s)
	}
	row := r.row("P1")
	if row.Kind != "limit" || row.Policy != kernel.EntryPolicyMarketInZone || row.Condition != "acceptance" {
		t.Fatalf("row kind/policy/condition: %+v", row)
	}
	if row.Source != store.ArmSourcePicture || row.SourceRef != "opp-place" || row.SourceRule != kernel.MachineRulePictureH1CloseBreak {
		t.Fatalf("row source stamp: source=%q ref=%q rule=%q", row.Source, row.SourceRef, row.SourceRule)
	}
	if row.EligibleUntilMs == nil || *row.EligibleUntilMs != sc.Machine.EligibleUntilMs || row.SourceRunEpoch == nil || *row.SourceRunEpoch != epoch {
		t.Fatalf("row deadline/epoch: until=%v epoch=%v (want %d / %d)", row.EligibleUntilMs, row.SourceRunEpoch, sc.Machine.EligibleUntilMs, epoch)
	}
	if row.ZoneLo == nil || *row.ZoneLo != 100 || *row.ZoneHi != 100.5 || row.EntryPx != 100.5 || row.StopPx != 97 || row.TargetPx != 110 {
		t.Fatalf("row zone/prices: %+v", row)
	}
	if row.State != store.StatePlacePending || row.SignalID != s.SignalID {
		t.Fatalf("the row is place_pending under the sent signal: %+v", row)
	}
}

// D10 (CTO Q18) — a Picture stop is NEVER widened: a stop that fails the
// min-SL floor at the NEAR edge refuses (no row, no frame, counted), where the
// SAME geometry authored by the planner is widened by composeArmStop and
// places (the control).
func TestPictureScenarioStopNeverWidened(t *testing.T) {
	tight := picDefault
	tight.stop = 98.3 // 100.00 − 98.30 = 1.70 < 1.5 × ATR5m at the near edge
	r, epoch := newPicRig(t, "w5b-nowiden", nil)
	picPlan(r, picScenario("P1", "opp-nowiden", r.now, epoch, tight))
	picPass(r, 0, 100.25)
	picPass(r, time.Second, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a Picture stop that fails min-SL must refuse, never widen: %+v", sigs)
	}
	if rows := r.rows(); len(rows) != 0 {
		t.Fatalf("a refused Picture leg authors no row: %+v", rows)
	}
	if n := r.armRefusals(pictureClassSharedPrefix + "min_sl"); n != 1 {
		t.Fatalf("the refusal is counted once as picture_scenario:min_sl, got %d", n)
	}
	// The control: the planner's identical leg is widened by composeArmStop.
	c, _ := newPicRig(t, "w5b-nowiden-control", nil)
	planner := plannerZone("S1", 100, 100.5, 100)
	planner.Condition = "acceptance"
	planner.Confirm = nil
	planner.Arm.Stop = 98.3
	picPlan(c, planner)
	picPass(c, 0, 100.25)
	sigs, _ := c.drain()
	if len(sigs) != 1 || sigs[0].StopLoss >= 98.3 {
		t.Fatalf("control: the planner's identical leg is widened by composeArmStop and places: %+v", sigs)
	}
}

// D11 — R:R at the FAR edge must clear the evidence's floor even when the arm
// chain's own floor passes.
func TestPictureScenarioFarEdgeRRFloor(t *testing.T) {
	g := picDefault
	g.rrFloor = 5 // far 100.50, stop 97, target 110 → 2.71 < 5 (the arm floor 2 passes)
	r, epoch := newPicRig(t, "w5b-rrfloor", nil)
	picPlan(r, picScenario("P1", "opp-rr", r.now, epoch, g))
	picPass(r, 0, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("below the Picture floor nothing is placed: %+v", sigs)
	}
	if n := r.armRefusals(pictureClassRRFloor); n != 1 {
		t.Fatalf("picture_rr_floor counted once, got %d", n)
	}
}

// ── the eligibility window (D6) ─────────────────────────────────────────────

// Never placed after the deadline: a scenario whose window closed authors no
// row (counted once), and a row authored inside the window but never placed
// is retired terminal when the window closes.
func TestPictureScenarioNeverPlacedAfterItsWindow(t *testing.T) {
	t.Run("expired before any row", func(t *testing.T) {
		g := picDefault
		g.window = -time.Millisecond // [now−1s, now−1ms]
		r, epoch := newPicRig(t, "w5b-expired", nil)
		picPlan(r, picScenario("P1", "opp-expired", r.now, epoch, g))
		picPass(r, 0, 100.25)
		picPass(r, time.Second, 100.25)
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("an expired Picture scenario is never placed: %+v", sigs)
		}
		if rows := r.rows(); len(rows) != 0 {
			t.Fatalf("an expired Picture scenario authors no row: %+v", rows)
		}
		if n := r.armRefusals(pictureClassWindowClosed); n != 1 {
			t.Fatalf("the expiry is counted once, got %d", n)
		}
	})
	t.Run("authored, then the window closes", func(t *testing.T) {
		g := picDefault
		g.window = 5 * time.Second
		r, epoch := newPicRig(t, "w5b-closes", nil)
		picPlan(r, picScenario("P1", "opp-closes", r.now, epoch, g))
		picPass(r, 0, 99.6) // short of the zone: the row waits, armed
		if row := r.row("P1"); row.State != store.StateArmed {
			t.Fatalf("fixture: the row waits armed: %+v", row)
		}
		picPass(r, 6*time.Second, 100.25) // inside the zone, but the window closed
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("never placed after the deadline: %+v", sigs)
		}
		row := r.row("P1")
		if row.State != store.StateCancelled || row.StateReason != pictureWindowClosedNeverPlaced {
			t.Fatalf("the unplaced row is terminal %q: %+v", pictureWindowClosedNeverPlaced, row)
		}
		if picSysCount(t, r.st, "window_closed") != 1 || r.armRefusals(pictureClassWindowClosed) != 1 {
			t.Fatal("the closure is counted (row event + scenario refusal)")
		}
	})
	// CTO pre-review F3: a v1 Picture row whose scenario is NOT in the active
	// v2 (the re-append refused or skipped) is refused arm_not_admitted (G1)
	// every pass — it never places under v2 — and ends terminal at its
	// deadline, retired by placeZoneRow (supersede spares machine rows).
	t.Run("a v1 row under an active v2 without its scenario", func(t *testing.T) {
		g := picDefault
		g.window = 5 * time.Second
		r, epoch := newPicRig(t, "w5b-orphan", nil)
		picPlan(r, picScenario("P1", "opp-orphan", r.now, epoch, g))
		picPass(r, 0, 99.6)
		picPlan(r)                        // v2, active, WITHOUT P1
		picPass(r, 2*time.Second, 100.25) // inside the zone AND inside the window
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("the v1 row must never place under a v2 that does not carry its scenario: %+v", sigs)
		}
		if row := r.row("P1"); row.State != store.StateArmed || !strings.HasPrefix(row.LastVerdict, "refused: arm_not_admitted") {
			t.Fatalf("inside its window the v1 row is refused arm_not_admitted (G1), still armed: %+v", row)
		}
		picPass(r, 6*time.Second, 100.25)
		if sigs, _ := r.drain(); len(sigs) != 0 {
			t.Fatalf("placeZoneRow must never place a Picture row past its deadline: %+v", sigs)
		}
		if row := r.row("P1"); row.State != store.StateCancelled || row.StateReason != pictureWindowClosedNeverPlaced {
			t.Fatalf("at the deadline the v1 row is retired at placement: %+v", row)
		}
	})
}

// CTO pre-review F3 (second half): a v2 that is NOT active (no_trade) makes
// the provider report no active plan, and the "1.4" branch cancels the v1
// Picture row at once — it carries the branch's reason, "no active plan".
func TestPictureRowOnANoTradeVersionIsCancelledAtOnce(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-notrade", nil)
	picPlan(r, picScenario("P1", "opp-notrade", r.now, epoch, picDefault))
	picPass(r, 0, 99.6)
	if row := r.row("P1"); row.State != store.StateArmed {
		t.Fatalf("fixture: v1 row armed: %+v", row)
	}
	td, _ := kernel.PlanChainTradeDate(&kernel.SessionDef{Name: "TEST", WindowStartCT: "00:00", WindowEndCT: "23:59"}, r.now)
	blob, _ := json.Marshal(zoneDoc())
	if _, err := r.st.Plan().AppendPlan(&store.PlanDB{PlanID: r.pid, TradeDate: td, Session: "TEST", StrategyID: r.at.id, Lifecycle: "no_trade", Doc: string(blob), CreatedAt: r.now}); err != nil {
		t.Fatal(err)
	}
	picPass(r, time.Second, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("nothing places under a no_trade version: %+v", sigs)
	}
	if row := r.row("P1"); row.State != store.StateCancelled || row.StateReason != "no active plan" {
		t.Fatalf("the 1.4 branch cancels the v1 row at once with reason %q: %+v", "no active plan", row)
	}
}

// A resting Picture limit past its deadline gets its cancel REQUESTED; the
// zone rest cap (30 min) never applies to it — its deadline replaces it.
func TestPictureScenarioRestingPastDeadlineCancelRequested(t *testing.T) {
	g := picDefault
	g.window = 45 * time.Minute
	r, epoch := newPicRig(t, "w5b-rest", nil)
	picPlan(r, picScenario("P1", "opp-rest", r.now, epoch, g))
	picPass(r, 0, 101.95) // beyond: rests at 100.50
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: one placement, got %d", len(sigs))
	}
	sid := sigs[0].SignalID
	at31 := r.now.Add(31 * time.Minute)
	workingBook(r, at31, sid, 100.5)
	picPass(r, 31*time.Minute, 101.95)
	if _, cancels := r.drain(); len(cancels) != 0 {
		t.Fatalf("31 min is past zone_rest_max_min but inside the Picture window — no cancel: %+v", cancels)
	}
	at46 := r.now.Add(46 * time.Minute)
	workingBook(r, at46, sid, 100.5)
	picPass(r, 46*time.Minute, 101.95)
	_, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid {
		t.Fatalf("past the deadline the resting limit's cancel is requested once: %+v", cancels)
	}
	row := r.row("P1")
	if row.State != store.StateCancelPending || row.StateReason != pictureWindowClosedCancel {
		t.Fatalf("the row is cancel_pending %q: %+v", pictureWindowClosedCancel, row)
	}
	if picSysCount(t, r.st, "deadline_cancel") != 1 {
		t.Fatal("the deadline cancel is counted")
	}
}

// ── the run epoch (D21, CTO 1790192366762) ──────────────────────────────────

// Authoring: a scenario recorded under another run (or with no run alive) is
// refused and authors no row.
func TestPictureScenarioRecordedByAPreviousRunIsRefused(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-prevrun", nil)
	picPlan(r, picScenario("P1", "opp-prevrun", r.now, epoch-1, picDefault))
	picPass(r, 0, 100.25)
	picPass(r, time.Second, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 || len(r.rows()) != 0 {
		t.Fatalf("a previous run's scenario is never authored or placed: sigs=%+v rows=%+v", sigs, r.rows())
	}
	if n := r.armRefusals(pictureClassPreviousRun); n != 1 {
		t.Fatalf("picture_previous_run counted once, got %d", n)
	}
	r.at.clearPictureRunEpoch() // no run alive at all
	picPass(r, 2*time.Second, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 || len(r.rows()) != 0 {
		t.Fatal("with no live run epoch nothing is authored or placed")
	}
}

// Placement (the CTO's requirement): a row stamped with an OLD epoch reaches
// placeZoneRow after a reload and places NOTHING — terminal, named, counted.
func TestPictureRowFromAPreviousRunNeverPlacedAtPlacement(t *testing.T) {
	r, e1 := newPicRig(t, "w5b-reload", nil)
	picPlan(r, picScenario("P1", "opp-reload", r.now, e1, picDefault))
	picPass(r, 0, 99.6) // short of the zone: armed under e1
	if row := r.row("P1"); row.State != store.StateArmed || row.SourceRunEpoch == nil || *row.SourceRunEpoch != e1 {
		t.Fatalf("fixture: an armed row stamped with epoch %d: %+v", e1, row)
	}
	e2 := r.at.markPictureRunEpoch(r.now.Add(time.Second)) // the reload
	picPass(r, 2*time.Second, 100.25)                      // inside the zone
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a previous run's row must place nothing: %+v", sigs)
	}
	row := r.row("P1")
	want := "recorded by a previous run (epoch " + strconv.FormatInt(e1, 10) + ", live " + strconv.FormatInt(e2, 10) + ") — not placed"
	if row.State != store.StateCancelled || row.StateReason != want {
		t.Fatalf("the row is terminal %q, got %+v", want, row)
	}
	if picSysCount(t, r.st, "previous_run") != 1 {
		t.Fatal("the placement refusal is counted")
	}
}

// D21 at the SEND POINT: a pass already in flight when Stop lands (running
// false, the epoch not yet cleared) must not place a Picture row — admitChain
// asks a source=picture arm row Picture's running question (one chain).
func TestPictureRowRefusedAtItsSendPointWhenStopped(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-sendstop", nil)
	picPlan(r, picScenario("P1", "opp-sendstop", r.now, epoch, picDefault))
	r.at.isRunningMutex.Lock()
	r.at.isRunning = false
	r.at.isRunningMutex.Unlock()
	before := gateBlocks(r.at.id, "trader_stopped")
	picPass(r, 0, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a stopped trader never places a Picture row: %+v", sigs)
	}
	if row := r.row("P1"); row.State != store.StateArmed || row.SignalID != "" || row.LastVerdict != "refused: "+pictureRefusalStopped {
		t.Fatalf("the row is refused at its send point (armed, unstamped, verdict trader_stopped): %+v", row)
	}
	if gateBlocks(r.at.id, "trader_stopped") != before+1 {
		t.Fatal("the refusal is counted (trader_stopped)")
	}
}

// ── one_setup (D8 ruling) ───────────────────────────────────────────────────

// one_setup ON: a Picture scenario still places (the planner's non-reject play
// beside it is declined), its target is never re-targeted, and the boot WARN
// says exactly why.
func TestPictureScenarioPlacesWithOneSetupOn(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-onesetup", func(c *store.StrategyConfig) { c.DayPlan.OneSetupEnabled = nil }) // nil = ON
	r.at.oneSetupFactsForTest = func(time.Time) oneSetupTestFacts { return oneSetupTestFacts{Price: 100.25, BandPts: 5} }
	planner := plannerZone("S1", 101, 101.5, 101)
	planner.Condition = "acceptance"
	planner.Confirm = nil
	p1 := picScenario("P1", "opp-onesetup", r.now, epoch, picDefault)
	obstacle := 105.0
	p1.Economics.FirstObstacle = &kernel.ScenarioObstacle{Price: &obstacle}
	picPlan(r, planner, p1)
	// Pass 1 short of the zone: the P1 row waits armed — one_setup's retire
	// pass (a declined scenario's unplaced row is cancelled) must not touch it.
	picPass(r, 0, 99.6)
	if row := r.row("P1"); row.State != store.StateArmed {
		t.Fatalf("one_setup must not retire a waiting Picture row: %+v", row)
	}
	picPass(r, time.Second, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 || sigs[0].TakeProfit != 110 {
		t.Fatalf("one_setup ON: the Picture scenario places at its own target 110 (never the obstacle): %+v", sigs)
	}
	limitOnly(t, sigs)
	for _, row := range r.rows() {
		if row.Scenario == "S1" {
			t.Fatalf("the planner's non-reject play is still declined by one_setup: %+v", row)
		}
	}
	w := pictureOneSetupWarn(r.at.GetStrategyConfig(), true)
	if !strings.Contains(w, "one_setup=ON") || !strings.Contains(w, "governs planner plays only — Picture scenarios are admitted by their own switch") {
		t.Fatalf("the D8 WARN must say exactly that: %q", w)
	}
	if pictureOneSetupWarn(r.at.GetStrategyConfig(), false) != "" {
		t.Fatal("no WARN while Picture is off")
	}
	off := store.StrategyConfig{DayPlan: &store.DayPlanConfig{}}
	oneSetupOff(&off)
	if pictureOneSetupWarn(&off, true) != "" {
		t.Fatal("no WARN while one_setup is OFF")
	}
}

// The WARN and the 🖼 line have ONE production call site, where the day-plan
// boot lines print (A29).
func TestPictureBootLinesHaveAProductionCallSite(t *testing.T) {
	b, err := os.ReadFile("auto_trader_dayplan.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "at.logPictureBootLines()") != 1 {
		t.Fatal("auto_trader_dayplan.go must call at.logPictureBootLines() exactly once")
	}
}

// ── strict (W5 correction) ──────────────────────────────────────────────────

// Under plan_mode=strict the Picture scenario is admitted like any cited plan
// scenario (EntryGate leg 0: arm path, cites P1, direction matches) — the W0
// strict refusal is gone.
func TestPictureScenarioAdmittedUnderStrict(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-strict", func(c *store.StrategyConfig) { c.DayPlan.PlanMode = "strict" })
	picPlan(r, picScenario("P1", "opp-strict", r.now, epoch, picDefault))
	picPass(r, 0, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("strict admits the Picture scenario, got %d frame(s)", len(sigs))
	}
	limitOnly(t, sigs)
}

// D12 — the shared legs apply (a parity CORRECTION) and their refusals of a
// Picture scenario are counted under picture_scenario:<class>. CTO ruling
// 1790194913337: on an AI plan the AI bias governs — a long Picture scenario
// against a short plan bias in direction mode is refused, and the refusal
// NAMES the rule that judged it ("direction: plan bias short").
func TestPictureScenarioFacesTheSharedLegs(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-shared", func(c *store.StrategyConfig) { c.DayPlan.PlanMode = "direction" })
	warns := picWarns(t)
	d := zoneDoc(picScenario("P1", "opp-shared", r.now, epoch, picDefault))
	d.Bias.Direction = "short"
	blob, _ := json.Marshal(d)
	r.pid = shadowPlanAtTime(t, r.at, r.st, string(blob), r.now)
	picPass(r, 0, 100.25)
	picPass(r, time.Second, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a long Picture scenario against a short AI bias in direction mode is refused: %+v", sigs)
	}
	verdict := `direction: plan bias short — against plan bias "short" (plan_mode=direction)`
	named := 0
	for _, w := range warns() {
		if strings.Contains(w, "arm REFUSED") && strings.Contains(w, "P1") && strings.Contains(w, verdict) {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("the refusal must name the rule that judged it (%q), once: %q", verdict, warns())
	}
	class := armRefusalClass(verdict)
	if n := r.armRefusals(pictureClassSharedPrefix + class); n != 1 {
		t.Fatalf("the shared-leg refusal is counted once as picture_scenario:%s, got %d", class, n)
	}
}

// picMachinePlan writes the no-plan door's machine plan v1 (D1 b: trigger
// machine:picture_htf, model machine, bias neutral, no levels, the one machine
// scenario) and installs the provider on the rig's clock.
func picMachinePlan(r *zoneRig, sc kernel.PlanScenario) {
	r.t.Helper()
	doc := kernel.PlanDoc{Reasoning: "MACHINE-AUTHORED (Picture HTF rule v1) — fixture",
		Bias: kernel.PlanBias{Direction: "neutral"}, DeathCondition: "machine plan: ends when an AI plan is written",
		Levels: []kernel.PlanLevel{}, Scenarios: []kernel.PlanScenario{sc}, NoTrade: []string{}}
	sess := shadowEnableTestSession(r.t, r.st)
	r.at.config.StrategyConfig.DayPlan.SessionsEnabled = []string{sess}
	td, _ := kernel.PlanChainTradeDate(&kernel.SessionDef{Name: sess, WindowStartCT: "00:00", WindowEndCT: "23:59"}, r.now)
	pid := store.MakePlanIDForTrader(r.at.id, td, sess)
	blob, _ := json.Marshal(doc)
	if _, err := r.st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: sess, StrategyID: r.at.id, Lifecycle: "active",
		Doc: string(blob), TriggerReason: kernel.MachinePlanTriggerPicture, ModelID: kernel.MachinePlanModelID, CreatedAt: r.now.Add(-time.Minute)}); err != nil {
		r.t.Fatal(err)
	}
	installActivePlanProviderAt(r.at, r.st, func() time.Time { return r.now })
	r.pid = pid
}

// CTO ruling 1790194913337: direction mode on a MACHINE plan (bias neutral =
// no AI opinion) judges the Picture scenario on its own direction — it places.
func TestPictureScenarioOnAMachinePlanUnderDirectionPlaces(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-dir-machine", func(c *store.StrategyConfig) { c.DayPlan.PlanMode = "direction" })
	picMachinePlan(r, picScenario("P1", "opp-dir-machine", r.now, epoch, picDefault))
	picPass(r, 0, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("direction mode + a machine plan: the long P1 is judged on its own direction and places, got %+v", sigs)
	}
	limitOnly(t, sigs)
	bias, rule := r.at.pictureDirectionRule(kernel.ActivePlanFor(r.at.id, r.at.futuresSymbol()), picScenario("P1", "x", r.now, epoch, picDefault), true, "neutral")
	if bias != "long" || rule != "direction: machine plan — judged on the scenario's own direction (long)" {
		t.Fatalf("the machine-plan rule judges on the scenario's own direction and says so, got bias %q rule %q", bias, rule)
	}
}

// ── invalidation (D13) ──────────────────────────────────────────────────────

// A resting Picture limit whose newest completed 5m close falls back inside
// the broken body is invalidated (EntryGate leg 3, the machine resolver) and
// its cancel is requested; no WARN-and-pass.
func TestPictureScenarioInvalidatedBackInsideTheBody(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-invalid", nil)
	picPlan(r, picScenario("P1", "opp-invalid", r.now, epoch, picDefault))
	picPass(r, 0, 101.95)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: placed once, got %d", len(sigs))
	}
	at := r.now.Add(2 * time.Minute)
	workingBook(r, at, sigs[0].SignalID, 100.5)
	picPass(r, 2*time.Minute, 98.4) // back below the body top 99.00
	_, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sigs[0].SignalID {
		t.Fatalf("the invalidated scenario's resting limit is cancelled: %+v", cancels)
	}
	if row := r.row("P1"); row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "invalidated") {
		t.Fatalf("cancel_pending for the invalidation: %+v", row)
	}
	if n := r.armRefusals(pictureClassSharedPrefix + "entry_gate:invalidated"); n != 1 {
		t.Fatalf("counted once as picture_scenario:entry_gate:invalidated, got %d", n)
	}
}

// The resolver itself, both sides, the deadline, and the evidence fallback.
func TestMachineScenarioInvalidationTable(t *testing.T) {
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC)
	long := picScenario("P1", "opp-l", now, 1, picDefault)
	sg := picDefault
	sg.dir, sg.lo, sg.hi, sg.entry, sg.stop, sg.target, sg.bodyTop, sg.bodyBot = "short", 99.5, 100, 100, 103, 90, 102.5, 101
	short := picScenario("P2", "opp-s", now, 1, sg)
	for _, c := range []struct {
		name  string
		sc    kernel.PlanScenario
		tape  []market.Kline
		asOf  time.Time
		inval bool
		ok    bool
	}{
		{"long alive", long, zoneTape(100.25, now, 0), now, false, true},
		{"long back inside", long, zoneTape(98.4, now, 0), now, true, true},
		{"short alive", short, zoneTape(99.8, now, 0), now, false, true},
		{"short back inside", short, zoneTape(101.6, now, 0), now, true, true},
		{"deadline passed", long, zoneTape(100.25, now, 0), now.Add(31 * time.Minute), true, true},
		{"no tape after the break: the evidence's H1 close (alive)", long, zoneTape(98.4, now.Add(-30*time.Minute), 0), now, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, ok := machineScenarioInvalidation(c.sc, c.tape, c.asOf)
			if ok != c.ok || v.Invalidated != c.inval {
				t.Fatalf("got %+v ok=%v, want invalidated=%v ok=%v", v, ok, c.inval, c.ok)
			}
		})
	}
}

// ── Stop / Day Plan OFF (D21) ───────────────────────────────────────────────

// Stop (the production AutoTrader.Stop): the run epoch is cleared, an unplaced
// Picture row goes terminal "trader stopped", a resting one gets its cancel
// requested; the planner's row is untouched.
func TestPictureStopInvalidatesPictureRows(t *testing.T) {
	t.Run("unplaced", func(t *testing.T) {
		r, epoch := newPicRig(t, "w5b-stop-u", nil)
		picPlan(r, plannerZone("S1", 101, 101.5, 101), picScenario("P1", "opp-stop-u", r.now, epoch, picDefault))
		picPass(r, 0, 99.6) // both short of their zones: both armed
		r.at.stopMonitorCh = make(chan struct{})
		r.at.Stop()
		if _, ok := r.at.pictureRunEpoch(); ok {
			t.Fatal("Stop must clear the run epoch")
		}
		if row := r.row("P1"); row.State != store.StateCancelled || row.StateReason != "picture: trader stopped — never placed" {
			t.Fatalf("the unplaced Picture row is terminal: %+v", row)
		}
		if row := r.row("S1"); row.State != store.StateArmed {
			t.Fatalf("the planner row is untouched by Stop: %+v", row)
		}
		if picSysCount(t, r.st, "stopped") != 1 {
			t.Fatal("counted")
		}
	})
	t.Run("resting", func(t *testing.T) {
		r, epoch := newPicRig(t, "w5b-stop-r", nil)
		picPlan(r, plannerZone("S1", 101, 101.5, 101), picScenario("P1", "opp-stop-r", r.now, epoch, picDefault))
		picPass(r, 0, 100.25)
		sigs, _ := r.drain()
		if len(sigs) != 1 {
			t.Fatalf("fixture: P1 placed, got %d", len(sigs))
		}
		// Stop reads the book at the wall clock (it has no pass clock).
		workingBook(r, time.Now(), sigs[0].SignalID, 100.5)
		r.at.stopMonitorCh = make(chan struct{})
		r.at.Stop()
		_, cancels := r.drain()
		if len(cancels) != 1 || cancels[0].SignalID != sigs[0].SignalID {
			t.Fatalf("Stop requests the resting Picture limit's cancel: %+v", cancels)
		}
		if row := r.row("P1"); row.State != store.StateCancelPending || row.StateReason != "picture: trader stopped" {
			t.Fatalf("cancel_pending 'picture: trader stopped': %+v", row)
		}
		if row := r.row("S1"); row.State != store.StateArmed {
			t.Fatalf("the planner row is untouched by Stop: %+v", row)
		}
	})
}

// Day Plan OFF (the pass head): the same invalidation, planner rows untouched.
func TestPictureDayPlanOffInvalidatesPictureRows(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-dpoff", nil)
	picPlan(r, plannerZone("S1", 101, 101.5, 101), picScenario("P1", "opp-dpoff", r.now, epoch, picDefault), picScenario("P2", "opp-dpoff-2", r.now, epoch, picGeo{lo: 102, hi: 102.5, entry: 102, stop: 98, target: 115, bodyTop: 99, bodyBot: 97.5, window: 30 * time.Minute, rrFloor: 2}))
	picPass(r, 0, 100.25) // P1 places; S1 and P2 wait armed (D9 keeps S1; P2 is Picture's own)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: P1 placed, got %d", len(sigs))
	}
	at := r.now.Add(time.Minute)
	workingBook(r, at, sigs[0].SignalID, 100.5)
	r.at.config.StrategyConfig.DayPlan.PlanEnabled = false
	picPass(r, time.Minute, 100.25)
	_, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sigs[0].SignalID {
		t.Fatalf("Day Plan OFF requests the resting Picture limit's cancel: %+v", cancels)
	}
	if row := r.row("P1"); row.State != store.StateCancelPending || row.StateReason != "picture: Day Plan master off" {
		t.Fatalf("P1 cancel_pending: %+v", row)
	}
	if row := r.row("S1"); row.State != store.StateArmed {
		t.Fatalf("the planner row is untouched by Day Plan OFF: %+v", row)
	}
	for _, row := range r.rows() {
		if row.Scenario == "P2" && row.State != store.StateCancelled {
			t.Fatalf("an unplaced Picture row is terminal (P2 was cancelled one-live-entry by P1 or retired here): %+v", row)
		}
	}
}

// ── one live entry, both directions (D9 + mirror) ───────────────────────────

// Picture placed → the planner arm is REFUSED (counted), never cancelled →
// once the Picture row is terminal and the position closed, the planner arm
// places. One entry at a time, the other source's authorization survives.
func TestPicturePlacementKeepsThePlannerArm(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-d9", nil)
	picPlan(r, plannerZone("S1", 101, 101.5, 101), picScenario("P1", "opp-d9", r.now, epoch, picDefault))
	picPass(r, 0, 100.25) // P1 inside → places; S1 short of its zone
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("P1 places: %+v", sigs)
	}
	if row := r.row("S1"); row.State != store.StateArmed {
		t.Fatalf("D9: the Picture placement must NOT cancel the planner's unplaced arm: %+v", row)
	}
	d9Flow(t, r, sigs[0].SignalID, "S1", "P1", 101.25, 101.5)
}

// The mirror: planner placed → the Picture arm is refused, never cancelled →
// then places inside its window.
func TestPlannerPlacementKeepsThePictureArm(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-d9m", nil)
	g := picDefault
	g.lo, g.hi, g.entry, g.target = 101, 101.5, 101, 112
	picPlan(r, plannerZone("S1", 99.5, 100.5, 100), picScenario("P1", "opp-d9m", r.now, epoch, g))
	picPass(r, 0, 100.25) // S1 inside → places; P1 short of its zone
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("S1 places: %+v", sigs)
	}
	if row := r.row("P1"); row.State != store.StateArmed {
		t.Fatalf("D9 mirror: the planner placement must NOT cancel the Picture arm: %+v", row)
	}
	d9Flow(t, r, sigs[0].SignalID, "P1", "S1", 101.25, 101.5)
}

// d9Flow: the first entry is working → the waiting arm is refused at the
// broker book (counted); the entry fills → refused on the open position
// (counted); the position closes → the waiting arm places.
func d9Flow(t *testing.T, r *zoneRig, firstSig, waiting, first string, inside, far float64) {
	t.Helper()
	before := gateBlocks(r.at.id, "arm_one_contract_live")
	workingBook(r, r.now.Add(time.Minute), firstSig, 100.5)
	picPass(r, time.Minute, inside)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("while %s's entry is working, %s must be refused: %+v", first, waiting, sigs)
	}
	if gateBlocks(r.at.id, "arm_one_contract_live") != before+1 {
		t.Fatalf("the refusal of %s is counted (arm_one_contract_live)", waiting)
	}
	if row := r.row(waiting); row.State != store.StateArmed {
		t.Fatalf("%s stays armed — refused, not cancelled: %+v", waiting, row)
	}
	r.at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: firstSig, State: "filled", FillPrice: 100.5, Account: "Sim101"}, r.st.ArmedOrders())
	if row := r.row(first); row.State != store.StateFilled {
		t.Fatalf("fixture: %s filled: %+v", first, row)
	}
	r.flatBook(r.now.Add(2 * time.Minute))
	picPass(r, 2*time.Minute, inside)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("while the position is open, %s must be refused: %+v", waiting, sigs)
	}
	if row := r.row(waiting); row.State != store.StateArmed {
		t.Fatalf("%s stays armed while the position is open: %+v", waiting, row)
	}
	opens, err := r.st.Position().GetOpenPositions(r.at.id)
	if err != nil || len(opens) != 1 {
		t.Fatalf("fixture: one open position, got %d (%v)", len(opens), err)
	}
	if _, err := r.st.Position().ClosePosition(opens[0].ID, 102, "tp", 3, 0, "target"); err != nil {
		t.Fatal(err)
	}
	r.flatBook(r.now.Add(3 * time.Minute))
	// The broker adapter's B3 dupe/rate guard counts a WALL-CLOCK window; the
	// fixture clock says 3 minutes passed, so the adapter is re-made to stand
	// for that elapsed time (same server, same account) — W3's zone-pin idiom.
	r.at.trader = ntTrader.NewTCPTrader(r.srv, "MNQ", "Sim101")
	picPass(r, 3*time.Minute, inside)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != far {
		t.Fatalf("after %s is terminal and flat, %s places at %.2f: %+v", first, waiting, far, sigs)
	}
	limitOnly(t, sigs)
}

// ── D15 — the 🖼 line and the latch's holder ────────────────────────────────

func TestPictureRowsBootLineReadsTheLedgerAndTheEpoch(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-bootline", nil)
	if got := PictureRowsBootLine(r.st, 0, false); !strings.Contains(got, ": none · armed picture orders: none · run_epoch=n/a") {
		t.Fatalf("no rows, no run: %q", got)
	}
	mk := func(key string, stamp bool) int64 {
		row, _, err := r.st.PictureHtfClaim(&store.PictureHtfOpportunityDB{OppKey: key, TraderID: r.at.id, Stage: "confirmed"})
		if err != nil {
			t.Fatal(err)
		}
		if won, err := r.st.PictureHtfClaimSubmission(key, "claim-"+key); err != nil || !won {
			t.Fatalf("claim: %v %v", won, err)
		}
		if stamp {
			if err := r.st.PictureHtfStampSignal(key, "claim-"+key, "broker-"+key); err != nil {
				t.Fatal(err)
			}
		}
		return row.ID
	}
	unstamped, stamped := mk("pic-a", false), mk("pic-b", true)
	got := PictureRowsBootLine(r.st, epoch, true)
	for _, want := range []string{
		"#" + strconv.FormatInt(unstamped, 10) + " " + store.StatePlacePending + " submitted_at=n/a",
		"#" + strconv.FormatInt(stamped, 10) + " " + store.StatePlacePending + " submitted_at=" + stampedAt(t, r.st, "pic-b"),
		"run_epoch=" + strconv.FormatInt(epoch, 10),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the 🖼 line must carry %q: %q", want, got)
		}
	}
}

// stampedAt is the 🖼 line's rendering of a stamped row's submitted_at, READ
// from the row (the stamp is the store's wall clock).
func stampedAt(t *testing.T, st *store.Store, key string) string {
	t.Helper()
	row, ok, err := st.PictureHtfGet(key)
	if err != nil || !ok || row.SubmittedAt <= 0 {
		t.Fatalf("fixture: %s must carry a submission stamp: %+v %v %v", key, row, ok, err)
	}
	return kernel.ClockCTSeconds(time.UnixMilli(row.SubmittedAt))
}

// U4 — since W5 a live Picture order is an armed_orders row with
// source='picture' (#193 N1): the 🖼 line must list those too (id, state,
// signal short-id), READ from the store, "none" when none, never 0.
func TestPictureRowsBootLineListsArmedPictureOrders(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-bootline-armed", nil)
	arm := &store.ArmedOrderDB{
		TraderID: r.at.id, PlanID: "2026-09-24:NY", Scenario: "P1", Version: 1,
		State: store.StateWorking, Side: "long", EntryPx: 30000, StopPx: 29950,
		TargetPx: 30100, Source: store.ArmSourcePicture, SourceRef: "pic-armed-1",
		SignalID: "armed-signal-0001",
	}
	if err := r.st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	got := PictureRowsBootLine(r.st, epoch, true)
	want := "#" + strconv.FormatInt(arm.ID, 10) + " " + store.StateWorking +
		" signal=" + shortID("armed-signal-0001")
	if !strings.Contains(got, want) {
		t.Fatalf("the 🖼 line must list the armed source=picture order %q: %q", want, got)
	}
	if !strings.Contains(got, "armed picture orders") {
		t.Fatalf("the 🖼 line must name the armed picture set: %q", got)
	}
}

// The latch's ledger source (production: the latch calls it) names a Picture
// ledger row "latched by picture row #<id>" and a Picture-placed arm as such.
func TestEntryLatchNamesAPictureHolder(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-latchname", nil)
	picPlan(r, picScenario("P1", "opp-latch", r.now, epoch, picDefault))
	picPass(r, 0, 100.25)
	if sigs, _ := r.drain(); len(sigs) != 1 {
		t.Fatal("fixture: P1 placed")
	}
	row, _, err := r.st.PictureHtfClaim(&store.PictureHtfOpportunityDB{OppKey: "pic-legacy", TraderID: r.at.id, Stage: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.PictureHtfClaimSubmission("pic-legacy", "claim-l"); err != nil {
		t.Fatal(err)
	}
	if err := r.st.PictureHtfStampSignal("pic-legacy", "claim-l", "broker-l"); err != nil {
		t.Fatal(err)
	}
	ids, err := r.at.entryLatchLedgers()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ids, " | ")
	if !strings.Contains(joined, "latched by picture row #"+strconv.FormatInt(row.ID, 10)) {
		t.Fatalf("a Picture ledger row is named 'latched by picture row #%d': %s", row.ID, joined)
	}
	if !strings.Contains(joined, "latched by picture scenario P1") {
		t.Fatalf("a Picture-placed armed row says so: %s", joined)
	}
}

// ── D22 — the Picture path writes only LIMIT frames ─────────────────────────

// A Picture scenario driven through the pass over the real wire from
// placement to its cancel writes only limit entry frames — never a market
// order (order_type "" / "market").
func TestPicturePathWritesOnlyLimitFrames(t *testing.T) {
	for _, last := range []float64{100.25, 101.95} { // inside (marketable limit) and beyond (resting)
		r, epoch := newPicRig(t, "w5b-limit-"+strconv.FormatFloat(last, 'f', 2, 64), nil)
		picPlan(r, picScenario("P1", "opp-limit", r.now, epoch, picDefault))
		picPass(r, 0, last)
		picPass(r, time.Second, last)
		sigs, _ := r.drain()
		if len(sigs) != 1 {
			t.Fatalf("price %.2f: one entry frame, got %d", last, len(sigs))
		}
		limitOnly(t, sigs)
	}
}

// ── CTO pre-review F4 / F5 — Stop with a dark book, Stop never deadlocks ────

// picWarns captures every WARN+ line through the logger's own tee.
func picWarns(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	logger.AttachDBSink(func(_ int64, _, _, _, message, _ string) {
		mu.Lock()
		got = append(got, message)
		mu.Unlock()
	})
	t.Cleanup(func() { logger.AttachDBSink(func(int64, string, string, string, string, string) {}) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// F4: Stop with a STALE book (no pass runs after it): the resting Picture row
// must not be left "working" — it ends cancel_pending (held, never promoted),
// NO wire cancel is sent blind, and ONE WARN names the row id.
func TestPictureStopWithADarkBookHoldsCancelPending(t *testing.T) {
	r, epoch := newPicRig(t, "w5b-stop-dark", nil)
	picPlan(r, picScenario("P1", "opp-stop-dark", r.now, epoch, picDefault))
	picPass(r, 0, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: P1 placed, got %d", len(sigs))
	}
	row := r.row("P1")
	warns := picWarns(t)
	// The book was last put at the fixture instant; Stop reads the wall
	// clock, where that book is stale by days — a dark book.
	r.at.stopMonitorCh = make(chan struct{})
	r.at.Stop()
	if _, cancels := r.drain(); len(cancels) != 0 {
		t.Fatalf("no wire cancel may be sent blind on a dark book: %+v", cancels)
	}
	got := r.row("P1")
	if got.State != store.StateCancelPending || got.StateReason != "picture: trader stopped" {
		t.Fatalf("the resting row ends cancel_pending 'picture: trader stopped' (held, never promoted): %+v", got)
	}
	held := 0
	for _, w := range warns() {
		if strings.Contains(w, "picture cancel HELD") && strings.Contains(w, "row #"+strconv.FormatInt(row.ID, 10)+" ") {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("exactly ONE WARN names the held row #%d, got %d: %q", row.ID, held, warns())
	}
}

// F5: every caller of stopArmedEventLoop is AutoTrader.Stop (auto_trader.go),
// and every caller of Stop runs outside an armed pass (manager StopAll /
// RemoveTrader, the API stop / delete handlers, the agent's stop tool). Stop
// now takes armedPassMu (the Picture invalidation), so it must wait for a
// pass in flight — never interleave with it — and never deadlock: bounded
// here with no pass, with a scan pass held inside the lock, and with an event
// pass held inside the lock.
func TestPictureStopNeverDeadlocks(t *testing.T) {
	type start func(r *zoneRig)
	scanPass := func(r *zoneRig) { go r.at.maybeManageArmedOrdersAt(nil, r.now.Add(time.Second)) }
	eventPass := func(r *zoneRig) {
		r.at.zoneArmActive.Store(true)
		r.at.armedEvent.Load().poke()
	}
	for _, c := range []struct {
		name  string
		pass  start
		event bool
	}{{"no pass in flight", nil, false}, {"a scan pass in flight", scanPass, false}, {"an event pass in flight", eventPass, true}} {
		t.Run(c.name, func(t *testing.T) {
			r, epoch := newPicRig(t, "w5b-stop-"+strings.ReplaceAll(c.name, " ", "-"), nil)
			if c.event {
				if err := r.st.Trader().Create(&store.Trader{ID: r.at.id, Name: r.at.id, Account: "Sim101"}); err != nil {
					t.Fatal(err)
				}
				r.at.armedEventNowForTest = func() time.Time { return r.now.Add(time.Second) }
				r.at.startArmedEventLoop() // a new run: a new epoch
				epoch, _ = r.at.pictureRunEpoch()
			}
			picPlan(r, picScenario("P1", "opp-stop-"+c.name, r.now, epoch, picDefault))
			picPass(r, 0, 99.6) // P1 waits armed
			var once sync.Once
			entered, release := make(chan struct{}), make(chan struct{})
			if c.pass != nil {
				armedPassEnterForTest = func(id string) func() {
					if id == r.at.id {
						once.Do(func() { close(entered) })
						<-release
					}
					return func() {}
				}
				t.Cleanup(func() { armedPassEnterForTest = nil })
				c.pass(r)
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("fixture: the pass never entered")
				}
			}
			stopped := make(chan struct{})
			r.at.stopMonitorCh = make(chan struct{})
			go func() { r.at.Stop(); close(stopped) }()
			if c.pass != nil {
				select {
				case <-stopped:
					t.Fatal("Stop returned while a pass held armedPassMu — the invalidation interleaved with the pass")
				case <-time.After(300 * time.Millisecond):
				}
				close(release)
			}
			select {
			case <-stopped:
			case <-time.After(15 * time.Second):
				t.Fatal("DEADLOCK: Stop did not return within 15s")
			}
			// With a pass in flight, Stop clears the epoch first: the released
			// pass reaches placement and retires the row there ("a run that is
			// no longer live"); otherwise the Stop hook retires it.
			row := r.row("P1")
			byStop := row.StateReason == "picture: trader stopped — never placed"
			byPass := strings.HasPrefix(row.StateReason, "recorded by a run that is no longer live (epoch ") && strings.HasSuffix(row.StateReason, ", live none) — not placed")
			if row.State != store.StateCancelled || !(byStop || (c.pass != nil && byPass)) {
				t.Fatalf("after Stop the unplaced Picture row is terminal, never placed: %+v", row)
			}
			if sigs, _ := r.drain(); len(sigs) != 0 {
				t.Fatalf("nothing is placed across a Stop: %+v", sigs)
			}
		})
	}
}

// ── L4 — with no machine scenario the pass is W3's pass ─────────────────────

// Planner rows carry no W5 field, and one live entry per plan still cancels a
// planner row when a planner row of the same plan places (same source).
func TestPlannerRowsCarryNoW5FieldAndOneLiveEntryHolds(t *testing.T) {
	r, _ := newPicRig(t, "w5b-planner-only", nil)
	picPlan(r, plannerZone("S1", 99.5, 100.5, 100), plannerZone("S2", 99.5, 100.5, 100))
	picPass(r, 0, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("one entry per plan, got %d", len(sigs))
	}
	s1, s2 := r.row("S1"), r.row("S2")
	if s1.Source != "" || s1.SourceRef != "" || s1.SourceRule != "" || s1.EligibleUntilMs != nil || s1.SourceRunEpoch != nil {
		t.Fatalf("a planner row carries no W5 field: %+v", s1)
	}
	if s2.State != store.StateCancelled || s2.StateReason != "one_live_entry: S1 placed" {
		t.Fatalf("the same-source one-live-entry cancel is unchanged: %+v", s2)
	}
}

// W5 R8 (CTO round 2): Day Plan OFF must not strand a cancel_pending row. The
// pass head still runs the SETTLEMENT half (drain + confirmPendingCancels)
// before it returns, so the OFF sweep's cancel confirms from the broker book
// exactly as when ON — and the entry latch, which counts every non-terminal
// row with a signal as placed, frees for the AI decision path.
func TestDayPlanOffStillSettlesTheCancelItRequested(t *testing.T) {
	r, epoch := newPicRig(t, "w5-r8", nil)
	picPlan(r, picScenario("P1", "opp-r8", r.now, epoch, picDefault))
	picPass(r, 0, 100.25)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: P1 placed, got %d", len(sigs))
	}
	workingBook(r, r.now.Add(time.Minute), sigs[0].SignalID, 100.5)
	r.at.config.StrategyConfig.DayPlan.PlanEnabled = false
	picPass(r, time.Minute, 100.25)
	if _, cancels := r.drain(); len(cancels) != 1 {
		t.Fatalf("fixture: OFF requests the resting limit's cancel: %+v", cancels)
	}
	if row := r.row("P1"); row.State != store.StateCancelPending {
		t.Fatalf("fixture: P1 cancel_pending: %+v", row)
	}
	// The broker's fresh book no longer lists the order; Day Plan is still OFF.
	if err := r.st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: "[]", ReceivedMs: r.now.Add(2 * time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	picPass(r, 2*time.Minute, 100.25)
	if row := r.row("P1"); row.State != store.StateCancelled {
		t.Fatalf("Day Plan OFF must still settle the cancel from the fresh book (else the account latches): %+v", row)
	}
	ids, err := r.at.entryLatchLedgers()
	if err != nil || len(ids) != 0 {
		t.Fatalf("the entry latch must be free once the cancel settles: %v %v", ids, err)
	}
}
