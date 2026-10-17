package kernel

import (
	"strings"
	"testing"

	"nofx/market"
)

// ── CLASS 45 (2026-09-02) — THE PROMPT WITHHOLDS WHAT THE VALIDATOR KNOWS ────
//
// LONDON 2026-09-02, rows 92/93/94 of planner_rejected_prompts:
//
//   01:32:54  attempt 1 (25,903 ch)  S1 breakdown_continue @29021.25 → "a close
//                                    came back across 29021.25 — void"
//   01:35:04  attempt 2 ( 3,673 ch)  repair obeyed the hint (author a reject
//                                    fade) → killed by fade_requires_touch
//   01:37:44  attempt 3 (26,407 ch)  full RE-AUTHOR → S2 breakdown_continue at
//                                    the SAME 29021.25 → void again → fail-closed
//
// Why attempt 3 regressed, measured from the rendered prompts:
//   · the standing MUST ("If price sits BELOW PDL you MUST write a continuation
//     short") sits at char 18,544 of 26,408 — 70% in, full weight.
//   · the reject block is the LAST 239 chars — 59 tokens of 6,602, under 1%, at
//     99% depth — and it carried attempt 2's fade defect, not attempt 1's void.
//   · the facts block names 29021.25 SIX times and never once says it is void:
//     "VOID" / "reclaimed" / "came back across" each appear 0 times.
//
// So the model was ordered to write a continuation short, was never told the
// only breakdown level was dead, and was corrected about something else. It
// complied. The validator knew all along — BreakdownContinueState computes
// Reclaimed — and told nobody until the write.

// pinLondonBars rebuilds the shape of that tape: a breakdown through 29021.25
// followed by a completed 5m close back across it. Each stage below is
// synthetic; the original five-minute-long fixture never had a 5m break.
func pinLondonBars() []market.Kline {
	const lvl = 29021.25
	var out []market.Kline
	t := int64(1_756_800_000_000)
	t -= t % 300_000 // synthetic stages on canonical 5m boundaries
	add := func(c float64) {
		out = append(out, market.Kline{OpenTime: t, CloseTime: t + 59_000, Open: c, High: c + 3, Low: c - 3, Close: c})
		t += 60_000
	}
	add(lvl + 6) // above
	add(lvl - 8) // the breakdown close
	add(lvl - 12)
	add(lvl + 4) // ← a close came back ACROSS: the breakdown is VOID
	add(lvl + 7)
	return completeFiveMinuteFixture(out)
}

// TestClass45PinLondon0132 is the wave's pin. It asserts the three things the
// dispatch fixes, using only surfaces that exist on the pre-45 tree, so it
// compiles there and FAILS.
func TestClass45PinLondon0132(t *testing.T) {
	const lvl = 29021.25
	bars := pinLondonBars()

	// The validator's own function already knows the level is void.
	sc := PlanScenario{
		ID: "S1", Condition: "breakdown_continue", Direction: "short",
		Breakdown: &PlanBreakdownContinue{Level: lvl, EntryMode: "pullback"},
	}
	st := BreakdownContinueState(sc, bars, bars[0].OpenTime, bars[len(bars)-1].CloseTime)
	if !st.Reclaimed {
		t.Fatalf("fixture: the tape must reclaim %.2f (the validator's own predicate says void)", lvl)
	}

	// (E1) The MUST line must ask for a DIRECTION, not a condition. As shipped it
	// names "a continuation short", which the prompt binds to
	// breakdown_continue/breakup_continue — an instruction the validator then
	// refuses when the only breakdown level is void.
	prompt := BuildPlannerPrompt(PlannerInput{})
	must := "If price sits BELOW PDL you MUST write a continuation short"
	if strings.Contains(prompt, must) {
		t.Errorf("E1: the prompt still orders a CONDITION (%q). It must ask for a SHORT-DIRECTION scenario and name the legal conditions, so the order stays satisfiable when every breakdown level is void", must)
	}
	if !strings.Contains(prompt, "SHORT-direction scenario") {
		t.Errorf("E1: the prompt must ask for a SHORT-direction scenario (any legal condition)")
	}

	// (E1b) The validator's MESSAGE must use the same words as its RULE. The
	// rule is hasDirection(short) — ANY condition — while the message said
	// "continuation/breakdown short", which is part of what taught the model to
	// reach for breakdown_continue at a dead level.
	if !strings.Contains(planDocGapDownMessage(), "SHORT-direction") {
		t.Errorf("E1b: plan_doc's gap-down message must match its direction-only rule, got %q", planDocGapDownMessage())
	}

	// (E2) The facts block carries the void forward, from the SAME predicate.
	voidLine := RenderVoidBreakdownLevels([]VoidBreakdownLevel{{Price: lvl, Short: true, ReclaimedAtCT: "01:14 CT"}}, 12)
	for _, want := range []string{"VOID breakdown levels", "29021.25", "reclaimed 01:14 CT", "do NOT author breakdown_continue"} {
		if !strings.Contains(voidLine, want) {
			t.Errorf("E2: the void line must contain %q, got %q", want, voidLine)
		}
	}
	if RenderVoidBreakdownLevels(nil, 12) != "" {
		t.Errorf("E2: no void levels → no section at all, got %q", RenderVoidBreakdownLevels(nil, 12))
	}

	// (E3) The floor the composer enforces, stated from its own resolver.
	floor := RenderStopFloorLine(26.02, 1.5)
	if !strings.Contains(floor, "Minimum stop distance") || !strings.Contains(floor, "39.0 pts") {
		t.Errorf("E3: the floor line must state the resolved points, got %q", floor)
	}
	if RenderStopFloorLine(0, 1.5) != "" {
		t.Error("E3: no ATR → no floor line, never an invented number")
	}
}
