package trader

// WAVE PLANNER Lane A, FOLD 3 (CTO 2026-09-25): the planner_contract knob
// wiring at the PRODUCTION prompt-build call site. The CTO's mutant hard-wired
// auto_trader_planner.go:3118 to PlannerContractOn: true and no test caught it.
// This test drives the real read assembly (assemblePlannerInputWithCtx — the
// entry point the scheduled read calls), then renders the prompt with the
// production builder, and pins: strategy planner_contract=false → the prompt
// carries NONE of the A3/A4/A5 contract additions; nil (shipped default) →
// carries them.

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

func a3WireBars() (restore func()) {
	base := time.Date(2026, 9, 7, 18, 0, 0, 0, time.UTC)
	bars := make([]market.Kline, 300)
	for i := range bars {
		stamp := base.Add(time.Duration(i) * time.Minute)
		px := 30000 + float64(i%20)
		bars[i] = market.Kline{OpenTime: stamp.UnixMilli(), CloseTime: stamp.Add(time.Minute).UnixMilli(), Open: px, High: px + 3, Low: px - 3, Close: px + 1, Volume: 100}
	}
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return bars }
	return func() { market.FuturesBarsProvider = old }
}

// a3Fragments are the marker strings ONLY the contract-ON prompt carries
// (plannerContractSchemaFrag / confirmingCloseFrag / entryPolicyPlannedOrderFrag
// / rejectComposedStopFrag / gapReachFrag / the A5 obstacle chains).
var a3Fragments = []string{
	"displacement must be ≥ 1.5×ATR5m",
	"author it ONLY AFTER the tape prints its confirming close",
	"Entry policy: planned_order is legal on reject",
	"REJECT fades are the exception to authoring the stop yourself",
	"A planned_order REJECT's TARGET is composed too", // R22 P2: the gate's target rule, told
	"Gap-reach law:",
	"obstacles→",
}

func TestA3PlannerContractKnobAtTheProductionAssembly(t *testing.T) {
	restore := a3WireBars()
	defer restore()

	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"

	// OFF: explicit strategy knob false threads through line 3118 to the prompt.
	off := false
	at.config.StrategyConfig.DayPlan.PlannerContract = &off
	inOff := at.assemblePlannerInputWithCtx(time.Now(), "ASIA", "2026-09-07", "", nil)
	if inOff.PlannerContractOn {
		t.Fatal("strategy planner_contract=false must thread OFF through the assembly")
	}
	offPrompt := kernel.BuildPlannerPrompt(inOff)
	for _, frag := range a3Fragments {
		if strings.Contains(offPrompt, frag) {
			t.Fatalf("planner_contract=false prompt must carry NONE of the contract additions; found %q", frag)
		}
	}

	// ON (nil = shipped default): the SAME assembly carries every fragment.
	at.config.StrategyConfig.DayPlan.PlannerContract = nil
	if !at.dayPlanCfg().PlannerContractOn() {
		t.Fatal("nil knob must resolve ON (shipped default)")
	}
	inOn := at.assemblePlannerInputWithCtx(time.Now(), "ASIA", "2026-09-07", "", nil)
	if !inOn.PlannerContractOn {
		t.Fatal("nil knob must thread ON through the assembly")
	}
	onPrompt := kernel.BuildPlannerPrompt(inOn)
	for _, frag := range a3Fragments {
		if frag == "obstacles→" {
			continue // asserted below, only when the map carries an entry candidate
		}
		if !strings.Contains(onPrompt, frag) {
			t.Fatalf("contract-ON prompt must carry %q", frag)
		}
	}
	// The A5 obstacle chains ride the SAME knob: present exactly when the map
	// renders an entry candidate (the production builder's own map).
	cs := kernel.BuildMapCandidates(inOn.Levels, inOn.Price, inOn.ATR5m, kernel.MapCandidateOpts{MinRR: inOn.MinTargetRR})
	if len(kernel.EntryShortlist(cs)) > 0 && !strings.Contains(onPrompt, "obstacles→") {
		t.Fatal("contract-ON prompt with entry candidates must show the ordered obstacle list")
	}
}
