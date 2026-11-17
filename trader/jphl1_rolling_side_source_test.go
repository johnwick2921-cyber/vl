package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// J-PHL-1 rolling-only revert (review P1, 11-17) through the production
// resolver (mentorEvalOnce -> mentorEvaluatorConfig -> applyMentorTuning):
// unset = 5m with the 4h/1h gate off; "htf" = the 4h rolling side with the gate
// STILL off (the exact pre-J-PHL-1 live state); a bad value falls back to 5m and
// is counted.
func TestJPHL1RollingSideSourceReachesTheEvaluator(t *testing.T) {
	cfg := tuningTick(t, nil)
	if cfg.RollingSideSource != mentor.RollingSide5m || cfg.HTFGateEnabled {
		t.Fatalf("default: rolling side %q gate %v, want 5m with the gate off", cfg.RollingSideSource, cfg.HTFGateEnabled)
	}
	cfg = tuningTick(t, &store.MentorTuning{MentorRollingSideSource: "htf"})
	if cfg.RollingSideSource != mentor.RollingSideHTF || cfg.HTFGateEnabled {
		t.Fatalf("mentor_rolling_side_source=htf: rolling side %q gate %v, want htf with the 4h/1h gate still OFF", cfg.RollingSideSource, cfg.HTFGateEnabled)
	}
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{MentorRollingSideSource: "4h"}})
	got := mentorTuningResolve(at, at.mentorRiskControl())
	if got.RollingSideSource != mentor.RollingSide5m {
		t.Fatalf("a bad value must fall back to 5m, got %q", got.RollingSideSource)
	}
	if n := at.mentorCountSnapshot()["tuning_bad_value"]; n != 1 {
		t.Fatalf("tuning_bad_value = %d, want 1", n)
	}
}
