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
	"testing"

	"vl/kernel"
	"vl/store"
)

func firstObstaclePtr(v float64) *float64 { return &v }

// plannerSplitScenario is a planner scenario whose economics name a first
// obstacle (the plan's first target) below the arm's final target.
func plannerSplitScenario(id string, firstObstacle float64) kernel.PlanScenario {
	return kernel.PlanScenario{
		ID: id, Trigger: "t", Condition: "reject", Direction: "long",
		TargetChain: []float64{110}, Invalid: "i", Quality: "B",
		Economics: &kernel.ScenarioEconomics{Version: 1, FirstObstacle: &kernel.ScenarioObstacle{
			Price: firstObstaclePtr(firstObstacle), Level: "L1", Response: "reduce",
		}},
		Arm: &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 98, Target: 110},
	}
}

// TestFirstObstaclePrice — the plan's first target is the first_obstacle price;
// 0 when absent (legacy / a plan that never wrote one).
func TestFirstObstaclePrice(t *testing.T) {
	if got := firstObstaclePrice(plannerSplitScenario("S1", 101)); got != 101 {
		t.Fatalf("firstObstaclePrice = %.2f, want 101", got)
	}
	if got := firstObstaclePrice(kernel.PlanScenario{ID: "S2"}); got != 0 {
		t.Fatalf("firstObstaclePrice (no economics) = %.2f, want 0", got)
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
	stampPlannerSplit(row, rc, false, 0, 101)
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
	stampPlannerSplit(unset, &store.RiskControlConfig{}, false, 0, 101)
	if unset.Contracts != nil || unset.Leg1Qty != nil || unset.Leg1TP != 0 {
		t.Fatalf("unset knob must leave the row untouched: %+v", unset)
	}

	// a machine (Picture) arm and a 2-leg sweep_reclaim arm are never stamped.
	m := &store.ArmedOrderDB{Scenario: "P1"}
	stampPlannerSplit(m, rc, true, 0, 101)
	if m.Contracts != nil || m.Leg1Qty != nil {
		t.Fatalf("a machine arm must not be stamped: %+v", m)
	}
	two := &store.ArmedOrderDB{Scenario: "S3"}
	stampPlannerSplit(two, rc, false, 2, 101)
	if two.Contracts != nil || two.Leg1Qty != nil {
		t.Fatalf("a 2-leg sweep_reclaim arm must not be stamped: %+v", two)
	}

	// no first obstacle → the total is still stamped (one bracket of `total` at
	// the final target), never a split with a fabricated leg-1 price.
	noFo := &store.ArmedOrderDB{Scenario: "S4"}
	stampPlannerSplit(noFo, rc, false, 0, 0)
	if noFo.Contracts == nil || *noFo.Contracts != 4 || noFo.Leg1Qty == nil || *noFo.Leg1Qty != 2 || noFo.Leg1TP != 0 {
		t.Fatalf("no first obstacle: Contracts=%v Leg1Qty=%v Leg1TP=%.2f — want total 4, split stamped, Leg1TP 0 (wire drops the split)",
			noFo.Contracts, noFo.Leg1Qty, noFo.Leg1TP)
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
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{PlannerContracts: 4}})
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
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{PlannerContracts: 4}})
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
