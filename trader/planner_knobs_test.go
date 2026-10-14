package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── Release #25 lane planner-knobs — the STRICT-12345 loss-fix switches ─────
//
// Every switch ships OFF. Each item has: an OFF = today test, a RED→GREEN at
// its production call site, and the "what it would have done on 649–654"
// fixture built from the STRICT-12345 report's numbers (never the live DB).

func pkF(v float64) *float64 { return &v }
func pkB(v bool) *bool       { return &v }

// runFeasPlan drives the real planner write site (runPlannerReadCoreWithFactsGradesClock)
// with the same doc on every attempt and returns the stored doc + the prompts.
func runFeasPlan(t *testing.T, at *AutoTrader, hash, raw string) (kernel.PlanDoc, []string) {
	t.Helper()
	blocks := []string{}
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", hash, "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(userPrompt string) (string, error) { blocks = append(blocks, userPrompt); return raw, nil })
	if err != nil || lc != "active" {
		t.Fatalf("plan write: lc=%q err=%v", lc, err)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil {
		t.Fatal("no stored plan")
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatalf("doc unmarshal: %v", err)
	}
	if len(doc.Scenarios) == 0 || doc.Scenarios[0].Arm == nil {
		t.Fatalf("doc lost its arm: %s", row.Doc)
	}
	return doc, blocks
}

// The base feasibility fixture: entry 15550, authored stop 15540 (composed to
// the 1.5×ATR5m floor 15538 at ATR 8), target 15620, first obstacle 15560 →
// r_to_obstacle = 10/10 = 1.00.
var pkBasePlan = strings.ReplaceAll(infeasibleFeasPlanJSON, `"target":15620`, `"target":15620`)

// ── PK-1 ROOM GATE ──────────────────────────────────────────────────────────

// OFF = today: absent and an explicit "off" write the same doc, arm enabled.
func TestPK1RoomGateOffIsToday(t *testing.T) {
	absent := feasPlannerTrader(t, nil)
	feasStubBars(t)
	docA, _ := runFeasPlan(t, absent, "pk1off-a", pkBasePlan)
	off := feasPlannerTrader(t, nil)
	off.config.StrategyConfig.DayPlan.PlannerRoomGate = "off"
	off.config.StrategyConfig.DayPlan.PlannerRoomMinR = pkF(5) // ignored while off
	docB, _ := runFeasPlan(t, off, "pk1off-b", pkBasePlan)
	a, _ := json.Marshal(docA.Scenarios)
	b, _ := json.Marshal(docB.Scenarios)
	if string(a) != string(b) {
		t.Fatalf("room gate OFF must be byte-identical to absent:\n%s\n%s", a, b)
	}
	if !docA.Scenarios[0].Arm.Enabled || docA.Scenarios[0].Arm.Target != 15620 {
		t.Fatalf("today's write keeps the arm enabled at its target: %+v", docA.Scenarios[0].Arm)
	}
	var d kernel.PlanDoc
	_ = json.Unmarshal([]byte(pkBasePlan), &d)
	if got := absent.roomGateAtWrite(&d, "2026-08-14", "NY"); got != nil {
		t.Fatalf("OFF returns no issue and touches nothing, got %+v", got)
	}
}

// RED→GREEN at the write site: refuse with min 1.5 → the repair prompt carries
// the room numbers, the last attempt writes the arm disabled (room_gate) and
// the class is counted. MUTANT: drop the `out = append(...)` in
// roomGateAtWrite → the arm writes enabled → RED.
func TestPK1RoomGateRefuseAtTheWriteSite(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	at.config.StrategyConfig.DayPlan.PlannerRoomGate = "refuse"
	at.config.StrategyConfig.DayPlan.PlannerRoomMinR = pkF(1.5)
	doc, blocks := runFeasPlan(t, at, "pk1-refuse", pkBasePlan)
	if doc.Scenarios[0].Arm.Enabled || doc.Scenarios[0].Arm.DisabledReason != RoomGateClass {
		t.Fatalf("room 1.00R < 1.50 must be written DISABLED reason room_gate, got enabled=%v reason=%q",
			doc.Scenarios[0].Arm.Enabled, doc.Scenarios[0].Arm.DisabledReason)
	}
	if len(blocks) < 2 || !strings.Contains(blocks[1], "S1 room gate: r_to_obstacle 1.00 < planner_room_min_r 1.50 (first obstacle fixture reference 15560.00 is 10.00 pts from entry 15550.00, risk 10.00 pts)") {
		t.Fatalf("the repair prompt must carry the room refusal with its numbers:\n%v", blocks)
	}
	if n, _ := store.SystemCounter(at.store, "arm_disabled_at_write:t1:2026-08-14:NY:room_gate"); n != 1 {
		t.Fatalf("the disable is counted under room_gate: %d", n)
	}
	if n := dlGateBlocks(at.id)[RoomGateClass]; n < 1 {
		t.Fatalf("every room refusal is counted in the gate-block table: %d", n)
	}
	// The same plan at min 1.0 (the suggested value) has room exactly 1R → armed.
	at2 := feasPlannerTrader(t, nil)
	at2.config.StrategyConfig.DayPlan.PlannerRoomGate = "refuse"
	doc2, _ := runFeasPlan(t, at2, "pk1-pass", pkBasePlan)
	if !doc2.Scenarios[0].Arm.Enabled {
		t.Fatalf("r_to_obstacle 1.00 ≥ the default 1.00 must arm: %+v", doc2.Scenarios[0].Arm)
	}
}

// tp1_at_obstacle: the target moves to the obstacle and the EXISTING R:R floor
// judges it (10 pts / 12 pts composed risk = 0.83 < the arm floor) → rr.
func TestPK1RoomGateTP1AtObstacleThenRRJudges(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	at.config.StrategyConfig.DayPlan.PlannerRoomGate = "tp1_at_obstacle"
	at.config.StrategyConfig.DayPlan.PlannerRoomMinR = pkF(1.5)
	logBuf := captureTraderLog(t)
	doc, _ := runFeasPlan(t, at, "pk1-tp1", pkBasePlan)
	if doc.Scenarios[0].Arm.Target != 15560 {
		t.Fatalf("TP1 must move to the first obstacle 15560, got %.2f", doc.Scenarios[0].Arm.Target)
	}
	if doc.Scenarios[0].Arm.Enabled || doc.Scenarios[0].Arm.DisabledReason != "rr" {
		t.Fatalf("the moved target is judged by the R:R floor (refused rr), got enabled=%v reason=%q",
			doc.Scenarios[0].Arm.Enabled, doc.Scenarios[0].Arm.DisabledReason)
	}
	if !strings.Contains(logBuf.String(), "TP1 moved 15620.00 → first obstacle fixture reference 15560.00") {
		t.Fatalf("the move is logged:\n%s", logBuf.String())
	}
}

// ── PK-4 ATR FLOOR CAP AT THE AUTHORED INVALIDATION ─────────────────────────

func TestPK4CapVerdictPure(t *testing.T) {
	// 649 (short): authored 30873, floor 30882.78 → beyond → refused.
	if v := atrFloorCapVerdict("short", StopComposition{Authored: 30873, ATRFloorStop: 30882.78}, 0); !strings.Contains(v, "lies 9.78 pts past the authored invalidation 30873.00") {
		t.Fatalf("649 must refuse: %q", v)
	}
	// 653 (long): authored 30436, invalidation line 30506.25 (inside the stop), floor 30427.25 → refused.
	if v := atrFloorCapVerdict("long", StopComposition{Authored: 30436, ATRFloorStop: 30427.25}, 30506.25); !strings.Contains(v, "8.75 pts past the authored invalidation 30436.00") {
		t.Fatalf("653 must refuse: %q", v)
	}
	// An invalidation line FARTHER than the stop lets the floor widen up to it.
	if v := atrFloorCapVerdict("long", StopComposition{Authored: 30436, ATRFloorStop: 30427.25}, 30420); v != "" {
		t.Fatalf("a floor inside the invalidation line fits: %q", v)
	}
	// No widening (650: floor inside the authored stop) → nothing.
	if v := atrFloorCapVerdict("long", StopComposition{Authored: 30760.5, ATRFloorStop: 30770}, 0); v != "" {
		t.Fatalf("no widening, no refusal: %q", v)
	}
	if c := armRefusalClass("entry_gate: " + atrFloorCapVerdict("short", StopComposition{Authored: 30873, ATRFloorStop: 30882.78}, 0)); c != ATRFloorBeyondInvalidationClass {
		t.Fatalf("the refusal has its own class, got %q", c)
	}
}

// RED→GREEN at the write site: authored stop 15540, floor 15538 (1.5×ATR 8).
// Invalidation "5m close below 15545" sits inside the stop → the floor lies
// past the authored invalidation → refused. "2x5m<15470" sits beyond it → the
// floor fits → armed. OFF → armed with the widened stop, as today.
// MUTANT: make atrFloorCapVerdictFor return "" → RED.
func TestPK4CapAtTheWriteSite(t *testing.T) {
	inside := strings.Replace(pkBasePlan, `"invalid": "2x5m<15470"`, `"invalid": "5m close below 15545"`, 1)
	if inside == pkBasePlan {
		t.Fatal("fixture: the invalidation sentence was not replaced")
	}
	off := feasPlannerTrader(t, nil)
	feasStubBars(t)
	docOff, _ := runFeasPlan(t, off, "pk4-off", inside)
	if !docOff.Scenarios[0].Arm.Enabled {
		t.Fatalf("OFF = today: the floor widens and the arm writes enabled: %+v", docOff.Scenarios[0].Arm)
	}
	on := feasPlannerTrader(t, nil)
	on.config.StrategyConfig.DayPlan.PlannerATRFloorCapAtInvalidation = pkB(true)
	docOn, blocks := runFeasPlan(t, on, "pk4-on", inside)
	if docOn.Scenarios[0].Arm.Enabled || docOn.Scenarios[0].Arm.DisabledReason != ATRFloorBeyondInvalidationClass {
		t.Fatalf("ON: the floor 15538 past the authored invalidation 15540 must refuse, got enabled=%v reason=%q",
			docOn.Scenarios[0].Arm.Enabled, docOn.Scenarios[0].Arm.DisabledReason)
	}
	if len(blocks) < 2 || !strings.Contains(blocks[1], "the ATR stop floor 15538.00 lies 2.00 pts past the authored invalidation 15540.00") {
		t.Fatalf("the repair prompt names the floor and the invalidation:\n%v", blocks)
	}
	wide := feasPlannerTrader(t, nil)
	wide.config.StrategyConfig.DayPlan.PlannerATRFloorCapAtInvalidation = pkB(true)
	docWide, _ := runFeasPlan(t, wide, "pk4-wide", pkBasePlan) // invalid "2x5m<15470"
	if !docWide.Scenarios[0].Arm.Enabled {
		t.Fatalf("an invalidation line beyond the floor lets the floor widen up to it: %+v", docWide.Scenarios[0].Arm)
	}
}

// RED→GREEN at the ARM PASS (maybeManageArmedOrdersAt): zoneDoc's stop widens
// 98 → ~97.70 from the near edge. ON → the arm is refused and counted as
// atr_floor_beyond_invalidation, nothing is placed.
func TestPK4CapAtTheArmPass(t *testing.T) {
	r := newZoneRig(t, "pk4-arm", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.at.config.StrategyConfig.DayPlan.PlannerATRFloorCapAtInvalidation = pkB(true)
	logBuf := captureTraderLog(t)
	tape := zoneTape(99.0, r.now, 0)
	for j := range tape {
		tape[j].High, tape[j].Low = tape[j].Close+0.6, tape[j].Close-0.6
	}
	r.setTape(tape)
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	ap := kernel.ActivePlanFor(r.at.id, r.at.futuresSymbol())
	if n := store.ArmRefusalCount(r.st, r.at.id, kernel.PlanTradeDateFor(ap), ap.Session, ATRFloorBeyondInvalidationClass); n != 1 {
		t.Fatalf("ON: the widened floor past authored 98 must be refused + counted once, got %d\nlog:\n%s", n, logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "atr_floor_beyond_invalidation: the ATR stop floor") {
		t.Fatalf("the refusal is logged with its numbers:\n%s", logBuf.String())
	}
}

// ── PK-3 NO-CHASE ENFORCE + MOMENTUM INTO THE ZONE ──────────────────────────

// pkFallingTape: 1m bars over the 15 minutes before now, opening at from and
// closing at to (a straight fall/rise), the last bar forming at now-1m.
func pkFallingTape(now time.Time, from, to float64) []market.Kline {
	start := now.Add(-15 * time.Minute).UnixMilli()
	out := make([]market.Kline, 0, 15)
	step := (to - from) / 15
	for i := 0; i < 15; i++ {
		o := from + float64(i)*step
		c := o + step
		ot := start + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, Close: c, High: o + 0.5, Low: c - 0.5})
	}
	return out
}

func TestPK3MomentumVerdictOnTheReportNumbers(t *testing.T) {
	now := time.Date(2026, 10, 14, 5, 9, 23, 0, time.UTC)
	// 650: −57.75 in 15 min into a long, ATR5m 16.5 → 3.50×.
	v := momentumIntoZoneVerdict("long", pkFallingTape(now, 30871, 30813.25), now.UnixMilli(), 16.5, 3.0)
	if !v.Known || !v.Refuse || v.MoveATR < 3.49 || v.MoveATR > 3.51 {
		t.Fatalf("650 moved 3.5×ATR5m: X=3.0 must refuse, got %+v", v)
	}
	if v := momentumIntoZoneVerdict("long", pkFallingTape(now, 30871, 30813.25), now.UnixMilli(), 16.5, 4.0); v.Refuse {
		t.Fatalf("650 at X=4.0 passes: %+v", v)
	}
	// 653: −70.75, ATR5m 54.4 → 1.30×.
	if v := momentumIntoZoneVerdict("long", pkFallingTape(now, 30587, 30516.25), now.UnixMilli(), 54.4, 1.0); !v.Refuse {
		t.Fatalf("653 moved 1.3×: X=1.0 must refuse: %+v", v)
	}
	if v := momentumIntoZoneVerdict("long", pkFallingTape(now, 30587, 30516.25), now.UnixMilli(), 54.4, 1.5); v.Refuse {
		t.Fatalf("653 at X=1.5 passes: %+v", v)
	}
	// 652 (short): the 15-min move was −28 — AWAY from a short's zone above.
	if v := momentumIntoZoneVerdict("short", pkFallingTape(now, 30827, 30799), now.UnixMilli(), 20, 0.5); v.Refuse || v.MovePts >= 0 {
		t.Fatalf("a move away from the zone never refuses: %+v", v)
	}
	// No tape → unknown → refused while ON (fail-closed).
	if v := momentumIntoZoneVerdict("long", nil, now.UnixMilli(), 16.5, 3.0); v.Known || !v.Refuse {
		t.Fatalf("unmeasurable momentum refuses while ON: %+v", v)
	}
}

// RED→GREEN at entryGateForArm (the arm pass's gate). OFF (absent) passes the
// same intent. MUTANT: drop the momentum leg in EntryGate → RED.
func TestPK3MomentumLegAtTheArmGate(t *testing.T) {
	plan, sc := e12ArmFixture()
	sc.Condition = "reject"
	sc.Confirm = &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "below"}
	leg := kernel.PlanArmLeg{Entry: 100, Stop: 60, Target: 230}
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return pkFallingTape(e12PassClock, 157.75, 100) }

	off, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	if reason, refused := off.entryGateForArm(plan, sc, leg, "long", "long", 16.5, e12PassClock); refused {
		t.Fatalf("OFF = today: the intent passes: %s", reason)
	}
	on, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, PlannerMomentumIntoZoneMaxATR: 3.0}})
	reason, refused := on.entryGateForArm(plan, sc, leg, "long", "long", 16.5, e12PassClock)
	if !refused || !strings.Contains(reason, "momentum_into_zone: price moved 57.75 pts toward the zone in the last 15 min = 3.50×ATR5m") {
		t.Fatalf("ON at 3.0: a 3.5×ATR5m fall into the long's zone must refuse, got refused=%v %q", refused, reason)
	}
	if armRefusalClass(reason) != MomentumIntoZoneClass {
		t.Fatalf("its own class, got %q", armRefusalClass(reason))
	}
}

// RED→GREEN: the enforced no-chase refuses on the arm path and says so; OFF
// stays WARN-first (TestNoChaseArmMeasuresFromTheLivePrice pins that).
// MUTANT: drop `if v.Enforced { return … }` → RED.
func TestPK3NoChaseEnforcedAtTheArmGate(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, PlannerNoChaseEnforce: pkB(true)}})
	plan, sc := e12ArmFixture()
	sc.Condition = "reject"
	sc.Confirm = &kernel.PlanConfirm{Rule: "touch", RefPrice: 100, Side: "below"}
	leg := kernel.PlanArmLeg{Entry: 100, Stop: 60, Target: 230}
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		return []market.Kline{{Close: 158}, {Close: 160}} // live price 60 pts past the level
	}
	logBuf := captureTraderLog(t)
	reason, refused := at.entryGateForArm(plan, sc, leg, "long", "long", 20, e12PassClock)
	if !refused || !strings.HasPrefix(reason, "entry_gate: no_chase_refused — dist 3.00×ATR > 1.00") {
		t.Fatalf("ON: the WOULD_REFUSE verdict must refuse, got refused=%v %q", refused, reason)
	}
	if !strings.Contains(logBuf.String(), "no-chase REFUSED arm S1") || strings.Contains(logBuf.String(), "entry PROCEEDING") {
		t.Fatalf("the line must say REFUSED, never PROCEEDING:\n%s", logBuf.String())
	}
	if n, _ := store.SystemCounter(st, NoChaseCounterKey("arm", "would_refuse")); n != 1 {
		t.Fatalf("the measurement is still counted: %d", n)
	}
	if armRefusalClass(reason) != NoChaseRefusedClass {
		t.Fatalf("its own class, got %q", armRefusalClass(reason))
	}
}

// ── PK-5 BREAKEVEN IN R ─────────────────────────────────────────────────────

func TestPK5BreakevenTriggerRPure(t *testing.T) {
	on := true
	rc := store.RiskControlConfig{BreakevenEnabled: &on, BreakevenTriggerPoints: 40}
	if fire, _ := breakevenTriggerR(rc, "LONG", 100, 110, 1, 10); !fire {
		t.Fatal("+10 pts on a 10-pt risk is 1R → fire")
	}
	if fire, _ := breakevenTriggerR(rc, "SHORT", 100, 91, 1, 10); fire {
		t.Fatal("+9 pts on a 10-pt risk is 0.9R → hold")
	}
	off := false
	if fire, _ := breakevenTriggerR(store.RiskControlConfig{BreakevenEnabled: &off}, "LONG", 100, 200, 1, 10); fire {
		t.Fatal("breakeven_enabled off → never")
	}
}

// RED→GREEN at maybeMoveStopToBreakeven (the 60-s beat's caller): +11 pts on a
// 10-pt initial risk. Points rule (40): no move. breakeven_trigger_r 1 with the
// stop at fill on record (trade_excursions): the move is taken. Unknown risk:
// the points rule governs, logged + counted. MUTANT: make breakevenTriggerFor
// always return the points rule → RED.
func TestPK5BreakevenInRAtTheCallSite(t *testing.T) {
	t.Setenv("EXIT_MECHS_SUSPENDED", "0")
	telemetry.ResetGateBlocksForTest()
	on := true
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	cfg.RiskControl.BreakevenEnabled = &on
	cfg.RiskControl.BreakevenTriggerPoints = 40
	at, st := resetTrader(t, cfg)
	p := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1,
		EntryPrice: 100, Status: "OPEN", Source: "armed_entry", EntryOrderID: "sig-be", EntryTime: 1, CreatedAt: 1, UpdatedAt: 1}
	if err := st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
	done := func() bool { at.breakevenMu.Lock(); defer at.breakevenMu.Unlock(); return at.breakevenDone["MNQ_LONG"] }

	at.maybeMoveStopToBreakeven("MNQ", "LONG", 100, 111)
	if done() {
		t.Fatal("OFF = today: +11 pts < the 40-pt trigger must not move")
	}
	at.config.StrategyConfig.RiskControl.BreakevenTriggerR = 1
	at.maybeMoveStopToBreakeven("MNQ", "LONG", 100, 111)
	if done() {
		t.Fatal("unknown initial risk → the points rule governs (no move at +11)")
	}
	if n := dlGateBlocks(at.id)[BreakevenRUnknownRiskClass]; n != 1 {
		t.Fatalf("unknown risk is counted once: %d", n)
	}
	if _, err := st.TradeExcursions().Open(store.TradeExcursion{PositionID: p.ID, Side: "LONG", EntryPx: 100, StopPxInitial: 90, TargetPx: 130, Size: 1, Source: "live"}); err != nil {
		t.Fatal(err)
	}
	at.maybeMoveStopToBreakeven("MNQ", "LONG", 100, 111)
	if !done() {
		t.Fatal("ON at 1R with a 10-pt initial risk: +11 pts must move the stop to breakeven")
	}
}

// ── PK-2 EXIT ON THESIS DEATH ───────────────────────────────────────────────

type pk2Seed struct {
	planID  string
	version int
	sid     string
}

// pk2SeedPlannerPosition writes an authoring plan (lifecycle as given), its
// armed row (origin as given) and the open planner position it filled, plus
// the long on the wire.
func pk2SeedPlannerPosition(t *testing.T, f *wdFix, lifecycle, invalid, armOrigin, posOrigin string) pk2Seed {
	t.Helper()
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long"}, Scenarios: []kernel.PlanScenario{{ID: "S1", Direction: "long", Condition: "hold", Invalid: invalid}}}
	blob, _ := json.Marshal(doc)
	pid := "2026-09-11:PK2:" + f.at.id
	v, err := f.st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: "2026-09-11", Session: "NY", StrategyID: f.at.id, Lifecycle: "active", Doc: string(blob)})
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle != "active" {
		if err := f.st.Plan().UpdatePlanLifecycle(pid, v, lifecycle, "test"); err != nil {
			t.Fatal(err)
		}
	}
	sid := "pk2-" + f.at.id
	arm := &store.ArmedOrderDB{TraderID: f.at.id, PlanID: pid, Version: v, Session: "NY", Scenario: "S1", Side: "long",
		EntryPx: 100, StopPx: 90, TargetPx: 130, State: "filled", SignalID: sid, Origin: armOrigin}
	if err := f.st.ArmedOrders().DB().Create(arm).Error; err != nil {
		t.Fatal(err)
	}
	p := &store.TraderPosition{TraderID: f.at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1,
		EntryPrice: 100, Status: "OPEN", Source: "armed_entry", EntryOrderID: sid, Origin: posOrigin,
		PlanID: pid, PlanVersion: v, PlanTradeDate: "2026-09-11", PlanSession: "NY", CitedScenarioID: "S1",
		EntryTime: f.now.Add(-10 * time.Minute).UnixMilli(), CreatedAt: f.now.Add(-10 * time.Minute).UnixMilli(), UpdatedAt: f.now.UnixMilli()}
	if err := f.st.GormDB().Create(p).Error; err != nil {
		t.Fatal(err)
	}
	f.w.srv.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 100}})
	f.at.peakPnLCache = map[string]float64{}
	return pk2Seed{planID: pid, version: v, sid: sid}
}

// OFF = today, then RED→GREEN at monitorTick: the authoring plan is DORMANT.
// absent → no close frame; all → one close_position + counted.
// MUTANT: drop the planDormantLifecycle branch → RED.
func TestPK2ThesisDeathExitDormantPlan(t *testing.T) {
	telemetry.ResetGateBlocksForTest()
	off := withdrawFixture(t, "pk2-off", nil)
	pk2SeedPlannerPosition(t, off, "dormant", "i", "", "")
	off.at.monitorTick(off.now.Add(time.Minute))
	off.w.flush(t)
	if n := dlClosePositionFrames(off.w); n != 0 {
		t.Fatalf("OFF = today: a dormant plan's position is held (%d close frames)", n)
	}

	f := withdrawFixture(t, "pk2-dormant", func(c *store.StrategyConfig) { c.DayPlan.PlannerExitOnThesisDeath = "all" })
	pk2SeedPlannerPosition(t, f, "dormant", "i", "", "")
	logBuf := captureTraderLog(t)
	f.at.monitorTick(f.now.Add(time.Minute))
	f.w.waitFor(t, "the thesis-death exit (close_position)", func(fr wdFrame) bool { return fr.Type == ntwire.FrameClosePosition })
	if !strings.Contains(logBuf.String(), "thesis-death EXIT") || !strings.Contains(logBuf.String(), "reason=plan_dormant") {
		t.Fatalf("the exit names its reason:\n%s", logBuf.String())
	}
	if n := dlGateBlocks(f.at.id)[ThesisDeathExitClass]; n != 1 {
		t.Fatalf("counted once: %d", n)
	}
	// invalidation_only never exits on dormancy.
	g := withdrawFixture(t, "pk2-invonly", func(c *store.StrategyConfig) { c.DayPlan.PlannerExitOnThesisDeath = "invalidation_only" })
	pk2SeedPlannerPosition(t, g, "dormant", "i", "", "")
	g.at.monitorTick(g.now.Add(time.Minute))
	g.w.flush(t)
	if n := dlClosePositionFrames(g.w); n != 0 {
		t.Fatalf("invalidation_only ignores dormancy (%d close frames)", n)
	}
}

// A newer version of the same chain with the OPPOSITE bias exits under "all".
func TestPK2ThesisDeathExitOppositePlan(t *testing.T) {
	f := withdrawFixture(t, "pk2-opp", func(c *store.StrategyConfig) { c.DayPlan.PlannerExitOnThesisDeath = "all" })
	s := pk2SeedPlannerPosition(t, f, "active", "i", "", "")
	short, _ := json.Marshal(kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "short"}})
	if _, err := f.st.Plan().AppendPlan(&store.PlanDB{PlanID: s.planID, TradeDate: "2026-09-11", Session: "NY", StrategyID: f.at.id, Doc: string(short)}); err != nil {
		t.Fatal(err)
	}
	logBuf := captureTraderLog(t)
	f.at.monitorTick(f.now.Add(time.Minute))
	f.w.waitFor(t, "the thesis-death exit (close_position)", func(fr wdFrame) bool { return fr.Type == ntwire.FrameClosePosition })
	if !strings.Contains(logBuf.String(), "reason=opposite_plan") {
		t.Fatalf("the exit names the opposite plan:\n%s", logBuf.String())
	}
}

// invalidation_only: a 5m close completed AFTER the fill below the authored
// "5m close below 95" exits. MUTANT: drop the v.Invalidated check → the
// above-threshold fixture exits too → RED.
func TestPK2ThesisDeathExitAuthoredInvalidation(t *testing.T) {
	f := withdrawFixture(t, "pk2-inv", func(c *store.StrategyConfig) { c.DayPlan.PlannerExitOnThesisDeath = "invalidation_only" })
	pk2SeedPlannerPosition(t, f, "active", "5m close below 95", "", "")
	t1 := f.now.Add(time.Minute) // window 14:55–15:00 UTC, after the 14:50 fill
	tape := func(close float64) []market.Kline {
		out := []market.Kline{}
		end := t1.UnixMilli() / 300000 * 300000
		for ms := end - 300000; ms < end; ms += 60000 {
			out = append(out, market.Kline{OpenTime: ms, CloseTime: ms + 59999, Open: close, High: close + 1, Low: close - 1, Close: close})
		}
		return out
	}
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return tape(97) }
	f.at.monitorTick(t1)
	f.w.flush(t)
	if n := dlClosePositionFrames(f.w); n != 0 {
		t.Fatalf("a close ABOVE the line is not an invalidation (%d close frames)", n)
	}
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return tape(93) }
	f.at.monitorTick(t1.Add(10 * time.Second))
	f.w.waitFor(t, "the invalidation exit (close_position)", func(fr wdFrame) bool { return fr.Type == ntwire.FrameClosePosition })
}

// NEVER for a mentor arm, a manual position, or a mentor-mode trader.
func TestPK2ThesisDeathNeverMentorOrManual(t *testing.T) {
	for _, c := range []struct {
		name, armOrigin, posOrigin string
		mentorMode                 bool
	}{
		{"mentor-arm", store.ArmOriginMentor, "", false},
		{"manual", "", store.OriginManual, false},
		{"mentor-mode", "", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := withdrawFixture(t, "pk2-"+c.name, func(sc *store.StrategyConfig) {
				sc.DayPlan.PlannerExitOnThesisDeath = "all"
				sc.RiskControl.MentorMode = c.mentorMode
			})
			pk2SeedPlannerPosition(t, f, "dormant", "i", c.armOrigin, c.posOrigin)
			f.at.monitorTick(f.now.Add(time.Minute))
			f.w.flush(t)
			if n := dlClosePositionFrames(f.w); n != 0 {
				t.Fatalf("%s must never be exited by the thesis-death switch (%d close frames)", c.name, n)
			}
		})
	}
}

// ── THE FIVE STRICT TRADES (report fixtures) ────────────────────────────────

// What PK-1 would have done on 649/650/652/653/654: r_to_obstacle from the
// report's entry/stop/obstacle numbers, through roomVerdictFor
// (kernel.EconomicsFor). At the suggested 1.0 every one is refused.
func TestPK1OnTheFiveStrictTrades(t *testing.T) {
	type tr struct {
		id                           string
		dir                          string
		entry, stop, target, obstacl float64
		wantR                        float64
	}
	for _, c := range []tr{
		{"649", "short", 30843.5, 30873, 30744.5, 30828.75, 0.50},
		{"650", "long", 30798.75, 30760.5, 30944.75, 30822, 0.61},
		{"652", "short", 30802, 30849.25, 30705.75, 30776, 0.55},
		{"653", "long", 30517, 30436, 30720.25, 30537.25, 0.25},
		{"654", "short", 30456.5, 30551.25, 30257.5, 30416.75, 0.42},
	} {
		o := c.obstacl
		sc := kernel.PlanScenario{ID: c.id, Direction: c.dir,
			Arm:       &kernel.PlanArmSpec{Enabled: true, Entry: c.entry, Stop: c.stop, Target: c.target},
			Economics: &kernel.ScenarioEconomics{FirstObstacle: &kernel.ScenarioObstacle{Price: &o, Level: "obstacle"}}}
		v := roomVerdictFor(sc, store.RoomMinRDefault)
		if !v.Known || v.R < c.wantR-0.01 || v.R > c.wantR+0.01 || !v.Short {
			t.Fatalf("%s: r_to_obstacle %.3f (want %.2f) must be refused at 1.0: %+v", c.id, v.R, c.wantR, v)
		}
		t.Logf("%s r_to_obstacle %.2f < 1.00 → REFUSED at write (room_gate)", c.id, v.R)
	}
}
