package trader

import (
	"strings"
	"testing"

	"vl/store"
)

// m465 P5 (release #60): the D4.3 wick microscalp knobs ride
// risk_control.mentor_tuning → mentorTuningResolve → the evaluator Config. Both
// default OFF (the mentor calls the setup advanced — "đừng có tập khúc này đầu
// liền" [D4.3 @04:55–05:17]); a stored true reaches the kernel; the boot line
// prints the RESOLVED values.
func TestM60WickKnobsReachTheEvaluator(t *testing.T) {
	cfg := tuningTick(t, nil)
	if cfg.WickMicroscalpEnabled || cfg.WickSafePlay {
		t.Fatalf("absent mentor_tuning: wick_microscalp=%v wick_safe_play=%v, want both OFF", cfg.WickMicroscalpEnabled, cfg.WickSafePlay)
	}
	on := true
	cfg = tuningTick(t, &store.MentorTuning{WickMicroscalp: &on, WickSafePlay: &on})
	if !cfg.WickMicroscalpEnabled || !cfg.WickSafePlay {
		t.Fatalf("stored true must reach the evaluator: wick_microscalp=%v wick_safe_play=%v", cfg.WickMicroscalpEnabled, cfg.WickSafePlay)
	}
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{WickMicroscalp: &on}})
	line := mentorCourseEntryBootLine(at, at.mentorRiskControl())
	for _, want := range []string{"wick_microscalp=true", "wick_safe_play=false"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line %q is missing %q", line, want)
		}
	}
}
