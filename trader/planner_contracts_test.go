// dispatch-31 (owner order 2026-10-16 "set to 4 at least, sell 2 first so it's
// equal") — the PLANNER trader's contracts per entry. Before this wave every
// non-mentor arm placed quantity 1 (armed_executor.go "Non-mentor rows stay 1"),
// so a plan that wanted to sell part and run the rest could never split. This
// file pins the TWO production call sites: (1) the AUTHORING stamp
// (stampPlannerSplit — the row carries its total + equal split) and (2) the
// PLACEMENT through the fake wire (placeOneStopEntry sends quantity 4 as leg 1
// = 2 at the first obstacle + leg 2 = 2 at the final target). The mentor's size
// table is untouched.

package trader

import (
	"os"
	"testing"

	"vl/kernel"
	"vl/store"
)

func firstObstaclePtr(v float64) *float64 { return &v }

// plannerSplitScenario is a planner scenario whose target chain names a first
// target (target_chain[0] = 105) below the arm's final target (110), and whose
// economics ALSO carry a first obstacle (101) — the FALLBACK the CTO ruling
// (fold-564-b D5) uses only when target_chain[0] is unusable.
func plannerSplitScenario(id string, firstObstacle float64) kernel.PlanScenario {
	return kernel.PlanScenario{
		ID: id, Trigger: "t", Condition: "reject", Direction: "long",
		TargetChain: []float64{105, 110}, Invalid: "i", Quality: "B",
		Economics: &kernel.ScenarioEconomics{Version: 1, FirstObstacle: &kernel.ScenarioObstacle{
			Price: firstObstaclePtr(firstObstacle), Level: "L1", Response: "reduce",
		}},
		Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 98, Target: 110},
	}
}

// TestFirstObstaclePrice — the first_obstacle price; 0 when absent.
func TestFirstObstaclePrice(t *testing.T) {
	if got := firstObstaclePrice(plannerSplitScenario("S1", 101)); got != 101 {
		t.Fatalf("firstObstaclePrice = %.2f, want 101", got)
	}
	if got := firstObstaclePrice(kernel.PlanScenario{ID: "S2"}); got != 0 {
		t.Fatalf("firstObstaclePrice (no economics) = %.2f, want 0", got)
	}
}

// TestPlannerLeg1Target — the CTO RULING (fold-564-b D5): leg 1 exits at the
// plan's FIRST TARGET = target_chain[0], NOT the first obstacle. The first
// obstacle is only the fallback, and only when it is at least +1R away. A leg-1
// price that equals or passes the final target degrades to a single bracket (0).
func TestPlannerLeg1Target(t *testing.T) {
	// target_chain[0] = 105 wins over the first obstacle 101 (both ≥ +1R: risk 2).
	if got := plannerLeg1Target(plannerSplitScenario("S1", 101), "long", 100, 98, 110); got != 105 {
		t.Fatalf("leg1 target = %.2f, want 105 (target_chain[0], not the obstacle 101)", got)
	}
	// no target_chain → the first obstacle, when ≥ +1R away.
	noChain := plannerSplitScenario("S2", 103)
	noChain.TargetChain = nil
	if got := plannerLeg1Target(noChain, "long", 100, 98, 110); got != 103 {
		t.Fatalf("leg1 target (no chain) = %.2f, want 103 (the +1R first obstacle)", got)
	}
	// the first obstacle is 5.5 pts away (< 1R of 2) and target_chain[0] is the
	// final target — neither is a usable first target → single bracket (0).
	tooClose := plannerSplitScenario("S3", 105.5)
	tooClose.TargetChain = []float64{110}
	if got := plannerLeg1Target(tooClose, "long", 100, 98, 110); got != 0 {
		t.Fatalf("leg1 target (no usable first target) = %.2f, want 0", got)
	}
	// target_chain[0] EQUALS the final target → one 4-lot bracket (0).
	eqFinal := plannerSplitScenario("S4", 101)
	eqFinal.TargetChain = []float64{110}
	if got := plannerLeg1Target(eqFinal, "long", 100, 98, 110); got != 0 {
		t.Fatalf("leg1 target (tc0 == final) = %.2f, want 0", got)
	}
}

// TestPlannerContractsResolver — unset/0 → 1 (today); an explicit value wins.
func TestPlannerContractsResolver(t *testing.T) {
	if plannerContracts(nil) != 1 {
		t.Fatal("plannerContracts(nil) must be 1 (today's single-contract arm)")
	}
	if plannerContracts(&store.RiskControlConfig{}) != 1 {
		t.Fatal("plannerContracts(unset) must be 1")
	}
	if plannerContracts(&store.RiskControlConfig{PlannerContracts: 4}) != 4 {
		t.Fatal("plannerContracts(4) must be 4")
	}
}

// TestPlannerSplitStamp — the AUTHORING call site: a planner single arm with
// planner_contracts=4 is stamped Contracts=4, Leg1Qty=2, Leg1TP=first obstacle.
// Mutant: drop the stamp (or stamp 1) → RED.
func TestPlannerSplitStamp(t *testing.T) {
	rc := &store.RiskControlConfig{PlannerContracts: 4}
	row := &store.ArmedOrderDB{Scenario: "S1", Side: "long", TargetPx: 110}
	stampPlannerSplit(row, rc, false, 0, 101, 0)
	if row.Contracts == nil || *row.Contracts != 4 {
		t.Fatalf("Contracts = %v, want 4", row.Contracts)
	}
	if row.Leg1Qty == nil || *row.Leg1Qty != 2 {
		t.Fatalf("Leg1Qty = %v, want 2", row.Leg1Qty)
	}
	if row.Leg1TP != 101 {
		t.Fatalf("Leg1TP = %.2f, want 101 (the first obstacle)", row.Leg1TP)
	}

	// an unset knob leaves the row untouched (today's single-contract arm).
	unset := &store.ArmedOrderDB{Scenario: "S2"}
	stampPlannerSplit(unset, &store.RiskControlConfig{}, false, 0, 101, 0)
	if unset.Contracts != nil || unset.Leg1Qty != nil || unset.Leg1TP != 0 {
		t.Fatalf("unset knob must leave the row untouched: %+v", unset)
	}

	// a machine (Picture) arm and a 2-leg sweep_reclaim arm are never stamped.
	m := &store.ArmedOrderDB{Scenario: "P1"}
	stampPlannerSplit(m, rc, true, 0, 101, 0)
	if m.Contracts != nil || m.Leg1Qty != nil {
		t.Fatalf("a machine arm must not be stamped: %+v", m)
	}
	two := &store.ArmedOrderDB{Scenario: "S3"}
	stampPlannerSplit(two, rc, false, 2, 101, 0)
	if two.Contracts != nil || two.Leg1Qty != nil {
		t.Fatalf("a 2-leg sweep_reclaim arm must not be stamped: %+v", two)
	}

	// no first obstacle → the total is still stamped (one bracket of `total` at
	// the final target), never a split with a fabricated leg-1 price.
	noFo := &store.ArmedOrderDB{Scenario: "S4"}
	stampPlannerSplit(noFo, rc, false, 0, 0, 0)
	if noFo.Contracts == nil || *noFo.Contracts != 4 || noFo.Leg1Qty == nil || *noFo.Leg1Qty != 2 || noFo.Leg1TP != 0 {
		t.Fatalf("no first obstacle: Contracts=%v Leg1Qty=%v Leg1TP=%.2f — want total 4, split stamped, Leg1TP 0 (wire drops the split)",
			noFo.Contracts, noFo.Leg1Qty, noFo.Leg1TP)
	}
}

// TestPlannerContractsCeilingRefusesATypo — plannerContracts caps a typo of 40
// at the hard ceiling (10). Mutant: drop the ceiling → 40 is stamped → RED.
func TestPlannerContractsCeilingRefusesATypo(t *testing.T) {
	if got := plannerContracts(&store.RiskControlConfig{PlannerContracts: 40}); got != plannerContractsCeiling {
		t.Fatalf("plannerContracts(40) = %d, want %d (the hard ceiling)", got, plannerContractsCeiling)
	}
	if got := plannerContracts(&store.RiskControlConfig{PlannerContracts: 10}); got != 10 {
		t.Fatalf("plannerContracts(10) = %d, want 10 (at the ceiling, unchanged)", got)
	}
}

// TestPlannerSplitStampRespectsTheContractsClamp — the authoring stamp NEVER
// bypasses the always-on resolveMaxContracts() clamp. For "mnq 2" the clamp
// resolves to 1 (max_contracts_per_order 2, Stage-A ceiling 1), so
// planner_contracts=4 must leave the row UNSTAMPED (today's single-contract
// arm), not stamp a 4 the size cap would never allow. Mutant: drop the clamp →
// stamped 4 → RED (the gate's probe TestGate564_StampRespectsTheContractsClamp).
func TestPlannerSplitStampRespectsTheContractsClamp(t *testing.T) {
	rc := &store.RiskControlConfig{PlannerContracts: 4}
	clamped := &store.ArmedOrderDB{Scenario: "S1", Side: "long", TargetPx: 110}
	stampPlannerSplit(clamped, rc, false, 0, 101, 1) // resolveMaxContracts = 1
	if clamped.Contracts != nil || clamped.Leg1Qty != nil || clamped.Leg1TP != 0 {
		t.Fatalf("a clamp of 1 must leave the row unstamped (today's single-contract arm): %+v", clamped)
	}

	// a clamp of 2 (max_contracts_per_order=2, Stage-A cap raised) halves 4 → 2.
	half := &store.ArmedOrderDB{Scenario: "S2", Side: "long", TargetPx: 110}
	stampPlannerSplit(half, rc, false, 0, 101, 2)
	if half.Contracts == nil || *half.Contracts != 2 || half.Leg1Qty == nil || *half.Leg1Qty != 1 || half.Leg1TP != 101 {
		t.Fatalf("a clamp of 2 must stamp 2 = 1+1: %+v", half)
	}

	// a clamp at or above the knob leaves it intact.
	full := &store.ArmedOrderDB{Scenario: "S3", Side: "long", TargetPx: 110}
	stampPlannerSplit(full, rc, false, 0, 101, 4)
	if full.Contracts == nil || *full.Contracts != 4 || full.Leg1Qty == nil || *full.Leg1Qty != 2 || full.Leg1TP != 101 {
		t.Fatalf("a clamp of 4 must stamp 4 = 2+2: %+v", full)
	}
}

// TestPlannerSplitStampRefusesLeg1AtOrPastFinalTarget — a leg-1 price that
// would EQUAL or PASS the final target (both legs exit at one price) is refused
// as if absent: the total is stamped, the split degrades to a single bracket.
func TestPlannerSplitStampRefusesLeg1AtOrPastFinalTarget(t *testing.T) {
	rc := &store.RiskControlConfig{PlannerContracts: 4}
	long := &store.ArmedOrderDB{Scenario: "S1", Side: "long", TargetPx: 110}
	stampPlannerSplit(long, rc, false, 0, 115, 0) // 115 > 110 (past)
	if long.Contracts == nil || *long.Contracts != 4 || long.Leg1Qty == nil || *long.Leg1Qty != 2 || long.Leg1TP != 0 {
		t.Fatalf("a long leg-1 past the target must drop the split: %+v", long)
	}
	eq := &store.ArmedOrderDB{Scenario: "S2", Side: "long", TargetPx: 110}
	stampPlannerSplit(eq, rc, false, 0, 110, 0) // 110 == 110 (equal)
	if eq.Contracts == nil || *eq.Contracts != 4 || eq.Leg1TP != 0 {
		t.Fatalf("a long leg-1 EQUAL to the target must drop the split: %+v", eq)
	}
	short := &store.ArmedOrderDB{Scenario: "S3", Side: "short", TargetPx: 100}
	stampPlannerSplit(short, rc, false, 0, 95, 0) // 95 < 100 (past)
	if short.Contracts == nil || *short.Contracts != 4 || short.Leg1TP != 0 {
		t.Fatalf("a short leg-1 past the target must drop the split: %+v", short)
	}
}

// TestPlannerArmWireQty — the placement-side resolver reads the STAMP and
// RE-CLAMPS at send: a stamped row wires its total + split when the cap still
// allows it; an unstamped row wires 1/0; a stale stamp is clamped back down.
func TestPlannerArmWireQty(t *testing.T) {
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4}}}}
	four, two, tp := 4, 2, 105.0
	if q, l1, l1tp := at.plannerArmWireQty(store.ArmedOrderDB{Contracts: &four, Leg1Qty: &two, Leg1TP: tp}); q != 4 || l1 != 2 || l1tp != 105 {
		t.Fatalf("stamped row wired qty %.0f leg1 %d @ %.2f, want 4/2/105", q, l1, l1tp)
	}
	if q, l1, l1tp := at.plannerArmWireQty(store.ArmedOrderDB{}); q != 1 || l1 != 0 || l1tp != 0 {
		t.Fatalf("unstamped row wired qty %.0f leg1 %d @ %.2f, want 1/0/0", q, l1, l1tp)
	}
	// a stamped total with NO leg-1 TP (single 4-lot bracket) wires 4/0.
	if q, l1, _ := at.plannerArmWireQty(store.ArmedOrderDB{Contracts: &four, Leg1Qty: &two}); q != 4 || l1 != 0 {
		t.Fatalf("no leg-1 TP: wired qty %.0f leg1 %d, want 4/0", q, l1)
	}
	// RE-CLAMP: the stamp says 4 but the Stage-A cap moved back to 1 → wire 1.
	if err := os.Unsetenv("STAGE_A_CONTRACT_CAP"); err != nil {
		t.Fatal(err)
	}
	clamped := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4}}}}
	if q, l1, l1tp := clamped.plannerArmWireQty(store.ArmedOrderDB{Contracts: &four, Leg1Qty: &two, Leg1TP: tp}); q != 1 || l1 != 0 || l1tp != 0 {
		t.Fatalf("a stale 4 must re-clamp to the Stage-A 1 at send: wired qty %.0f leg1 %d @ %.2f, want 1/0/0", q, l1, l1tp)
	}
}

// plannerSplitRow builds a placeable planner stop-entry row that the authoring
// stamp would have produced for planner_contracts=4 (first obstacle 29660).
func plannerSplitRow(at *AutoTrader, contracts, leg1 int, leg1TP float64) store.ArmedOrderDB {
	return store.ArmedOrderDB{
		ID: 9, TraderID: at.id, PlanID: "2026-09-11:NY", Version: 1,
		Session: "NY", Scenario: "S1", Side: "long",
		EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
		Contracts: &contracts, Leg1Qty: &leg1, Leg1TP: leg1TP,
		// no Origin → a planner arm, not a mentor arm.
	}
}

// TestPlannerArmWiresSplit4 — the PLACEMENT call site: a stamped planner row
// sends quantity 4 as leg 1 = 2 at the first obstacle + leg 2 = 2 at the final
// target. Mutant: send 1 again (or drop the leg split) → RED.
func TestPlannerArmWiresSplit4(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4}})
	at.id = "planner-split"
	r := plannerSplitRow(at, 4, 2, 29660)
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 {
		t.Fatalf("wire calls = %d, want 1", len(pl.calls))
	}
	if pl.calls[0].qty != 4 {
		t.Fatalf("planner arm wired qty %.0f, want 4 (the size must reach the order)", pl.calls[0].qty)
	}
	if pl.calls[0].leg1Qty != 2 || pl.calls[0].leg1TP != 29660 {
		t.Fatalf("planner arm wired leg1Qty=%d leg1TP=%.2f, want 2 @ 29660 (sell 2 first, run 2)", pl.calls[0].leg1Qty, pl.calls[0].leg1TP)
	}
}

// TestPlannerArmUnstampedStaysOneContract — a planner strategy with the knob set
// but a row that was never stamped (legacy row, or a 2-leg arm, or a machine
// arm) still places 1 — the knob changes AUTHORING only, never a bare row.
func TestPlannerArmUnstampedStaysOneContract(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	t.Setenv("STAGE_A_CONTRACT_CAP", "4")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{PlannerContracts: 4, MaxContractsPerOrder: 4}})
	at.id = "planner-unstamped"
	r := store.ArmedOrderDB{
		ID: 9, TraderID: at.id, PlanID: "2026-09-11:NY", Version: 1,
		Session: "NY", Scenario: "S1", Side: "long",
		EntryPx: 29650, StopPx: 29640, TargetPx: 29670,
	}
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 || pl.calls[0].qty != 1 || pl.calls[0].leg1Qty != 0 {
		t.Fatalf("unstamped planner row wired qty %.0f leg1Qty %d (%d calls), want 1/0 (unchanged)",
			pl.calls[0].qty, pl.calls[0].leg1Qty, len(pl.calls))
	}
}

// TestMentorUntouchedByPlannerKnob — mentor sizing reads the mentor table, never
// planner_contracts: a mentor arm with its signed count still sends exactly that
// count even when planner_contracts=4 is set on the same strategy.
func TestMentorUntouchedByPlannerKnob(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "1")
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, PlannerContracts: 4}})
	at.id = "mentor-planner-knob"
	r := mentorSizeRow(at, store.IntPtr(5))
	d := decideStopEntry("LONG", r.EntryPx, testOffset(), testTick, 99)
	if d.Action != stopEntryPlace {
		t.Fatalf("fixture must be placeable, got %q", d.Action)
	}
	pl := &fakePlacer{}
	if got := at.placeOneStopEntry(pl, &fakeLedger{}, r, d, 99, rthInstant(), freeSlot()); got != stopPlaceCommitted {
		t.Fatalf("outcome = %d, want committed", got)
	}
	if len(pl.calls) != 1 || pl.calls[0].qty != 5 {
		t.Fatalf("mentor arm wired qty %.0f (%d calls), want 5 — planner_contracts must not touch mentor sizing",
			pl.calls[0].qty, len(pl.calls))
	}
}
