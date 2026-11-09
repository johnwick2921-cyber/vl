package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// TestM472Exec1mLiveStrategyStoresNoKnob — m472 A1 (OctD4.2-R28): "Trade khung
// 1 phut, chu khong la khung 2 phut. Khung 2 phut no quet banh xac" [Oct D4.2 @
// 22:56-23:46]; frame /home/hoang/mm-course/oct/scenes/Ngày 4.2 Setup gồng lời
// và cách move stoploss/s_01389.jpg (1m chart). Every live strategy stores no
// mentor_tuning key, so the resolver and the restart-rebuild reader must both
// say 1m (knob OFF) for nil / unset / empty, and an explicit ON still wins.
func TestM472Exec1mLiveStrategyStoresNoKnob(t *testing.T) {
	at := funnelAT(t)
	for name, rc := range map[string]*store.RiskControlConfig{
		"nil":   nil,
		"unset": {MentorMode: true},
		"empty": {MentorMode: true, MentorTuning: &store.MentorTuning{}},
	} {
		cfg := mentor.DefaultConfig()
		applyMentorTuning(at, &cfg, rc)
		if cfg.Exec2mAfter30m {
			t.Errorf("%s: evaluator config must trade the 1m all day, got Exec2mAfter30m=true", name)
		}
		if mentorExec2mKnobOn(rc) {
			t.Errorf("%s: mentorExec2mKnobOn must be false (agree with DefaultConfig)", name)
		}
	}
	on := true
	rc := &store.RiskControlConfig{MentorTuning: &store.MentorTuning{Exec2mAfter30m: &on}}
	cfg := mentor.DefaultConfig()
	applyMentorTuning(at, &cfg, rc)
	if !cfg.Exec2mAfter30m || !mentorExec2mKnobOn(rc) {
		t.Errorf("explicit ON must still enable the 2m schedule")
	}
}
