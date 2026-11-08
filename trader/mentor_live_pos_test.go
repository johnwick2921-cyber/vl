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
	// m472 A1: nil → OFF, the 1m all day [Oct D4.2 @ 22:56-23:46] (was ON, release #31).
	if mentorExec2mKnobOn(nil) {
		t.Fatal("nil risk control → OFF (the m472 A1 default)")
	}
	if mentorExec2mKnobOn(&store.RiskControlConfig{}) {
		t.Fatal("empty risk control → OFF")
	}
	if mentorExec2mKnobOn(&store.RiskControlConfig{MentorTuning: &store.MentorTuning{}}) {
		t.Fatal("nil tuning → OFF")
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
	// m472 A1 [Oct D4.2 @ 22:56-23:46]: nil tuning reads OFF (1m all day) — the
	// trail frame is the 1m, known, so no WARN.
	if n := at.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 0 {
		t.Fatalf("trail_tf_unknown_after_restart = %d, want 0 (nil tuning reads OFF, 1m trail)", n)
	}

	// An explicit ON (the September schedule) makes the frame unknown → WARN + count.
	on := true
	atOn := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{Exec2mAfter30m: &on}}}}}
	atOn.mentorRebuildTrailFrame(row, &mentorLivePos{})
	if n := atOn.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 1 {
		t.Fatalf("explicit ON must WARN; trail_tf_unknown_after_restart = %d, want 1", n)
	}

	off := false
	atOff := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{Exec2mAfter30m: &off}}}}}
	atOff.mentorRebuildTrailFrame(row, &mentorLivePos{})
	if n := atOff.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 0 {
		t.Fatalf("explicit OFF must not WARN; trail_tf_unknown_after_restart = %d", n)
	}
}

// TestMentorRebuildTrailFrameWarnsForPHL — F2: a PHL/PLH entered on the 2m is
// the SAME restart-unknown-frame case as an ISB ("Mình trade khung 2 phút …
// potential high low" [X8 @15:52–15:57]); the rebuild must WARN + count for it
// too, never silently trail the 1m. MUTANT (isMentorExec2mSetup drops PHL/PLH)
// → count 0 → RED.
func TestMentorRebuildTrailFrameWarnsForPHL(t *testing.T) {
	on := true // m472 A1: the 2m schedule is now opt-in
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTuning: &store.MentorTuning{Exec2mAfter30m: &on}}}}}
	for _, cond := range []string{"PHL", "PLH"} {
		row := store.ArmedOrderDB{Condition: cond, Scenario: "phl-x", SignalID: "sig", TraderID: "t"}
		at.mentorRebuildTrailFrame(row, &mentorLivePos{})
	}
	if n := at.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 2 {
		t.Fatalf("trail_tf_unknown_after_restart = %d, want 2 (one per PHL/PLH)", n)
	}
	// A setup OUTSIDE the named three (the box) never WARNs — it stays the 1m.
	box := store.ArmedOrderDB{Condition: "BOX", Scenario: "box-x", SignalID: "sig", TraderID: "t"}
	at.mentorRebuildTrailFrame(box, &mentorLivePos{})
	if n := at.mentorCountSnapshot()["trail_tf_unknown_after_restart"]; n != 2 {
		t.Fatalf("a BOX rebuild must not WARN; trail_tf_unknown_after_restart = %d, want still 2", n)
	}
}
