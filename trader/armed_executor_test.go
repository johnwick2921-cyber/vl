package trader

import (
	"encoding/json"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// Wave 2 armed orders — Phase 1 manager tests: gate-at-arm, ledger upsert,
// cancel on dormant (1.4), cancel on no active plan (2.4).

func armedDoc() string {
	d := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels: []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: []kernel.PlanScenario{{ID: "S1", Trigger: "t", Condition: "fvg_entry", Direction: "long",
			TargetChain: []float64{110}, Invalid: "i", Quality: "B",
			Fvg: &kernel.PlanFvgEntry{Lo: 98, Hi: 102, CE: 100, EntryMode: "ce", DisplacementATR: 1.5, OriginLevel: "ONH", Direction: "long"},
			Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}},
		},
		NoTrade: []string{}, DeathCondition: "n/a",
	}
	blob, _ := json.Marshal(d)
	return string(blob)
}

func TestArmedOrderUpsertAndGateRR(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	// ONE SETUP (dispatch 102, 2026-09-11): this fixture exercises the WIDE book
	// (a non-reject play / no map); the switch OFF restores it byte-identically (E2).
	oneSetupOff(&cfg)
	// 0C shadow demotion (2026-08-31): armedDoc's S1 is fvg_entry, which resolves
	// SHADOW by default — the arm seam would refuse it and this R:R-gate fixture
	// would never row up. Declare fvg_entry live for THIS fixture: it tests the
	// R:R gate, not the shadow map (the shadow map has its own fixtures).
	cfg.DayPlan.ConditionStatus = map[string]string{"fvg_entry": "live"}
	cfg.RiskControl = store.RiskControlConfig{MinRiskRewardRatio: 1.5}
	at, st := resetTrader(t, cfg)
	// W26 extra (test-only P2): session selection and the arm run at the SAME
	// pinned armable instant — never time.Now(), which at 08:19 CT (LONDON
	// last-entry) picked a cut session while the arm clock searched elsewhere.
	now := armTestClock(t, at)
	sess, ok := at.sessionRegistry(now).ActiveSession(now)
	if !ok {
		t.Skip("no active session right now")
	}
	cfg.DayPlan.SessionsEnabled = []string{sess.Name}
	trueV := true
	cfg.DayPlan.Sessions = []store.DayPlanSessionOverride{{Session: sess.Name, Enable: &trueV}}
	td, _ := kernel.PlanChainTradeDate(sess, now)
	pid := store.MakePlanIDForTrader(at.id, td, sess.Name)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: sess.Name, StrategyID: at.id, Lifecycle: "active", Doc: armedDoc(), CreatedAt: now.Add(-30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	installActivePlanProviderAt(at, st, func() time.Time { return now })

	at.maybeManageArmedOrdersAt(nil, now)

	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("arm rows = %d err=%v want 1 (R:R 2.0 ≥ min 1.5 must pass at arm time)", len(rows), err)
	}
	if rows[0].EntryClass != "armed_fill" || rows[0].Scenario != "S1" {
		t.Fatalf("armed row lineage wrong: %+v", rows[0])
	}
}

func TestArmedGateRefusesBadRR(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	// SUPERSEDED 2026-09-03 by R1 (ONE R:R FLOOR). This fixture asserted the
	// two-floor design directly — "a 4.0 GLOBAL floor does NOT block a 2.0 arm"
	// — which is exactly what the owner ruled away: the Studio
	// min_risk_reward_ratio now governs BOTH paths and ARM_MIN_RR is deleted.
	// The INTENT survives unchanged: an arm at R:R 2.0 passes when the floor is
	// 2.0, and is refused when the floor is raised above it. Only the SOURCE of
	// the floor moved, from an env var to the Studio value.
	at.config.StrategyConfig.RiskControl = store.RiskControlConfig{MinRiskRewardRatio: 2}
	sc := kernel.PlanScenario{ID: "S1", Condition: "reject", Direction: "long", Quality: "A",
		Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}} // R:R 2.0
	if v := at.armGateVerdict(sc, "long", nil, 0, "", at.config.StrategyConfig, "NY"); v != "" {
		t.Fatalf("arm R:R 2.0 must pass when the Studio floor is 2.0, got %q", v)
	}
	// Raise it in STUDIO — the one place — and the same arm is refused.
	at.config.StrategyConfig.RiskControl.MinRiskRewardRatio = 4
	if v := at.armGateVerdict(sc, "long", nil, 0, "", at.config.StrategyConfig, "NY"); v == "" {
		t.Fatal("arm R:R 2.0 below a Studio floor of 4 must be refused at arm time")
	}
	// And the deleted env var must NOT resurrect a second floor.
	t.Setenv("ARM_MIN_RR", "1")
	if v := at.armGateVerdict(sc, "long", nil, 0, "", at.config.StrategyConfig, "NY"); v == "" {
		t.Fatal("ARM_MIN_RR is deleted — it must not lower the Studio floor")
	}
	at.config.StrategyConfig.RiskControl.MinRiskRewardRatio = 2
	// quality floor: a C scenario below min_scenario_quality=B must refuse.
	scC := kernel.PlanScenario{ID: "S1", Condition: "reject", Direction: "long", Quality: "C",
		Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 95, Target: 110}}
	if v := at.armGateVerdict(scC, "long", nil, 0, "B", at.config.StrategyConfig, "NY"); v == "" {
		t.Fatal("C-quality below min_scenario_quality=B must be refused")
	}
	// plan_mode direction against bias
	at.config.StrategyConfig.DayPlan.PlanMode = "direction"
	sc.Direction = "short"
	if v := at.armGateVerdict(sc, "long", nil, 0, "", at.config.StrategyConfig, "NY"); v == "" {
		t.Fatal("short arm against long bias under plan_mode=direction must be refused")
	}
}

func TestArmedCancelOnDormant(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	at, st := resetTrader(t, cfg)
	// Same pinned armable instant as the arm run (W26 extra): session selection
	// and the arm must not diverge across the LONDON last-entry cut.
	now := armTestClock(t, at)
	sess, ok := at.sessionRegistry(now).ActiveSession(now)
	if !ok {
		t.Skip("no active session right now")
	}
	cfg.DayPlan.SessionsEnabled = []string{sess.Name}
	trueV := true
	cfg.DayPlan.Sessions = []store.DayPlanSessionOverride{{Session: sess.Name, Enable: &trueV}}
	td, _ := kernel.PlanChainTradeDate(sess, now)
	pid := store.MakePlanIDForTrader(at.id, td, sess.Name)
	v, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: sess.Name, StrategyID: at.id, Lifecycle: "active", Doc: armedDoc(), CreatedAt: now.Add(-30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: pid, Version: v, Session: sess.Name, Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "armed"}); err != nil {
		t.Fatal(err)
	}
	// go dormant → the manager must cancel instantly (1.4).
	if err := st.Plan().UpdatePlanLifecycle(pid, v, "dormant", "dormant:flip-condition: test"); err != nil {
		t.Fatal(err)
	}
	installActivePlanProviderAt(at, st, func() time.Time { return now })
	at.maybeManageArmedOrdersAt(nil, now)
	rows, err := st.ArmedOrders().ListNonTerminal(at.id)
	if err != nil || len(rows) != 0 {
		t.Fatalf("non-terminal rows = %d err=%v — dormant must cancel ALL arms", len(rows), err)
	}
}

func TestArmedCancelOnNoActivePlan(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	now := time.Now()
	if err := st.ArmedOrders().UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: "x:NY", Version: 1, Session: "NY", Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "armed"}); err != nil {
		t.Fatal(err)
	}
	_ = now
	at.maybeManageArmedOrdersAt(nil, armTestClock(t, at))
	if rows, err := st.ArmedOrders().ListNonTerminal(at.id); err != nil || len(rows) != 0 {
		t.Fatalf("no active plan must cancel arms (rows=%d err=%v)", len(rows), err)
	}
}

// PHASE 4 — the order_update event machine: filled → filled+lineage path,
// rejected/cancelled → disarmed with a reason (never silent).
func TestArmedOrderUpdateTransitions(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	ledger := st.ArmedOrders()
	if err := ledger.UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-08-27:NY:trader-1", Version: 1, Session: "NY", Scenario: "S1", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "working", SignalID: "sig-1"}); err != nil {
		t.Fatal(err)
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: "sig-1", State: "filled", FillPrice: 100.5}, ledger)
	rows, _ := ledger.ListForPlan("2026-08-27:NY:trader-1")
	if len(rows) != 1 || rows[0].State != "filled" {
		t.Fatalf("filled transition: %+v", rows)
	}
	// W117 F2 — a filled row is TERMINAL (the R2 CAS): the old test demoted it
	// back to working with SetState; that move is exactly what the guard now
	// refuses (a fill that became a position is never unwound). The reject leg
	// uses its own working row instead.
	if err := ledger.UpsertArm(&store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-08-27:NY:trader-1", Version: 1, Session: "NY", Scenario: "S2", Side: "long", EntryPx: 101, StopPx: 96, TargetPx: 111, State: "working", SignalID: "sig-2"}); err != nil {
		t.Fatal(err)
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: "sig-2", State: "rejected"}, ledger)
	rows, _ = ledger.ListForPlan("2026-08-27:NY:trader-1")
	for _, r := range rows {
		if r.SignalID == "sig-2" && (r.State != "rejected" || r.StateReason != "reason unavailable (NT8 frame omitted reason)") {
			t.Fatalf("reject must disarm with a reason: %+v", r)
		}
	}
}

// PHASE 4 — short twin of the R:R arm gate (long side covered elsewhere).
func TestArmedGateRRShortTwin(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	// SUPERSEDED 2026-09-03 by R1: the arm floor is the Studio value, so the
	// fixture states it rather than relying on a deleted env default of 2.0.
	at.config.StrategyConfig.RiskControl.MinRiskRewardRatio = 2
	sc := kernel.PlanScenario{ID: "S1", Condition: "reject", Direction: "short", Quality: "A",
		Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 105, Target: 90}} // R:R (100−90)/(105−100)=2.0
	// S1: default arm floor 2.0 → a 2.0-R short arm passes (no config floor).
	if v := at.armGateVerdict(sc, "short", nil, 0, "", at.config.StrategyConfig, "NY"); v != "" {
		t.Fatalf("short arm R:R 2.0 must pass when the Studio floor is 2.0, got %q", v)
	}
	// R1: raise the floor in STUDIO, the one place, not via a deleted env var.
	at.config.StrategyConfig.RiskControl.MinRiskRewardRatio = 3
	if v := at.armGateVerdict(sc, "short", nil, 0, "", at.config.StrategyConfig, "NY"); v == "" {
		t.Fatal("short arm R:R 2.0 below a Studio floor of 3 must be refused")
	}
	sc.Arm.Target = 85 // R:R 3.0 → pass
	if v := at.armGateVerdict(sc, "short", nil, 0, "", at.config.StrategyConfig, "NY"); v != "" {
		t.Fatalf("short arm R:R 3.0 must pass, got %q", v)
	}
}

// PHASE 4 — churn guard predicate (2.1): only a ≥2-tick SL/TP re-spec re-modifies.
func TestArmedChurnPredicate(t *testing.T) {
	tick := 0.25
	if churnNeedsModify(95, 110, 95.25, 110, tick) {
		t.Fatal("1-tick SL move must NOT trigger the churn guard")
	}
	if !churnNeedsModify(95, 110, 95.5, 110, tick) {
		t.Fatal("2-tick SL move must trigger the churn guard")
	}
	if !churnNeedsModify(95, 110, 95, 110.75, tick) {
		t.Fatal("3-tick TP move must trigger the churn guard")
	}
	if churnNeedsModify(95, 110, 95, 110, tick) {
		t.Fatal("no-op re-spec must not trigger")
	}
}

// PHASE 4 — reconnect predicate: stale only past the full window.
func TestArmedStalePredicate(t *testing.T) {
	now := time.Now()
	if workingStale(now.Add(-14*time.Minute), now, 15) {
		t.Fatal("14m < 15m stale window must be fresh")
	}
	if !workingStale(now.Add(-16*time.Minute), now, 15) {
		t.Fatal("16m > 15m stale window must be stale")
	}
}

// SUPERSEDED AND MIGRATED, not weakened.
//
// This asserted the PHASE 4 contract: a working row with no order_update for the
// stale window gets cancelled. That contract was the defect. order_update is an
// event frame, so a resting limit nobody touches is silent BY DESIGN, and the
// reaper was reading silence as death — then acting on it by cancelling a live
// order at the broker.
//
// The row is still SELECTED by silence; the verdict now comes from the broker's
// book. With no snapshot cache (this fixture's state) the verdict is UNKNOWN, and
// UNKNOWN cancels NOTHING — cancelling on absence of information is precisely
// what was removed. The positive case (a fresh book that omits the order, which
// DOES cancel) is pinned in reaper_snapshot_test.go.
func TestArmedReconcileStaleWorking_NoBookCancelsNothing(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	ledger := st.ArmedOrders()
	now := time.Now()
	row := &store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-08-27:NY:trader-1", Version: 1, Session: "NY", Scenario: "S1",
		Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110, State: "working", SignalID: "sig-9", UpdatedAt: now.Add(-20 * time.Minute)}
	if err := ledger.UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	fresh := &store.ArmedOrderDB{TraderID: at.id, PlanID: "2026-08-27:NY:trader-1", Version: 1, Session: "NY", Scenario: "S2",
		Side: "long", EntryPx: 101, StopPx: 96, TargetPx: 111, State: "working", SignalID: "sig-10", UpdatedAt: now}
	if err := ledger.UpsertArm(fresh); err != nil {
		t.Fatal(err)
	}
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		t.Fatal(err)
	}
	var cancelled []string
	at.reconcileStaleWorking(ledger, rows, now, 15, func(sid string) { cancelled = append(cancelled, sid) })
	// THE INVERSION: 20 minutes of silence with no broker book must cancel
	// NOTHING. Under the old contract this asserted exactly sig-9.
	if len(cancelled) != 0 {
		t.Fatalf("cancelFn = %v, want NOTHING — with no snapshot the link cannot answer, and silence is not death", cancelled)
	}
	rows, _ = ledger.ListForPlan("2026-08-27:NY:trader-1")
	byScenario := map[string]string{}
	for _, r := range rows {
		byScenario[r.Scenario] = r.State
	}
	if byScenario["S1"] != "working" {
		t.Fatalf("stale S1 state = %q, want STILL working — an unadjudicated row is left alone", byScenario["S1"])
	}
	if byScenario["S2"] != "working" {
		t.Fatalf("fresh S2 state = %q, want working", byScenario["S2"])
	}
}

// TestLimitMarketableWrongSide — the pure wrong-side predicate behind the
// placement guard that killed the 2026-08-30 S2 re-place loop (a marketable
// limit fills instantly at a worse price and must never be placed).
func TestLimitMarketableWrongSide(t *testing.T) {
	cases := []struct {
		price, entry float64
		side         string
		want         bool
	}{
		{29347.00, 29371.50, "long", true},  // market below a buy limit → marketable
		{29380.00, 29371.50, "long", false}, // market above → resting pullback
		{29347.00, 29371.50, "short", false},
		{29390.00, 29371.50, "short", true}, // market above a sell limit → marketable
		{0, 29371.50, "long", false},
		{29347.00, 0, "long", false},
		{29347.00, 29371.50, "LONG", true}, // case-insensitive
		{29390.00, 29371.50, "SHORT", true},
	}
	for _, c := range cases {
		if got := limitMarketableWrongSide(c.price, c.entry, c.side); got != c.want {
			t.Fatalf("price=%.2f entry=%.2f side=%s: got %v want %v", c.price, c.entry, c.side, got, c.want)
		}
	}
}

// TestMaterializeArmedEntryF3 — F3 (2026-08-30): the fill-time materialization
// creates the OPEN row from the armed fill so the sub-60s round-trip is
// ledger-visible (the priced close finds its open row on the normal path).
func TestMaterializeArmedEntryF3(t *testing.T) {
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	at, st := resetTrader(t, cfg)
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "2026-08-30:ASIA:trader", Version: 2, Session: "ASIA",
		Scenario: "S2", Side: "long", EntryPx: 29371.5, StopPx: 29350.0, TargetPx: 29420.0,
		State: "filled", SignalID: "sig-f3", FillPrice: 29347.25,
	}
	u := ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-f3", Account: "Sim101", FillPrice: 29347.25}
	at.materializeArmedEntry(row, u)
	// FIX 3 (class 27): rows are written with the UPPERCASE canonical side.
	pos, err := st.Position().GetOpenPositionBySymbol(at.id, at.futuresSymbol(), "LONG")
	if err != nil || pos == nil {
		t.Fatalf("open row not materialized: %v", err)
	}
	if pos.EntryOrderID != "sig-f3" || pos.EntryPrice != 29347.25 || pos.Source != "armed_entry" {
		t.Fatalf("row = %+v", pos)
	}
	if pos.PlanID != "2026-08-30:ASIA:trader" || pos.PlanVersion != 2 || pos.CitedScenarioID != "S2" || pos.PlanSession != "ASIA" {
		t.Fatalf("plan attribution missing: %+v", pos)
	}
	// Idempotent: a second call must not duplicate.
	at.materializeArmedEntry(row, u)
	opens, _ := st.Position().GetOpenPositions(at.id)
	if len(opens) != 1 {
		t.Fatalf("open count = %d, want 1", len(opens))
	}
}
