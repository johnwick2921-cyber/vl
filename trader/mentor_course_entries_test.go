package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// Release #25 course-entry knobs (fix/mentor-course-entries): every knob
// reaches the evaluator at the PRODUCTION call site (mentorEvalOnce →
// mentorEvaluatorConfig → applyMentorTuning), an absent block keeps the
// COMMON-25 defaults, and the per-trader boot line READS the resolver.
// Mutant: drop any new line of applyMentorTuning → the matching check is RED.

func TestCourseEntryKnobsDefaultsAtTheCallSite(t *testing.T) {
	cfg := tuningTick(t, nil)
	checks := []struct {
		name string
		ok   bool
	}{
		{"15m_ib_requires_5m_isb=require_5m_isb (a)", cfg.FifteenMinIBRule == mentor.FifteenMinRequire5mISB},
		{"tf_conflict_end=both_boxes (course-defaults-30)", cfg.TFConflictEnd == mentor.TFConflictBothBoxes},
		{"news_wait_both_lines=true (course-defaults-30)", cfg.NewsWaitBothLines},
		{"news_bars_excluded=true (a)", cfg.NewsBarsExcluded},
		{"isb_box_escape=close5m (m473 B3 OctD4.3-R67 [Oct D4.3 @ 65:00-66:13]; was close m466 G20/G49 D4.1-R34)", cfg.ISBBoxEscapeMode == mentor.ISBBoxEscapeCloseBoxTF},
		{"ema34_loss_relock=until_zone_exit (a)", cfg.EmaLossRelock == mentor.EmaRelockUntilZoneExit},
		{"orb_edge_veto_pts=0 (b)", cfg.OrbEdgeVetoPts == 0},
		{"d43_trend_source=structure (course-defaults-30)", cfg.D43TrendSource == mentor.D43TrendStructure},
		{"level_entries_need_trigger_side=false (b)", !cfg.LevelEntriesNeedTriggerSide},
		{"trendline_is_target=true (a)", cfg.TrendlineIsTarget},
		{"trendline_between_boxes=false (D3-G8 oct round 2: OctD3.3-R59 [@ 46:27]; was true, course-defaults-30)", !cfg.TrendlineBetweenBoxes},
		{"leg_reset_on_wave_break=true (a)", cfg.LegResetOnWaveBreak},
		{"flag_wall_target=true (course-defaults-30)", cfg.FlagWallTarget},
		{"orb_escape_completely=false (release #52 B19: body test until the owner rules)", !cfg.OrbEscapeCompletely},
		// m466 P1 (release #61): boxes, breaks, ranges — the course defaults.
		{"mentor_5m_box_source=merge (OctD4.3-R59)", cfg.FiveMinBoxSource == mentor.FiveMinBoxMerge},
		{"mentor_break_direction_lock=true (D4.1-R31/R55)", cfg.BreakDirectionLock},
		{"mentor_small_range_guard=false (D4.2-R33 — release #61 final: OFF, the course states no candle count / breakout margin)", !cfg.SmallRangeGuard},
		{"mentor_mid_range_reject_side=true (D2.2-R71)", cfg.MidRangeRejectSide},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("absent mentor_tuning: %s did not hold", c.name)
		}
	}
}

func TestCourseEntryKnobsReachTheEvaluator(t *testing.T) {
	on, off := true, false
	cfg := tuningTick(t, &store.MentorTuning{
		FifteenMinIBRequires5mISB:   "no_trade_until_escape",
		TFConflictEnd:               "both_boxes",
		NewsWaitBothLines:           &on,
		NewsBarsExcluded:            &on,
		ISBBoxEscape:                "body", // m466: close is the default now — the stored body must win
		EMA34LossRelock:             "session",
		OrbEdgeVetoPts:              3,
		D43TrendSource:              "structure",
		LevelEntriesNeedTriggerSide: &on,
		TrendlineIsTarget:           &off,
		TrendlineBetweenBoxes:       &on,
		LegResetOnWaveBreak:         &off,
		FlagWallTarget:              &on,
		OrbEscapeCompletely:         &on,
		Mentor5mBoxSource:           "first",
		MentorBreakDirectionLock:    &off,
		MentorSmallRangeGuard:       &on, // default OFF since #61 final — a stored true must win
		MentorMidRangeRejectSide:    &off,
	})
	checks := []struct {
		name string
		ok   bool
	}{
		{"15m_ib_requires_5m_isb", cfg.FifteenMinIBRule == mentor.FifteenMinUntilEscape},
		{"tf_conflict_end", cfg.TFConflictEnd == mentor.TFConflictBothBoxes},
		{"news_wait_both_lines", cfg.NewsWaitBothLines},
		{"news_bars_excluded", cfg.NewsBarsExcluded},
		{"isb_box_escape", cfg.ISBBoxEscapeMode == mentor.ISBBoxEscapeBody},
		{"ema34_loss_relock", cfg.EmaLossRelock == mentor.EmaRelockSession},
		{"orb_edge_veto_pts", cfg.OrbEdgeVetoPts == 3},
		{"d43_trend_source", cfg.D43TrendSource == mentor.D43TrendStructure},
		{"level_entries_need_trigger_side", cfg.LevelEntriesNeedTriggerSide},
		{"trendline_is_target", !cfg.TrendlineIsTarget},
		{"trendline_between_boxes", cfg.TrendlineBetweenBoxes},
		{"leg_reset_on_wave_break", !cfg.LegResetOnWaveBreak},
		{"flag_wall_target", cfg.FlagWallTarget},
		{"orb_escape_completely", cfg.OrbEscapeCompletely},
		{"mentor_5m_box_source", cfg.FiveMinBoxSource == mentor.FiveMinBoxFirst},
		{"mentor_break_direction_lock", !cfg.BreakDirectionLock},
		{"mentor_small_range_guard", cfg.SmallRangeGuard},
		{"mentor_mid_range_reject_side", !cfg.MidRangeRejectSide},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("stored %s did not reach the evaluator config", c.name)
		}
	}
}

// A bad enum / out-of-range number fails closed to the default (L7: counted).
func TestCourseEntryKnobsBadValueFailsClosed(t *testing.T) {
	cfg := tuningTick(t, &store.MentorTuning{
		FifteenMinIBRequires5mISB: "sometimes",
		ISBBoxEscape:              "wick",
		OrbEdgeVetoPts:            999,
		Mentor5mBoxSource:         "oldest",
	})
	if cfg.FiveMinBoxSource != mentor.FiveMinBoxMerge {
		t.Fatalf("a bad mentor_5m_box_source must fall back to merge (the default), got %q", cfg.FiveMinBoxSource)
	}
	if cfg.FifteenMinIBRule != mentor.FifteenMinRequire5mISB || cfg.ISBBoxEscapeMode != mentor.ISBBoxEscapeCloseBoxTF || cfg.OrbEdgeVetoPts != 0 {
		t.Fatalf("bad values must fall back to the defaults: %q %q %g", cfg.FifteenMinIBRule, cfg.ISBBoxEscapeMode, cfg.OrbEdgeVetoPts)
	}
}

// The boot line prints the RESOLVED values (never literals): an override shows.
func TestCourseEntryBootLineReadsTheResolver(t *testing.T) {
	on := true
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{TFConflictEnd: "both_boxes", NewsWaitBothLines: &on}})
	line := mentorCourseEntryBootLine(at, at.mentorRiskControl())
	for _, want := range []string{"tf_conflict_end=both_boxes", "news_wait_both_lines=true", "15m_ib_requires_5m_isb=require_5m_isb", "isb_box_escape=close5m", "trendline_is_target=true", "orb_escape_completely=false", "htf_gate_news_only=true", "phl_room_multiple=1", "isb_target_fill_close=false", "isb_no_level_fill_close=true", "trigger_from_open=true", "phl_min_above_old_extreme_pts=0", "mentor_5m_box_source=merge", "mentor_break_direction_lock=true", "mentor_small_range_guard=false", "mentor_mid_range_reject_side=true"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line %q is missing %q", line, want)
		}
	}
}
