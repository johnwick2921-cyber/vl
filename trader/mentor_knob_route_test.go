package trader

import (
	"testing"

	"vl/market"
	"vl/store"
)

// TestMentorStoreKnobsReachTheEvaluator pins K1 at the production call site:
// the three store knobs mentor_leg_budget_enabled / mentor_leg_reset_on /
// mentor_loc_trigger_filter used to have resolvers nobody called, so saving
// them did nothing. The evaluator that a production tick builds
// (mentorEvalOnce → mentorEvaluatorConfig) must carry the stored value.
// Mutant: drop any one cfg.X = mentorX(rc) copy in mentorEvaluatorConfig → RED.
func TestMentorStoreKnobsReachTheEvaluator(t *testing.T) {
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(_, _ string, _ int) []market.Kline { return nil }
	t.Cleanup(func() { market.FuturesBarsProvider = old })

	bars := []market.Kline{{OpenTime: 1_790_000_000_000, CloseTime: 1_790_000_059_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}}

	at := mentoredTrader(t, store.RiskControlConfig{
		MentorMode:                  true,
		MentorLegBudgetEnabled:      boolPtr(false),
		MentorLegResetOn:            "touch",
		MentorLocationTriggerFilter: boolPtr(false),
	})
	at.mentorEvalOnce(bars)
	if at.mentorEval == nil {
		t.Fatal("a production tick must build the evaluator")
	}
	got := at.mentorEval.Cfg
	if got.LegBudgetEnabled {
		t.Error("mentor_leg_budget_enabled=false did not reach the evaluator (LegBudgetEnabled still true)")
	}
	if got.LegResetOn != "touch" {
		t.Errorf("mentor_leg_reset_on=touch did not reach the evaluator: LegResetOn=%q", got.LegResetOn)
	}
	if got.LocTriggerFilter {
		t.Error("mentor_loc_trigger_filter=false did not reach the evaluator (LocTriggerFilter still true)")
	}

	// Unset (and no strategy at all) keeps the ruled defaults: ON / close / ON.
	for name, a := range map[string]*AutoTrader{
		"unset": mentoredTrader(t, store.RiskControlConfig{MentorMode: true}),
		"naked": {id: "t-naked"},
	} {
		cfg := a.mentorEvaluatorConfig()
		if !cfg.LegBudgetEnabled || cfg.LegResetOn != "close" || !cfg.LocTriggerFilter {
			t.Errorf("%s: defaults changed: budget=%v reset=%q trigger_filter=%v — want true/close/true",
				name, cfg.LegBudgetEnabled, cfg.LegResetOn, cfg.LocTriggerFilter)
		}
	}

	// A bad leg-reset value fails closed to "close" through the same path.
	bad := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorLegResetOn: "garbage"})
	if cfg := bad.mentorEvaluatorConfig(); cfg.LegResetOn != "close" {
		t.Errorf("bad mentor_leg_reset_on must fail closed to close, got %q", cfg.LegResetOn)
	}
}
