package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// m465 P3 (release #60) — the four structure-fidelity knobs reach the evaluator
// through the production resolver (mentorEvalOnce -> mentorEvaluatorConfig ->
// applyMentorTuning); unset = the course default; a bad value fails closed to
// the default and is counted (tuning_bad_value).

// TestM465KnobsDefaultsAreTheCourse — no stored tuning: the evaluator config
// carries the course reading from the full video read (D2.3-R7, D3.2-R29/R30,
// D2.2-R64, D5.4-R7, D5.1-R26, D5.1-R18). MUTANT: seed any field with a literal
// in mentorTuningResolve → RED.
func TestM465KnobsDefaultsAreTheCourse(t *testing.T) {
	cfg := tuningTick(t, nil)
	if cfg.TrendlineEntryTouch != mentor.TrendlineEntryThird /* m472 A2 [Oct D3.2 @ 31:00-31:17] supersedes Sept D2.3-R7 */ || cfg.NearBoxBetweenExempt ||
		cfg.PHLFlatLow != mentor.PHLFlatLowNear || cfg.ISBReverseTouch != mentor.ISBReverseTouchPair ||
		cfg.ISBStackTinyStopPts != 12.5 || cfg.DayGateSpentPts != 265 {
		t.Fatalf("m465 defaults: entry=%q exempt=%v flat=%q rev=%q tiny=%g spent=%g",
			cfg.TrendlineEntryTouch, cfg.NearBoxBetweenExempt, cfg.PHLFlatLow, cfg.ISBReverseTouch,
			cfg.ISBStackTinyStopPts, cfg.DayGateSpentPts)
	}
}

// TestM465KnobsReachTheEvaluator — each stored pre-#60 value lands on its
// Config field. Mutant: drop any line of applyMentorTuning → RED.
func TestM465KnobsReachTheEvaluator(t *testing.T) {
	on := true
	cfg := tuningTick(t, &store.MentorTuning{
		TrendlineEntryTouch:  "fourth", // m472 A2: "fourth" is now the opt-in value, so it proves the stored knob reaches the evaluator
		NearBoxBetweenExempt: &on,
		PHLFlatLow:           "strict",
		ISBReverseTouch:      "isb_candle",
		ISBStackTinyStopPts:  0.01, // effectively OFF (no ISB stop is under 3 pts)
		DayGateSpentPts:      300,
	})
	if cfg.TrendlineEntryTouch != mentor.TrendlineEntryFourth || !cfg.NearBoxBetweenExempt ||
		cfg.PHLFlatLow != mentor.PHLFlatLowStrict || cfg.ISBReverseTouch != mentor.ISBReverseTouchISBCandle ||
		cfg.ISBStackTinyStopPts != 0.01 || cfg.DayGateSpentPts != 300 {
		t.Fatalf("stored m465 knobs did not reach the evaluator: entry=%q exempt=%v flat=%q rev=%q tiny=%g spent=%g",
			cfg.TrendlineEntryTouch, cfg.NearBoxBetweenExempt, cfg.PHLFlatLow, cfg.ISBReverseTouch,
			cfg.ISBStackTinyStopPts, cfg.DayGateSpentPts)
	}
}

// TestM465KnobsBadValueFailsClosed — an out-of-enum value falls back to the
// course default and is counted once per field.
func TestM465KnobsBadValueFailsClosed(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{
		TrendlineEntryTouch: "fifth",
		PHLFlatLow:          "loose",
		ISBReverseTouch:     "mother",
	}})
	got := mentorTuningResolve(at, at.mentorRiskControl())
	if got.TrendlineEntryTouch != mentor.TrendlineEntryThird /* m472 A2 [Oct D3.2 @ 31:00-31:17] */ || got.PHLFlatLow != mentor.PHLFlatLowNear ||
		got.ISBReverseTouch != mentor.ISBReverseTouchPair {
		t.Fatalf("bad values must fall back to the course defaults: %+v", got)
	}
	if n := at.mentorCountSnapshot()["tuning_bad_value"]; n != 3 {
		t.Fatalf("tuning_bad_value = %d, want 3 (one per bad field)", n)
	}
}
