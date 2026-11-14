package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

// OCT-EXACT build step A2 (CTO-decisions.md section A2; owner order 2026-11-14
// "mentor mode = the October course exactly"): the production call site
// (mentorEvaluatorConfig) hands the evaluator every A2 gate OFF by default, and
// a STORED value (the revert) reaches it. Mentor mode only: the evaluator
// Config is built for mentor mode, AI mode never reads it.
func TestOctA2_EvaluatorConfigDefaultsAndReverts(t *testing.T) {
	c := tuningTick(t, nil)
	off := map[string]bool{
		"HTFBoxGates": c.HTFBoxGates, "Early15mRead": c.Early15mRead, "TFConflictEnabled": c.TFConflictEnabled,
		"MTFConfluenceEnabled": c.MTFConfluenceEnabled, "RollingLevelTouchOwns": c.RollingLevelTouchOwns,
		"KeypairRuleEnabled": c.KeypairRuleEnabled, "NearBoxRoomPHL": c.NearBoxRoomPHL, "FlagWallTarget": c.FlagWallTarget,
		"TriggerRetestWrongWayRefuse": c.TriggerRetestWrongWayRefuse, "BoxPingPongGate": c.BoxPingPongGate,
		"PHLISBPreferredAtBox": c.PHLISBPreferredAtBox, "TriggerBoxZoneGate": c.TriggerBoxZoneGate,
		"ISBLocationSizeCuts": c.ISBLocationSizeCuts, "PullbackWarnCut": c.PullbackWarnCut,
		"TrendlineBoxBeats": c.TrendlineBoxBeats, "TrendlineIsTarget": c.TrendlineIsTarget, "ConfluenceHold2R": c.ConfluenceHold2R,
	}
	for name, v := range off {
		if v {
			t.Errorf("default %s = true, want false (October)", name)
		}
	}
	if c.FifteenMinIBRule != mentor.FifteenMinOff || c.EmaLossRelock != mentor.EmaRelockOff || c.PHLPullbackLow != mentor.PHLPullbackLowSignal {
		t.Errorf("enum defaults: 15m=%q ema=%q pullback=%q", c.FifteenMinIBRule, c.EmaLossRelock, c.PHLPullbackLow)
	}
	if c.NearBoxRoomMultiple != 0 || c.BoxRoomMultiple != 1 || c.Box.TouchBandPts != 0 || c.KeyLevelPrunePts != 0 ||
		c.TrendlineLevelPreferPts != 0 || c.TrendlineMaxLinesPerAnchor != 3 {
		t.Errorf("number defaults: nearbox=%v boxroom=%v band=%v prune=%v prefer=%v lines=%d", c.NearBoxRoomMultiple, c.BoxRoomMultiple,
			c.Box.TouchBandPts, c.KeyLevelPrunePts, c.TrendlineLevelPreferPts, c.TrendlineMaxLinesPerAnchor)
	}

	tr := true
	two, band, prune, prefer := 2.0, 0.25, 20.0, 2.0
	c = tuningTick(t, &store.MentorTuning{
		MentorHTFBoxGates: &tr, MentorTFConflict: &tr, MentorMTFConfluence: &tr, MentorRollingLevelTouchOwns: &tr,
		KeypairRuleEnabled: &tr, MentorTriggerRetestWrongWay: &tr, MentorBoxPingPong: &tr, MentorPHLISBPreferredAtBox: &tr,
		MentorTriggerBoxZone: &tr, MentorISBLocationSizeCuts: &tr, MentorPullbackWarnCut: &tr, MentorTrendlineBoxBeats: &tr,
		MentorConfluenceHold2R: &tr, MentorBoxRoomMultiple: &two, MentorBoxTouchBandPts: &band, MentorKeyLevelPrunePts: &prune,
		TrendlineLevelPreferPts: &prefer, TrendlineMaxLinesPerAnchor: 1,
		EMA34LossRelock: mentor.EmaRelockUntilZoneExit, PHLPullbackLow: mentor.PHLPullbackLowPullback,
		FifteenMinIBRequires5mISB: mentor.FifteenMinRequire5mISB, Early15mRead: &tr, FlagWallTarget: &tr, TrendlineIsTarget: &tr,
	})
	on := map[string]bool{
		"HTFBoxGates": c.HTFBoxGates, "TFConflictEnabled": c.TFConflictEnabled, "MTFConfluenceEnabled": c.MTFConfluenceEnabled,
		"RollingLevelTouchOwns": c.RollingLevelTouchOwns, "KeypairRuleEnabled": c.KeypairRuleEnabled,
		"TriggerRetestWrongWayRefuse": c.TriggerRetestWrongWayRefuse, "BoxPingPongGate": c.BoxPingPongGate,
		"PHLISBPreferredAtBox": c.PHLISBPreferredAtBox, "TriggerBoxZoneGate": c.TriggerBoxZoneGate,
		"ISBLocationSizeCuts": c.ISBLocationSizeCuts, "PullbackWarnCut": c.PullbackWarnCut, "TrendlineBoxBeats": c.TrendlineBoxBeats,
		"ConfluenceHold2R": c.ConfluenceHold2R, "Early15mRead": c.Early15mRead, "FlagWallTarget": c.FlagWallTarget, "TrendlineIsTarget": c.TrendlineIsTarget,
	}
	for name, v := range on {
		if !v {
			t.Errorf("stored true %s did not reach the evaluator (the revert is broken)", name)
		}
	}
	if c.BoxRoomMultiple != 2 || c.Box.TouchBandPts != 0.25 || c.KeyLevelPrunePts != 20 || c.TrendlineLevelPreferPts != 2 || c.TrendlineMaxLinesPerAnchor != 1 {
		t.Errorf("stored numbers did not reach the evaluator: %v %v %v %v %d", c.BoxRoomMultiple, c.Box.TouchBandPts, c.KeyLevelPrunePts, c.TrendlineLevelPreferPts, c.TrendlineMaxLinesPerAnchor)
	}
	if c.EmaLossRelock != mentor.EmaRelockUntilZoneExit || c.PHLPullbackLow != mentor.PHLPullbackLowPullback || c.FifteenMinIBRule != mentor.FifteenMinRequire5mISB {
		t.Errorf("stored enums did not reach the evaluator: %q %q %q", c.EmaLossRelock, c.PHLPullbackLow, c.FifteenMinIBRule)
	}
	// A stored "off" is a legal enum value for ema34_loss_relock.
	c = tuningTick(t, &store.MentorTuning{EMA34LossRelock: "off"})
	if c.EmaLossRelock != mentor.EmaRelockOff {
		t.Errorf("ema34_loss_relock=off -> %q", c.EmaLossRelock)
	}
}

// Row 17 (trader exits): the X8 trap-dodge exit is OFF and the one-bracket
// resonance BE needs no distance beyond the entry; the stored values revert.
func TestOctA2_ExitKnobsDefaultOffAndRevert(t *testing.T) {
	if mentorX8TrapDodgeEnabled(nil) || mentorX8TrapDodgeEnabled(&store.RiskControlConfig{}) {
		t.Fatalf("X8 trap-dodge default ON, want OFF (A2 row 17)")
	}
	tr, minRR := true, 0.25
	rc := &store.RiskControlConfig{MentorTuning: &store.MentorTuning{MentorX8TrapDodge: &tr, MentorResonanceBEMinRR: &minRR}}
	if !mentorX8TrapDodgeEnabled(rc) {
		t.Fatalf("stored mentor_x8_trap_dodge=true did not revert")
	}
	if got := mentorResonanceBEMinRRFor(nil); got != 0 {
		t.Fatalf("default resonance BE min = %v, want 0", got)
	}
	if got := mentorResonanceBEMinRRFor(rc); got != mentorResonanceBEMinRR {
		t.Fatalf("stored 0.25 -> %v, want the Sept %v", got, mentorResonanceBEMinRR)
	}
	bad := 9.0
	rc.MentorTuning.MentorResonanceBEMinRR = &bad
	if got := mentorResonanceBEMinRRFor(rc); got != 0 {
		t.Fatalf("out-of-range value -> %v, want 0", got)
	}
}

// pinPreOctA2 pins every OCT-EXACT A2 revert (the Sept entry/level gates and
// exits) on a stored strategy: replay and parity tests whose reference day ran
// the Sept gates use it. pinPreOctA1 calls it, so every A1 pin carries A2 too.
// It only FILLS knobs the caller has not already pinned (an older pin such as
// the pre-#25 course entries keeps its value).
func pinPreOctA2(rc *store.RiskControlConfig) {
	two, band, prune, prefer, minRR := 2.0, 0.25, 20.0, 2.0, mentorResonanceBEMinRR
	if rc.MentorTuning == nil {
		rc.MentorTuning = &store.MentorTuning{}
	}
	t := rc.MentorTuning
	for _, p := range []**bool{
		&t.MentorHTFBoxGates, &t.MentorTFConflict, &t.MentorMTFConfluence, &t.MentorRollingLevelTouchOwns,
		&t.KeypairRuleEnabled, &t.MentorTriggerRetestWrongWay, &t.MentorBoxPingPong, &t.MentorPHLISBPreferredAtBox,
		&t.MentorTriggerBoxZone, &t.MentorISBLocationSizeCuts, &t.MentorPullbackWarnCut, &t.MentorTrendlineBoxBeats,
		&t.MentorConfluenceHold2R, &t.MentorX8TrapDodge, &t.Early15mRead, &t.FlagWallTarget, &t.TrendlineIsTarget, &t.NearBoxRoomPHL,
	} {
		if *p == nil {
			on := true
			*p = &on
		}
	}
	for _, f := range []struct {
		p **float64
		v float64
	}{
		{&t.NearBoxRoomMultiple, 2}, {&t.MentorBoxRoomMultiple, two}, {&t.MentorBoxTouchBandPts, band},
		{&t.MentorKeyLevelPrunePts, prune}, {&t.TrendlineLevelPreferPts, prefer}, {&t.MentorResonanceBEMinRR, minRR},
	} {
		if *f.p == nil {
			v := f.v
			*f.p = &v
		}
	}
	if t.TrendlineMaxLinesPerAnchor == 0 {
		t.TrendlineMaxLinesPerAnchor = 1
	}
	if t.FifteenMinIBRequires5mISB == "" {
		t.FifteenMinIBRequires5mISB = mentor.FifteenMinRequire5mISB
	}
	if t.EMA34LossRelock == "" {
		t.EMA34LossRelock = mentor.EmaRelockUntilZoneExit
	}
	if t.PHLPullbackLow == "" {
		t.PHLPullbackLow = mentor.PHLPullbackLowPullback
	}
}
