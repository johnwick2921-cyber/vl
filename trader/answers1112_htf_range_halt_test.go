package trader

import (
	"testing"

	"vl/store"
)

// TestAnswers1112HTFRangeHaltKnob — answers-1112 Q1 extra rule
// (htf_opposed_range_halt_pts): unset / 0 = OFF (today — the video gives a
// range, 300-400, not one number, and it is September [Sept D5.1 p1
// @19:22-19:26]); a stored 50..2000 reaches the evaluator; an out-of-range
// value fails closed to OFF and is counted. The kernel tests
// (kernel/mentor/answers1112_htf_range_halt_test.go) pin what the evaluator
// does with it.
func TestAnswers1112HTFRangeHaltKnob(t *testing.T) {
	if c := tuningTick(t, nil); c.HTFOpposedRangeHaltPts != 0 {
		t.Fatalf("no mentor_tuning block: htf_opposed_range_halt_pts = %v, want 0 (OFF)", c.HTFOpposedRangeHaltPts)
	}
	if c := tuningTick(t, &store.MentorTuning{}); c.HTFOpposedRangeHaltPts != 0 {
		t.Fatalf("unset htf_opposed_range_halt_pts = %v, want 0 (OFF)", c.HTFOpposedRangeHaltPts)
	}
	if c := tuningTick(t, &store.MentorTuning{HTFOpposedRangeHaltPts: 300}); c.HTFOpposedRangeHaltPts != 300 {
		t.Fatalf("stored 300 must reach the evaluator, got %v", c.HTFOpposedRangeHaltPts)
	}
	for _, bad := range []float64{10, -5, 5000} {
		at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{HTFOpposedRangeHaltPts: bad}})
		if got := mentorTuningResolve(at, at.mentorRiskControl()); got.HTFOpposedRangeHaltPts != 0 {
			t.Fatalf("bad %v must fail closed to OFF, got %v", bad, got.HTFOpposedRangeHaltPts)
		}
		if n := at.mentorCountSnapshot()["tuning_bad_value"]; n != 1 {
			t.Fatalf("bad %v: tuning_bad_value = %d, want 1", bad, n)
		}
	}
}
