package kernel

import (
	"fmt"
	"strings"
	"testing"

	"nofx/market"
)

// ── CLASS 45 F2 — PARITY: the facts-block void list IS the validator's ──────
//
// The whole point of E2 is that the prompt cannot hold a second opinion. This
// fixture proves it across 20 generated tapes: for every ranked level, being in
// the facts-block void list must be EXACTLY equivalent to the write-site
// validator refusing a waterfall play there for the reclaim reason.
//
// (Generated tapes rather than stored production snapshots: planner_rejected_
// prompts stores the rendered PROMPT, not the bar window behind it, so a stored
// snapshot cannot be replayed through the predicate. The 20 cases below vary
// break depth, reclaim presence and side, which is what the predicate keys on.)
func TestClass45VoidListMatchesValidator(t *testing.T) {
	const lvl = 29000.0
	mk := func(seed int) []market.Kline {
		var out []market.Kline
		tms := int64(1_756_800_000_000)
		add := func(c float64) {
			out = append(out, market.Kline{OpenTime: tms, CloseTime: tms + 59_000, Open: c, High: c + 2, Low: c - 2, Close: c})
			tms += 60_000
		}
		short := seed%2 == 0         // even seeds break DOWN, odd break UP
		depth := float64(4 + seed%7) // varying displacement
		reclaims := seed%3 != 0      // 1 in 3 never reclaims
		if short {
			add(lvl + 5)
			add(lvl - depth)
			add(lvl - depth - 2)
			if reclaims {
				add(lvl + 3) // close back across → VOID
			} else {
				add(lvl - depth - 4)
			}
		} else {
			add(lvl - 5)
			add(lvl + depth)
			add(lvl + depth + 2)
			if reclaims {
				add(lvl - 3)
			} else {
				add(lvl + depth + 4)
			}
		}
		return out
	}

	levels := []ScoredLevel{{DetectedLevel: DetectedLevel{Price: lvl, Label: "PDL"}}}
	agree, disagree := 0, 0
	for seed := 1; seed <= 20; seed++ {
		bars := mk(seed)
		now := bars[len(bars)-1].CloseTime // VOID PARITY: sinceMs now comes from the resolver, not the fixture
		inList := map[bool]bool{}
		for _, v := range ComputeVoidBreakdownLevels(levels, tapeScope(bars), now) {
			inList[v.Short] = true
		}
		for _, short := range []bool{true, false} {
			cond, dir := "breakup_continue", "long"
			if short {
				cond, dir = "breakdown_continue", "short"
			}
			doc := &PlanDoc{Scenarios: []PlanScenario{{
				ID: "S1", Condition: cond, Direction: dir,
				Breakdown: &PlanBreakdownContinue{Level: lvl, EntryMode: "pullback"},
			}}}
			err := ValidateBreakdownContinueScenarios(doc, tapeScope(bars), 20, lvl, now)
			validatorRefusesForReclaim := err != nil && strings.Contains(err.Error(), "came back across")
			if inList[short] == validatorRefusesForReclaim {
				agree++
			} else {
				disagree++
				t.Errorf("seed %d %s: facts-block says void=%v but the validator says refuse=%v (err=%v)",
					seed, cond, inList[short], validatorRefusesForReclaim, err)
			}
		}
	}
	if disagree != 0 {
		t.Fatalf("PARITY BROKEN: %d of %d checks disagreed — the prompt and the validator hold two opinions", disagree, agree+disagree)
	}
	t.Logf("parity: %d/%d checks agree across 20 tapes", agree, agree+disagree)
}

// F3 — the floor line states the SAME number the composer enforces.
func TestClass45FloorLineMatchesResolver(t *testing.T) {
	const atr = 26.02
	mult := MinSLATRMult()
	line := RenderStopFloorLine(atr, mult)
	want := fmt.Sprintf("%.1f pts", mult*atr)
	if !strings.Contains(line, want) {
		t.Fatalf("the floor line must state %s (mult %.2f × ATR %.2f), got %q", want, mult, atr, line)
	}
	if !strings.Contains(line, "WIDENED by the executor") {
		t.Error("the line must say what happens to a tighter stop, or it is just a number")
	}
}

// The boot line reads its fields (A24: never a literal).
func TestClass45BootLineReadsFields(t *testing.T) {
	line := PromptFeedsForwardBootLine(3, 26.02, 1.5)
	for _, want := range []string{"prompt feeds forward:", "void-levels=3", "1.5×ATR5m=39.0pts", "reject-block=top+tail", "class 45"} {
		if !strings.Contains(line, want) {
			t.Errorf("boot line %q missing %q", line, want)
		}
	}
	noATR := PromptFeedsForwardBootLine(0, 0, 1.5)
	if !strings.Contains(noATR, "n/a") || strings.Contains(noATR, "pts") {
		t.Errorf("no ATR → n/a, never a fabricated floor: %q", noATR)
	}
	// measured-and-empty is a REAL answer and must not read as uncomputed.
	if !strings.Contains(noATR, "void-levels=0") {
		t.Errorf("a computed empty list is 0, not n/a: %q", noATR)
	}
}

// E2 end-to-end through the prompt builder: a void level reaches the model.
func TestClass45VoidReachesTheRenderedPrompt(t *testing.T) {
	in := PlannerInput{
		VoidBreakdownLevels: []VoidBreakdownLevel{{Price: 29021.25, Short: true, ReclaimedAtCT: "01:14 CT"}},
		StopFloorATR5m:      26.02,
		StopFloorMult:       1.5,
	}
	p := BuildPlannerPrompt(in)
	if !strings.Contains(p, "VOID breakdown levels") || !strings.Contains(p, "29021.25") {
		t.Fatal("the rendered prompt must name the void level")
	}
	if !strings.Contains(p, "39.0 pts") {
		t.Fatal("the rendered prompt must state the resolved stop floor")
	}
	// The order the model reads: the void section must precede the ranked levels
	// it would otherwise author from blind.
	if strings.Index(p, "VOID breakdown levels") > strings.Index(p, "## Ranked levels") {
		t.Fatal("the void section must come BEFORE the ranked levels")
	}
	// And the standing order must now be satisfiable alongside it.
	if strings.Contains(p, "you MUST write a continuation short") {
		t.Fatal("the condition-order must be gone")
	}
	// Empty inputs render nothing at all.
	if q := BuildPlannerPrompt(PlannerInput{}); strings.Contains(q, "VOID breakdown levels") || strings.Contains(q, "Minimum stop distance") {
		t.Fatal("no void levels and no ATR → neither section may render")
	}
}

// The boot line must not report a MEASUREMENT it has not taken. At boot there
// are no bars, so the void list is not "zero levels are void" — it is "not
// computed yet". Checklist class 49: an instrument that cannot be wrong is not
// evidence, and a default that reads as data is worse than no field.
func TestClass45BootLineDoesNotFakeAMeasurement(t *testing.T) {
	boot := PromptFeedsForwardBootLine(-1, 0, 1.5)
	if strings.Contains(boot, "void-levels=0") || strings.Contains(boot, "void-levels=-1") {
		t.Errorf("boot must not print a void count it never computed: %q", boot)
	}
	if !strings.Contains(boot, "n/a") {
		t.Errorf("boot must say the void list is not computed yet: %q", boot)
	}
	// The multiplier IS known at boot even when ATR is not — state it.
	if !strings.Contains(boot, "1.5") {
		t.Errorf("boot must state the floor multiplier it will enforce: %q", boot)
	}
	// A real reading still reports a real count.
	live := PromptFeedsForwardBootLine(2, 26.02, 1.5)
	if !strings.Contains(live, "void-levels=2") || !strings.Contains(live, "39.0") {
		t.Errorf("a computed line must report the count and the floor: %q", live)
	}
	t.Logf("boot: %s", boot)
	t.Logf("live: %s", live)
}
