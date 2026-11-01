package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// m463 — D5.2-R31 [D5.2 p2 @19:34–21:43]: "mà stop loss ở level gần cả trăm
// điểm thì không ngon nữa… Phải chi 30-40, 50-60 ok, vô 2 contracts được…
// nhưng 100 điểm quá nhiều rủi ro" (his example: 91.75 pts). Owner order
// 2026-11-01 ("full of it", after being shown this quote) REVERSES m186: the
// swing is refused ABOVE mentor_swing_max_stop_pts (default 90). The kernel
// refuses first (swing_max_stop); the trader rule gate reads the SAME resolved
// number as its backstop — one number, not two.

func TestM463SwingRuleGateRefusesAboveCeiling(t *testing.T) {
	gate := func(stop, skip float64) string {
		in := mentor.Intent{Setup: "SWING4H", Side: mentor.SideLong, Price: 30000, Stop: 30000 - stop}
		return mentorRuleGate(in, mentorTierInputs{Setup: "SWING4H", StopPts: stop, SwingSkipStopPts: skip})
	}
	// trades 709/710: 102.25 pts → refused, named R31.
	if why := gate(102.25, 0); !strings.HasPrefix(why, "R31:") || !strings.Contains(why, "mentor_swing_max_stop_pts") {
		t.Fatalf("a 102.25-pt swing stop must be refused by R31 at the default 90, got %q", why)
	}
	// E11 91.75 → refused.
	if why := gate(91.75, 0); !strings.HasPrefix(why, "R31:") {
		t.Fatalf("E11's 91.75-pt stop must be refused, got %q", why)
	}
	// 90 exactly, 60, 30 → taken.
	for _, stop := range []float64{90, 60, 30} {
		if why := gate(stop, 0); why != "" {
			t.Fatalf("a %.2f-pt swing stop is inside the course band, refused: %q", stop, why)
		}
	}
	// the knob raises the ceiling.
	if why := gate(102.25, 110); why != "" {
		t.Fatalf("mentor_swing_max_stop_pts=110 must take 102.25, refused: %q", why)
	}
	// the refusal class is its own gate-block bucket.
	if c := mentorRuleGateClass("r31"); c != "mentor_rule_r31" {
		t.Fatalf("R31 gate class = %q, want mentor_rule_r31", c)
	}
}

func TestM463CourseExitsResolveSwingMaxStop(t *testing.T) {
	if r := mentorCourseExitsResolve(nil, nil); r.SwingMaxStopPts != 90 {
		t.Fatalf("no strategy: swing max stop = %v, want 90", r.SwingMaxStopPts)
	}
	rc := &store.RiskControlConfig{}
	if r := mentorCourseExitsResolve(nil, rc); r.SwingMaxStopPts != 90 {
		t.Fatalf("unset knob: swing max stop = %v, want 90", r.SwingMaxStopPts)
	}
	rc.MentorSwingMaxStopPts = 110
	r := mentorCourseExitsResolve(nil, rc)
	if r.SwingMaxStopPts != 110 {
		t.Fatalf("knob 110: swing max stop = %v", r.SwingMaxStopPts)
	}
	// one number: the kernel SwingCfg.SkipStopPts is the resolved knob.
	cfg := mentor.DefaultConfig()
	applyMentorCourseExits(nil, &cfg, rc)
	if cfg.Swing.SkipStopPts != 110 {
		t.Fatalf("kernel SkipStopPts = %v, want the resolved 110", cfg.Swing.SkipStopPts)
	}
	if mentor.DefaultConfig().Swing.SkipStopPts != 90 {
		t.Fatalf("kernel default SkipStopPts = %v, want 90", mentor.DefaultConfig().Swing.SkipStopPts)
	}
}
