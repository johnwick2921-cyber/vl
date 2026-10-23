package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// m407 — the intraday trendlines are drawn on the mentor's EXECUTION frame:
// "Em lúc nào cũng prefer trade khung 2 phút … Trong dòng 30 phút đầu giờ em vẫn
// sẽ trade khung 1 phút … sau 30 phút em sẽ chuyển qua khung 2 phút em trade"
// [Routine trước khi trade p1 @00:00:34–00:01:14]; "Thường em sẽ vẽ trendline
// KHUNG 2 PHÚT — nó sẽ CLEAR HƠN" [Cách vẽ trendline p1 R5 / X9 @03:35–03:49].
//
// The fixture is the trendlineReturnTape read as 2m candles (14 of them), each
// expanded to two 1m bars with the candle's LOW in its SECOND minute. A 2m swing
// low therefore sits at an EVEN bucket open on the 2m chart, and at an ODD
// minute on the 1m chart — the two builds draw visibly different anchors.

func execTFTape(t0 int64, extra ...[4]float64) []market.Kline {
	c2, _ := trendlineReturnTape()
	var cs [][4]float64
	for _, k := range c2 {
		cs = append(cs, [4]float64{k.Open, k.High, k.Low, k.Close})
	}
	cs = append(cs, extra...)
	var out []market.Kline
	for i, c := range cs {
		o, h, l, cl := c[0], c[1], c[2], c[3]
		mid := (o + cl) / 2
		lowA := mid
		if o < lowA {
			lowA = o
		}
		open := t0 + int64(i)*120_000
		out = append(out,
			market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: o, High: h, Low: lowA, Close: mid},
			market.Kline{OpenTime: open + 60_000, CloseTime: open + 119_999, Open: mid, High: max(mid, cl), Low: l, Close: cl},
		)
	}
	return out
}

func execTFEval(bars []market.Kline, now int64, knob bool) *Evaluator {
	e := trendlineEval(bars, now, "none", 0)
	e.Cfg.Exec2mAfter30m = knob
	return e
}

func tlSupport(tls []Trendline) (Trendline, bool) {
	for _, x := range tls {
		if x.Side == SideLong {
			return x, true
		}
	}
	return Trendline{}, false
}

func ctAt(h, m int) int64 {
	return time.Date(2026, time.September, 15, h, m, 0, 0, ctime()).UnixMilli()
}

// (a) 10:00 CT, knob ON: the line forms from the CLOSED 2m swings (P0 = the 2m
// bucket open of candle 2, P1 = candle 8), is VALID on the 3rd-touch return, and
// is NOT the line the 1m build draws.
func TestTrendlineExecTFAfter30mBuildsOn2m(t *testing.T) {
	t0 := ctAt(10, 0)
	bars := execTFTape(t0)
	now := bars[len(bars)-1].CloseTime + 1
	e := execTFEval(bars, now, true)
	e.Tick(bars, now)
	tl, ok := tlSupport(e.State.ChartTrendlines)
	if !ok {
		t.Fatalf("no support line on the 2m build: %+v", e.State.ChartTrendlines)
	}
	if tl.P0T != t0+4*60_000 || tl.P1T != t0+16*60_000 || tl.P0Px != 97 || tl.P1Px != 98.2 {
		t.Fatalf("2m line anchors = %+v, want P0T=%d (2m open) P1T=%d", tl, t0+4*60_000, t0+16*60_000)
	}
	if tl.ValidAt == 0 || tl.Dead || tl.TFMin != 0 {
		t.Fatalf("2m line = %+v, want valid (3rd-touch return), alive, TFMin 0 (intraday, not persistent HTF)", tl)
	}
	one, _ := trendlinesBuild(closedOnly(bars, now), time.UnixMilli(now))
	if l1, ok := tlSupport(one); ok && l1.P0T == tl.P0T && l1.P1T == tl.P1T {
		t.Fatalf("the 1m build drew the same line %+v — the fixture does not tell the frames apart", l1)
	}
}

// (b) 08:45 CT (inside 08:30 <= t < 09:00): the 1m build is used.
func TestTrendlineExecTFFirst30mStays1m(t *testing.T) {
	t0 := ctAt(8, 20)
	bars := execTFTape(t0)
	now := bars[len(bars)-1].CloseTime + 1 // 08:48 — the read bar opens 08:47
	if exec2mReads2m(now) {
		t.Fatal("fixture: 08:48 must be on the 1m schedule")
	}
	e := execTFEval(bars, now, true)
	e.Tick(bars, now)
	tl, ok := tlSupport(e.State.ChartTrendlines)
	if !ok {
		t.Fatalf("no support line: %+v", e.State.ChartTrendlines)
	}
	if tl.P0T != t0+5*60_000 { // candle 2's low sits in its 2nd minute on the 1m chart
		t.Fatalf("08:48 line P0T=%d, want the 1m anchor %d", tl.P0T, t0+5*60_000)
	}
}

// (c) knob OFF: the 1m build all day — identical to trendlinesBuild.
func TestTrendlineExecTFKnobOffIs1mAllDay(t *testing.T) {
	t0 := ctAt(10, 0)
	bars := execTFTape(t0)
	now := bars[len(bars)-1].CloseTime + 1
	e := execTFEval(bars, now, false)
	e.Tick(bars, now)
	want, _ := trendlinesBuild(bars, time.UnixMilli(now))
	got := e.State.ChartTrendlines
	if len(got) != len(want) || len(want) == 0 {
		t.Fatalf("knob OFF lines = %+v, want the 1m build %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("knob OFF line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// (d) the 3rd-touch rule and the 5m-close break still hold on a 2m-built line.
func TestTrendlineExecTFThirdTouchAnd5mBreakOn2mLine(t *testing.T) {
	t0 := ctAt(10, 0)
	bars := execTFTape(t0)
	// before the RETURN candle (2m candle 13) has closed: left, not back → forming.
	pre := bars[:26]
	nowPre := pre[len(pre)-1].CloseTime + 1
	lines, _ := trendlinesBuildExec(pre, time.UnixMilli(nowPre), true)
	if tl, ok := tlSupport(lines); !ok || tl.ValidAt != 0 {
		t.Fatalf("before the return = %+v, want a FORMING 2m line (ValidAt 0)", lines)
	}
	// closed 5m candles CLOSING below the line kill it.
	brk := execTFTape(t0,
		[4]float64{100, 100, 94.5, 95}, [4]float64{95, 95.5, 94.5, 95},
		[4]float64{95, 95.5, 94.5, 95}, [4]float64{95, 95.5, 94.5, 95})
	nowB := brk[len(brk)-1].CloseTime + 1
	lines, _ = trendlinesBuildExec(brk, time.UnixMilli(nowB), true)
	tl, ok := tlSupport(lines)
	if !ok || !tl.Dead {
		t.Fatalf("after closed 5m candles through the line = %+v, want Dead", lines)
	}
}
