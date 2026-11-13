package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// answers-1112 Q13: mentor_swing_max_stop_pts default 90 → 80 — "có những lúc
// mà cái nến 5 phút nó tới 7-80 điểm … cứ vô 1-2 [MNQ] cũng được … risk reward
// phải 1-1" [Sept D5.2 p2 @05:25-05:33]; his worked example is 71.75 pts
// (frame part2/f_000158). The trader rule gate (R31) reads the SAME resolved
// number as the kernel; the revert value 90 reproduces today exactly.
func TestAnswers1112SwingRuleGateEighty(t *testing.T) {
	gate := func(stop, skip float64) string {
		in := mentor.Intent{Setup: "SWING4H", Side: mentor.SideLong, Price: 28689, Stop: 28689 - stop}
		return mentorRuleGate(in, mentorTierInputs{Setup: "SWING4H", StopPts: stop, SwingSkipStopPts: skip})
	}
	// default (the resolved knob, unset → 80).
	def := mentorCourseExitsResolve(nil, nil).SwingMaxStopPts
	if def != 80 {
		t.Fatalf("resolved default = %v, want 80", def)
	}
	for _, stop := range []float64{71.75, 80} {
		if why := gate(stop, def); why != "" {
			t.Fatalf("a %.2f-pt swing stop is inside \"7-80\", refused: %q", stop, why)
		}
	}
	for _, stop := range []float64{80.25, 85, 90} {
		if why := gate(stop, def); !strings.HasPrefix(why, "R31:") || !strings.Contains(why, "80-pt ceiling") {
			t.Fatalf("a %.2f-pt swing stop must be refused by R31 at the default 80, got %q", stop, why)
		}
	}
	// unset at the gate (0) resolves to the same default.
	if why := gate(85, 0); !strings.HasPrefix(why, "R31:") {
		t.Fatalf("SwingSkipStopPts 0 must resolve to 80 and refuse 85, got %q", why)
	}
	// revert: mentor_swing_max_stop_pts = 90 is today.
	rc := &store.RiskControlConfig{MentorSwingMaxStopPts: 90}
	rev := mentorCourseExitsResolve(nil, rc).SwingMaxStopPts
	if rev != 90 {
		t.Fatalf("revert knob 90 resolved to %v", rev)
	}
	for _, stop := range []float64{71.75, 80, 85, 90} {
		if why := gate(stop, rev); why != "" {
			t.Fatalf("revert 90: a %.2f-pt stop is taken today, refused: %q", stop, why)
		}
	}
	if why := gate(91.75, rev); !strings.HasPrefix(why, "R31:") {
		t.Fatalf("revert 90: his 91.75 example stays refused, got %q", why)
	}
	cfg := mentor.DefaultConfig()
	applyMentorCourseExits(nil, &cfg, rc)
	if cfg.Swing.SkipStopPts != 90 {
		t.Fatalf("revert: kernel SkipStopPts = %v, want 90", cfg.Swing.SkipStopPts)
	}
	cfg = mentor.DefaultConfig()
	applyMentorCourseExits(nil, &cfg, &store.RiskControlConfig{})
	if cfg.Swing.SkipStopPts != 80 {
		t.Fatalf("unset: kernel SkipStopPts = %v, want 80", cfg.Swing.SkipStopPts)
	}
}
