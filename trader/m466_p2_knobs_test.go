package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// m466 P2 (release #61) — the four levels-and-touches knobs reach the evaluator
// through the production resolver (mentorEvalOnce -> mentorEvaluatorConfig ->
// applyMentorTuning); unset = the course default; a bad value fails closed to
// the default and is counted (tuning_bad_value).

// TestM466P2KnobsDefaultsAreTheCourse — no stored tuning: G2 D1.3-R21 on,
// G10/G11 D3.1-R26/R20 on, G17 D2.3-R34 current_line, G41 D2.2-R59 pullback.
func TestM466P2KnobsDefaultsAreTheCourse(t *testing.T) {
	cfg := tuningTick(t, nil)
	if cfg.UnoKeyLevels != mentor.UnoKeyLevelsOn || cfg.SessionOpenLevels != mentor.SessionOpenLevelsOn ||
		cfg.LevelArmLineCancel != mentor.LevelArmLineCancelCurrent || cfg.PHLPullbackLow != mentor.PHLPullbackLowPullback {
		t.Fatalf("m466 P2 defaults: uno=%q open=%q line=%q pullback=%q",
			cfg.UnoKeyLevels, cfg.SessionOpenLevels, cfg.LevelArmLineCancel, cfg.PHLPullbackLow)
	}
}

// TestM466P2KnobsReachTheEvaluator — each stored pre-#61 value lands on its
// Config field. Mutant: drop any line of applyMentorTuning → RED.
func TestM466P2KnobsReachTheEvaluator(t *testing.T) {
	cfg := tuningTick(t, &store.MentorTuning{
		UnoKeyLevels:       "off",
		SessionOpenLevels:  "off",
		LevelArmLineCancel: "placement_price",
		PHLPullbackLow:     "signal",
	})
	if cfg.UnoKeyLevels != mentor.UnoKeyLevelsOff || cfg.SessionOpenLevels != mentor.SessionOpenLevelsOff ||
		cfg.LevelArmLineCancel != mentor.LevelArmLineCancelPlacement || cfg.PHLPullbackLow != mentor.PHLPullbackLowSignal {
		t.Fatalf("stored m466 P2 knobs did not reach the evaluator: uno=%q open=%q line=%q pullback=%q",
			cfg.UnoKeyLevels, cfg.SessionOpenLevels, cfg.LevelArmLineCancel, cfg.PHLPullbackLow)
	}
}

// TestM466P2KnobsBadValueFailsClosed — an out-of-enum value falls back to the
// course default and is counted once per field.
func TestM466P2KnobsBadValueFailsClosed(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{
		UnoKeyLevels:       "maybe",
		SessionOpenLevels:  "sometimes",
		LevelArmLineCancel: "frozen",
		PHLPullbackLow:     "candle",
	}})
	got := mentorTuningResolve(at, at.mentorRiskControl())
	if got.UnoKeyLevels != mentor.UnoKeyLevelsOn || got.SessionOpenLevels != mentor.SessionOpenLevelsOn ||
		got.LevelArmLineCancel != mentor.LevelArmLineCancelCurrent || got.PHLPullbackLow != mentor.PHLPullbackLowPullback {
		t.Fatalf("bad values must fall back to the course defaults: %+v", got)
	}
	if n := at.mentorCountSnapshot()["tuning_bad_value"]; n != 4 {
		t.Fatalf("tuning_bad_value = %d, want 4 (one per bad field)", n)
	}
}
