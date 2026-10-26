package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// Frame-switch carry (m407 review-back): a VALID 1m line is a live location and
// must not leave the Level set at the 09:00 switch to the 2m build.

// carryTape: trendlineReturnTape on the 1m chart from 08:45 (valid at 08:58 —
// its 3rd touch; c52 W9 B7-R7 moved the start one minute earlier because the
// FTGL box edge 0.9 pt under the fresh line now beats it ON ITS FIRST TEST, so
// the 08:59 reading is the line PAST its first test), then flat bars well above the line (no touch, no swing), then — withBreak —
// closed bars through the line from 09:15.
func carryTape(withBreak bool) ([]market.Kline, int64) {
	t0 := ctAt(8, 45)
	base, _ := trendlineReturnTape()
	var out []market.Kline
	for i, k := range base {
		k.OpenTime = t0 + int64(i)*60_000
		k.CloseTime = k.OpenTime + 59_999
		out = append(out, k)
	}
	n := 30
	if withBreak {
		n = 35
	}
	for i := len(out); i < n; i++ {
		o := t0 + int64(i)*60_000
		k := market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 106, High: 107, Low: 105, Close: 106}
		if withBreak && o >= ctAt(9, 15) {
			k = market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: 100, High: 100.5, Low: 94.5, Close: 95}
		}
		out = append(out, k)
	}
	return out, t0
}

func carryTickRange(e *Evaluator, bars []market.Kline, from, to int) {
	for i := from; i <= to; i++ {
		e.Tick(bars[:i], BarCloseInstant(bars[i-1]))
	}
}

func hasLevel(e *Evaluator, key string) bool {
	for _, l := range e.State.ChartLevels {
		if l.Kind == KindTrendline && l.Key == key {
			return true
		}
	}
	return false
}

func hasLine(e *Evaluator, key string) bool {
	for _, tl := range e.State.ChartTrendlines {
		if tl.key() == key {
			return true
		}
	}
	return false
}

// 08:59 valid 1m line → still a Level and still drawn at 09:00 and 09:10; a
// closed 5m candle closing through it at 09:15 → gone.
func TestTrendlineCarryValid1mLineAcross0900Switch(t *testing.T) {
	bars, t0 := carryTape(true)
	key := Trendline{Side: SideLong, P0T: t0 + 2*60_000, P1T: t0 + 8*60_000}.key()
	idx := func(h, m int) int { return int((ctAt(h, m)-t0)/60_000) + 1 } // bars[:idx] ends with that minute
	e := execTFEval(bars, 0, true)
	carryTickRange(e, bars, 3, idx(8, 59))
	if !hasLevel(e, key) || !hasLine(e, key) {
		t.Fatalf("fixture: the 1m line must be a valid Level at 08:59 — lines=%+v", e.State.ChartTrendlines)
	}
	prev := idx(8, 59)
	for _, hm := range [][2]int{{9, 0}, {9, 10}} {
		carryTickRange(e, bars, prev+1, idx(hm[0], hm[1]))
		prev = idx(hm[0], hm[1])
		if !hasLine(e, key) || !hasLevel(e, key) {
			t.Fatalf("%02d:%02d: the valid 1m line left the set at the 2m switch — lines=%+v", hm[0], hm[1], e.State.ChartTrendlines)
		}
	}
	carryTickRange(e, bars, prev+1, len(bars)) // 09:20 — the 09:15 5m candle closed at 95
	if hasLine(e, key) || hasLevel(e, key) {
		t.Fatalf("09:20: a closed 5m close through the line must remove it — lines=%+v", e.State.ChartTrendlines)
	}
}

// the mirror: a valid 2m line carries into the 08:30–09:00 1m period.
func TestTrendlineCarryValid2mLineInto0830(t *testing.T) {
	e := New(DefaultConfig())
	t0 := ctAt(8, 0)
	line := Trendline{Side: SideLong, P0Px: 97, P1Px: 98.2, P0T: t0, P1T: t0 + 12*60_000, ValidAt: 5}
	e.carryIntradayLines([]Trendline{line}, nil, time.UnixMilli(t0+29*60_000), 2, true)
	got := e.carryIntradayLines(nil, nil, time.UnixMilli(t0+31*60_000), 1, true)
	if len(got) != 1 || !got[0].carried || got[0].key() != line.key() {
		t.Fatalf("2m valid line not carried into the 1m period: %+v", got)
	}
}

// not duplicated when the new frame draws the same line; forming lines are not
// carried; knob OFF carries nothing.
func TestTrendlineCarryDedupAndForming(t *testing.T) {
	e := New(DefaultConfig())
	t0 := ctAt(8, 40)
	valid := Trendline{Side: SideLong, P0Px: 97, P1Px: 98.2, P0T: t0 + 60_000, P1T: t0 + 13*60_000, ValidAt: 9}
	forming := Trendline{Side: SideShort, P0Px: 110, P1Px: 109, P0T: t0, P1T: t0 + 600_000}
	e.carryIntradayLines([]Trendline{valid, forming}, nil, time.UnixMilli(t0+20*60_000), 1, true)
	// 2m frame: same anchors (times inside the same 2m buckets, prices within a tick).
	same := Trendline{Side: SideLong, P0Px: 97.25, P1Px: 98.2, P0T: t0, P1T: t0 + 12*60_000}
	got := e.carryIntradayLines([]Trendline{same}, nil, time.UnixMilli(t0+31*60_000), 2, true)
	n := 0
	for _, tl := range got {
		if tl.Side == SideLong {
			n++
			if !tl.carried {
				t.Fatalf("the surviving line must be the carried valid one: %+v", tl)
			}
		}
		if tl.Side == SideShort {
			t.Fatalf("a FORMING 1m line was carried: %+v", tl)
		}
	}
	if n != 1 {
		t.Fatalf("support lines = %d, want exactly 1 (no duplicate): %+v", n, got)
	}
	off := New(DefaultConfig())
	if r := off.carryIntradayLines(nil, nil, time.UnixMilli(t0), 2, false); len(r) != 0 || off.tlCarry != nil {
		t.Fatalf("knob OFF must carry nothing: %+v", r)
	}
}

// m311 first-test suppression must not re-apply to a carried line.
func TestTrendlineCarriedLineSkipsFirstTestSuppression(t *testing.T) {
	bars, _ := trendlineReturnTape()
	tl := Trendline{Side: SideLong, P0Px: 97, P1Px: 98.2, P0T: bars[2].OpenTime, P1T: bars[8].OpenTime, ValidAt: len(bars) - 1}
	price := tl.priceAt(bars[len(bars)-1].OpenTime)
	respected := []Level{{Key: "k", Kind: KindKeyLevel, Price: price}}
	if lv, ref := TrendlineLevels([]Trendline{tl}, nil, respected, bars); len(lv) != 0 || len(ref) != 1 {
		t.Fatalf("control: a fresh first-test line beside a level must be refused: %+v %v", lv, ref)
	}
	tl.carried = true
	if lv, ref := TrendlineLevels([]Trendline{tl}, nil, respected, bars); len(lv) != 1 || len(ref) != 0 {
		t.Fatalf("a carried line must stay a location: %+v %v", lv, ref)
	}
}
