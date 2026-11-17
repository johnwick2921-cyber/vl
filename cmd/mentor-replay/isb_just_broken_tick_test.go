package main

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// jbFixtureNight1116 is jbFixture1116 plus the 11-16 09:00 -> 11-17 07:00 CT
// night tape (extracted read-only from data.db bars, contract MNQ 12-26; the
// same file the #71/#72 night replay ran on, REPORT-night-1116-trades.md §5).
func jbFixtureNight1116(t *testing.T) *barSet {
	t.Helper()
	a := jbFixture1116(t)
	b := loadFixtureFile(t, "mnq_12-26_1m_20261116_0900-20261117_0700.csv.gz")
	rows := a.byTF["1m"][fixtureContract]
	last := rows[len(rows)-1].T
	for _, r := range b.byTF["1m"][fixtureContract] {
		if r.T > last {
			rows = append(rows, r)
		}
	}
	a.byTF["1m"][fixtureContract] = rows
	return a
}

// jbTol builds a strategy whose mentor tuning pins the ISB-M1 tolerance.
func jbTol(pts float64) *store.StrategyConfig {
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorISBJustBrokenTolPts: &pts}
	return sc
}

// ISB-M1 one-tick tolerance (owner order 2026-11-17 "build", after the 11-16
// night replay of #72 refused 9 winners / 6 losers per trader). October gives
// no points distance (Oct D3.3 @00:46-01:00, @02:40); the owner rules a break of
// ONE TICK (0.25 pt) OR LESS is not a break. On the night tape (bar = close min):
//
//	21:48 short ISB 27,424.00 (pos 813): mother 21:46 O 27,441.25 C 27,434.75,
//	      ~0.03 pt under the sloping trendline 27,434.78 [B]
//	22:01 short ISB 27,377.00 (pos 819): mother 21:59 O 27,390.00 C 27,383.00,
//	      0.25 under key 27,383.25 [A]
//	05:17 long  ISB 27,271.50 (pos 902): mother 05:15 O 27,255.25 C 27,261.25,
//	      OPENED 0.25 under key 27,255.50 and closed 5.75 over it [A] — a
//	      one-tick OPEN, not a one-tick break (05:14 closed one tick under the
//	      key, 05:13 four under): it arms only because the both-ends read
//	      ignores a break split at a bar seam (#73 review P1, owner ruling
//	      pending; kernel TestISBM1Tick_BarSeamPendingOwnerRuling)
//
// all three ARM under the default (0.25). The real breaks stay refused:
//
//	19:14 long (mother 19:12 O 27,330.50 C 27,368.00 through key 27,359: 28.50 / 9.00)
//	19:26 long (mother 19:24 O 27,345.50 C 27,357.00 through the 27,353.39 line)
//	19:58 short (19:55 O 27,377.50 C 27,365.50 down through the 27,367.08 line, J-BOX-14)
//
// and tolerance 0 is the exact #72 read: the three are refused again.
func TestISBM1Tick_1116NightOneTickBreaksArm(t *testing.T) {
	bs := jbFixtureNight1116(t)
	end := time.Date(2026, 11, 17, 5, 20, 0, 0, kernel.CTLocation())
	oneTick := []struct {
		hhmm, side string
		px         float64
		why        string
	}{
		{"21:48", "short", 27424.00, "pos 813: ~0.03 pt through the 27,434.78 trendline"},
		{"22:01", "short", 27377.00, "pos 819: the mother closed 0.25 through key 27,383.25"},
		{"05:17", "long", 27271.50, "pos 902: the mother opened 0.25 below key 27,255.50, closed 5.75 over it (bar seam, owner ruling pending)"},
	}
	realBreaks := []struct{ hhmm, side, why string }{
		{"19:14", "long", "key 27,359 (live 19:14 loser, rows 608/609)"},
		{"19:26", "long", "trendline 27,353.39 (pos 797 loser)"},
		{"19:58", "short", "trendline 27,367.08 (pos 803 loser)"},
	}

	arms, ref, _ := jbReplay(t, bs, "2026-11-17", end, nil)
	for _, w := range oneTick {
		if !jbArmAt(arms, w.hhmm, w.side, "ISB", w.px) {
			t.Errorf("default (tolerance 0.25): no %s %s ISB %.2f (%s); refusals then: %v", w.hhmm, w.side, w.px, w.why, jbAt(ref, w.hhmm))
		}
		if n := ref[w.hhmm+" isb_just_broken_level"]; n != 0 {
			t.Errorf("default (tolerance 0.25): %s refused isb_just_broken_level x%d - a one-tick break is not a break (%s)", w.hhmm, n, w.why)
		}
	}
	for _, w := range realBreaks {
		if ref[w.hhmm+" isb_just_broken_level"] == 0 {
			t.Errorf("default: %s %s ISB no longer refused isb_just_broken_level (%s); refusals then: %v", w.hhmm, w.side, w.why, jbAt(ref, w.hhmm))
		}
		if jbArmAt(arms, w.hhmm, w.side, "ISB", 0) {
			t.Errorf("default: a %s %s ISB armed - the break was more than one tick (%s)", w.hhmm, w.side, w.why)
		}
	}

	arms0, ref0, _ := jbReplay(t, bs, "2026-11-17", end, jbTol(0))
	for _, w := range oneTick {
		if ref0[w.hhmm+" isb_just_broken_level"] == 0 {
			t.Errorf("tolerance 0 (exact #72): %s not refused isb_just_broken_level (%s); refusals then: %v", w.hhmm, w.why, jbAt(ref0, w.hhmm))
		}
		if jbArmAt(arms0, w.hhmm, w.side, "ISB", w.px) {
			t.Errorf("tolerance 0 (exact #72): the %s %s ISB %.2f armed", w.hhmm, w.side, w.px)
		}
	}
	for _, w := range realBreaks {
		if ref0[w.hhmm+" isb_just_broken_level"] == 0 {
			t.Errorf("tolerance 0: %s not refused isb_just_broken_level (%s)", w.hhmm, w.why)
		}
	}
}

// #73 review P2 (both reviewers): the knob's ceiling must not reach the pinned
// real breaks. The nearest one on the night is the 19:58 short (pos 803, a
// loser): 19:55 O 27,377.50 C 27,365.50 closed 0.714 pt under the 27,367.08 line
// (J-BOX-14). At the ceiling (0.5, two ticks) it stays refused; a stored 0.75 or
// 5 (the old ceiling, which released it) is out of range, fails closed to the
// 0.25 default and is counted tuning_bad_value — so 19:14 / 19:26 / 19:58 stay
// refused for every stored value. Production replay, the night tape.
func TestISBM1Tick_CeilingKeepsTheNightRealBreaks(t *testing.T) {
	bs := jbFixtureNight1116(t)
	end := time.Date(2026, 11, 16, 20, 5, 0, 0, kernel.CTLocation())
	realBreaks := []struct{ hhmm, side, why string }{
		{"19:14", "long", "key 27,359, 9.00 through"},
		{"19:26", "long", "trendline 27,353.39, 3.61 through"},
		{"19:58", "short", "trendline 27,367.08, 0.714 through"},
	}
	for _, v := range []float64{0.5, 0.75, 5} {
		arms, ref, _ := jbReplay(t, bs, "2026-11-17", end, jbTol(v))
		for _, w := range realBreaks {
			if ref[w.hhmm+" isb_just_broken_level"] == 0 {
				t.Errorf("stored tolerance %v: %s %s ISB not refused isb_just_broken_level (%s); refusals then: %v", v, w.hhmm, w.side, w.why, jbAt(ref, w.hhmm))
			}
			if jbArmAt(arms, w.hhmm, w.side, "ISB", 0) {
				t.Errorf("stored tolerance %v: a %s %s ISB armed (%s) - the knob's ceiling released a real break", v, w.hhmm, w.side, w.why)
			}
		}
	}
}
