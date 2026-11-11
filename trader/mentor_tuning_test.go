package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func tuningTick(t *testing.T, tn *store.MentorTuning) mentor.Config {
	t.Helper()
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline { return nil }
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: tn})
	at.mentorEvalOnce([]market.Kline{{OpenTime: 1_790_000_000_000, CloseTime: 1_790_000_059_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}})
	if at.mentorEval == nil {
		t.Fatal("a production tick must build the evaluator")
	}
	return at.mentorEval.Cfg
}

// TestMentorTuningReachesTheEvaluator pins all ten K3 knobs at the production
// call site (mentorEvalOnce builds the evaluator through mentorEvaluatorConfig).
// Mutant: drop any line of applyMentorTuning → the matching assertion is RED.
func TestMentorTuningReachesTheEvaluator(t *testing.T) {
	on, off := true, false
	cfg := tuningTick(t, &store.MentorTuning{
		TriggerSchool:          2,
		PingPongMinGapPts:      77,
		PingPongCandleMaxPts:   33,
		PingPongCandleLookback: 12,
		LevelMaxVisits:         intPtr(5),
		OrbGateEnabled:         &off,
		ISBReverseEMA9Enabled:  &off,
		HTFGateNewsOnly:        &off,
		Exec2mAfter30m:         &on,
		DayGateSpentPts:        420,
		DayGateTargetCapPts:    12,
		SwingMaxStopPts:        80,
		SwingMinStopPts:        25,
		OrbISBFractionOutside:  floatPtr(0.55),
	})
	checks := []struct {
		name string
		ok   bool
	}{
		{"trigger_school=2", cfg.TriggerSchool == 2},
		{"ping_pong_min_gap_pts=77", cfg.PingPongMinGapPts == 77},
		{"ping_pong_candle_max_pts=33", cfg.PingPongCandleMaxPts == 33},
		{"ping_pong_candle_lookback=12", cfg.PingPongCandleLookback == 12},
		{"level_max_visits=5", cfg.LevelMaxVisits == 5},
		{"orb_gate_enabled=false", !cfg.OrbGateEnabled},
		{"isb_reverse_ema9_enabled=false", !cfg.ISBReverseEMA9Enabled},
		{"htf_gate_news_only=false (stored: the m462 all-session gate; default is true since D2-G7)", !cfg.HTFGateNewsOnly},
		{"exec_2m_after_30m=true (X5-10 Studio switch)", cfg.Exec2mAfter30m},
		{"day_gate_spent_pts=420", cfg.DayGateSpentPts == 420},
		{"day_gate_target_cap_pts=12", cfg.DayGateTargetCapPts == 12},
		{"swing_max_stop_pts=80 (SwingCfg.MaxStopPts)", cfg.Swing.MaxStopPts == 80},
		{"swing_min_stop_pts=25 (SwingCfg.MinStopPts)", cfg.Swing.MinStopPts == 25},
		{"orb_isb_fraction_outside=0.55 (rule 5)", cfg.OrbISBFractionOutside == 0.55},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s did not reach the evaluator", c.name)
		}
	}

	// An explicit level_max_visits=0 turns the cap OFF; unset keeps 3.
	if got := tuningTick(t, &store.MentorTuning{LevelMaxVisits: intPtr(0)}).LevelMaxVisits; got != 0 {
		t.Errorf("explicit level_max_visits=0 must turn the cap off, got %d", got)
	}
	// turning the booleans ON explicitly
	if cfg := tuningTick(t, &store.MentorTuning{OrbGateEnabled: &on, ISBReverseEMA9Enabled: &on}); !cfg.OrbGateEnabled || !cfg.ISBReverseEMA9Enabled {
		t.Error("explicit true must stay true")
	}
	// fold #565 (D5): an explicit orb_isb_fraction_outside=0 turns rule 5 OFF
	// (the kernel reads <= 0 as off) — not silently back to the 0.80 default.
	if got := tuningTick(t, &store.MentorTuning{OrbISBFractionOutside: floatPtr(0)}).OrbISBFractionOutside; got != 0 {
		t.Errorf("explicit orb_isb_fraction_outside=0 must turn the rule off, got %.2f", got)
	}
	// unset stays the 0.80 default.
	if got := tuningTick(t, &store.MentorTuning{}).OrbISBFractionOutside; got != 0.80 {
		t.Errorf("unset orb_isb_fraction_outside must stay 0.80, got %.2f", got)
	}
	// m465 P1 (release #60): a STORED pre-#60 value reaches the evaluator —
	// 15m_early_read=false, mtf_confluence_source=last_pair,
	// trigger_escalation=false; unset = the #60 defaults (ON / standing_boxes / ON).
	on = true
	if c := tuningTick(t, &store.MentorTuning{Early15mRead: &off, MTFConfluenceSource: "last_pair", TriggerEscalation: &on}); c.Early15mRead || c.MTFConfluenceSource != mentor.MTFConfluenceLastPair || !c.TriggerEscalation {
		t.Errorf("stored m465 knobs did not reach the evaluator: early=%v source=%q escalation=%v", c.Early15mRead, c.MTFConfluenceSource, c.TriggerEscalation)
	}
	if c := tuningTick(t, &store.MentorTuning{}); !c.Early15mRead || c.MTFConfluenceSource != mentor.MTFConfluenceStandingBoxes || c.TriggerEscalation {
		t.Errorf("unset m465 knobs must run the #60 defaults (trigger_escalation OFF since G3): early=%v source=%q escalation=%v", c.Early15mRead, c.MTFConfluenceSource, c.TriggerEscalation)
	}
}

// TestMentorTuningDefaultsAreTheRuledOnes — unset resolves to the owner's
// 2026-10-04 rulings: school 1, spent 300, cap 15 (R-B), ISB reverse ON (R-C),
// swing max stop 100 (R-D). No block, an empty block and a nil strategy agree.
func TestMentorTuningDefaultsAreTheRuledOnes(t *testing.T) {
	at := funnelAT(t)
	for name, rc := range map[string]*store.RiskControlConfig{
		"nil":   nil,
		"unset": {MentorMode: true},
		"empty": {MentorMode: true, MentorTuning: &store.MentorTuning{}},
	} {
		got := mentorTuningResolve(at, rc)
		want := mentorTuned{
			TriggerSchool: 1, PingPongMinGapPts: 50, PingPongCandleMaxPts: 20, PingPongCandleLookback: 30,
			LevelMaxVisits: 3, OrbGateEnabled: true, ISBReverseEMA9Enabled: true,
			HTFGateNewsOnly:       true,                                               // D2-G7 oct round 2: the 4h/1h gate only in news + open by default (October: the 5m is the only big frame, OctD4.3-R9)
			Exec2mAfter30m:        false,                                              // m472 A1 [Oct D4.2 @ 22:56-23:46]: the 1m all day (was ON, X5-10 rule 4 release #31)
			OrbISBFractionOutside: 0.80,                                               // rule 5 (release #31): the ORB-edge ISB candle must be ≥ 80% outside
			DayGateSpentPts:       265, DayGateTargetCapPts: 15, SwingMaxStopPts: 100, // m465 (#60) D5.1-R18: spent line 265
			// Release #25 course-entry knobs (course-defaults-30 changes).
			FifteenMinIBRule: mentor.FifteenMinRequire5mISB, TFConflictEnd: mentor.TFConflictBothBoxes,
			// m465 P1 (release #60): early 15m read ON (D4.2-R2), mode C from the
			// standing boxes (D4.2-R17), trigger escalation ON (D3.4-R43).
			Early15mRead: true, MTFConfluenceSource: mentor.MTFConfluenceStandingBoxes, TriggerEscalation: false, // G3 oct round 2: was true (D3.4-R43)
			NewsWaitBothLines: true,                                                                        // CE-3 — course-defaults-30 [D4.4 p1 @21:55–22:12]
			NewsBarsExcluded:  true,                                                                        // CE-4 (a, release #29 rework: freeze the pre-print read, candles whole)
			ISBBoxEscapeMode:  mentor.ISBBoxEscapeCloseBoxTF, EmaLossRelock: mentor.EmaRelockUntilZoneExit, // m473 B3 OctD4.3-R67 [Oct D4.3 @ 65:00-66:13] (was close, m466 G20/G49 D4.1-R34)
			D43TrendSource: mentor.D43TrendStructure, TrendlineIsTarget: true,
			TrendlineBetweenBoxes: false, // D3-G8 oct round 2: OctD3.3-R59 [@ 46:27] (was true, CE-11/B13)
			FlagWallTarget:        true,  // CE-11 — course-defaults-30 [X9 @05:59–06:12]
			LegResetOnWaveBreak:   true,
			// Release #29 B52: a new zone after a range escape — course, no
			// ruling against → ON (COMMON-25).
			BoxNewZoneAfterEscape: true,
			// Release #52 W11 invented-constant knobs: every default is the value
			// the kernel hard-coded before (all "ours", none from the course).
			ISBStackCancelCandle2m: 4, EveningSlowPts: 100, EveningSlowFromHour: 15,
			NearBoxRoomMultiple: 2, NearBoxRoomPHL: true, PHLTargetShyPts: 5, ISBOldExtremePts: 2,
			// m465 P3 (release #60): the course defaults from the full video read.
			ISBStackTinyStopPts:  0,                          // G2 oct round 2: OFF (was 12.5, D5.1-R26)
			TrendlineEntryTouch:  mentor.TrendlineEntryThird, // m472 A2 [Oct D3.2 @ 31:00-31:17] supersedes Sept D2.3-R7
			NearBoxBetweenExempt: false,                      // D3.2-R29/R30
			PHLFlatLow:           mentor.PHLFlatLowNear,      // D2.2-R64
			ISBReverseTouch:      mentor.ISBReverseTouchPair, // D5.4-R7
			// m466 P2 (release #61): the course defaults.
			UnoKeyLevels:       mentor.UnoKeyLevelsOn,            // G2 D1.3-R21
			SessionOpenLevels:  mentor.SessionOpenLevelsOn,       // G10/G11 D3.1-R26/R20
			LevelArmLineCancel: mentor.LevelArmLineCancelCurrent, // G17 D2.3-R34
			PHLPullbackLow:     mentor.PHLPullbackLowPullback,    // G41 D2.2-R59
			// m466 P1 (release #61): boxes, breaks, ranges — the course defaults.
			FiveMinBoxSource:   mentor.FiveMinBoxMerge, // ISB5-F1 OctD4.3-R59 (October wins over D4.1-R37)
			BreakDirectionLock: true,                   // D4.1-R31/R33, D4.1-R55
			SmallRangeGuard:    false,                  // D4.2-R33 — OFF since #61 final (count / margin unstated)
			MidRangeRejectSide: true,                   // D2.2-R71
			// isbTF audit F4/F6: today's values, [B] readings (the course states none).
			SwingISBOnePerCandle: true, CrossingISBLookback: 3, CrossingISBMin: 2,
			// phl audit PHL-1: the 1m swing width that reproduces the four Oct D2.3 worked examples.
			PHLOldExtreme1mK:           4,
			KeyLevelDeleteOn1HClose:    mentor.KeyLevelDeleteOn1HCloseOff, // LL-1
			PHLRoomMultiple:            2,                                 // D2-G2 review: today's 2R
			ISBTargetFillClose:         false,                             // D2-G3 review: OFF (course does not state it)
			TriggerFromOpen:            true,                              // G4 October [OctD4.3-R5 @ 03:10]
			RollingLeftBreakCancel:     true,                              // D2-G6 October [OctD2.3-R69 @ 78:10]
			HTFTrendlineFlip:           true,                              // D3-G12
			TrendlineMaxSlopePtsPerBar: 0,                                 // D3-G14
			TrendlineMinBouncePts:      0,                                 // D3-G10
			KnockKnockZone:             true,                              // D2-G10
			TrendlineFirstBottomAnchor: true,                              // D3-G9
			BoxFlipReversible:          true,                              // D3-G7
			BoxCarriedStaysLive:        true,                              // D3-G4
			BoxSwingBothRoles:          true,                              // D3-G2
			BoxPreOpenLookbackMin:      10,                                // D3-G1
			PHLMinAbovePriorPts:        0,                                 // G7 OFF — experience, not law [OctD4.2-R39 @ 36:45]
			KeyLevelBreakRetest:        mentor.KeyLevelBreakRetestOn,      // LL-2
			KeyLevelRearm:              mentor.KeyLevelRearmOff,           // D3-G15
			ISBReverseTrend:            mentor.ISBReverseTrendEMA,         // D5.3-R43/R44
			Counter5mReduceSize:        mentor.Counter5mReduceOn,          // D5.4-SCALP
			KeyLevelHistorySource:      mentor.KeyLevelHistoryOwn,         // LL-4
		}
		if got != want {
			t.Errorf("%s: resolved %+v, want %+v", name, got, want)
		}
	}
}

// TestMentorDayOffRecheckDefaultsOff — P1-7: the LIVE trader resolver
// (mentor_mode.go:408) must read OFF when the knob is nil/unset (the whole-day
// latch), and ON only on an explicit true. Mutant: revert to nil → ON → RED.
func TestMentorDayOffRecheckDefaultsOff(t *testing.T) {
	on, off := true, false
	if mentorDayOffRecheck(nil) {
		t.Fatal("nil rc must read OFF (nil → OFF, the whole-day latch)")
	}
	if mentorDayOffRecheck(&store.RiskControlConfig{MentorMode: true}) {
		t.Fatal("an unset knob must read OFF (nil → OFF)")
	}
	if !mentorDayOffRecheck(&store.RiskControlConfig{MentorDayOffRecheck: &on}) {
		t.Fatal("an explicit true must read ON")
	}
	if mentorDayOffRecheck(&store.RiskControlConfig{MentorDayOffRecheck: &off}) {
		t.Fatal("an explicit false must read OFF")
	}
}

// TestMentorTuningBadValuesFailClosed — out-of-range values keep the default
// and are counted, never half-applied.
func TestMentorTuningBadValuesFailClosed(t *testing.T) {
	at := funnelAT(t)
	got := mentorTuningResolve(at, &store.RiskControlConfig{MentorTuning: &store.MentorTuning{
		TriggerSchool: 3, PingPongMinGapPts: -5, PingPongCandleMaxPts: 9999, PingPongCandleLookback: 9999,
		LevelMaxVisits: intPtr(99), DayGateSpentPts: 1, DayGateTargetCapPts: 500, SwingMaxStopPts: 5,
		OrbISBFractionOutside: floatPtr(1.5), // rule 5: (0,1] only; >1 is bad
	}})
	want := mentorTuningResolve(at, nil)
	if got != want {
		t.Errorf("bad values changed the result: %+v, want defaults %+v", got, want)
	}
	if n := at.mentorCountSnapshot()["tuning_bad_value"]; n != 9 {
		t.Errorf("tuning_bad_value = %d, want 9 (one per bad field)", n)
	}
}

// TestMentorISBMaxKnobReachesR8AndBand (re-gate D2) — ONE knob
// (mentor_isb_max_pts) feeds BOTH the kernel ceiling and the trader R8/band at
// the production dispatch call site. Mutant M09 (mentor_actions.go:263 wiring
// dropped) and M10 (band top ignores the knob) → RED.
func TestMentorISBMaxKnobReachesR8AndBand(t *testing.T) {
	isb := func(stop float64) mentor.Intent {
		return mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "ISB", Side: mentor.SideLong,
			Price: 30000, Stop: 30000 - stop, Target: 30000 + 2*stop, StopPts: stop, TargetPts: 2 * stop, ExpiryMs: 100_000}
	}
	// knob 35: a 32-pt ISB passes R8 (35) and sizes the reduced band (20–<35).
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorISBMaxPts: 35})
	if cfg := at.mentorEvaluatorConfig(); cfg.CourseExits.ISBMaxStopPts != 35 {
		t.Fatalf("kernel ISB max = %g, want 35 (one knob, not two)", cfg.CourseExits.ISBMaxStopPts)
	}
	resetMentorCounters(at)
	at.mentorDispatchIntent(isb(32), mentorTierInputs{}, 1000, 1100)
	snap := at.mentorCountSnapshot()
	if snap["refused_r8"] != 0 {
		t.Fatalf("knob 35: a 32-pt ISB must pass R8, refused_r8=%d", snap["refused_r8"])
	}
	if snap["tier_reduced"] != 1 {
		t.Fatalf("knob 35: a 32-pt ISB must size the reduced 3, tier_reduced=%d", snap["tier_reduced"])
	}

	// knob 25: a 28.25-pt ISB is refused R8 (25).
	at2 := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorISBMaxPts: 25})
	if cfg := at2.mentorEvaluatorConfig(); cfg.CourseExits.ISBMaxStopPts != 25 {
		t.Fatalf("kernel ISB max = %g, want 25", cfg.CourseExits.ISBMaxStopPts)
	}
	resetMentorCounters(at2)
	at2.mentorDispatchIntent(isb(28.25), mentorTierInputs{}, 1000, 1100)
	if n := at2.mentorCountSnapshot()["refused_r8"]; n != 1 {
		t.Fatalf("knob 25: a 28.25-pt ISB must be refused R8, refused_r8=%d", n)
	}
}

// TestRetaggedPHLInRangeSizesThree (re-gate D3) — the P2-2 trader half: the ISB
// location size flags are read for ANY setup, so a reverse ISB retagged PHL that
// carries isb_in_range still sizes the reduced 3, never base 5. Mutant M13
// (flags read only for Setup "ISB") → RED.
func TestRetaggedPHLInRangeSizesThree(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	in := mentor.Intent{Action: mentor.PlaceStopEntry, Setup: "PHL", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, StopPts: 12, TargetPts: 24, Flag: mentor.FlagISBInRange}
	got, err := at.mentorSizeFor(in, mentorTierInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Contracts != 3 || got.Tier != "isb_in_range" {
		t.Fatalf("a retagged PHL in a range: got %d %s, want 3 isb_in_range", got.Contracts, got.Tier)
	}
}

// TestMentorTuningOneValueForGateAndKernel — the trader rule gate and the
// kernel read the SAME number: the resolver fills extra.* and the evaluator
// config carries the same values. The swing ~100-pt skip is GONE (m186 D5.2
// @21:25-21:43: take 1-2 MNQ and sit), so a swing stop of 80 passes even with
// swing_max_stop_pts=80; the spent-day R9 cap still shares its number with
// the kernel. Mutant: the dispatch stops filling extra.* from the resolver →
// RED (the R9 cap falls back to 15 and the spent-day stop passes).
func TestMentorTuningOneValueForGateAndKernel(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{
		SwingMaxStopPts: 80, DayGateTargetCapPts: 12,
	}})
	cfg := at.mentorEvaluatorConfig()
	if cfg.Swing.MaxStopPts != 80 || cfg.DayGateTargetCapPts != 12 {
		t.Fatalf("kernel cfg swing=%.0f cap=%.0f, want 80/12", cfg.Swing.MaxStopPts, cfg.DayGateTargetCapPts)
	}

	swing := func(stop float64) mentor.Intent {
		return mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "SWING4H", Side: mentor.SideLong,
			Price: 30000, Stop: 30000 - stop, Target: 30000 + 2*stop, StopPts: stop, TargetPts: 2 * stop, ExpiryMs: 100_000}
	}
	resetMentorCounters(at)
	at.mentorDispatchIntent(swing(80), mentorTierInputs{}, 1000, 1100)
	if n := at.mentorCountSnapshot()["refused_r8"]; n != 0 {
		t.Errorf("a swing stop of 80 must PASS now (no skip, m186 D5.2 @21:25-21:43) (refused_r8=%d)", n)
	}
	resetMentorCounters(at)
	at.mentorDispatchIntent(swing(79), mentorTierInputs{}, 1000, 1100)
	if n := at.mentorCountSnapshot()["refused_r8"]; n != 0 {
		t.Errorf("a swing stop of 79 must pass the R8 gate (refused_r8=%d)", n)
	}

	spent := mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "PHL", Side: mentor.SideLong,
		Price: 30000, Stop: 29987, Target: 30026, StopPts: 13, TargetPts: 26, SpentDay: true, ExpiryMs: 100_000}
	resetMentorCounters(at)
	at.mentorDispatchIntent(spent, mentorTierInputs{SpentDay: true}, 1000, 1100)
	if n := at.mentorCountSnapshot()["refused_r9"]; n != 1 {
		t.Errorf("a spent-day stop of 13 with the cap at 12 must be refused (refused_r9=%d)", n)
	}
}

// The bare table (no strategy) falls back to the kernel defaults. m463
// D5.2-R31 (REVERSES m186, owner order 2026-11-01 "full of it"): the swing
// skip default is 90 — a 90-pt stop passes, 90.25 / 99 / 100 are refused
// ("100 điểm quá nhiều rủi ro… đừng vô" [D5.2 p2 @19:34–21:43]). The 100 of
// the old name is the B5 SIZING ceiling (swing_max_stop_pts), not a skip.
func TestMentorRuleGateSwingDefaultIs100(t *testing.T) {
	in := func(stop float64) mentor.Intent {
		return mentor.Intent{Setup: "SWING4H", Side: "long", Price: 30000, Stop: 30000 - stop, StopPts: stop, TargetPts: 2 * stop}
	}
	if why := mentorRuleGate(in(90), mentorTierInputs{}); why != "" {
		t.Errorf("SWING4H 90-pt stop must pass under the 90 skip default: %q", why)
	}
	for _, stop := range []float64{90.25, 99, 100} {
		if why := mentorRuleGate(in(stop), mentorTierInputs{}); !strings.HasPrefix(why, "R31") {
			t.Errorf("SWING4H %.2f-pt stop must be REFUSED R31 (m463): %q", stop, why)
		}
	}
}

// FU-1 (CTO gate): the LIVE evaluator is real-fill-only — the G1 leg budget and
// the G2 loss box are fed by the broker's real fill, never by a candle touching
// a never-placed (or refused) arm. Pinned at the production tick that builds
// the evaluator (mentorEvalOnce → mentorEvaluatorConfig). Mutant: set
// RealFillOnly false in mentorEvaluatorConfig → RED (phantom fills return).
func TestMentorLiveEvaluatorIsRealFillOnly(t *testing.T) {
	if cfg := tuningTick(t, nil); !cfg.RealFillOnly {
		t.Fatal("the live mentor evaluator must be RealFillOnly (FU-1): a candle touch must never fill a live arm")
	}
}

// TestISBTFKnobsReachTheEvaluator — isbTF-F4/F6: the three new mentor_tuning
// knobs reach the evaluator config through the ONE resolver (production
// mentorTuningResolve → applyMentorTuning), unset = today's values, out of
// range fails closed to the default and is counted.
func TestISBTFKnobsReachTheEvaluator(t *testing.T) {
	off := false
	if c := tuningTick(t, &store.MentorTuning{}); c.Swing.ISBRepeatAfterFill || c.CrossingISBLookback != 3 || c.CrossingISBMin != 2 {
		t.Fatalf("unset isbTF knobs must keep today's values (one per candle, 3 / 2): repeat=%v lookback=%d min=%d", c.Swing.ISBRepeatAfterFill, c.CrossingISBLookback, c.CrossingISBMin)
	}
	if c := tuningTick(t, &store.MentorTuning{MentorSwingISBOnePerCandle: &off, CrossingISBLookback: 5, CrossingISBMin: 3}); !c.Swing.ISBRepeatAfterFill || c.CrossingISBLookback != 5 || c.CrossingISBMin != 3 {
		t.Fatalf("stored isbTF knobs did not reach the evaluator: repeat=%v lookback=%d min=%d", c.Swing.ISBRepeatAfterFill, c.CrossingISBLookback, c.CrossingISBMin)
	}
	if c := tuningTick(t, &store.MentorTuning{CrossingISBLookback: 2, CrossingISBMin: 3}); c.CrossingISBLookback != 3 || c.CrossingISBMin != 2 {
		t.Fatalf("out-of-range crossing knobs must fail closed to 3 / 2: lookback=%d min=%d", c.CrossingISBLookback, c.CrossingISBMin)
	}
}

// TestPHL1OldExtremeKnobReachesTheEvaluator — phl audit PHL-1: the rolling
// PHL/PLH 1m old-extreme width reaches the evaluator through the ONE resolver
// (production mentorEvalOnce → mentorTuningResolve → applyMentorTuning):
// unset = 4, -1 = the pre-PHL-1 5m/15m source (Config 0), 1..10 = k, anything
// else fails closed to 4.
func TestPHL1OldExtremeKnobReachesTheEvaluator(t *testing.T) {
	for _, c := range []struct{ stored, want int }{{0, 4}, {-1, 0}, {2, 2}, {10, 10}, {11, 4}, {-2, 4}} {
		if got := tuningTick(t, &store.MentorTuning{PHLOldExtreme1mK: c.stored}).PHLOldExtreme1mK; got != c.want {
			t.Fatalf("stored phl_old_extreme_1m_k=%d → Config.PHLOldExtreme1mK %d, want %d", c.stored, got, c.want)
		}
	}
}

// TestLL1KeyLevelDeleteKnobReachesTheEvaluator — levels audit LL-1: the
// knob reaches the evaluator config through the ONE resolver (production
// mentorTuningResolve → applyMentorTuning): unset = off (October keeps the
// level), "on" = the deck deletion, a bad value fails closed to off.
func TestLL1KeyLevelDeleteKnobReachesTheEvaluator(t *testing.T) {
	for _, c := range []struct{ stored, want string }{
		{"", mentor.KeyLevelDeleteOn1HCloseOff},
		{"on", mentor.KeyLevelDeleteOn1HCloseOn},
		{"off", mentor.KeyLevelDeleteOn1HCloseOff},
		{"sometimes", mentor.KeyLevelDeleteOn1HCloseOff},
	} {
		if got := tuningTick(t, &store.MentorTuning{KeyLevelDeleteOn1HClose: c.stored}).KeyLevelDeleteOn1HClose; got != c.want {
			t.Errorf("stored key_level_delete_on_1h_close=%q → Config %q, want %q", c.stored, got, c.want)
		}
	}
}

// TestLL2KeyLevelBreakRetestKnobReachesTheEvaluator — levels audit LL-2: unset
// = on (October break-and-retest), "off" = the September ISB-only, a bad value
// fails closed to on — through the production resolver.
func TestLL2KeyLevelBreakRetestKnobReachesTheEvaluator(t *testing.T) {
	for _, c := range []struct{ stored, want string }{
		{"", mentor.KeyLevelBreakRetestOn},
		{"off", mentor.KeyLevelBreakRetestOff},
		{"on", mentor.KeyLevelBreakRetestOn},
		{"twice", mentor.KeyLevelBreakRetestOn},
	} {
		if got := tuningTick(t, &store.MentorTuning{KeyLevelBreakRetest: c.stored}).KeyLevelBreakRetest; got != c.want {
			t.Errorf("stored key_level_break_retest=%q → Config %q, want %q", c.stored, got, c.want)
		}
	}
}

// TestD3G15KeyLevelRearmKnobReachesTheEvaluator — oct round 2 D3-G15: unset =
// off (today), "on" re-arms an ISB-only key level on departure, a bad value
// fails closed to off — through the production resolver.
func TestD3G15KeyLevelRearmKnobReachesTheEvaluator(t *testing.T) {
	for _, c := range []struct{ stored, want string }{
		{"", mentor.KeyLevelRearmOff},
		{"off", mentor.KeyLevelRearmOff},
		{"on", mentor.KeyLevelRearmOn},
		{"always", mentor.KeyLevelRearmOff},
	} {
		if got := tuningTick(t, &store.MentorTuning{KeyLevelRearm: c.stored}).KeyLevelRearm; got != c.want {
			t.Errorf("stored key_level_rearm=%q → Config %q, want %q", c.stored, got, c.want)
		}
	}
}

// TestRound2D53D54KnobsReachTheEvaluator — oct round 2: isb_reverse_trend
// (unset = ema, "trigger" = today) and counter_5m_reduce_size (unset = on, "off"
// = today); a bad value fails closed to the default — through the resolver.
func TestRound2D53D54KnobsReachTheEvaluator(t *testing.T) {
	for _, c := range []struct{ stored, want string }{
		{"", mentor.ISBReverseTrendEMA},
		{"ema", mentor.ISBReverseTrendEMA},
		{"trigger", mentor.ISBReverseTrendTrigger},
		{"fish", mentor.ISBReverseTrendEMA},
	} {
		if got := tuningTick(t, &store.MentorTuning{ISBReverseTrend: c.stored}).ISBReverseTrend; got != c.want {
			t.Errorf("stored isb_reverse_trend=%q → Config %q, want %q", c.stored, got, c.want)
		}
	}
	for _, c := range []struct{ stored, want string }{
		{"", mentor.Counter5mReduceOn},
		{"on", mentor.Counter5mReduceOn},
		{"off", mentor.Counter5mReduceOff},
		{"half", mentor.Counter5mReduceOn},
	} {
		if got := tuningTick(t, &store.MentorTuning{Counter5mReduceSize: c.stored}).Counter5mReduceSize; got != c.want {
			t.Errorf("stored counter_5m_reduce_size=%q → Config %q, want %q", c.stored, got, c.want)
		}
	}
}

// TestLL4KeyLevelHistorySourceKnobReachesTheEvaluator — levels audit LL-4:
// unset = own, "stitched" = the pre-LL-4 back-adjusted history, a bad value
// fails closed to own — through the production resolver.
func TestLL4KeyLevelHistorySourceKnobReachesTheEvaluator(t *testing.T) {
	for _, c := range []struct{ stored, want string }{
		{"", mentor.KeyLevelHistoryOwn},
		{"stitched", mentor.KeyLevelHistoryStitched},
		{"own", mentor.KeyLevelHistoryOwn},
		{"native", mentor.KeyLevelHistoryOwn},
	} {
		if got := tuningTick(t, &store.MentorTuning{KeyLevelHistorySource: c.stored}).KeyLevelHistorySource; got != c.want {
			t.Errorf("stored key_level_history_source=%q → Config %q, want %q", c.stored, got, c.want)
		}
	}
}

// TestD2G2PHLRoomMultipleKnob — D2-G2 (review): unset = 2 (today), a stored 1
// reaches the evaluator (the 1:1 floor reading), an out-of-range value fails
// closed to the default 2.
func TestD2G2PHLRoomMultipleKnob(t *testing.T) {
	one, bad := 1.0, 50.0
	if c := tuningTick(t, &store.MentorTuning{}); c.PHLRoomMultiple != 2 {
		t.Fatalf("unset phl_room_multiple = %v, want 2", c.PHLRoomMultiple)
	}
	if c := tuningTick(t, &store.MentorTuning{PHLRoomMultiple: &one}); c.PHLRoomMultiple != 1 {
		t.Fatalf("stored phl_room_multiple=1 → %v", c.PHLRoomMultiple)
	}
	if c := tuningTick(t, &store.MentorTuning{PHLRoomMultiple: &bad}); c.PHLRoomMultiple != 2 {
		t.Fatalf("out-of-range phl_room_multiple must fail closed to 2, got %v", c.PHLRoomMultiple)
	}
}

// TestD2G6RollingLeftBreakCancelKnob — D2-G6: unset = ON (October), a stored
// false reaches the evaluator (the pre-round-2 per-rung refusal).
func TestD2G6RollingLeftBreakCancelKnob(t *testing.T) {
	off := false
	if c := tuningTick(t, &store.MentorTuning{}); !c.RollingLeftBreakCancel {
		t.Fatal("unset rolling_left_break_cancel must be ON")
	}
	if c := tuningTick(t, &store.MentorTuning{RollingLeftBreakCancel: &off}); c.RollingLeftBreakCancel {
		t.Fatal("stored rolling_left_break_cancel=false did not reach the evaluator")
	}
}

// TestG7PHLMinAboveOldExtremeKnob — G7: unset = 15, a stored 0 turns it off,
// a stored 22 reaches the evaluator, out of range fails closed to 15.
func TestG7PHLMinAboveOldExtremeKnob(t *testing.T) {
	v, bad := 22.0, 500.0
	if c := tuningTick(t, &store.MentorTuning{}); c.PHLMinAbovePriorPts != 0 {
		t.Fatalf("unset phl_min_above_old_extreme_pts = %v, want 0 (off)", c.PHLMinAbovePriorPts)
	}
	if c := tuningTick(t, &store.MentorTuning{PHLMinAbovePriorPts: &v}); c.PHLMinAbovePriorPts != 22 {
		t.Fatalf("stored 22 → %v", c.PHLMinAbovePriorPts)
	}
	if c := tuningTick(t, &store.MentorTuning{PHLMinAbovePriorPts: &bad}); c.PHLMinAbovePriorPts != 0 {
		t.Fatalf("out of range must fail closed to 0, got %v", c.PHLMinAbovePriorPts)
	}
}

// TestD3G1Knob - D3-G1: unset = 30, a stored 45 reaches the evaluator, out of range fails closed to the default.
func TestD3G1Knob(t *testing.T) {
	v, bad := float64(45), float64(900)
	if c := tuningTick(t, &store.MentorTuning{}); c.BoxPreOpenLookbackMin != 10 {
		t.Fatalf("unset box_preopen_lookback_min = %v, want 10", c.BoxPreOpenLookbackMin)
	}
	if c := tuningTick(t, &store.MentorTuning{BoxPreOpenLookbackMin: &v}); c.BoxPreOpenLookbackMin != 45 {
		t.Fatalf("stored box_preopen_lookback_min did not reach the evaluator: %v", c.BoxPreOpenLookbackMin)
	}
	if c := tuningTick(t, &store.MentorTuning{BoxPreOpenLookbackMin: &bad}); c.BoxPreOpenLookbackMin != 10 {
		t.Fatalf("out of range must fail closed to the default, got %v", c.BoxPreOpenLookbackMin)
	}
}

// TestD3G2Knob - D3-G2: unset = true, a stored false reaches the evaluator.
func TestD3G2Knob(t *testing.T) {
	v := false
	if c := tuningTick(t, &store.MentorTuning{}); c.BoxSwingBothRoles != true {
		t.Fatalf("unset box_swing_both_roles = %v, want true", c.BoxSwingBothRoles)
	}
	if c := tuningTick(t, &store.MentorTuning{BoxSwingBothRoles: &v}); c.BoxSwingBothRoles != false {
		t.Fatalf("stored box_swing_both_roles did not reach the evaluator: %v", c.BoxSwingBothRoles)
	}
}

// TestD3G4Knob - D3-G4: unset = true, a stored false reaches the evaluator.
func TestD3G4Knob(t *testing.T) {
	v := false
	if c := tuningTick(t, &store.MentorTuning{}); c.BoxCarriedStaysLive != true {
		t.Fatalf("unset box_carried_stays_live = %v, want true", c.BoxCarriedStaysLive)
	}
	if c := tuningTick(t, &store.MentorTuning{BoxCarriedStaysLive: &v}); c.BoxCarriedStaysLive != false {
		t.Fatalf("stored box_carried_stays_live did not reach the evaluator: %v", c.BoxCarriedStaysLive)
	}
}

// TestD3G7Knob - D3-G7: unset = true, a stored false reaches the evaluator.
func TestD3G7Knob(t *testing.T) {
	v := false
	if c := tuningTick(t, &store.MentorTuning{}); c.BoxFlipReversible != true {
		t.Fatalf("unset box_flip_reversible = %v, want true", c.BoxFlipReversible)
	}
	if c := tuningTick(t, &store.MentorTuning{BoxFlipReversible: &v}); c.BoxFlipReversible != false {
		t.Fatalf("stored box_flip_reversible did not reach the evaluator: %v", c.BoxFlipReversible)
	}
}

// TestD3G9Knob - D3-G9: unset = true, a stored false reaches the evaluator.
func TestD3G9Knob(t *testing.T) {
	v := false
	if c := tuningTick(t, &store.MentorTuning{}); c.TrendlineFirstBottomAnchor != true {
		t.Fatalf("unset trendline_first_bottom_anchor = %v, want true", c.TrendlineFirstBottomAnchor)
	}
	if c := tuningTick(t, &store.MentorTuning{TrendlineFirstBottomAnchor: &v}); c.TrendlineFirstBottomAnchor != false {
		t.Fatalf("stored trendline_first_bottom_anchor did not reach the evaluator: %v", c.TrendlineFirstBottomAnchor)
	}
}

// TestD2G10Knob - D2-G10: unset = true, a stored false reaches the evaluator.
func TestD2G10Knob(t *testing.T) {
	v := false
	if c := tuningTick(t, &store.MentorTuning{}); c.KnockKnockZone != true {
		t.Fatalf("unset knock_knock_zone = %v, want true", c.KnockKnockZone)
	}
	if c := tuningTick(t, &store.MentorTuning{KnockKnockZone: &v}); c.KnockKnockZone != false {
		t.Fatalf("stored knock_knock_zone did not reach the evaluator: %v", c.KnockKnockZone)
	}
}

// TestD3G10Knob - D3-G10: unset = 0, a stored 20 reaches the evaluator, out of range fails closed to the default.
func TestD3G10Knob(t *testing.T) {
	v, bad := float64(20), float64(5000)
	if c := tuningTick(t, &store.MentorTuning{}); c.TrendlineMinBouncePts != 0 {
		t.Fatalf("unset trendline_min_bounce_pts = %v, want 0", c.TrendlineMinBouncePts)
	}
	if c := tuningTick(t, &store.MentorTuning{TrendlineMinBouncePts: &v}); c.TrendlineMinBouncePts != 20 {
		t.Fatalf("stored trendline_min_bounce_pts did not reach the evaluator: %v", c.TrendlineMinBouncePts)
	}
	if c := tuningTick(t, &store.MentorTuning{TrendlineMinBouncePts: &bad}); c.TrendlineMinBouncePts != 0 {
		t.Fatalf("out of range must fail closed to the default, got %v", c.TrendlineMinBouncePts)
	}
}

// TestD3G14Knob - D3-G14: unset = 0, a stored 1.5 reaches the evaluator, out of range fails closed to the default.
func TestD3G14Knob(t *testing.T) {
	v, bad := float64(1.5), float64(5000)
	if c := tuningTick(t, &store.MentorTuning{}); c.TrendlineMaxSlopePtsPerBar != 0 {
		t.Fatalf("unset trendline_max_slope_pts_per_bar = %v, want 0", c.TrendlineMaxSlopePtsPerBar)
	}
	if c := tuningTick(t, &store.MentorTuning{TrendlineMaxSlopePtsPerBar: &v}); c.TrendlineMaxSlopePtsPerBar != 1.5 {
		t.Fatalf("stored trendline_max_slope_pts_per_bar did not reach the evaluator: %v", c.TrendlineMaxSlopePtsPerBar)
	}
	if c := tuningTick(t, &store.MentorTuning{TrendlineMaxSlopePtsPerBar: &bad}); c.TrendlineMaxSlopePtsPerBar != 0 {
		t.Fatalf("out of range must fail closed to the default, got %v", c.TrendlineMaxSlopePtsPerBar)
	}
}

// TestD3G12Knob - D3-G12: unset = true, a stored false reaches the evaluator.
func TestD3G12Knob(t *testing.T) {
	v := false
	if c := tuningTick(t, &store.MentorTuning{}); c.HTFTrendlineFlip != true {
		t.Fatalf("unset htf_trendline_flip = %v, want true", c.HTFTrendlineFlip)
	}
	if c := tuningTick(t, &store.MentorTuning{HTFTrendlineFlip: &v}); c.HTFTrendlineFlip != false {
		t.Fatalf("stored htf_trendline_flip did not reach the evaluator: %v", c.HTFTrendlineFlip)
	}
}
