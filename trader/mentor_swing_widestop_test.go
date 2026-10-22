package trader

import (
	"testing"

	"vl/kernel/mentor"
)

// B5 (#45 review): a swing stop >= swing_max_stop_pts is SIZED DOWN to <= 2
// MNQ, never refused and never sent at the owner's larger swing size.
func TestSwingWideStopSizesDownNeverRefuses(t *testing.T) {
	cases := []struct {
		name  string
		stop  float64
		knob  int
		wantN int
		tier  string
	}{
		{"stop 80 normal size", 80, 2, 2, "swing4h"},
		{"stop 80 owner knob 4 keeps normal size", 80, 4, 4, "swing4h"},
		{"stop 99.75 under ceiling", 99.75, 4, 4, "swing4h"},
		{"stop 100 -> 2", 100, 2, 2, "swing4h_wide_stop"},
		{"stop 160 -> 2", 160, 2, 2, "swing4h_wide_stop"},
		{"stop 100 owner knob 6 -> still 2", 100, 6, 2, "swing4h_wide_stop"},
		{"stop 160 owner knob 20 -> still 2", 160, 20, 2, "swing4h_wide_stop"},
		{"stop 160 owner base 1 -> never above base", 160, 1, 1, "swing4h_wide_stop"},
	}
	for _, c := range cases {
		in := mentorTierInputs{Setup: "SWING4H", StopPts: c.stop}
		got, err := mentorContractsFor(in, 5, 5, 5, 3, c.knob, 2, 20)
		if err != nil || got.Contracts != c.wantN || got.Tier != c.tier {
			t.Errorf("%s: got (%d,%s,%v), want (%d,%s)", c.name, got.Contracts, got.Tier, err, c.wantN, c.tier)
		}
		// never refused by the rule gate at any stop width
		intent := mentor.Intent{Setup: "SWING4H", Side: mentor.SideLong, Price: 30000, Stop: 30000 - c.stop}
		if why := mentorRuleGate(intent, in); why != "" {
			t.Errorf("%s: rule gate refused a swing: %s", c.name, why)
		}
	}
}

// The ceiling is the resolved swing_max_stop_pts knob, not a hardcoded 100.
func TestSwingWideStopUsesResolvedCeiling(t *testing.T) {
	in := mentorTierInputs{Setup: "SWING4H", StopPts: 90, SwingMaxStopPts: 80}
	if got, _ := mentorContractsFor(in, 5, 5, 5, 3, 6, 2, 20); got.Contracts != 2 {
		t.Fatalf("stop 90 with ceiling 80 = %d, want 2", got.Contracts)
	}
	in.SwingMaxStopPts = 120
	if got, _ := mentorContractsFor(in, 5, 5, 5, 3, 6, 2, 20); got.Contracts != 6 {
		t.Fatalf("stop 90 with ceiling 120 = %d, want 6", got.Contracts)
	}
}
