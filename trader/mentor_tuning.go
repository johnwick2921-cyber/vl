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
	BEOffsetTicks          int // m472 A5: 0 = OFF (BE at the entry exactly)
	DayGateSpentPts        float64
	DayGateTargetCapPts    float64
	HTFOpposedRangeHaltPts float64 // answers-1112 Q1 extra rule: 0 = OFF
	SwingMaxStopPts        float64
	SwingMinStopPts        float64

	// Release #25 course-entry knobs (fix/mentor-course-entries).
	FifteenMinIBRule            string
	Early15mRead                bool   // m465 P1 D4.2-R2/R11, D4.3-R21
	MTFConfluenceSource         string // m465 P1 D4.2-R17
	TriggerEscalation           bool   // m465 P1 D3.4-R43
	TFConflictEnd               string
	NewsWaitBothLines           bool
	NewsBarsExcluded            bool
	ISBBoxEscapeMode            string
	EmaLossRelock               string
	OrbEdgeVetoPts              float64
	OrbEscapeCompletely         bool
	WickMicroscalp              bool // m465 P5: D4.3 wick microscalp (default OFF)
	WickSafePlay                bool // m465 P5: D4.3-R20 safe play (default OFF)
	OrbISBFractionOutside       float64
	D43TrendSource              string
	LevelEntriesNeedTriggerSide bool
	TrendlineIsTarget           bool
	TrendlineBetweenBoxes       bool
	LegResetOnWaveBreak         bool
	FlagWallTarget              bool
	BoxNewZoneAfterEscape       bool

	// Release #52 W11 invented-constant knobs (all "ours, not the course";
	// defaults = the kernel's DefaultConfig, i.e. unchanged).
	ISBStackCancelCandle2m int
	ISBStackTinyStopPts    float64
	EveningSlowPts         float64
	EveningSlowFromHour    int
	EveningSlowWindowMin   int
	NearBoxRoomMultiple    float64
	NearBoxRoomPHL         bool
	NearBoxRoomPHLMultiple float64 // answers-1112 Q2: resolved (nil → the resolved NearBoxRoomMultiple)
	PHLTargetShyPts        float64
	PHLOldExtreme1mK       int // PHL-1: 4 = default; 0 = the pre-PHL-1 source
	ISBOldExtremePts       float64
	ISBInRangeGapPts       float64

	// m465 P3 (release #60) structure-fidelity knobs (defaults = the course).
	TrendlineEntryTouch  string
	NearBoxBetweenExempt bool
	PHLFlatLow           string
	ISBReverseTouch      string

	// m466 P2 (release #61) levels-and-touches knobs (defaults = the course).
	UnoKeyLevels       string
	SessionOpenLevels  string
	LevelArmLineCancel string
	PHLPullbackLow     string
	// m466 P1 (release #61) boxes, breaks, ranges (defaults = the course).
	FiveMinBoxSource   string
	BreakDirectionLock bool
	SmallRangeGuard    bool // default OFF (release #61 final): course gives no count / margin
	MidRangeRejectSide bool
	// isbTF audit F4/F6 (defaults = today's behaviour; the course states none).
	SwingISBOnePerCandle       bool
	CrossingISBLookback        int
	CrossingISBMin             int
	KeyLevelDeleteOn1HClose    string  // LL-1
	PHLMinAbovePriorPts        float64 // G7
	RollingLeftBreakCancel     bool    // D2-G6
	RollingStaleTouchReleases  bool    // FIX-1112 B
	RollingKeypairTapeExempt   bool    // FIX-1112 B
	HTFTrendlineFlip           bool    // D3-G12
	TrendlineMaxSlopePtsPerBar float64 // D3-G14
	TrendlineMinBouncePts      float64 // D3-G10
	KnockKnockZone             bool    // D2-G10
	TrendlineFirstBottomAnchor bool    // D3-G9
	BoxFlipReversible          bool    // D3-G7
	BoxCarriedStaysLive        bool    // D3-G4
	BoxSwingBothRoles          bool    // D3-G2
	BoxPreOpenLookbackMin      float64 // D3-G1
	TriggerFromOpen            bool    // G4
	ISBTargetFillClose         bool    // D2-G3
	ISBNoLevelFillClose        bool    // answers-1112 Q3
	TouchStopCoverLaterWick    bool    // answers-1112 Q7
	BoxTrendlinePartialNotRoom bool    // answers-1112 Q12
	PHL5mBoxSameDirAllowed     bool    // answers-1112 Q9
	PHLRoomMultiple            float64 // D2-G2
	KeyLevelBreakRetest        string  // LL-2
	KeyLevelRearm              string  // D3-G15
	ISBReverseTrend            string  // D5.3-R43/R44
	Counter5mReduceSize        string  // D5.4-SCALP
	KeyLevelHistorySource      string  // LL-4
}

// phlNearBoxFollow resolves the kernel's NearBoxRoomPHLMultiple default
// (NearBoxRoomPHLMultipleFollow, negative) to the near-box multiple it follows,
// so the resolver, the effective-settings row and the evaluator all carry the
// number the PHL/PLH leg really asks (answers-1112 Q2).
func phlNearBoxFollow(phl, nearBox float64) float64 {
	if phl < 0 {
		return nearBox
	}
	return phl
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
		HTFOpposedRangeHaltPts: d.HTFOpposedRangeHaltPts,
		SwingMaxStopPts:        d.Swing.MaxStopPts,
		SwingMinStopPts:        d.Swing.MinStopPts,

		FifteenMinIBRule:            d.FifteenMinIBRule,
		Early15mRead:                d.Early15mRead,
		MTFConfluenceSource:         d.MTFConfluenceSource,
		TriggerEscalation:           d.TriggerEscalation,
		TFConflictEnd:               d.TFConflictEnd,
		NewsWaitBothLines:           d.NewsWaitBothLines,
		NewsBarsExcluded:            d.NewsBarsExcluded,
		ISBBoxEscapeMode:            d.ISBBoxEscapeMode,
		EmaLossRelock:               d.EmaLossRelock,
		OrbEdgeVetoPts:              d.OrbEdgeVetoPts,
		OrbEscapeCompletely:         d.OrbEscapeCompletely,
		WickMicroscalp:              d.WickMicroscalpEnabled,
		WickSafePlay:                d.WickSafePlay,
		OrbISBFractionOutside:       d.OrbISBFractionOutside,
		D43TrendSource:              d.D43TrendSource,
		LevelEntriesNeedTriggerSide: d.LevelEntriesNeedTriggerSide,
		TrendlineIsTarget:           d.TrendlineIsTarget,
		TrendlineBetweenBoxes:       d.TrendlineBetweenBoxes,
		LegResetOnWaveBreak:         d.LegResetOnWaveBreak,
		FlagWallTarget:              d.FlagWallTarget,
		BoxNewZoneAfterEscape:       d.BoxNewZoneAfterEscape,
		SwingISBOnePerCandle:        !d.Swing.ISBRepeatAfterFill,
		KeyLevelHistorySource:       d.KeyLevelHistorySource,
		KeyLevelBreakRetest:         d.KeyLevelBreakRetest,
		KeyLevelRearm:               d.KeyLevelRearm,
		ISBReverseTrend:             d.ISBReverseTrend,
		Counter5mReduceSize:         d.Counter5mReduceSize,
		KeyLevelDeleteOn1HClose:     d.KeyLevelDeleteOn1HClose,
		PHLMinAbovePriorPts:         d.PHLMinAbovePriorPts,
		RollingLeftBreakCancel:      d.RollingLeftBreakCancel,
		RollingStaleTouchReleases:   d.RollingStaleTouchReleases,
		RollingKeypairTapeExempt:    d.RollingKeypairTapeExempt,
		HTFTrendlineFlip:            d.HTFTrendlineFlip,
		TrendlineMaxSlopePtsPerBar:  d.TrendlineMaxSlopePtsPerBar,
		TrendlineMinBouncePts:       d.TrendlineMinBouncePts,
		KnockKnockZone:              d.KnockKnockZone,
		TrendlineFirstBottomAnchor:  d.TrendlineFirstBottomAnchor,
		BoxFlipReversible:           d.BoxFlipReversible,
		BoxCarriedStaysLive:         d.BoxCarriedStaysLive,
		BoxSwingBothRoles:           d.BoxSwingBothRoles,
		BoxPreOpenLookbackMin:       d.BoxPreOpenLookbackMin,
		TriggerFromOpen:             d.TriggerFromOpen,
		ISBTargetFillClose:          d.ISBTargetFillClose,
		ISBNoLevelFillClose:         d.ISBNoLevelFillClose,
		TouchStopCoverLaterWick:     d.Swing.CoverLaterWick,
		BoxTrendlinePartialNotRoom:  d.BoxTrendlinePartialNotRoom,
		PHL5mBoxSameDirAllowed:      d.PHL5mBoxSameDirAllowed,
		PHLRoomMultiple:             d.PHLRoomMultiple,
		CrossingISBLookback:         d.CrossingISBLookback,
		CrossingISBMin:              d.CrossingISBMin,

		ISBStackCancelCandle2m: d.ISBStackCancelCandle2m,
		ISBStackTinyStopPts:    d.ISBStackTinyStopPts,
		EveningSlowPts:         d.EveningSlowPts,
		EveningSlowFromHour:    d.EveningSlowFromHour,
		EveningSlowWindowMin:   d.EveningSlowWindowMin,
		NearBoxRoomMultiple:    d.NearBoxRoomMultiple,
		NearBoxRoomPHL:         d.NearBoxRoomPHL,
		NearBoxRoomPHLMultiple: phlNearBoxFollow(d.NearBoxRoomPHLMultiple, d.NearBoxRoomMultiple),
		PHLTargetShyPts:        d.PHLTargetShyPts,
		PHLOldExtreme1mK:       d.PHLOldExtreme1mK,
		ISBOldExtremePts:       d.ISBOldExtremePts,
		ISBInRangeGapPts:       d.ISBInRangeGapPts,

		TrendlineEntryTouch:  d.TrendlineEntryTouch,
		NearBoxBetweenExempt: d.NearBoxBetweenExempt,
		PHLFlatLow:           d.PHLFlatLow,
		ISBReverseTouch:      d.ISBReverseTouch,

		UnoKeyLevels:       d.UnoKeyLevels,
		SessionOpenLevels:  d.SessionOpenLevels,
		LevelArmLineCancel: d.LevelArmLineCancel,
		PHLPullbackLow:     d.PHLPullbackLow,
		FiveMinBoxSource:   d.FiveMinBoxSource,
		BreakDirectionLock: d.BreakDirectionLock,
		SmallRangeGuard:    d.SmallRangeGuard,
		MidRangeRejectSide: d.MidRangeRejectSide,
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
	pick(t.HTFOpposedRangeHaltPts, 50, 2000, &out.HTFOpposedRangeHaltPts) // answers-1112 Q1 extra rule: 0 / out of range = OFF
	pick(t.SwingMaxStopPts, 30, 300, &out.SwingMaxStopPts)
	// W26 P2-4: 0 = OFF (R5). A floor above the MaxStopPts ceiling is refused.
	pick(t.SwingMinStopPts, 0.01, 100, &out.SwingMinStopPts)

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
	if t.BEOffsetTicks != nil {
		if v := *t.BEOffsetTicks; v >= 0 && v <= mentorBEOffsetMaxTicks {
			out.BEOffsetTicks = v // 0 = OFF
		} else {
			bad()
		}
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
	enum(t.MTFConfluenceSource, &out.MTFConfluenceSource, mentor.MTFConfluenceStandingBoxes, mentor.MTFConfluenceLastPair) // m465 P1
	enum(t.TFConflictEnd, &out.TFConflictEnd, mentor.TFConflictFirstBox, mentor.TFConflictBothBoxes)
	enum(t.ISBBoxEscape, &out.ISBBoxEscapeMode, mentor.ISBBoxEscapeCloseBoxTF, mentor.ISBBoxEscapeClose, mentor.ISBBoxEscapeBody) // m473 B3: close5m default [Oct D4.3 @ 65:00-66:13]
	enum(t.EMA34LossRelock, &out.EmaLossRelock, mentor.EmaRelockSession, mentor.EmaRelockUntilZoneExit)
	enum(t.D43TrendSource, &out.D43TrendSource, mentor.D43TrendTriggerLine, mentor.D43TrendStructure)
	pick(t.OrbEdgeVetoPts, 0.25, 50, &out.OrbEdgeVetoPts)
	// Rule 5 (release #31, fold #565 D5): nil → 0.80 (ON); an explicit 0 → OFF
	// (the kernel reads <= 0 as off); (0,1] → the fraction. The pointer lets a
	// Studio 0 reach the kernel instead of being dropped by omitempty.
	if t.OrbISBFractionOutside != nil {
		v := *t.OrbISBFractionOutside
		switch {
		case v == 0:
			out.OrbISBFractionOutside = 0
		case v > 0 && v <= 1:
			out.OrbISBFractionOutside = v
		default:
			bad()
		}
	}
	flag := func(p *bool, dst *bool) {
		if p != nil {
			*dst = *p
		}
	}
	flag(t.Early15mRead, &out.Early15mRead)           // m465 P1
	flag(t.TriggerEscalation, &out.TriggerEscalation) // m465 P1
	flag(t.NewsWaitBothLines, &out.NewsWaitBothLines)
	flag(t.NewsBarsExcluded, &out.NewsBarsExcluded)
	flag(t.LevelEntriesNeedTriggerSide, &out.LevelEntriesNeedTriggerSide)
	flag(t.TrendlineIsTarget, &out.TrendlineIsTarget)
	flag(t.TrendlineBetweenBoxes, &out.TrendlineBetweenBoxes)
	flag(t.LegResetOnWaveBreak, &out.LegResetOnWaveBreak)
	flag(t.FlagWallTarget, &out.FlagWallTarget)
	flag(t.BoxNewZoneAfterEscape, &out.BoxNewZoneAfterEscape)
	flag(t.OrbEscapeCompletely, &out.OrbEscapeCompletely)
	flag(t.WickMicroscalp, &out.WickMicroscalp)
	flag(t.WickSafePlay, &out.WickSafePlay)
	flag(t.RollingLeftBreakCancel, &out.RollingLeftBreakCancel)         // D2-G6
	flag(t.RollingStaleTouchReleases, &out.RollingStaleTouchReleases)   // FIX-1112 B
	flag(t.RollingKeypairTapeExempt, &out.RollingKeypairTapeExempt)     // FIX-1112 B
	flag(t.HTFTrendlineFlip, &out.HTFTrendlineFlip)                     // D3-G12
	flag(t.KnockKnockZone, &out.KnockKnockZone)                         // D2-G10
	flag(t.TrendlineFirstBottomAnchor, &out.TrendlineFirstBottomAnchor) // D3-G9
	flag(t.BoxFlipReversible, &out.BoxFlipReversible)                   // D3-G7
	flag(t.BoxCarriedStaysLive, &out.BoxCarriedStaysLive)               // D3-G4
	flag(t.BoxSwingBothRoles, &out.BoxSwingBothRoles)                   // D3-G2
	flag(t.TriggerFromOpen, &out.TriggerFromOpen)                       // G4
	flag(t.ISBTargetFillClose, &out.ISBTargetFillClose)                 // D2-G3
	flag(t.ISBNoLevelFillClose, &out.ISBNoLevelFillClose)               // answers-1112 Q3
	flag(t.TouchStopCoverLaterWick, &out.TouchStopCoverLaterWick)       // answers-1112 Q7
	flag(t.BoxTrendlinePartialNotRoom, &out.BoxTrendlinePartialNotRoom) // answers-1112 Q12
	flag(t.PHL5mBoxSameDirAllowed, &out.PHL5mBoxSameDirAllowed)         // answers-1112 Q9

	// Release #52 W11 invented-constant knobs. Zero = unset = the default for
	// the plain ones; the pointer ones let an explicit 0 through (0 is a real
	// value there: near-box OFF, PHL target AT the extreme, an exact touch).
	pick(t.ISBStackTinyStopPts, 0.01, 25, &out.ISBStackTinyStopPts)
	pick(t.EveningSlowPts, 1, 1000, &out.EveningSlowPts)
	pick(t.ISBInRangeGapPts, 0.01, 500, &out.ISBInRangeGapPts)
	pickInt := func(v, lo, hi int, dst *int) {
		if v == 0 {
			return
		}
		if v >= lo && v <= hi {
			*dst = v
			return
		}
		bad()
	}
	pickInt(t.ISBStackCancelCandle2m, 3, 12, &out.ISBStackCancelCandle2m)
	// PHL-1: 0 / absent = the default width; -1 = the pre-PHL-1 source
	// (Config 0); 1..10 = k. Anything else fails closed to the default, counted.
	if t.PHLOldExtreme1mK == -1 {
		out.PHLOldExtreme1mK = 0
	} else {
		pickInt(t.PHLOldExtreme1mK, mentor.PHLOldExtreme1mKMin, mentor.PHLOldExtreme1mKMax, &out.PHLOldExtreme1mK)
	}
	pickInt(t.EveningSlowFromHour, 15, 17, &out.EveningSlowFromHour)
	pickInt(t.EveningSlowWindowMin, 1, 600, &out.EveningSlowWindowMin)
	pickPtr := func(v *float64, lo, hi float64, dst *float64) {
		if v == nil {
			return
		}
		if *v >= lo && *v <= hi {
			*dst = *v
			return
		}
		bad()
	}
	pickPtr(t.NearBoxRoomMultiple, 0, 10, &out.NearBoxRoomMultiple)
	// answers-1112 Q2: nil (or a bad value) keeps the kernel's follow default,
	// resolved to the near_box_room_multiple just picked — today exactly.
	out.NearBoxRoomPHLMultiple = phlNearBoxFollow(d.NearBoxRoomPHLMultiple, out.NearBoxRoomMultiple)
	pickPtr(t.NearBoxRoomPHLMultiple, 0, 10, &out.NearBoxRoomPHLMultiple)
	pickPtr(t.PHLMinAbovePriorPts, 0, 100, &out.PHLMinAbovePriorPts)                // G7
	pickPtr(t.TrendlineMaxSlopePtsPerBar, 0, 1000, &out.TrendlineMaxSlopePtsPerBar) // D3-G14
	pickPtr(t.TrendlineMinBouncePts, 0, 1000, &out.TrendlineMinBouncePts)           // D3-G10
	pickPtr(t.BoxPreOpenLookbackMin, 0, 240, &out.BoxPreOpenLookbackMin)            // D3-G1
	pickPtr(t.PHLRoomMultiple, 0, 10, &out.PHLRoomMultiple)                         // D2-G2
	pickPtr(t.PHLTargetShyPts, 0, 50, &out.PHLTargetShyPts)
	pickPtr(t.ISBOldExtremePts, 0, 20, &out.ISBOldExtremePts)
	flag(t.NearBoxRoomPHL, &out.NearBoxRoomPHL)

	// m465 P3 (release #60): unset = the course default; a stored value outside
	// the enum fails closed to the default, counted (tuning_bad_value).
	enum(t.TrendlineEntryTouch, &out.TrendlineEntryTouch, mentor.TrendlineEntryFourth, mentor.TrendlineEntryThird)
	flag(t.NearBoxBetweenExempt, &out.NearBoxBetweenExempt)
	enum(t.PHLFlatLow, &out.PHLFlatLow, mentor.PHLFlatLowNear, mentor.PHLFlatLowStrict)
	enum(t.ISBReverseTouch, &out.ISBReverseTouch, mentor.ISBReverseTouchPair, mentor.ISBReverseTouchISBCandle)

	// m466 P2 (release #61): same contract — unset = the course default, a
	// value outside the enum fails closed to the default (tuning_bad_value).
	enum(t.UnoKeyLevels, &out.UnoKeyLevels, mentor.UnoKeyLevelsOn, mentor.UnoKeyLevelsOff)
	// levels audit LL-4: unset = own; a value outside the enum fails closed to
	// the default, counted (tuning_bad_value).
	enum(t.KeyLevelHistorySource, &out.KeyLevelHistorySource, mentor.KeyLevelHistoryOwn, mentor.KeyLevelHistoryStitched)
	// levels audit LL-2: unset = on (October break-and-retest); a value
	// outside the enum fails closed to the default, counted (tuning_bad_value).
	enum(t.KeyLevelBreakRetest, &out.KeyLevelBreakRetest, mentor.KeyLevelBreakRetestOn, mentor.KeyLevelBreakRetestOff)
	// D3-G15 (oct round 2): unset = off (today: a key level ISB-only after its flip
	// stays so for the day); a value outside the enum fails closed to the default,
	// counted (tuning_bad_value).
	enum(t.KeyLevelRearm, &out.KeyLevelRearm, mentor.KeyLevelRearmOn, mentor.KeyLevelRearmOff)
	// D5.3-R43/R44 (oct round 2): unset = ema (October: the lines name the trend the
	// reverse ISB trades with); "trigger" = today's governing-5m-trigger read; a bad
	// value fails closed to the default, counted (tuning_bad_value).
	enum(t.ISBReverseTrend, &out.ISBReverseTrend, mentor.ISBReverseTrendEMA, mentor.ISBReverseTrendTrigger)
	// D5.4-SCALP (oct round 2): unset = on (a counter-5m PHL/PLH sizes the reduced
	// tier); "off" = today's table. Bad value fails closed to the default.
	enum(t.Counter5mReduceSize, &out.Counter5mReduceSize, mentor.Counter5mReduceOn, mentor.Counter5mReduceOff)
	// levels audit LL-1: unset = off (October keeps the level); a value outside
	// the enum fails closed to the default, counted (tuning_bad_value).
	enum(t.KeyLevelDeleteOn1HClose, &out.KeyLevelDeleteOn1HClose, mentor.KeyLevelDeleteOn1HCloseOn, mentor.KeyLevelDeleteOn1HCloseOff)
	enum(t.SessionOpenLevels, &out.SessionOpenLevels, mentor.SessionOpenLevelsOn, mentor.SessionOpenLevelsOff)
	enum(t.LevelArmLineCancel, &out.LevelArmLineCancel, mentor.LevelArmLineCancelCurrent, mentor.LevelArmLineCancelPlacement)
	enum(t.PHLPullbackLow, &out.PHLPullbackLow, mentor.PHLPullbackLowPullback, mentor.PHLPullbackLowSignal)
	// m466 P1 (release #61): unset = the course default; a stored value outside
	// the enum fails closed to the default, counted (tuning_bad_value).
	enum(t.Mentor5mBoxSource, &out.FiveMinBoxSource, mentor.FiveMinBoxMerge, mentor.FiveMinBoxNewest, mentor.FiveMinBoxFirst)
	flag(t.MentorBreakDirectionLock, &out.BreakDirectionLock)
	flag(t.MentorSmallRangeGuard, &out.SmallRangeGuard)
	flag(t.MentorMidRangeRejectSide, &out.MidRangeRejectSide)
	// isbTF audit F4/F6: unset = today's value; out of range fails closed to
	// the default, counted (tuning_bad_value).
	flag(t.MentorSwingISBOnePerCandle, &out.SwingISBOnePerCandle)
	pickInt(t.CrossingISBLookback, mentor.CrossingISBLookbackMin, mentor.CrossingISBLookbackMax, &out.CrossingISBLookback)
	if t.CrossingISBMin != 0 {
		if t.CrossingISBMin >= mentor.CrossingISBMinMin && t.CrossingISBMin <= out.CrossingISBLookback-1 {
			out.CrossingISBMin = t.CrossingISBMin
		} else {
			bad()
		}
	}
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
	cfg.HTFOpposedRangeHaltPts = t.HTFOpposedRangeHaltPts // answers-1112 Q1 extra rule
	cfg.Swing.MaxStopPts = t.SwingMaxStopPts
	cfg.Swing.MinStopPts = t.SwingMinStopPts
	cfg.FifteenMinIBRule = t.FifteenMinIBRule
	cfg.Early15mRead = t.Early15mRead
	cfg.MTFConfluenceSource = t.MTFConfluenceSource
	cfg.TriggerEscalation = t.TriggerEscalation
	cfg.TFConflictEnd = t.TFConflictEnd
	cfg.NewsWaitBothLines = t.NewsWaitBothLines
	cfg.NewsBarsExcluded = t.NewsBarsExcluded
	cfg.ISBBoxEscapeMode = t.ISBBoxEscapeMode
	cfg.EmaLossRelock = t.EmaLossRelock
	cfg.OrbEdgeVetoPts = t.OrbEdgeVetoPts
	cfg.OrbEscapeCompletely = t.OrbEscapeCompletely
	cfg.WickMicroscalpEnabled = t.WickMicroscalp
	cfg.WickSafePlay = t.WickSafePlay
	cfg.OrbISBFractionOutside = t.OrbISBFractionOutside
	cfg.D43TrendSource = t.D43TrendSource
	cfg.LevelEntriesNeedTriggerSide = t.LevelEntriesNeedTriggerSide
	cfg.TrendlineIsTarget = t.TrendlineIsTarget
	cfg.TrendlineBetweenBoxes = t.TrendlineBetweenBoxes
	cfg.LegResetOnWaveBreak = t.LegResetOnWaveBreak
	cfg.FlagWallTarget = t.FlagWallTarget
	cfg.BoxNewZoneAfterEscape = t.BoxNewZoneAfterEscape
	cfg.ISBStackCancelCandle2m = t.ISBStackCancelCandle2m
	cfg.ISBStackTinyStopPts = t.ISBStackTinyStopPts
	cfg.EveningSlowPts = t.EveningSlowPts
	cfg.EveningSlowFromHour = t.EveningSlowFromHour
	cfg.EveningSlowWindowMin = t.EveningSlowWindowMin
	cfg.NearBoxRoomMultiple = t.NearBoxRoomMultiple
	cfg.NearBoxRoomPHL = t.NearBoxRoomPHL
	cfg.NearBoxRoomPHLMultiple = t.NearBoxRoomPHLMultiple // answers-1112 Q2
	cfg.PHLTargetShyPts = t.PHLTargetShyPts
	cfg.PHLOldExtreme1mK = t.PHLOldExtreme1mK // phl audit PHL-1
	cfg.ISBOldExtremePts = t.ISBOldExtremePts
	cfg.ISBInRangeGapPts = t.ISBInRangeGapPts
	cfg.TrendlineEntryTouch = t.TrendlineEntryTouch
	cfg.NearBoxBetweenExempt = t.NearBoxBetweenExempt
	cfg.PHLFlatLow = t.PHLFlatLow
	cfg.ISBReverseTouch = t.ISBReverseTouch
	cfg.UnoKeyLevels = t.UnoKeyLevels             // m466 P2 G2
	cfg.SessionOpenLevels = t.SessionOpenLevels   // m466 P2 G10/G11
	cfg.LevelArmLineCancel = t.LevelArmLineCancel // m466 P2 G17
	cfg.PHLPullbackLow = t.PHLPullbackLow         // m466 P2 G41
	cfg.FiveMinBoxSource = t.FiveMinBoxSource
	cfg.BreakDirectionLock = t.BreakDirectionLock
	cfg.SmallRangeGuard = t.SmallRangeGuard
	cfg.MidRangeRejectSide = t.MidRangeRejectSide
	cfg.Swing.ISBRepeatAfterFill = !t.SwingISBOnePerCandle        // isbTF-F4
	cfg.CrossingISBLookback = t.CrossingISBLookback               // isbTF-F6
	cfg.CrossingISBMin = t.CrossingISBMin                         // isbTF-F6
	cfg.KeyLevelDeleteOn1HClose = t.KeyLevelDeleteOn1HClose       // LL-1
	cfg.PHLMinAbovePriorPts = t.PHLMinAbovePriorPts               // G7
	cfg.RollingLeftBreakCancel = t.RollingLeftBreakCancel         // D2-G6
	cfg.RollingStaleTouchReleases = t.RollingStaleTouchReleases   // FIX-1112 B
	cfg.RollingKeypairTapeExempt = t.RollingKeypairTapeExempt     // FIX-1112 B
	cfg.HTFTrendlineFlip = t.HTFTrendlineFlip                     // D3-G12
	cfg.TrendlineMaxSlopePtsPerBar = t.TrendlineMaxSlopePtsPerBar // D3-G14
	cfg.TrendlineMinBouncePts = t.TrendlineMinBouncePts           // D3-G10
	cfg.KnockKnockZone = t.KnockKnockZone                         // D2-G10
	cfg.TrendlineFirstBottomAnchor = t.TrendlineFirstBottomAnchor // D3-G9
	cfg.BoxFlipReversible = t.BoxFlipReversible                   // D3-G7
	cfg.BoxCarriedStaysLive = t.BoxCarriedStaysLive               // D3-G4
	cfg.BoxSwingBothRoles = t.BoxSwingBothRoles                   // D3-G2
	cfg.BoxPreOpenLookbackMin = t.BoxPreOpenLookbackMin           // D3-G1
	cfg.TriggerFromOpen = t.TriggerFromOpen                       // G4
	cfg.ISBTargetFillClose = t.ISBTargetFillClose                 // D2-G3
	cfg.ISBNoLevelFillClose = t.ISBNoLevelFillClose               // answers-1112 Q3
	cfg.Swing.CoverLaterWick = t.TouchStopCoverLaterWick          // answers-1112 Q7
	cfg.BoxTrendlinePartialNotRoom = t.BoxTrendlinePartialNotRoom // answers-1112 Q12
	cfg.PHL5mBoxSameDirAllowed = t.PHL5mBoxSameDirAllowed         // answers-1112 Q9
	cfg.PHLRoomMultiple = t.PHLRoomMultiple                       // D2-G2
	cfg.KeyLevelBreakRetest = t.KeyLevelBreakRetest               // LL-2
	cfg.KeyLevelRearm = t.KeyLevelRearm                           // D3-G15
	cfg.ISBReverseTrend = t.ISBReverseTrend                       // D5.3-R43/R44
	cfg.Counter5mReduceSize = t.Counter5mReduceSize               // D5.4-SCALP
	cfg.KeyLevelHistorySource = t.KeyLevelHistorySource           // LL-4
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
	return fmt.Sprintf("🧑‍🏫 mentor course-entry knobs for trader %s: 15m_ib_requires_5m_isb=%s 15m_early_read=%v mtf_confluence_source=%s trigger_escalation=%v tf_conflict_end=%s news_wait_both_lines=%v news_bars_excluded=%v isb_box_escape=%s ema34_loss_relock=%s orb_edge_veto_pts=%g d43_trend_source=%s level_entries_need_trigger_side=%v trendline_is_target=%v trendline_between_boxes=%v leg_reset_on_wave_break=%v flag_wall_target=%v box_new_zone_after_escape=%v orb_escape_completely=%v wick_microscalp=%v wick_safe_play=%v mentor_5m_box_source=%s mentor_break_direction_lock=%v mentor_small_range_guard=%v mentor_mid_range_reject_side=%v htf_gate_news_only=%v isb_stack_tiny_stop_pts=%g phl_room_multiple=%g phl_min_above_old_extreme_pts=%g isb_target_fill_close=%v isb_no_level_fill_close=%v trigger_from_open=%v key_level_rearm=%v isb_reverse_trend=%v counter_5m_reduce_size=%v",
		id, t.FifteenMinIBRule, t.Early15mRead, t.MTFConfluenceSource, t.TriggerEscalation, t.TFConflictEnd, t.NewsWaitBothLines, t.NewsBarsExcluded, t.ISBBoxEscapeMode, t.EmaLossRelock,
		t.OrbEdgeVetoPts, t.D43TrendSource, t.LevelEntriesNeedTriggerSide, t.TrendlineIsTarget, t.TrendlineBetweenBoxes, t.LegResetOnWaveBreak, t.FlagWallTarget, t.BoxNewZoneAfterEscape, t.OrbEscapeCompletely, t.WickMicroscalp, t.WickSafePlay,
		t.FiveMinBoxSource, t.BreakDirectionLock, t.SmallRangeGuard, t.MidRangeRejectSide,
		t.HTFGateNewsOnly, t.ISBStackTinyStopPts, t.PHLRoomMultiple, t.PHLMinAbovePriorPts, t.ISBTargetFillClose, t.ISBNoLevelFillClose, t.TriggerFromOpen, t.KeyLevelRearm, t.ISBReverseTrend, t.Counter5mReduceSize)
}
