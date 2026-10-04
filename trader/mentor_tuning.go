package trader

import (
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
	DayGateSpentPts        float64
	DayGateTargetCapPts    float64
	SwingMaxStopPts        float64
}

func mentorTuningResolve(rc *store.RiskControlConfig) mentorTuned {
	d := mentor.DefaultConfig()
	out := mentorTuned{
		TriggerSchool:          d.TriggerSchool,
		PingPongMinGapPts:      d.PingPongMinGapPts,
		PingPongCandleMaxPts:   d.PingPongCandleMaxPts,
		PingPongCandleLookback: d.PingPongCandleLookback,
		LevelMaxVisits:         d.LevelMaxVisits,
		OrbGateEnabled:         d.OrbGateEnabled,
		ISBReverseEMA9Enabled:  d.ISBReverseEMA9Enabled,
		DayGateSpentPts:        d.DayGateSpentPts,
		DayGateTargetCapPts:    d.DayGateTargetCapPts,
		SwingMaxStopPts:        d.Swing.MaxStopPts,
	}
	if rc == nil || rc.MentorTuning == nil {
		return out
	}
	t := rc.MentorTuning
	bad := func() { mentorCount("tuning_bad_value") }

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
	return out
}

// applyMentorTuning writes the resolved numbers onto an evaluator config.
func applyMentorTuning(cfg *mentor.Config, rc *store.RiskControlConfig) {
	t := mentorTuningResolve(rc)
	cfg.TriggerSchool = t.TriggerSchool
	cfg.PingPongMinGapPts = t.PingPongMinGapPts
	cfg.PingPongCandleMaxPts = t.PingPongCandleMaxPts
	cfg.PingPongCandleLookback = t.PingPongCandleLookback
	cfg.LevelMaxVisits = t.LevelMaxVisits
	cfg.OrbGateEnabled = t.OrbGateEnabled
	cfg.ISBReverseEMA9Enabled = t.ISBReverseEMA9Enabled
	cfg.DayGateSpentPts = t.DayGateSpentPts
	cfg.DayGateTargetCapPts = t.DayGateTargetCapPts
	cfg.Swing.MaxStopPts = t.SwingMaxStopPts
}
