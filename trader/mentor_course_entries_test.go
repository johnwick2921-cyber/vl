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
		{"tf_conflict_end=first_box (b)", cfg.TFConflictEnd == mentor.TFConflictFirstBox},
		{"news_wait_both_lines=false (b)", !cfg.NewsWaitBothLines},
		{"news_bars_excluded=false (b)", !cfg.NewsBarsExcluded},
		{"isb_box_escape=body (a)", cfg.ISBBoxEscapeMode == mentor.ISBBoxEscapeBody},
		{"ema34_loss_relock=until_zone_exit (a)", cfg.EmaLossRelock == mentor.EmaRelockUntilZoneExit},
		{"orb_edge_veto_pts=0 (b)", cfg.OrbEdgeVetoPts == 0},
		{"d43_trend_source=trigger_line (b)", cfg.D43TrendSource == mentor.D43TrendTriggerLine},
		{"level_entries_need_trigger_side=false (b)", !cfg.LevelEntriesNeedTriggerSide},
		{"trendline_is_target=true (a)", cfg.TrendlineIsTarget},
		{"trendline_between_boxes=false (b)", !cfg.TrendlineBetweenBoxes},
		{"leg_reset_on_wave_break=true (a)", cfg.LegResetOnWaveBreak},
		{"flag_wall_target=false (b)", !cfg.FlagWallTarget},
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
		ISBBoxEscape:                "close",
		EMA34LossRelock:             "session",
		OrbEdgeVetoPts:              3,
		D43TrendSource:              "structure",
		LevelEntriesNeedTriggerSide: &on,
		TrendlineIsTarget:           &off,
		TrendlineBetweenBoxes:       &on,
		LegResetOnWaveBreak:         &off,
		FlagWallTarget:              &on,
	})
	checks := []struct {
		name string
		ok   bool
	}{
		{"15m_ib_requires_5m_isb", cfg.FifteenMinIBRule == mentor.FifteenMinUntilEscape},
		{"tf_conflict_end", cfg.TFConflictEnd == mentor.TFConflictBothBoxes},
		{"news_wait_both_lines", cfg.NewsWaitBothLines},
		{"news_bars_excluded", cfg.NewsBarsExcluded},
		{"isb_box_escape", cfg.ISBBoxEscapeMode == mentor.ISBBoxEscapeClose},
		{"ema34_loss_relock", cfg.EmaLossRelock == mentor.EmaRelockSession},
		{"orb_edge_veto_pts", cfg.OrbEdgeVetoPts == 3},
		{"d43_trend_source", cfg.D43TrendSource == mentor.D43TrendStructure},
		{"level_entries_need_trigger_side", cfg.LevelEntriesNeedTriggerSide},
		{"trendline_is_target", !cfg.TrendlineIsTarget},
		{"trendline_between_boxes", cfg.TrendlineBetweenBoxes},
		{"leg_reset_on_wave_break", !cfg.LegResetOnWaveBreak},
		{"flag_wall_target", cfg.FlagWallTarget},
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
	})
	if cfg.FifteenMinIBRule != mentor.FifteenMinRequire5mISB || cfg.ISBBoxEscapeMode != mentor.ISBBoxEscapeBody || cfg.OrbEdgeVetoPts != 0 {
		t.Fatalf("bad values must fall back to the defaults: %q %q %g", cfg.FifteenMinIBRule, cfg.ISBBoxEscapeMode, cfg.OrbEdgeVetoPts)
	}
}

// The boot line prints the RESOLVED values (never literals): an override shows.
func TestCourseEntryBootLineReadsTheResolver(t *testing.T) {
	on := true
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{TFConflictEnd: "both_boxes", NewsWaitBothLines: &on}})
	line := mentorCourseEntryBootLine(at, at.mentorRiskControl())
	for _, want := range []string{"tf_conflict_end=both_boxes", "news_wait_both_lines=true", "15m_ib_requires_5m_isb=require_5m_isb", "isb_box_escape=body", "trendline_is_target=true"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line %q is missing %q", line, want)
		}
	}
}
