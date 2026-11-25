package store

import (
	"strings"
	"testing"
)

// m474 — release #65 (v2026.11.09.1) booted 06:59:15 CT 11-09 and REFUSED the
// owner's mentor traders "hoang" (strategy a5b7662e) and "4567489 mess."
// (de7d04b2) at load: "consecutive_loss_halt stores absent: before W1 that meant
// 'inherit' (breaker 8), after W1 it means OFF (breaker 4[I])". m472 A4 moved the
// SHIPPED default of a mentor_mode strategy to 4 [Oct D4.1 @ 14:40]; the W1
// report's frozen pre-W1 model still said 8, so an absent breaker — which means
// INHERIT both before and after W1 — read as a W1 meaning flip. A shipped-default
// change is not the W1 flip: the report must compare like with like.
func TestM474MentorAbsentBreakerIsNotAW1Flip(t *testing.T) {
	rows := []SettingsTruthInput{
		// the live shape of a5b7662e / de7d04b2: mentor mode, breaker key absent
		{ID: "mentor-absent", Bound: 1, Config: `{"ai_config":{"risk_control":{"mentor_mode":true}}}`},
		{ID: "mentor-null", Bound: 1, Config: `{"ai_config":{"risk_control":{"mentor_mode":true,"consecutive_loss_halt":null}}}`},
		// the W1 flip itself must still refuse on a mentor strategy
		{ID: "mentor-zero", Bound: 1, Config: `{"ai_config":{"risk_control":{"mentor_mode":true,"consecutive_loss_halt":0}}}`},
		// an AI-mode strategy keeps 8 and is untouched
		{ID: "ai-absent", Bound: 1, Config: `{"ai_config":{"risk_control":{}}}`},
	}
	res := SettingsTruthReport(rows, envMap(nil))
	got := map[string]SettingsTruthRow{}
	for _, r := range res.Rows {
		got[r.ID] = r
	}
	for _, id := range []string{"mentor-absent", "mentor-null"} {
		r := got[id]
		if r.Refuse != "" {
			t.Errorf("%s: refused at load: %s", id, r.Refuse)
		}
		if r.Breaker.Changed || r.Breaker.Before != "4" || r.Breaker.After != "4[I]" {
			t.Errorf("%s: breaker before=%q after=%q changed=%v, want 4 → 4[I] unchanged", id, r.Breaker.Before, r.Breaker.After, r.Breaker.Changed)
		}
	}
	if r := got["mentor-zero"]; !strings.Contains(r.Refuse, "before W1 that meant 'inherit' (breaker 4)") || !strings.Contains(r.Refuse, "(breaker off[O])") {
		t.Errorf("mentor-zero: an unconfirmed stored 0 must still refuse with the mentor inherit value; got %q", r.Refuse)
	}
	if r := got["ai-absent"]; r.Refuse != "" || r.Breaker.Changed || r.Breaker.After != "8[I]" {
		t.Errorf("ai-absent: refuse=%q after=%q changed=%v, want 8[I] unchanged", r.Refuse, r.Breaker.After, r.Breaker.Changed)
	}
	// env BREAKER_HALT_N still wins over both defaults, before and after
	res = SettingsTruthReport(rows[:1], envMap(map[string]string{BreakerHaltEnv: "6"}))
	if r := res.Rows[0]; r.Refuse != "" || r.Breaker.Before != "6" || r.Breaker.After != "6[E]" {
		t.Errorf("mentor-absent with env 6: refuse=%q before=%q after=%q", r.Refuse, r.Breaker.Before, r.Breaker.After)
	}
}
