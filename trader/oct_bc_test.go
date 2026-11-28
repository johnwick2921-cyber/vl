package trader

import (
	"testing"
	"time"

	"vl/store"
)

// OCT-EXACT build step BC (CTO-decisions.md sections B and C, owner order
// 2026-11-14 "mentor mode = the October course exactly"; mentor mode only: the
// readers below take the mentor RiskControlConfig and AI-mode never reaches
// them).

// octBEDefault runs one test with the PRODUCTION unset default of
// be_offset_ticks (4); the trader test binary runs with 0 (TestMain) so the
// pre-October mechanics tests keep asserting BE at the entry.
func octBEDefault(t *testing.T) {
	t.Helper()
	prev := mentorBEOffsetDefaultTicks
	mentorBEOffsetDefaultTicks = 4
	t.Cleanup(func() { mentorBEOffsetDefaultTicks = prev })
}

// B7: be_offset_ticks defaults to 4 ticks = 1 point ("10 -> 11 đô" [Oct D4.2
// @13:00-13:25]); a stored 0 reverts to BE at the entry; out of range falls
// back to the default (reader and resolver agree).
func TestOctBC_B7BEOffsetDefaultIsFourTicks(t *testing.T) {
	octBEDefault(t)
	rc := func(v *int) *store.RiskControlConfig {
		return &store.RiskControlConfig{MentorTuning: &store.MentorTuning{BEOffsetTicks: v}}
	}
	at, _ := resetTrader(t, store.StrategyConfig{})
	for _, c := range []struct {
		name string
		rc   *store.RiskControlConfig
		want int
	}{
		{"nil rc", nil, 4}, {"empty rc", &store.RiskControlConfig{}, 4}, {"nil knob", rc(nil), 4},
		{"stored 0 = the revert", rc(m472Int(0)), 0}, {"stored 2", rc(m472Int(2)), 2},
		{"negative falls back to the default", rc(m472Int(-1)), 4}, {"over 20 falls back to the default", rc(m472Int(21)), 4},
	} {
		if got := mentorBEOffsetTicks(c.rc); got != c.want {
			t.Errorf("%s: reader = %d, want %d", c.name, got, c.want)
		}
		if c.rc != nil {
			if got := mentorTuningResolve(at, c.rc).BEOffsetTicks; got != c.want {
				t.Errorf("%s: resolver = %d, want %d", c.name, got, c.want)
			}
		}
	}
	// the stop itself: a long BE at 28,215.00 sits 1.00 above, a short 1.00 below.
	if got := mentorBEPastEntry("long", 28215, 0.25, mentorBEOffsetTicks(rc(nil))); got != 28216 {
		t.Errorf("long BE = %.2f, want 28216.00", got)
	}
	if got := mentorBEPastEntry("short", 28215, 0.25, mentorBEOffsetTicks(rc(nil))); got != 28214 {
		t.Errorf("short BE = %.2f, want 28214.00", got)
	}
}

// C6: after BE (armed on a prior candle, 1:1 partial confirmed) the mode-A
// runner trails one tick beyond each closed candle like mode B [Oct D4.2-R31/R32,
// later than R13 "no trail"; D4.2 #5: BE then trailed to 28,218.75 on the LONG;
// this fixture is the mirrored SHORT #651 numbers of the r22 suite]. The revert
// mentor_resonance_trail=false is the old no-trail mode A.
func TestOctBC_C6ResonanceRunnerTrailsAfterBE(t *testing.T) {
	run := func(tuning *store.MentorTuning) (stops []float64) {
		b := r22Fixture(t, store.RiskControlConfig{MentorTuning: tuning})
		b.positions("short", 3)
		b.book(r22FullBook(r22Stop)...)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00)
		b.candle(r22Minute(1), 30739.00, 30741.00, 30737.00, 30738.00)
		b.at.mentorArmResonanceOnISB("short")
		if lp := b.at.mentorLivePos[r22Sig]; lp == nil || lp.Pos.Mode != "A-resonance" {
			t.Fatalf("fixture: mode A expected: %+v", lp)
		}
		b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(3).Add(10*time.Second))
		b.positions("short", 1)
		b.book(r22RunnerBook(r22Stop)...)
		b.candle(r22Minute(3), 30733.00, 30734.00, 30724.00, 30726.00) // BE candle: only arms BE
		if got := b.waitMoves(1); len(got) != 1 || got[0].NewStopLoss != r22Entry {
			t.Fatalf("fixture: the runner goes to BE %.2f first; got %+v", r22Entry, got)
		}
		b.book(r22RunnerBook(r22Entry)...) // NT8 applied BE
		b.candle(r22Minute(4), 30726.00, 30736.00, 30722.00, 30730.00)
		time.Sleep(200 * time.Millisecond)
		for _, m := range b.waitMoves(2) {
			stops = append(stops, m.NewStopLoss)
		}
		return stops
	}
	got := run(nil)
	if len(got) != 2 || got[1] != 30736.25 {
		t.Fatalf("October (mentor_resonance_trail default ON): BE then the runner trails to 1 tick above the closed candle's high 30736.25; got %v", got)
	}
	off := false
	got = run(&store.MentorTuning{MentorResonanceTrail: &off})
	if len(got) != 1 {
		t.Fatalf("revert mentor_resonance_trail=false: the old no-trail mode A moves BE only; got %v", got)
	}
}

// pinPreOctBC pins the pre-October (Sept) values of the OCT-EXACT BC knobs for
// the live-day replays and mechanics tests that ran Sept behaviour: section B
// back to OFF / 0, C3 locks the level again, C4 follows the shared 2x room, C1
// has no scalp target, C6 keeps the no-trail mode A. Only a nil field is set,
// so a test that stores its own value wins. Called from pinPreOctA2 (and so
// from every pinPreOctA1 caller).
func pinPreOctBC(rc *store.RiskControlConfig) {
	if rc.MentorTuning == nil {
		rc.MentorTuning = &store.MentorTuning{}
	}
	t := rc.MentorTuning
	off, on := false, true
	for _, p := range []**bool{
		&t.PHL5mBoxSameDirAllowed, &t.TouchStopCoverLaterWick, &t.RollingStaleTouchReleases, &t.RollingKeypairTapeExempt,
		&t.ISBTargetFillClose, &t.BoxTrendlinePartialNotRoom, &t.MentorResonanceTrail,
	} {
		if *p == nil {
			v := off
			*p = &v
		}
	}
	if t.MentorLevelFirstTouchLock == nil {
		v := on
		t.MentorLevelFirstTouchLock = &v
	}
	zeroF, twoF := 0.0, 2.0
	if t.PHLMinAbovePriorPts == nil {
		t.PHLMinAbovePriorPts = &zeroF
	}
	if t.MentorCounter5mScalpPts == nil {
		t.MentorCounter5mScalpPts = &zeroF
	}
	if t.MentorISBRoomMultiple == nil {
		t.MentorISBRoomMultiple = &twoF
	}
	if t.BEOffsetTicks == nil {
		z := 0
		t.BEOffsetTicks = &z
	}
}
