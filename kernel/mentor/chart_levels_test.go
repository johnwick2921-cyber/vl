package mentor

import (
	"testing"
	"time"
)

// T7 (release #22, UI-GUIDE-TRUTH): the chart / truth panel read the level set
// the evaluator stored BEFORE the trigger lines, the EMA 34 location, the box
// edges and the trendlines were appended (eval.go `e.State.Levels = levels`
// sits above every append), so none of those places could ever be drawn.
// ChartLevels is the SAME tick's set read AFTER every append.
//
// Production call site: Evaluator.Tick. Mutant: move the ChartLevels
// assignment up beside `e.State.Levels = levels` → this test goes RED (no
// trendline, no HTF trigger, no box edge in the chart set).
func TestTickChartLevelsCarryEveryAppendedPlace(t *testing.T) {
	bars, mk := trendlineReturnTape()
	// c52 W9 B7-R7: bar 14 is a 4th touch so the line is past its first test (at
	// the first test the FTGL box top 0.9 pt below it beats it as a Level).
	bars = append(bars, mk(14, 101, 101.5, 99.3, 101))
	now := bars[14].OpenTime + 59_999
	tls := TrendlinesBuild(bars, time.UnixMilli(now).In(ctime()))
	var tl Trendline
	for _, x := range tls {
		if x.Side == SideLong && x.ValidAt != 0 {
			tl = x
		}
	}
	if tl.ValidAt == 0 {
		t.Fatalf("fixture: no valid support trendline in %+v", tls)
	}
	e := trendlineEval(bars, now, tl.key(), tl.priceAt(bars[14].OpenTime))
	e.Tick(bars, now)

	kinds := map[LevelKind]int{}
	for _, l := range e.State.ChartLevels {
		kinds[l.Kind]++
	}
	for _, want := range []LevelKind{KindTrendline, KindHTFTrigger, KindOldExtreme} {
		if kinds[want] == 0 {
			t.Errorf("chart set has no %q after Tick — kinds=%v", want, kinds)
		}
	}
	if kinds[KindFTGHEdge]+kinds[KindFTGLEdge] == 0 {
		t.Errorf("chart set has no box edge after Tick — kinds=%v", kinds)
	}
	found := false
	for _, x := range e.State.ChartTrendlines {
		if x.Side == SideLong && x.ValidAt != 0 && !x.Dead && x.P0T == tl.P0T && x.P1T == tl.P1T {
			found = true
		}
	}
	if !found {
		t.Errorf("ChartTrendlines lacks the valid support line %+v — got %+v", tl, e.State.ChartTrendlines)
	}

	// Display only: the TRADING set (State.Levels, read by the fill drain's
	// row fallback) is unchanged — no appended kind leaks into it.
	for _, l := range e.State.Levels {
		switch l.Kind {
		case KindTrendline, KindHTFTrigger, KindTriggerRetest, KindFTGHEdge, KindFTGLEdge, KindEMA34HTF, KindWickMicroscalp:
			t.Errorf("State.Levels (trading) gained display-only kind %q", l.Kind)
		}
	}
}
