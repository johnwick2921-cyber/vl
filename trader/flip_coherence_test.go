package trader

import (
	"encoding/json"
	"strings"
	"testing"

	"vl/kernel"
	"vl/store"
)

// R22 F3 — flip-line coherence at the PRODUCTION write site
// (runPlannerReadCoreWithFactsGradesClock, the planner's attempt loop). The
// plan is long-biased and flips SHORT on a 2x5m close below 15500, yet its one
// armed scenario buys 15480 — BELOW that line: it can only fill where the plan
// is about to flip against it (the #649 shape, mirrored). Every other
// validator passes, so the flip relation is the only thing that can stop it.
const flipIncoherentPlanJSON = `{
  "reasoning": "Long bias above 15500; buy the PWL sweep.",
  "bias": {"direction": "long", "conviction": "medium", "flip_condition": "2x5m < 15500"},
  "levels": [
    {"price": 15480, "label": "PWL", "grade": "A", "instruction": "fade"},
    {"price": 15500, "label": "RN 15500", "grade": "B", "instruction": "fade"},
    {"price": 15620, "label": "PDH", "grade": "A", "instruction": "fade"}
  ],
  "scenarios": [{"id": "S1", "trigger": "sweep 15480 reclaim", "condition": "sweep_reclaim", "direction": "long", "target_chain": [15560, 15630], "invalid": "2x5m<15470", "quality": "A", "confirm":{"rule":"touch","ref_price":15480,"side":"below"},"economics":{"entry_zone":[15480,15480],"geometry":{"entry":15480,"stop":15460,"target":15630},"first_obstacle":{"price":15560,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":4.0,"r_to_arm_target":7.5},"arm":{"enabled":true,"entry":15480,"stop":15460,"target":15630,"wait_confirm":true}}],
  "no_trade": ["first 5m"],
  "death_condition": "acceptance above 15620",
  "death": {"price": 15620, "side": "above", "rule": "2x5m"},
  "flip": {"price": 15500, "side": "below", "rule": "2x5m", "flip_to": "short"},
  "day_type": "balance"
}`

func runFlipPlan(t *testing.T, planJSON, hash string) (*AutoTrader, kernel.PlanDoc, int, string) {
	t.Helper()
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	logBuf := captureTraderLog(t)
	calls := 0
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", hash, "", "", "", "FULLPROMPT",
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15500: "RN 15500", 15620: "PDH"}, nil, true,
		func(string) (string, error) { calls++; return planJSON, nil })
	if err != nil || lc != "active" {
		t.Fatalf("write: lc=%q err=%v\nlog:\n%s", lc, err, logBuf.String())
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-08-14", "NY")
	if row == nil {
		t.Fatal("no stored plan")
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatal(err)
	}
	return at, doc, calls, logBuf.String()
}

func TestFlipIncoherentArmDisabledAtWrite(t *testing.T) {
	at, doc, calls, log := runFlipPlan(t, flipIncoherentPlanJSON, "hashFlip1")
	sc := doc.Scenarios[0]
	if sc.Arm == nil || sc.Arm.Enabled || sc.Arm.DisabledReason != kernel.FlipIncoherentClass {
		t.Fatalf("a long armed BELOW its own 'below 15500 → short' flip line must be written disabled (flip_incoherent): %+v", sc.Arm)
	}
	if calls != 1 {
		t.Fatalf("the flip relation disables at write without a repair round-trip: %d planner calls", calls)
	}
	if !strings.Contains(log, "arm disabled at write: NY S1 flip_incoherent") || !strings.Contains(log, "entry 15480.00 sits below the plan's own flip line 15500.00") {
		t.Fatalf("the disable must be logged with its numbers:\n%s", log)
	}
	if n, err := store.SystemCounter(at.store, "arm_disabled_at_write:t1:2026-08-14:NY:flip_incoherent"); err != nil || n != 1 {
		t.Fatalf("the disable must be counted once: n=%d err=%v", n, err)
	}
}

// Control: the same plan with its flip line BELOW the entry (15470) is coherent
// and keeps its arm; an entry exactly ON the line is coherent too.
func TestFlipCoherentArmKeepsItsArm(t *testing.T) {
	for _, flip := range []string{"15470", "15480"} {
		j := strings.ReplaceAll(flipIncoherentPlanJSON, `"price": 15500, "side": "below"`, `"price": `+flip+`, "side": "below"`)
		j = strings.Replace(j, `2x5m < 15500`, `2x5m < `+flip, 1)
		_, doc, _, log := runFlipPlan(t, j, "hashFlip2"+flip)
		if a := doc.Scenarios[0].Arm; a == nil || !a.Enabled || a.DisabledReason != "" {
			t.Fatalf("flip %s: an entry at/above a 'below → short' line is coherent and stays armed: %+v\nlog:\n%s", flip, a, log)
		}
	}
}
