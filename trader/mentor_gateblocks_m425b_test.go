package trader

import (
	"testing"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// m425b S4: every mentor refusal in the dispatch/placement path must reach the
// gate-block table (GET /api/risk/gate-blocks), not only the mentorCount
// ledger. Each pin drives the PRODUCTION entry (mentorDispatchIntent /
// mentorPlaceIntent). MUTANT per test: delete the named mentorGateBlock call →
// the delta is 0 → RED.

func m425bGate(t *testing.T, at *AutoTrader, class string, f func()) int {
	t.Helper()
	telemetry.ResetGateBlocksForTest()
	f()
	_, table := telemetry.GateBlockSnapshot()
	return table[at.id][class]
}

func m425bIntent(stop, target float64) mentor.Intent {
	return mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "m425b", Setup: "ISB",
		Side: mentor.SideLong, Price: 29600, Stop: stop, Target: target,
		StopPts: 29600 - stop, TargetPts: target - 29600}
}

func TestM425bRuleGateRefusalsCounted(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "")
	cases := []struct {
		name, class string
		in          mentor.Intent
	}{
		{"bad_geometry", "mentor_bad_geometry", m425bIntent(29605, 29610)},
		{"r8", "mentor_rule_r8", m425bIntent(29500, 29800)},
		{"r9", "mentor_rule_r9", m425bIntent(29590, 29605)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
			if got := m425bGate(t, at, c.class, func() { at.mentorDispatchIntent(c.in, mentorTierInputs{}, 1000, 1100) }); got != 1 {
				t.Fatalf("%s: gate-block %s = %d, want 1", c.name, c.class, got)
			}
		})
	}
}

func TestM425bDryRunPlaceOffCounted(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if got := m425bGate(t, at, "mentor_place_off", func() {
		at.mentorDispatchIntent(m425bIntent(29595, 29610), mentorTierInputs{}, 1000, 1100)
	}); got != 1 {
		t.Fatalf("MENTOR_PLACE off must tick mentor_place_off, got %d", got)
	}
}

func TestM425bResonanceHoldCounted(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	evening := eveningInstant()
	pinMentorClock(t, evening)
	pinBars(t, evening.UnixMilli())
	mentorLatestPriceSource = func() (float64, bool) { return 29601, true }
	at.mentorOpenSideSource = func() string { return "short" }
	t.Cleanup(func() { at.mentorOpenSideSource = nil })
	m151ShortPos(at, "sig-phl")
	captureMoves(t)
	if got := m425bGate(t, at, "mentor_resonance_hold", func() {
		at.mentorPlaceIntent(m151ISB(mentor.SideShort), mentorSizeChoice{Contracts: 1, Tier: "base"}, evening.UnixMilli(), 1100)
	}); got != 1 {
		t.Fatalf("resonance hold must tick mentor_resonance_hold, got %d", got)
	}
}
