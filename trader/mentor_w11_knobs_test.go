package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// Release #52 W11 — the invented-constant knobs reach the evaluator through the
// production resolver (mentorEvalOnce -> mentorEvaluatorConfig ->
// applyMentorTuning), unset keys change nothing, and a bad value fails closed to
// the default and is counted.

// TestW11KnobsDefaultsAreUnchanged — no stored tuning: every W11 field of the
// evaluator config equals mentor.DefaultConfig (the values the kernel
// hard-coded before this wave). MUTANT: change a default in DefaultConfig or
// seed a field with a literal in mentorTuningResolve → RED.
func TestW11KnobsDefaultsAreUnchanged(t *testing.T) {
	cfg := tuningTick(t, nil)
	d := mentor.DefaultConfig()
	if cfg.ISBStackCancelCandle2m != 4 || cfg.ISBStackTinyStopPts != 0 ||
		cfg.EveningSlowPts != 100 || cfg.EveningSlowFromHour != 15 || cfg.EveningSlowWindowMin != 0 ||
		cfg.NearBoxRoomMultiple != 2 || !cfg.NearBoxRoomPHL || cfg.PHLTargetShyPts != 5 ||
		cfg.ISBOldExtremePts != 2 || cfg.ISBInRangeGapPts != 0 {
		t.Fatalf("W11 defaults drifted: %+v", cfg)
	}
	if cfg.ISBStackCancelCandle2m != d.ISBStackCancelCandle2m || cfg.EveningSlowPts != d.EveningSlowPts ||
		cfg.NearBoxRoomMultiple != d.NearBoxRoomMultiple || cfg.PHLTargetShyPts != d.PHLTargetShyPts {
		t.Fatal("the evaluator config must equal DefaultConfig with no stored tuning")
	}
}

// TestW11KnobsReachTheEvaluator — each stored W11 key lands on its Config field.
// Mutant: drop any line of applyMentorTuning / any pick in mentorTuningResolve →
// the matching assertion is RED.
func TestW11KnobsReachTheEvaluator(t *testing.T) {
	off := false
	cfg := tuningTick(t, &store.MentorTuning{
		ISBStackCancelCandle2m: 6,
		ISBStackTinyStopPts:    12,
		EveningSlowPts:         60,
		EveningSlowFromHour:    17,
		EveningSlowWindowMin:   90,
		NearBoxRoomMultiple:    floatPtr(1.5),
		NearBoxRoomPHL:         &off,
		PHLTargetShyPts:        floatPtr(8),
		ISBOldExtremePts:       floatPtr(3),
		ISBInRangeGapPts:       40,
	})
	checks := []struct {
		name string
		ok   bool
	}{
		{"isb_stack_cancel_candle_2m", cfg.ISBStackCancelCandle2m == 6},
		{"isb_stack_tiny_stop_pts", cfg.ISBStackTinyStopPts == 12},
		{"evening_slow_pts", cfg.EveningSlowPts == 60},
		{"evening_slow_from_hour", cfg.EveningSlowFromHour == 17},
		{"evening_slow_window_min", cfg.EveningSlowWindowMin == 90},
		{"near_box_room_multiple", cfg.NearBoxRoomMultiple == 1.5},
		{"near_box_room_phl", !cfg.NearBoxRoomPHL},
		{"phl_target_shy_pts", cfg.PHLTargetShyPts == 8},
		{"isb_old_extreme_pts", cfg.ISBOldExtremePts == 3},
		{"isb_in_range_gap_pts", cfg.ISBInRangeGapPts == 40},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("stored %s did not reach the evaluator config", c.name)
		}
	}
}

// TestW11KnobsExplicitZeroIsARealValue — for the pointer knobs an explicit 0
// reaches the kernel (near-box skip OFF, PHL target AT the extreme, an exact
// old-extreme touch); a nil leaves the default.
func TestW11KnobsExplicitZeroIsARealValue(t *testing.T) {
	cfg := tuningTick(t, &store.MentorTuning{
		NearBoxRoomMultiple: floatPtr(0),
		PHLTargetShyPts:     floatPtr(0),
		ISBOldExtremePts:    floatPtr(0),
	})
	if cfg.NearBoxRoomMultiple != 0 || cfg.PHLTargetShyPts != 0 || cfg.ISBOldExtremePts != 0 {
		t.Fatalf("an explicit 0 must reach the kernel: near=%g shy=%g old=%g", cfg.NearBoxRoomMultiple, cfg.PHLTargetShyPts, cfg.ISBOldExtremePts)
	}
}

// TestW11KnobsBadValueFailsClosed — out-of-range values fall back to the default
// and are counted once each (L7).
func TestW11KnobsBadValueFailsClosed(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{
		ISBStackCancelCandle2m: 2,    // < 3
		ISBStackTinyStopPts:    99,   // > 25
		EveningSlowPts:         5000, // > 1000
		EveningSlowFromHour:    12,   // outside 15..17
		EveningSlowWindowMin:   9999, // > 600
		NearBoxRoomMultiple:    floatPtr(-1),
		PHLTargetShyPts:        floatPtr(500),
		ISBOldExtremePts:       floatPtr(99),
		ISBInRangeGapPts:       9999,
	}})
	got := mentorTuningResolve(at, at.mentorRiskControl())
	d := mentor.DefaultConfig()
	if got.ISBStackCancelCandle2m != d.ISBStackCancelCandle2m || got.ISBStackTinyStopPts != d.ISBStackTinyStopPts ||
		got.EveningSlowPts != d.EveningSlowPts || got.EveningSlowFromHour != d.EveningSlowFromHour ||
		got.EveningSlowWindowMin != d.EveningSlowWindowMin || got.NearBoxRoomMultiple != d.NearBoxRoomMultiple ||
		got.PHLTargetShyPts != d.PHLTargetShyPts || got.ISBOldExtremePts != d.ISBOldExtremePts ||
		got.ISBInRangeGapPts != d.ISBInRangeGapPts {
		t.Fatalf("bad values must fall back to the defaults: %+v", got)
	}
	if n := at.mentorCountSnapshot()["tuning_bad_value"]; n != 9 {
		t.Fatalf("tuning_bad_value = %d, want 9 (one per bad field)", n)
	}
}
