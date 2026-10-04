package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

func intPtr(v int) *int { return &v }

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
		DayGateSpentPts:        420,
		DayGateTargetCapPts:    12,
		SwingMaxStopPts:        80,
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
		{"day_gate_spent_pts=420", cfg.DayGateSpentPts == 420},
		{"day_gate_target_cap_pts=12", cfg.DayGateTargetCapPts == 12},
		{"swing_max_stop_pts=80 (SwingCfg.MaxStopPts)", cfg.Swing.MaxStopPts == 80},
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
}

// TestMentorTuningDefaultsAreTheRuledOnes — unset resolves to the owner's
// 2026-10-04 rulings: school 1, spent 300, cap 15 (R-B), ISB reverse ON (R-C),
// swing max stop 100 (R-D). No block, an empty block and a nil strategy agree.
func TestMentorTuningDefaultsAreTheRuledOnes(t *testing.T) {
	for name, rc := range map[string]*store.RiskControlConfig{
		"nil":   nil,
		"unset": {MentorMode: true},
		"empty": {MentorMode: true, MentorTuning: &store.MentorTuning{}},
	} {
		got := mentorTuningResolve(rc)
		want := mentorTuned{
			TriggerSchool: 1, PingPongMinGapPts: 50, PingPongCandleMaxPts: 20, PingPongCandleLookback: 30,
			LevelMaxVisits: 3, OrbGateEnabled: true, ISBReverseEMA9Enabled: true,
			DayGateSpentPts: 300, DayGateTargetCapPts: 15, SwingMaxStopPts: 100,
		}
		if got != want {
			t.Errorf("%s: resolved %+v, want %+v", name, got, want)
		}
	}
}

// TestMentorTuningBadValuesFailClosed — out-of-range values keep the default
// and are counted, never half-applied.
func TestMentorTuningBadValuesFailClosed(t *testing.T) {
	ResetMentorCountersForTest()
	got := mentorTuningResolve(&store.RiskControlConfig{MentorTuning: &store.MentorTuning{
		TriggerSchool: 3, PingPongMinGapPts: -5, PingPongCandleMaxPts: 9999, PingPongCandleLookback: 9999,
		LevelMaxVisits: intPtr(99), DayGateSpentPts: 1, DayGateTargetCapPts: 500, SwingMaxStopPts: 5,
	}})
	want := mentorTuningResolve(nil)
	if got != want {
		t.Errorf("bad values changed the result: %+v, want defaults %+v", got, want)
	}
	if n := MentorCountSnapshot()["tuning_bad_value"]; n != 8 {
		t.Errorf("tuning_bad_value = %d, want 8 (one per bad field)", n)
	}
}

// TestMentorTuningOneValueForGateAndKernel — the trader rule gate and the
// kernel read the SAME number: with swing_max_stop_pts=80 / day_gate_target_cap
// =12 the dispatch path refuses at exactly those values, and the evaluator
// config carries them. Mutant: the dispatch stops filling extra.* from the
// resolver (gate falls back to 100 / 15) → RED.
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
	resetMentorCounters()
	at.mentorDispatchIntent(swing(80), mentorTierInputs{}, 1000, 1100)
	if n := MentorCountSnapshot()["refused_r8"]; n != 1 {
		t.Errorf("a swing stop of 80 with swing_max_stop_pts=80 must be refused at the gate (refused_r8=%d)", n)
	}
	resetMentorCounters()
	at.mentorDispatchIntent(swing(79), mentorTierInputs{}, 1000, 1100)
	if n := MentorCountSnapshot()["refused_r8"]; n != 0 {
		t.Errorf("a swing stop of 79 must pass the R8 gate (refused_r8=%d)", n)
	}

	spent := mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "PHL", Side: mentor.SideLong,
		Price: 30000, Stop: 29987, Target: 30026, StopPts: 13, TargetPts: 26, SpentDay: true, ExpiryMs: 100_000}
	resetMentorCounters()
	at.mentorDispatchIntent(spent, mentorTierInputs{SpentDay: true}, 1000, 1100)
	if n := MentorCountSnapshot()["refused_r9"]; n != 1 {
		t.Errorf("a spent-day stop of 13 with the cap at 12 must be refused (refused_r9=%d)", n)
	}
}

// The bare table (no strategy) falls back to the kernel defaults, never to a
// second constant: SWING4H stop 99 passes, 100 refuses (R-D).
func TestMentorRuleGateSwingDefaultIs100(t *testing.T) {
	in := func(stop float64) mentor.Intent {
		return mentor.Intent{Setup: "SWING4H", Side: "long", Price: 30000, Stop: 30000 - stop, StopPts: stop, TargetPts: 2 * stop}
	}
	if why := mentorRuleGate(in(99), mentorTierInputs{}); why != "" {
		t.Errorf("SWING4H 99-pt stop must pass under the 100 default: %q", why)
	}
	if why := mentorRuleGate(in(100), mentorTierInputs{}); why == "" {
		t.Error("SWING4H 100-pt stop must be skipped (R-D: skip at >= 100)")
	}
}
