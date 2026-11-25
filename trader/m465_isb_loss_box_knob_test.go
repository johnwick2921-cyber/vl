package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// TestM465ISBLossBoxKnobResolves — release #60 P2 (D4.2-R27): the knob
// mentor_isb_loss_box resolves "" → range (the course), band → band, and an
// unknown value fails closed to range and is COUNTED; the resolved value is
// the one the kernel evaluator reads (applyMentorCourseExits → cfg.ISBLossBox)
// and the boot line prints it.
func TestM465ISBLossBoxKnobResolves(t *testing.T) {
	if r := mentorCourseExitsResolve(nil, nil); r.ISBLossBox != mentor.ISBLossBoxRange {
		t.Fatalf("no strategy: isb loss box = %q, want range", r.ISBLossBox)
	}
	if mentor.DefaultConfig().ISBLossBox != mentor.ISBLossBoxRange {
		t.Fatalf("kernel default = %q, want range", mentor.DefaultConfig().ISBLossBox)
	}
	rc := &store.RiskControlConfig{}
	if r := mentorCourseExitsResolve(nil, rc); r.ISBLossBox != mentor.ISBLossBoxRange {
		t.Fatalf("unset knob = %q, want range", r.ISBLossBox)
	}
	rc.MentorISBLossBox = " Band "
	cfg := mentor.DefaultConfig()
	applyMentorCourseExits(nil, &cfg, rc)
	if cfg.ISBLossBox != mentor.ISBLossBoxBand {
		t.Fatalf("knob band: kernel cfg.ISBLossBox = %q, want band", cfg.ISBLossBox)
	}
	at := &AutoTrader{}
	rc.MentorISBLossBox = "whole_day"
	if r := mentorCourseExitsResolve(at, rc); r.ISBLossBox != mentor.ISBLossBoxRange {
		t.Fatalf("bad value = %q, want range (fail closed to the course)", r.ISBLossBox)
	}
	if at.mentorCounters["isb_loss_box_bad_value"] != 1 {
		t.Fatalf("bad value must be counted, counters=%v", at.mentorCounters)
	}
}

// TestM465BootLineReadsISBLossBox — the boot line READS the knob (canon 45).
func TestM465BootLineReadsISBLossBox(t *testing.T) {
	at := &AutoTrader{}
	at.config.StrategyConfig = &store.StrategyConfig{}
	at.config.StrategyConfig.RiskControl.MentorISBLossBox = "band"
	if line := at.mentorCourseExitsBootLine(); !strings.Contains(line, "isb_loss_box=band") {
		t.Fatalf("boot line must read the knob: %s", line)
	}
}
