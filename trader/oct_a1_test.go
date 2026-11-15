package trader

import (
	"testing"

	"vl/store"
)

// OCT-EXACT build step A1 (CTO-decisions.md section A1; owner order 2026-11-14
// "mentor mode = the October course exactly"): the production call site
// (mentorEvalOnce -> mentorEvaluatorConfig) hands the evaluator every A1 gate
// OFF by default, and a STORED true (the revert) reaches it. Mentor mode only:
// mentorEvaluatorConfig builds the mentor evaluator's Config, AI mode never
// reads it.
func TestOctA1_EvaluatorConfigDefaultsAndReverts(t *testing.T) {
	c := tuningTick(t, nil)
	off := map[string]bool{
		"HTFGateEnabled":     c.HTFGateEnabled,
		"DayGateEnabled":     c.DayGateEnabled,
		"Swing4HEnabled":     c.Swing4HEnabled,
		"OrbPart2Enabled":    c.OrbPart2Enabled,
		"Open5mGateEnabled":  c.Open5mGateEnabled,
		"NewsWaitBothLines":  c.NewsWaitBothLines,
		"NewsBarsExcluded":   c.NewsBarsExcluded,
		"LegBudgetEnabled":   c.LegBudgetEnabled,
		"LossBoxEnabled":     c.LossBoxEnabled,
		"MidRangeRejectSide": c.MidRangeRejectSide,
		"BreakDirectionLock": c.BreakDirectionLock,
	}
	for name, v := range off {
		if v {
			t.Errorf("default %s = true, want false (October)", name)
		}
	}
	if c.OrbISBFractionOutside != 0 {
		t.Errorf("default OrbISBFractionOutside = %v, want 0", c.OrbISBFractionOutside)
	}
	if !c.OrbGateEnabled {
		t.Errorf("OrbGateEnabled must stay true (orb_inside, part 1)")
	}

	tr := true
	frac := 0.8
	c = tuningTick(t, &store.MentorTuning{
		MentorHTFGateEnabled: &tr, MentorDayGateEnabled: &tr, MentorSwing4HEnabled: &tr,
		MentorOrbPart2Enabled: &tr, MentorOpen5mGateEnabled: &tr, MentorLossBoxEnabled: &tr,
		NewsWaitBothLines: &tr, NewsBarsExcluded: &tr, MentorBreakDirectionLock: &tr,
		MentorMidRangeRejectSide: &tr, OrbISBFractionOutside: &frac,
	})
	on := map[string]bool{
		"HTFGateEnabled": c.HTFGateEnabled, "DayGateEnabled": c.DayGateEnabled, "Swing4HEnabled": c.Swing4HEnabled,
		"OrbPart2Enabled": c.OrbPart2Enabled, "Open5mGateEnabled": c.Open5mGateEnabled, "LossBoxEnabled": c.LossBoxEnabled,
		"NewsWaitBothLines": c.NewsWaitBothLines, "NewsBarsExcluded": c.NewsBarsExcluded,
		"BreakDirectionLock": c.BreakDirectionLock, "MidRangeRejectSide": c.MidRangeRejectSide,
	}
	for name, v := range on {
		if !v {
			t.Errorf("stored true %s did not reach the evaluator (the revert is broken)", name)
		}
	}
	if c.OrbISBFractionOutside != 0.8 {
		t.Errorf("stored orb_isb_fraction_outside 0.8 -> %v", c.OrbISBFractionOutside)
	}
}

// Rows 7, 8, 9: the trader-side gates (news hold, done-after-win, leg budget).
func TestOctA1_TraderGatesDefaultOffAndRevert(t *testing.T) {
	tr := true
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	if at.mentorNewsHoldEnabled() {
		t.Errorf("news hold default ON, want OFF (A1 row 7)")
	}
	if hold, _ := at.mentorNewsGate(); hold {
		t.Errorf("mentorNewsGate held with the knob OFF")
	}
	if at.mentorDoneAfterWinEnabled() {
		t.Errorf("done_after_win default ON, want OFF (A1 row 8)")
	}
	if refuse, _ := at.mentorDoneAfterWinGate(); refuse {
		t.Errorf("done_after_win gate refused with the knob OFF")
	}
	if mentorLegBudgetEnabled(at.mentorRiskControl()) {
		t.Errorf("leg budget default ON, want OFF (A1 row 9)")
	}
	if mentorLegBudgetEnabled(nil) {
		t.Errorf("leg budget with no risk_control must be OFF")
	}

	at = mentoredTrader(t, store.RiskControlConfig{
		MentorMode:             true,
		MentorDoneAfterWin:     &tr,
		MentorLegBudgetEnabled: &tr,
		MentorTuning:           &store.MentorTuning{MentorNewsHoldEnabled: &tr},
	})
	if !at.mentorNewsHoldEnabled() || !at.mentorDoneAfterWinEnabled() || !mentorLegBudgetEnabled(at.mentorRiskControl()) {
		t.Errorf("stored true reverts (news hold / done_after_win / leg budget) did not take")
	}
}

// octNewsHoldOn restores the Sept news_hold behaviour (A1 row 7 revert:
// mentor_tuning.mentor_news_hold_enabled=true) for the tests that pin the hold's
// and the news flatten's mechanics.
func octNewsHoldOn(t *testing.T, at *AutoTrader) {
	t.Helper()
	rc := at.mentorRiskControl()
	if rc == nil {
		t.Fatal("octNewsHoldOn: trader has no strategy config")
	}
	on := true
	if rc.MentorTuning == nil {
		rc.MentorTuning = &store.MentorTuning{}
	}
	rc.MentorTuning.MentorNewsHoldEnabled = &on
}

// pinPreOctA1 pins the PRE-OCT-EXACT-A1 (Sept) values of every A1 gate onto a
// strategy's risk_control, for the tests that replay a day the bot traded under
// those gates (the live 10-13 / 10-14 / 10-15 days): the HTF 4h/1h gate, the day
// gate, SWING4H, ORB part 2, the open-5m gate, the news hold / wait / freeze, the
// leg budget, the loss box, mid-range reject side, the break direction lock,
// done-after-win and the 0.80 ORB-edge ISB fraction. Every value is the revert.
func pinPreOctA1(rc *store.RiskControlConfig) {
	on := true
	frac := 0.80
	if rc.MentorTuning == nil {
		rc.MentorTuning = &store.MentorTuning{}
	}
	t := rc.MentorTuning
	t.MentorHTFGateEnabled, t.MentorDayGateEnabled, t.MentorSwing4HEnabled = &on, &on, &on
	t.MentorOrbPart2Enabled, t.MentorOpen5mGateEnabled, t.MentorNewsHoldEnabled = &on, &on, &on
	t.MentorLossBoxEnabled, t.MentorBreakDirectionLock, t.MentorMidRangeRejectSide = &on, &on, &on
	t.NewsWaitBothLines, t.NewsBarsExcluded = &on, &on
	t.OrbISBFractionOutside = &frac
	rc.MentorLegBudgetEnabled = &on
	rc.MentorDoneAfterWin = &on
	pinPreOctA2(rc) // OCT-EXACT A2: the same live days ran the Sept entry/level gates and exits
}

// octPreA1Tuning returns a MentorTuning with the Sept A1 gates pinned (see pinPreOctA1).
func octPreA1Tuning() *store.MentorTuning {
	rc := store.RiskControlConfig{}
	pinPreOctA1(&rc)
	return rc.MentorTuning
}
