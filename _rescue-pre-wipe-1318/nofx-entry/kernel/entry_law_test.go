package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"nofx/market"
)

// ENTRY-MECHANICS E2 (2026-08-30) — per-condition entry-law fixtures: ONE
// accept + ONE reject per condition (9 × 2 = 18), plus the sweep split
// contract, the 2x5m_reserved rule, the fade stop law, and the rehearsal S4
// case (must STILL reject).

// lawDoc builds a minimal schema-valid doc for one condition + confirm pair.
func lawDoc(condition, rule string, opts ...func(*PlanScenario)) *PlanDoc {
	ref := 29648.25
	side := "below"
	dir := "short"
	if condition == "breakup_continue" || condition == "acceptance" || condition == "hold" {
		side, dir = "above", "long"
	}
	d := &PlanDoc{
		Reasoning: "law fixture",
		Bias:      PlanBias{Direction: dir, Conviction: "low", FlipCondition: "5m close above 29748.25 flips"},
		Levels:    []PlanLevel{{Price: ref, Label: "OR-L", Grade: "A"}, {Price: 29441, Label: "PDL", Grade: "A"}},
		Scenarios: []PlanScenario{{
			ID: "S1", Condition: condition, Direction: dir,
			Trigger:     fmt.Sprintf("fixture setup at %.2f", ref),
			Invalid:     fmt.Sprintf("fixture invalid at %.2f", ref),
			TargetChain: []float64{29441},
			Quality:     "B",
			Confirm:     &PlanConfirm{Rule: rule, RefPrice: ref, Side: side},
		}},
		DeathCondition:  "5m close below 29441.00 kills the plan",
		DeathStructured: &PlanCondition{Price: 29441, Side: "below", Rule: "5m_close"},
	}
	if condition == "fvg_entry" {
		d.Scenarios[0].Fvg = &PlanFvgEntry{Lo: 29630, Hi: 29660, EntryMode: "ce", DisplacementATR: 1.5, OriginLevel: "EQH", Direction: dir}
	}
	if condition == "breakdown_continue" || condition == "breakup_continue" {
		d.Scenarios[0].Breakdown = &PlanBreakdownContinue{Level: ref, LevelLabel: "OR-L", EntryMode: "immediate"}
	}
	for _, o := range opts {
		o(&d.Scenarios[0])
	}
	return d
}

// TestEntryLawPerCondition — the 18 twins.
func TestEntryLawPerCondition(t *testing.T) {
	cases := []struct {
		cond    string
		legal   []string
		illegal map[string]string // rule → expected rejection token
	}{
		{"reject", []string{"touch"}, map[string]string{"1x5m_close": "fade_requires_touch", "2x5m_close": "fade_requires_touch", "1m_mss": "fade_requires_touch", "time_hold": "fade_requires_touch"}},
		{"fvg_entry", []string{"touch"}, map[string]string{"1x5m_close": "fade_requires_touch", "2x5m_close": "fade_requires_touch", "1m_mss": "fade_requires_touch"}},
		{"sweep_reclaim", []string{"touch", "1x5m_close", "1m_mss"}, map[string]string{"2x5m_close": "2x5m_reserved", "time_hold": "not allowed"}},
		{"reclaim", []string{"1x5m_close", "1m_mss"}, map[string]string{"2x5m_close": "2x5m_reserved", "touch": "not allowed", "time_hold": "not allowed"}},
		{"breakout_retest", []string{"touch", "1x5m_close"}, map[string]string{"2x5m_close": "2x5m_reserved", "1m_mss": "not allowed", "time_hold": "not allowed"}},
		{"acceptance", []string{"time_hold", "1x5m_close"}, map[string]string{"2x5m_close": "2x5m_reserved", "touch": "not allowed", "1m_mss": "not allowed"}},
		{"hold", []string{"time_hold", "1x5m_close"}, map[string]string{"2x5m_close": "2x5m_reserved", "touch": "not allowed"}},
		{"breakdown_continue", []string{"1x5m_close", "2x5m_close"}, map[string]string{"touch": "not allowed", "1m_mss": "not allowed", "time_hold": "not allowed"}},
		{"breakup_continue", []string{"1x5m_close", "2x5m_close"}, map[string]string{"touch": "not allowed", "1m_mss": "not allowed", "time_hold": "not allowed"}},
	}
	for _, c := range cases {
		for _, legal := range c.legal {
			if err := ValidatePlanDocWithCaps(lawDoc(c.cond, legal), 8, 3); err != nil {
				t.Errorf("%s + %s must PASS (got %v)", c.cond, legal, err)
			}
		}
		for rule, token := range c.illegal {
			err := ValidatePlanDocWithCaps(lawDoc(c.cond, rule), 8, 3)
			if err == nil || !strings.Contains(err.Error(), token) {
				t.Errorf("%s + %s must FAIL with %q (got %v)", c.cond, rule, token, err)
			}
		}
	}
}

// TestEntryLawSweepSplitContract — leg 1 touch, leg 2 1m_mss|1x5m_close.
func TestEntryLawSweepSplitContract(t *testing.T) {
	// Legal two-leg split.
	d := lawDoc("sweep_reclaim", "touch", func(s *PlanScenario) {
		s.Confirm2 = &PlanConfirm{Rule: "1m_mss", RefPrice: 29648.25, Side: "below"}
	})
	if err := ValidatePlanDocWithCaps(d, 8, 3); err != nil {
		t.Fatalf("legal sweep split must pass: %v", err)
	}
	d.Scenarios[0].Confirm2.Rule = "1x5m_close" // the accepted leg-2 alternative
	if err := ValidatePlanDocWithCaps(d, 8, 3); err != nil {
		t.Fatalf("1x5m_close leg-2 alternative must pass: %v", err)
	}
	// Leg 1 must be the sweep touch.
	d = lawDoc("sweep_reclaim", "1x5m_close", func(s *PlanScenario) {
		s.Confirm2 = &PlanConfirm{Rule: "1m_mss", RefPrice: 29648.25, Side: "below"}
	})
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "sweep_leg1_requires_touch") {
		t.Fatalf("non-touch leg 1 must fail sweep_leg1_requires_touch (got %v)", err)
	}
	// Leg 2 with 2x5m → reserved.
	d = lawDoc("sweep_reclaim", "touch", func(s *PlanScenario) {
		s.Confirm2 = &PlanConfirm{Rule: "2x5m_close", RefPrice: 29648.25, Side: "below"}
	})
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "2x5m_reserved") {
		t.Fatalf("2x5m leg 2 must fail 2x5m_reserved (got %v)", err)
	}
}

// TestEntryLawFadeStopBeyondLevel — an armed fade's stop must sit ≥2 ticks
// BEYOND the level (structure stop).
func TestEntryLawFadeStopBeyondLevel(t *testing.T) {
	// Short reject: stop must be level + 2 ticks.
	d := lawDoc("reject", "touch", func(s *PlanScenario) {
		s.Arm = &PlanArmSpec{Enabled: true, Entry: 29648.00, Stop: 29648.50, Target: 29600.00} // 1 tick beyond — tight
	})
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "structure stop") {
		t.Fatalf("tight fade stop must fail the structure-stop law (got %v)", err)
	}
	d.Scenarios[0].Arm.Stop = 29649.00 // ≥ level + 0.50 (2 ticks)
	if err := ValidatePlanDocWithCaps(d, 8, 3); err != nil {
		t.Fatalf("structure stop beyond the level must pass: %v", err)
	}
	// Long reject mirror: stop must be level − 2 ticks.
	d = lawDoc("reject", "touch", func(s *PlanScenario) {
		s.Direction = "long"
		s.Arm = &PlanArmSpec{Enabled: true, Entry: 29648.00, Stop: 29647.90, Target: 29700.00} // 0.35 below — tight
	})
	if err := ValidatePlanDocWithCaps(d, 8, 3); err == nil || !strings.Contains(err.Error(), "structure stop") {
		t.Fatalf("tight long fade stop must fail (got %v)", err)
	}
}

// TestRehearsalS4CaseStillRejects — the dress-rehearsal S4 (breakdown_continue
// 29437, pullback, 2x5m+1x5m) on its OWN tape: the flip leg had already
// reclaimed, so the write path must STILL reject (reclaimed=true).
func TestRehearsalS4CaseStillRejects(t *testing.T) {
	start := time.Date(2026, 8, 30, 10, 0, 0, 0, time.Local)
	bars := []market.Kline{
		mkTapeBar(start, 29440, 29444, 29434, 29436), // beyond close
		mkTapeBar(start.Add(time.Minute), 29436, 29442, 29430, 29434),
		mkTapeBar(start.Add(2*time.Minute), 29434, 29440, 29430, 29438), // reclaims
		mkTapeBar(start.Add(3*time.Minute), 29438, 29440, 29432, 29436),
	}
	plan := PlanDoc{Scenarios: []PlanScenario{{
		ID: "S4", Trigger: "2x5m close below 29437.00 PDL — this is the flip leg of the plan, no blind front-running of the level.",
		Condition: "breakdown_continue", Direction: "short",
		TargetChain: []float64{29424},
		Invalid:     "A 5m close back above 29437.00 before entry voids the breakdown.",
		Confirm:     &PlanConfirm{Rule: "2x5m_close", RefPrice: 29437, Side: "below"},
		Confirm2:    &PlanConfirm{Rule: "1x5m_close", RefPrice: 29437, Side: "below"},
		Quality:     "B",
		Breakdown:   &PlanBreakdownContinue{Level: 29437, LevelLabel: "PDL", EntryMode: "pullback"},
	}}}
	err := ValidateBreakdownContinueScenarios(&plan, bars, 15.0, 29436, bars[len(bars)-1].CloseTime)
	if err == nil || !strings.Contains(err.Error(), "void") {
		t.Fatalf("rehearsal S4 (reclaimed) must STILL reject — the new entry law must not resurrect it (got %v)", err)
	}
	st := BreakdownContinueState(plan.Scenarios[0], bars, 0, bars[len(bars)-1].CloseTime)
	if !st.Reclaimed {
		t.Fatalf("S4 tape must be machine-reclaimed: %+v", st)
	}
}

// TestEntryLawLegacyDocsStillParse — the read-path contract: a stored doc with
// a pre-law confirm shape unmarshals (json) and the LOAD path stays armored.
func TestEntryLawLegacyDocsStillParse(t *testing.T) {
	raw := `{"reasoning":"legacy","bias":{"direction":"short"},"levels":[{"price":29648.25,"label":"OR-L","grade":"A"}],
"scenarios":[{"id":"S1","condition":"reject","direction":"short","target_chain":[29441],"invalid":"above 29648.25","quality":"B",
"confirm":{"rule":"15m_close","ref_price":29648.25,"side":"above"}}],
"death_condition":"below 29441","death":{"price":29441,"side":"below","rule":"15m_close"}}`
	var d PlanDoc
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("a legacy stored doc must UNMARSHAL (parse) fine: %v", err)
	}
	if d.Scenarios[0].Confirm == nil || d.Scenarios[0].Confirm.Rule != "15m_close" {
		t.Fatal("legacy confirm lost on parse")
	}
	// New AUTHORSHIP of that same shape is rejected by name.
	if err := ValidatePlanDocWithCaps(&d, 8, 3); err == nil || !strings.Contains(err.Error(), "confirm_rule_15m_removed") {
		t.Fatalf("new authorship of a 15m confirm must be rejected by name (got %v)", err)
	}
}
