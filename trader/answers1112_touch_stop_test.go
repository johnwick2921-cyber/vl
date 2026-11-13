package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// TestAnswers1112TouchStopCoverLaterWickKnob — answers-1112 Q7: unset = OFF
// since the 2026-11-12 review (today's line+30 stop; the rule is stated for a
// level trade and carried onto the swing re-arm as a [B] port, report Q7 "needs
// owner sign-off"); a stored true reaches the evaluator (a candle-2 swing
// entry's stop covers candle 2's further wick [Oct D5.2 @19:42-20:03]).
// The knob changes only the swing leeway re-arm; the stop-size / R31 gates are
// untouched and read the wider stop (below).
func TestAnswers1112TouchStopCoverLaterWickKnob(t *testing.T) {
	off, on := false, true
	if c := tuningTick(t, &store.MentorTuning{}); c.Swing.CoverLaterWick || c.Swing.LaterWickStopBufferPts != 1.0 {
		t.Fatalf("unset: Swing.CoverLaterWick=%v buffer=%v, want false (review: owner opt-in) / 1.0 [Oct D2.3 @46:20-47:20]", c.Swing.CoverLaterWick, c.Swing.LaterWickStopBufferPts)
	}
	if c := tuningTick(t, nil); c.Swing.CoverLaterWick {
		t.Fatal("no mentor_tuning block: touch_stop_cover_later_wick must resolve OFF")
	}
	if c := tuningTick(t, &store.MentorTuning{TouchStopCoverLaterWick: &off}); c.Swing.CoverLaterWick {
		t.Fatal("stored touch_stop_cover_later_wick=false (revert) must reach the evaluator")
	}
	if c := tuningTick(t, &store.MentorTuning{TouchStopCoverLaterWick: &on}); !c.Swing.CoverLaterWick {
		t.Fatal("stored touch_stop_cover_later_wick=true must reach the evaluator")
	}
	// the course-exits resolver (mentor_swing_max_stop_pts) leaves the knob alone.
	cfg := mentor.DefaultConfig()
	cfg.Swing.CoverLaterWick = true // as a stored true would set it
	applyMentorCourseExits(nil, &cfg, &store.RiskControlConfig{MentorSwingMaxStopPts: 90})
	if !cfg.Swing.CoverLaterWick || cfg.Swing.SkipStopPts != 90 {
		t.Fatalf("applyMentorCourseExits must not reset CoverLaterWick: %+v", cfg.Swing)
	}
}

// TestAnswers1112TouchStopWiderStopMeetsR31 — the covered stop is a plain
// intent Stop: the trader's R31 gate refuses it above mentor_swing_max_stop_pts
// exactly like any swing stop. Kernel example (answers1112_touch_stop_test.go):
// candle-2 sell stop 28,123.50, covered stop 28,191.00 (67.50 pts, taken) and
// 28,211.00 (87.50 pts, refused).
func TestAnswers1112TouchStopWiderStopMeetsR31(t *testing.T) {
	skip := mentorCourseExitsResolve(nil, nil).SwingMaxStopPts
	gate := func(stop float64) string {
		in := mentor.Intent{Setup: "SWING4H", Side: mentor.SideShort, Price: 28123.50, Stop: stop}
		return mentorRuleGate(in, mentorTierInputs{Setup: "SWING4H", StopPts: stop - 28123.50, SwingSkipStopPts: skip})
	}
	if why := gate(28191); why != "" {
		t.Fatalf("covered stop 28,191.00 (67.50 pts) is inside 80, refused: %q", why)
	}
	if why := gate(28211); !strings.HasPrefix(why, "R31:") {
		t.Fatalf("covered stop 28,211.00 (87.50 pts) must be refused by R31, got %q", why)
	}
}
