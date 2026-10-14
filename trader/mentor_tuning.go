package trader

import (
	"fmt"
	"strings"

	"vl/kernel/mentor"
	"vl/store"
)

// ── MENTOR TUNING (K3, OWNER RULING 2026-10-04 "do all as mentor") ──────────
//
// risk_control.mentor_tuning carries the ten method numbers the owner may tune
// from Studio. mentorTuningResolve is the ONE resolver: it starts from the
// kernel's DefaultConfig (the ruled defaults live THERE, once), applies each
// stored override that is set and in range, and fails closed to the default —
// counted — for anything out of range. The evaluator config and the trader's
// rule gate both read its result, so a swing max stop or a spent-day cap is
// never two numbers.

// mentorTuned is the resolved view: every field is a usable value.
type mentorTuned struct {
	TriggerSchool          int
	PingPongMinGapPts      float64
	PingPongCandleMaxPts   float64
	PingPongCandleLookback int
	LevelMaxVisits         int
	OrbGateEnabled         bool
	ISBReverseEMA9Enabled  bool
	HTFGateNewsOnly        bool
	Exec2mAfter30m         bool
	DayGateSpentPts        float64
	DayGateTargetCapPts    float64
	SwingMaxStopPts        float64

	// Release #25 course-entry knobs (fix/mentor-course-entries).
	FifteenMinIBRule            string
	TFConflictEnd               string
	NewsWaitBothLines           bool
	NewsBarsExcluded            bool
	ISBBoxEscapeMode            string
	EmaLossRelock               string
	OrbEdgeVetoPts              float64
	D43TrendSource              string
	LevelEntriesNeedTriggerSide bool
	TrendlineIsTarget           bool
	TrendlineBetweenBoxes       bool
	LegResetOnWaveBreak         bool
	FlagWallTarget              bool
}

func mentorTuningResolve(at *AutoTrader, rc *store.RiskControlConfig) mentorTuned {
	d := mentor.DefaultConfig()
	out := mentorTuned{
		TriggerSchool:          d.TriggerSchool,
		PingPongMinGapPts:      d.PingPongMinGapPts,
		PingPongCandleMaxPts:   d.PingPongCandleMaxPts,
		PingPongCandleLookback: d.PingPongCandleLookback,
		LevelMaxVisits:         d.LevelMaxVisits,
		OrbGateEnabled:         d.OrbGateEnabled,
		ISBReverseEMA9Enabled:  d.ISBReverseEMA9Enabled,
		HTFGateNewsOnly:        d.HTFGateNewsOnly,
		Exec2mAfter30m:         d.Exec2mAfter30m,
		DayGateSpentPts:        d.DayGateSpentPts,
		DayGateTargetCapPts:    d.DayGateTargetCapPts,
		SwingMaxStopPts:        d.Swing.MaxStopPts,

		FifteenMinIBRule:            d.FifteenMinIBRule,
		TFConflictEnd:               d.TFConflictEnd,
		NewsWaitBothLines:           d.NewsWaitBothLines,
		NewsBarsExcluded:            d.NewsBarsExcluded,
		ISBBoxEscapeMode:            d.ISBBoxEscapeMode,
		EmaLossRelock:               d.EmaLossRelock,
		OrbEdgeVetoPts:              d.OrbEdgeVetoPts,
		D43TrendSource:              d.D43TrendSource,
		LevelEntriesNeedTriggerSide: d.LevelEntriesNeedTriggerSide,
		TrendlineIsTarget:           d.TrendlineIsTarget,
		TrendlineBetweenBoxes:       d.TrendlineBetweenBoxes,
		LegResetOnWaveBreak:         d.LegResetOnWaveBreak,
		FlagWallTarget:              d.FlagWallTarget,
	}
	if rc == nil || rc.MentorTuning == nil {
		return out
	}
	t := rc.MentorTuning
	bad := func() { at.mentorCount("tuning_bad_value") }

	if t.TriggerSchool != 0 {
		if t.TriggerSchool == 1 || t.TriggerSchool == 2 {
			out.TriggerSchool = t.TriggerSchool
		} else {
			bad()
		}
	}
	pick := func(v, lo, hi float64, dst *float64) {
		if v == 0 {
			return
		}
		if v >= lo && v <= hi {
			*dst = v
			return
		}
		bad()
	}
	pick(t.PingPongMinGapPts, 0.01, 500, &out.PingPongMinGapPts)
	pick(t.PingPongCandleMaxPts, 0.01, 200, &out.PingPongCandleMaxPts)
	pick(t.DayGateSpentPts, 50, 2000, &out.DayGateSpentPts)
	pick(t.DayGateTargetCapPts, 1, 100, &out.DayGateTargetCapPts)
	pick(t.SwingMaxStopPts, 30, 300, &out.SwingMaxStopPts)

	if t.PingPongCandleLookback != 0 {
		if t.PingPongCandleLookback >= 1 && t.PingPongCandleLookback <= 200 {
			out.PingPongCandleLookback = t.PingPongCandleLookback
		} else {
			bad()
		}
	}
	if t.LevelMaxVisits != nil {
		if v := *t.LevelMaxVisits; v >= 0 && v <= 20 {
			out.LevelMaxVisits = v // 0 = the per-day cap is off
		} else {
			bad()
		}
	}
	if t.OrbGateEnabled != nil {
		out.OrbGateEnabled = *t.OrbGateEnabled
	}
	if t.ISBReverseEMA9Enabled != nil {
		out.ISBReverseEMA9Enabled = *t.ISBReverseEMA9Enabled
	}
	if t.HTFGateNewsOnly != nil {
		out.HTFGateNewsOnly = *t.HTFGateNewsOnly
	}
	if t.Exec2mAfter30m != nil {
		out.Exec2mAfter30m = *t.Exec2mAfter30m
	}

	// Release #25 course-entry knobs: absent → the DefaultConfig value; an
	// unknown enum value fails closed to that default and is counted (L7).
	enum := func(v string, dst *string, allowed ...string) {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
				return
			}
		}
		at.mentorCount("tuning_bad_value")
	}
	enum(t.FifteenMinIBRequires5mISB, &out.FifteenMinIBRule, mentor.FifteenMinOff, mentor.FifteenMinRequire5mISB, mentor.FifteenMinUntilEscape)
	enum(t.TFConflictEnd, &out.TFConflictEnd, mentor.TFConflictFirstBox, mentor.TFConflictBothBoxes)
	enum(t.ISBBoxEscape, &out.ISBBoxEscapeMode, mentor.ISBBoxEscapeClose, mentor.ISBBoxEscapeBody)
	enum(t.EMA34LossRelock, &out.EmaLossRelock, mentor.EmaRelockSession, mentor.EmaRelockUntilZoneExit)
	enum(t.D43TrendSource, &out.D43TrendSource, mentor.D43TrendTriggerLine, mentor.D43TrendStructure)
	pick(t.OrbEdgeVetoPts, 0.25, 50, &out.OrbEdgeVetoPts)
	flag := func(p *bool, dst *bool) {
		if p != nil {
			*dst = *p
		}
	}
	flag(t.NewsWaitBothLines, &out.NewsWaitBothLines)
	flag(t.NewsBarsExcluded, &out.NewsBarsExcluded)
	flag(t.LevelEntriesNeedTriggerSide, &out.LevelEntriesNeedTriggerSide)
	flag(t.TrendlineIsTarget, &out.TrendlineIsTarget)
	flag(t.TrendlineBetweenBoxes, &out.TrendlineBetweenBoxes)
	flag(t.LegResetOnWaveBreak, &out.LegResetOnWaveBreak)
	flag(t.FlagWallTarget, &out.FlagWallTarget)
	return out
}

// applyMentorTuning writes the resolved numbers onto an evaluator config.
func applyMentorTuning(at *AutoTrader, cfg *mentor.Config, rc *store.RiskControlConfig) {
	t := mentorTuningResolve(at, rc)
	cfg.TriggerSchool = t.TriggerSchool
	cfg.PingPongMinGapPts = t.PingPongMinGapPts
	cfg.PingPongCandleMaxPts = t.PingPongCandleMaxPts
	cfg.PingPongCandleLookback = t.PingPongCandleLookback
	cfg.LevelMaxVisits = t.LevelMaxVisits
	cfg.OrbGateEnabled = t.OrbGateEnabled
	cfg.ISBReverseEMA9Enabled = t.ISBReverseEMA9Enabled
	cfg.HTFGateNewsOnly = t.HTFGateNewsOnly
	cfg.Exec2mAfter30m = t.Exec2mAfter30m
	cfg.DayGateSpentPts = t.DayGateSpentPts
	cfg.DayGateTargetCapPts = t.DayGateTargetCapPts
	cfg.Swing.MaxStopPts = t.SwingMaxStopPts
	cfg.FifteenMinIBRule = t.FifteenMinIBRule
	cfg.TFConflictEnd = t.TFConflictEnd
	cfg.NewsWaitBothLines = t.NewsWaitBothLines
	cfg.NewsBarsExcluded = t.NewsBarsExcluded
	cfg.ISBBoxEscapeMode = t.ISBBoxEscapeMode
	cfg.EmaLossRelock = t.EmaLossRelock
	cfg.OrbEdgeVetoPts = t.OrbEdgeVetoPts
	cfg.D43TrendSource = t.D43TrendSource
	cfg.LevelEntriesNeedTriggerSide = t.LevelEntriesNeedTriggerSide
	cfg.TrendlineIsTarget = t.TrendlineIsTarget
	cfg.TrendlineBetweenBoxes = t.TrendlineBetweenBoxes
	cfg.LegResetOnWaveBreak = t.LegResetOnWaveBreak
	cfg.FlagWallTarget = t.FlagWallTarget
}

// mentorCourseEntryBootLine renders the release #25 course-entry knobs for
// one trader. Every value is READ from mentorTuningResolve (the same resolver
// the evaluator config reads) — never a literal (L7).
func mentorCourseEntryBootLine(at *AutoTrader, rc *store.RiskControlConfig) string {
	t := mentorTuningResolve(at, rc)
	id := "n/a"
	if at != nil && at.id != "" {
		id = at.id
	}
	return fmt.Sprintf("🧑‍🏫 mentor course-entry knobs for trader %s: 15m_ib_requires_5m_isb=%s tf_conflict_end=%s news_wait_both_lines=%v news_bars_excluded=%v isb_box_escape=%s ema34_loss_relock=%s orb_edge_veto_pts=%g d43_trend_source=%s level_entries_need_trigger_side=%v trendline_is_target=%v trendline_between_boxes=%v leg_reset_on_wave_break=%v flag_wall_target=%v",
		id, t.FifteenMinIBRule, t.TFConflictEnd, t.NewsWaitBothLines, t.NewsBarsExcluded, t.ISBBoxEscapeMode, t.EmaLossRelock,
		t.OrbEdgeVetoPts, t.D43TrendSource, t.LevelEntriesNeedTriggerSide, t.TrendlineIsTarget, t.TrendlineBetweenBoxes, t.LegResetOnWaveBreak, t.FlagWallTarget)
}
