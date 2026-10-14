package kernel

import (
	"strings"
	"testing"
)

// CLASS 34 (owner ruling 2026-08-31) — validator hints must name only legal
// conditions. Tonight both ASIA chains failed: the breakdown-void reject said
// "author a reject/retest play instead", the model authored condition
// "reject_retest", and parse/schema rejected it. These fixtures pin the guard.

// The table test IS the guard: every condition token a hint names must exist
// in the enum and must not be shadowed by default.
func TestValidatorHintsNameOnlyLegalLiveConditions(t *testing.T) {
	known := map[string]bool{}
	for _, c := range KnownConditions() {
		known[c] = true
	}
	resolved := ResolvedConditionStatuses(nil, nil, "")
	for _, h := range ValidatorHints() {
		// CLASS 38 generalisation: the registry now covers TWO enum families —
		// condition names (class 34) and rule tokens (class 38, e.g. the entry
		// law Style strings, which name confirm rules and no condition at all).
		// A hint must be checkable against at least one of them; a hint that
		// declares neither is invisible to both guards.
		if len(h.Conditions) == 0 && h.RuleField == HintFieldNone {
			t.Errorf("hint %q declares neither condition tokens nor a rule field — no guard can check it", h.Site)
		}
		for _, c := range h.Conditions {
			if !known[c] {
				t.Errorf("hint %q names unknown condition %q (enum: %v)", h.Site, c, KnownConditions())
			}
			if resolved[c] == ConditionShadow {
				t.Errorf("hint %q names shadowed condition %q — a hint must not steer toward a demoted condition", h.Site, c)
			}
		}
	}
	if err := ValidateValidatorHints(); err != nil {
		t.Fatalf("ValidateValidatorHints: %v", err)
	}
}

// Tonight's reproduction: the breakdown-void hint must name `reject` and must
// never carry the composite "reject/retest" as an authoring instruction. The
// only mention of the composite token is the explicit "not a valid condition"
// warning.
func TestBreakdownHintsNameRejectNotComposite(t *testing.T) {
	// N2 (rescan 10-14, planner-touch-fills): the DISPLACEMENT hint now steers
	// a conditional breakdown to a machine-verified `acceptance` play (1x5m_close
	// beyond the level, wait_confirm:true) — steering it to `reject`+`touch` is
	// how #654 was born. The void-breakdown hints still name `reject`.
	if !strings.Contains(BreakdownDisplacementHint, "`acceptance` play") || strings.Contains(BreakdownDisplacementHint, "`reject` play") ||
		!strings.Contains(BreakdownDisplacementHint, "1x5m_close") || !strings.Contains(BreakdownDisplacementHint, "wait_confirm:true") {
		t.Errorf("the displacement hint must name the conditional acceptance play: %q", BreakdownDisplacementHint)
	}
	if !strings.Contains(BreakdownDisplacementHint, "`reject_retest` is not a valid condition") {
		t.Errorf("hint must explicitly forbid the composite: %q", BreakdownDisplacementHint)
	}
	for _, s := range []string{BreakdownReclaimedHint, RepairBreakdownLaw} {
		if !strings.Contains(s, "`reject` play") {
			t.Errorf("hint must name the legal condition: %q", s)
		}
		if strings.Contains(s, "author a reject/retest") || strings.Contains(s, "author a normal reject/retest") {
			t.Errorf("hint still instructs the composite token: %q", s)
		}
		if !strings.Contains(s, "`reject_retest` is not a valid condition") {
			t.Errorf("hint must explicitly forbid the composite: %q", s)
		}
	}
}

// The resolved live vocabulary excludes shadowed conditions and is sorted.
func TestResolvedLiveConditionsExcludesShadowed(t *testing.T) {
	live := ResolvedLiveConditions(nil, nil, "")
	in := func(c string) bool {
		for _, l := range live {
			if l == c {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"reject", "sweep_reclaim", "breakdown_continue"} {
		if !in(want) {
			t.Errorf("live list missing %q: %v", want, live)
		}
	}
	for _, banned := range []string{"fvg_entry", "breakout_retest"} {
		if in(banned) {
			t.Errorf("live list must exclude shadowed %q: %v", banned, live)
		}
	}
	for i := 1; i < len(live); i++ {
		if live[i-1] >= live[i] {
			t.Fatalf("live list not sorted at %d: %v", i, live)
		}
	}
}

// The reject-block suffix renders the vocabulary (fix 5).
func TestLiveConditionsLine(t *testing.T) {
	line := LiveConditionsLine([]string{"acceptance", "reject", "sweep_reclaim"})
	for _, want := range []string{"Valid conditions: [acceptance, reject, sweep_reclaim]", "use exactly ONE token"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q missing %q", line, want)
		}
	}
	if LiveConditionsLine(nil) != "" {
		t.Fatal("empty list must render empty")
	}
}
