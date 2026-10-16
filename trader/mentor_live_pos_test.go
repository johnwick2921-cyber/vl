package trader

import (
	"testing"

	"vl/store"
)

// TestMentorExec2mKnobOnNilIsOn — D2 (fold #565): the restart rebuild's knob
// reader must agree with the kernel DefaultConfig (ONE canonicalizer per
// identifier). nil / empty / nil-tuning → ON (the release #31 default); only an
// explicit false reads OFF. MUTANT (nil → OFF) → RED.
func TestMentorExec2mKnobOnNilIsOn(t *testing.T) {
	if !mentorExec2mKnobOn(nil) {
		t.Fatal("nil risk control → ON (the release #31 default)")
	}
	if !mentorExec2mKnobOn(&store.RiskControlConfig{}) {
		t.Fatal("empty risk control → ON")
	}
	if !mentorExec2mKnobOn(&store.RiskControlConfig{MentorTuning: &store.MentorTuning{}}) {
		t.Fatal("nil tuning → ON")
	}
	off := false
	if mentorExec2mKnobOn(&store.RiskControlConfig{MentorTuning: &store.MentorTuning{Exec2mAfter30m: &off}}) {
		t.Fatal("explicit OFF → OFF")
	}
	on := true
	if !mentorExec2mKnobOn(&store.RiskControlConfig{MentorTuning: &store.MentorTuning{Exec2mAfter30m: &on}}) {
		t.Fatal("explicit ON → ON")
	}
}

// TestMentorRebuildTrailFrameWarnsOnTheNewDefault — D2 (fold #565) at the
// restart-rebuild call site: an ISB rebuilt with NO mentor_tuning (the live MNQ
// strategy) reads the knob ON, so the trail frame is UNKNOWN and the rebuild
// must WARN + count — never silently trail the 1m. An explicit OFF reads OFF and
// does not WARN. MUTANT (mentorExec2mKnobOn nil→OFF) → count 0 → RED.
func TestMentorRebuildTrailFrameWarnsOnTheNewDefault(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}}}
	row := store.ArmedOrderDB{Condition: "ISB", Scenario: "isb-x", SignalID: "sig", TraderID: "t"}
	at.mentorRebuildTrailFrame(row, &mentorLivePos{})
	if n := at.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 1 {
		t.Fatalf("trail_tf_unknown_after_restart = %d, want 1 (nil tuning reads ON and WARNs)", n)
	}

	off := false
	atOff := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{Exec2mAfter30m: &off}}}}}
	atOff.mentorRebuildTrailFrame(row, &mentorLivePos{})
	if n := atOff.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 0 {
		t.Fatalf("explicit OFF must not WARN; trail_tf_unknown_after_restart = %d", n)
	}
}
