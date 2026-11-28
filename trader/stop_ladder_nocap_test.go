package trader

import (
	"math"
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// Stop-size ladder, NO CAP — trader half (owner order 2026-11-15: "tarde all
// isb or any thing just need adjust contact"; "what u mean all sto loss just
// change the size of contact"). Mentor mode only. With
// mentor_wide_stop_max_pts unset (0, the new default) no non-swing stop is
// refused by R8 for its size; the size table keeps the ladder exactly —
// ≤20 the earned tier, (20,30] the reduced tier 3, >30
// mentor_wide_stop_contracts (1 MNQ "Trade vẫn được. 1 MNQ" [OctD2.3-R47
// @48:12]) — with NO upper limit. A stored positive value restores the R8
// refusal above it (revert = 40, the #70 behaviour). A NEGATIVE / NaN / Inf
// stored value is a bad value: it fails CLOSED to 40 and is counted
// wide_stop_max_pts_bad_value — never "no cap".

// TestStopLadderNoCapSizesOne — through the PRODUCTION call sites
// (mentorEntryGateInputs → mentorRuleGate → mentorSizeFor, see ladderRun):
// default knobs, ISB / PHL / BOX at 45..150 pts → 1 MNQ, tier
// stop_ladder_wide, no R8. The stored-40 revert refuses 45 at R8 again.
// RED on #70: the default max is 40, so every case here is refused at R8.
func TestStopLadderNoCapSizesOne(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	for _, setup := range []string{"ISB", "PHL", "PLH", "BOX"} {
		for _, stop := range []float64{45, 60, 100, 150} {
			why, got := ladderRun(t, at, ladderIntent(setup, stop))
			if why != "" {
				t.Errorf("no cap: %s %.0f refused %q, want 1 MNQ (stop_ladder_wide)", setup, stop, why)
				continue
			}
			if got.Contracts != 1 || got.Tier != "stop_ladder_wide" {
				t.Errorf("no cap: %s %.0f got %d (%s — %s), want 1 (stop_ladder_wide)", setup, stop, got.Contracts, got.Tier, got.Why)
			}
			if strings.Contains(got.Why, "Inf") || strings.Contains(got.Why, "%!") {
				t.Errorf("no cap: %s %.0f size reason %q leaks +Inf / a bad verb", setup, stop, got.Why)
			}
			if !strings.Contains(got.Why, "no stop-size cap") {
				t.Errorf("no cap: %s %.0f size reason %q must say there is no stop-size cap", setup, stop, got.Why)
			}
		}
	}
	// Below the old 40 the ladder is unchanged: (20,30] → 3, (30,40] → 1.
	for _, tc := range []struct {
		setup string
		stop  float64
		want  int
		tier  string
	}{{"ISB", 12, 5, "base"}, {"ISB", 25, 3, "stop_ladder_reduced"}, {"ISB", 30, 3, "stop_ladder_reduced"}, {"ISB", 35, 1, "stop_ladder_wide"}, {"BOX", 40, 1, "stop_ladder_wide"}} {
		if why, got := ladderRun(t, at, ladderIntent(tc.setup, tc.stop)); why != "" || got.Contracts != tc.want || got.Tier != tc.tier {
			t.Errorf("no cap: %s %.0f why=%q got %d (%s), want %d (%s)", tc.setup, tc.stop, why, got.Contracts, got.Tier, tc.want, tc.tier)
		}
	}
	// SWING4H is untouched: R31 at 80 still refuses a 90-pt swing.
	if why, _ := ladderRun(t, at, ladderIntent("SWING4H", 90)); !strings.HasPrefix(why, "R31:") {
		t.Errorf("no cap must not touch the swing: SWING4H 90 → %q, want R31", why)
	}

	// REVERT PIN: a stored 40 is exactly the #70 behaviour — 40 → 1, 45 → R8.
	at40 := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWideStopMaxPts: 40})
	for _, setup := range []string{"ISB", "PHL", "BOX"} {
		if why, got := ladderRun(t, at40, ladderIntent(setup, 40)); why != "" || got.Contracts != 1 {
			t.Errorf("stored 40: %s 40 why=%q got %d, want 1", setup, why, got.Contracts)
		}
		why, _ := ladderRun(t, at40, ladderIntent(setup, 45))
		if !strings.HasPrefix(why, "R8:") || !strings.Contains(why, "mentor_wide_stop_max_pts") {
			t.Errorf("stored 40 (revert): %s 45 → %q, want R8 naming mentor_wide_stop_max_pts", setup, why)
		}
	}
}

// TestWideStopMaxNegativeFailsClosed — a stored negative / NaN / +Inf
// mentor_wide_stop_max_pts is a bad value: counted
// wide_stop_max_pts_bad_value and resolved to 40 (the #70 cap), so a bad
// stored value never silently means "no cap" (fail CLOSED). The resolved 40
// reaches the kernel config and the trader R8 through the production paths.
// Passes on #70 (whose default IS 40); RED on the first no-cap draft, which
// fell back to the new default 0 = no cap (fail OPEN).
func TestWideStopMaxNegativeFailsClosed(t *testing.T) {
	for _, bad := range []float64{-5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		rc := store.RiskControlConfig{MentorMode: true, MentorWideStopMaxPts: bad}
		at := mentoredTrader(t, rc)
		r := mentorCourseExitsResolve(at, &rc)
		if r.WideStopMaxPts != 40 {
			t.Errorf("stored %g: resolved WideStopMaxPts %g, want 40 (fail closed)", bad, r.WideStopMaxPts)
		}
		if n := at.mentorCountSnapshot()["wide_stop_max_pts_bad_value"]; n == 0 {
			t.Errorf("stored %g: wide_stop_max_pts_bad_value not counted (%v)", bad, at.mentorCountSnapshot())
		}
		// The trader R8 refuses 45 at the fail-closed 40 (production path).
		why, _ := ladderRun(t, at, ladderIntent("ISB", 45))
		if !strings.HasPrefix(why, "R8:") || !strings.Contains(why, "40-pt mentor_wide_stop_max_pts") {
			t.Errorf("stored %g: ISB 45 → %q, want R8 at the fail-closed 40", bad, why)
		}
		// The kernel half reads the same 40.
		cfg := mentor.DefaultConfig()
		applyMentorCourseExits(at, &cfg, &rc)
		if c := mentor.StopLadderCeiling(cfg, 20); c != 40 {
			t.Errorf("stored %g: kernel StopLadderCeiling %g, want 40", bad, c)
		}
	}
}

// TestWideStopMaxBootLineNone — the boot line READS the resolved knob and
// prints wide_stop_max_pts=none (no cap) when unset — never "off" (which
// reads as "the ladder is off") and never +Inf; a stored 40 prints 40; a bad
// stored value prints the fail-closed 40. The effective-settings row stays a
// finite, JSON-encodable number (0 = no cap).
func TestWideStopMaxBootLineNone(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	line := at.mentorCourseExitsBootLine()
	if !strings.Contains(line, "wide_stop_max_pts=none (no cap)") {
		t.Errorf("default boot line %q must print wide_stop_max_pts=none (no cap)", line)
	}
	if strings.Contains(line, "Inf") || strings.Contains(line, "wide_stop_max_pts=off") {
		t.Errorf("default boot line %q must not print +Inf or off for wide_stop_max_pts", line)
	}
	if !strings.Contains(line, "stop_size_ladder=ON") || !strings.Contains(line, "wide_stop_contracts=1") {
		t.Errorf("default boot line %q must keep stop_size_ladder=ON · wide_stop_contracts=1", line)
	}
	at40, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorWideStopMaxPts: 40}})
	if l := at40.mentorCourseExitsBootLine(); !strings.Contains(l, "wide_stop_max_pts=40") {
		t.Errorf("stored 40 boot line %q must print wide_stop_max_pts=40", l)
	}
	atBad, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorWideStopMaxPts: -5}})
	if l := atBad.mentorCourseExitsBootLine(); !strings.Contains(l, "wide_stop_max_pts=40") {
		t.Errorf("stored -5 boot line %q must print the fail-closed wide_stop_max_pts=40", l)
	}

	// The effective-settings row: a finite number, JSON-safe, 0 = no cap.
	const path = "ai_config.risk_control.mentor_wide_stop_max_pts"
	rows := effRowsFor(t, `{"strategy_type":"ai_trading","ai_config":{"risk_control":{"mentor_mode":true}}}`, "ninjatrader", "")
	row, ok := rows[path]
	if !ok {
		t.Fatalf("no effective-settings row %s", path)
	}
	if v := jsonVal(t, row.Effective); v != float64(0) {
		t.Errorf("effective %s = %v, want 0 (no cap)", path, v)
	}
	if !strings.Contains(row.Resolver, "no cap") {
		t.Errorf("effective %s resolver %q must say 0 = no cap", path, row.Resolver)
	}
}

// TestStopLadderRefuseCeilingUnresolvedBadValue — reviewer P2-1 (7e790a741):
// a mentorTierInputs built WITHOUT mentorWideStopMaxResolve (a future caller,
// a test) that carries a negative / NaN / ±Inf WideStopMaxPts must still fail
// CLOSED at the R8 table — the kernel's StopLadderCeiling owns that, and this
// pins the trader's pass-through reads it. Only an exact 0 is no cap.
func TestStopLadderRefuseCeilingUnresolvedBadValue(t *testing.T) {
	for _, bad := range []float64{-5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if c := mentorStopLadderRefuseCeiling(20, mentorTierInputs{WideStopMaxPts: bad}); c != mentor.WideStopMaxFailClosedPts {
			t.Errorf("unresolved %g: R8 ceiling %g, want fail-closed %g", bad, c, mentor.WideStopMaxFailClosedPts)
		}
	}
	if c := mentorStopLadderRefuseCeiling(20, mentorTierInputs{}); !math.IsInf(c, 1) {
		t.Errorf("unresolved 0: R8 ceiling %g, want +Inf (no cap)", c)
	}
	if c := mentorStopLadderRefuseCeiling(20, mentorTierInputs{WideStopMaxPts: -5, StopLadderOff: true}); c != 20 {
		t.Errorf("ladder OFF + -5: R8 ceiling %g, want 20", c)
	}
	if mentorWideStopMaxFailClosed != mentor.WideStopMaxFailClosedPts {
		t.Errorf("trader fail-closed %g != kernel %g", mentorWideStopMaxFailClosed, mentor.WideStopMaxFailClosedPts)
	}
}
