package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// m428 — owner rulings of 2026-10-25 on trendlines (R1 HTF break = 5m wholly
// beyond, R2 flat pair = support/resistance, R3 a broken line is deleted, R5 the
// tight non-adjacent cap). Fixture for the Friday pins: the real 2026-10-23 MNQ
// 1m tape (testdata/m423_mnq_1m_20261023.csv, helpers in m423_trendline_test.go).

// withFlatNear switches the m428 R2 "near" distance ON (scale 1) for one test;
// the package's TestMain turns it off so the older synthetic tapes keep their
// premise (see flatNearScale).
func withFlatNear(t *testing.T) {
	t.Helper()
	old := flatNearScale
	flatNearScale = 1
	t.Cleanup(func() { flatNearScale = old })
}

func m428Eval() *Evaluator {
	cfg := DefaultConfig()
	cfg.Exec2mAfter30m = false // the synthetic tapes are 1m
	e := New(cfg)
	e.Cfg.Enabled = true
	return e
}

// TestM428R1HTFLineBreaksOnA5mCandleWhollyBeyond — owner Q3: a 1H trendline
// breaks the SAME way as the intraday line, on a closed 5m candle wholly beyond
// it, not on the 1H close. The tape's 1h support line (97 -> 97.5, ~100 at hour
// 20) is cut by ONE 5m candle (09:..+20h10m) wholly below it, inside an hour
// whose 1h CLOSE stays at 110. Production path: Evaluator.htFTrendlines.
// Mutant (restore the HTF-close break inside brokenBy5m) -> the line survives -> RED.
func TestM428R1HTFLineBreaksOnA5mCandleWhollyBeyond(t *testing.T) {
	bars, _ := htfPersistenceTape()
	for i := 1210; i < 1215; i++ {
		bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close = 96, 96, 95, 95.5
	}
	e := &Evaluator{}
	live, _ := e.htFTrendlines(bars[:1210], time.UnixMilli(BarCloseInstant(bars[1209])))
	n1h := 0
	for _, tl := range live {
		if tl.TFMin == 60 {
			n1h++
		}
	}
	if n1h == 0 {
		t.Fatal("control: the 1h support line must stand before the 5m candle")
	}
	live, broken := e.htFTrendlines(bars[:1216], time.UnixMilli(BarCloseInstant(bars[1215])))
	for _, tl := range live {
		if tl.TFMin == 60 {
			t.Fatalf("1h line %+v survived a closed 5m candle wholly below it", tl)
		}
	}
	got := 0
	for _, tl := range broken {
		if tl.TFMin == 60 {
			got++
		}
	}
	if got == 0 {
		t.Fatal("the 1h line was not reported broken by the 5m candle")
	}
	// A 5m candle that CLOSES below the line but keeps a wick back on it is NOT a
	// break, for the HTF line either (same rule as the intraday one).
	bars2, _ := htfPersistenceTape()
	for i := 1210; i < 1215; i++ {
		bars2[i].Open, bars2[i].High, bars2[i].Low, bars2[i].Close = 96, 100.5, 95, 95.5
	}
	e2 := &Evaluator{}
	e2.htFTrendlines(bars2[:1210], time.UnixMilli(BarCloseInstant(bars2[1209])))
	live, _ = e2.htFTrendlines(bars2[:1216], time.UnixMilli(BarCloseInstant(bars2[1215])))
	n1h = 0
	for _, tl := range live {
		if tl.TFMin == 60 {
			n1h++
		}
	}
	if n1h == 0 {
		t.Fatal("a 5m candle with a wick back above the line must not break the 1h line")
	}
}

// m428Tape is trendlineBars with the rising-lows gap and the candle size
// parameterised: bars from 3 on are lifted by gap-1.2 (so the two swing lows are
// `gap` apart) and every high is raised by pad (taller candles = a faster day).
func m428Tape(gap, pad float64) []market.Kline {
	bars, _ := trendlineBars()
	d := gap - 1.2
	for i := range bars {
		if i >= 3 {
			bars[i].Open += d
			bars[i].Close += d
			bars[i].Low += d
			bars[i].High += d
		}
		bars[i].High += pad
	}
	return bars
}

func m428Tick(bars []market.Kline) *Evaluator {
	e := m428Eval()
	now := bars[len(bars)-1].OpenTime + 60_000
	e.Tick(bars, now)
	return e
}

func m428FlatLevels(e *Evaluator) (out []Level) {
	for _, l := range e.State.ChartLevels {
		if l.Kind == KindOldExtreme && strings.Contains(l.Key, "flat_") {
			out = append(out, l)
		}
	}
	return out
}

// TestM428R2NearDependsOnTheDay — owner Q2: two same-role wicks at (nearly) the
// same price are a horizontal support, never a trendline; "near" is half the
// average high-low range of the last 20 traded-frame candles. The SAME 6-pt gap
// between two rising lows is a trendline on the quiet tape (candles ~2.5 tall,
// near ~1.3) and a flat SUPPORT on the run-day tape (candles ~18 tall, near ~9).
// Production path: Evaluator.Tick (trendline set + level set).
// Mutant (drop the near test in newSlopedLine) -> the run-day tape draws a line -> RED.
func TestM428R2NearDependsOnTheDay(t *testing.T) {
	withFlatNear(t)
	quiet := m428Tick(m428Tape(6, 0))
	if len(quiet.State.ChartTrendlines) != 1 {
		t.Fatalf("quiet day: want the 6-pt rising-low line drawn, got %+v", quiet.State.ChartTrendlines)
	}
	if fl := m428FlatLevels(quiet); len(fl) != 0 {
		t.Fatalf("quiet day: a 6-pt gap is not flat, got flat levels %+v", fl)
	}
	run := m428Tick(m428Tape(6, 16))
	if len(run.State.ChartTrendlines) != 0 {
		t.Fatalf("run day: a 6-pt gap is NEAR (half the ~18-pt candle) — it is a support, not a trendline; got %+v", run.State.ChartTrendlines)
	}
	fl := m428FlatLevels(run)
	if len(fl) != 1 || fl[0].Role != OldLow || !strings.Contains(fl[0].Key, flatSupportTag) {
		t.Fatalf("run day: want one flat SUPPORT (old-low role), got %+v", fl)
	}
	if fl[0].Price != 97+6 {
		t.Fatalf("flat support sits at the later low (103), got %.2f", fl[0].Price)
	}
}

// TestM428R2FlatLevelTradesLikeAnOldLow — the flat support is an old LOW for
// every trading rule (Kind old_extreme, Role low): role fit, the old-extreme
// index, and the same break rule — a later closed 1m candle trading below it
// removes it. Mutant (never test the break) -> the traded-through level stays -> RED.
func TestM428R2FlatLevelTradesLikeAnOldLow(t *testing.T) {
	withFlatNear(t)
	bars := m428Tape(6, 16)
	e := m428Tick(bars)
	fl := m428FlatLevels(e)
	if len(fl) != 1 {
		t.Fatalf("want one flat support, got %+v", fl)
	}
	l := fl[0]
	if !oldExtremeRoleFits(l, SideShort) || oldExtremeRoleFits(l, SideLong) {
		t.Fatal("a flat support is an old LOW: it serves a short's target, never a long's")
	}
	if got := oldExtremeIndexes([]Level{l}, bars); len(got) != 1 || got[0].isHigh {
		t.Fatalf("oldExtremeIndexes = %+v, want the flat support as one low", got)
	}
	// price trades through it afterwards: a closed 1m candle with a low under 103
	mk := trendlineMk(bars[0].OpenTime)
	bars = append(bars, mk(10, 106, 107, 104, 105), mk(11, 105, 106, 100, 101), mk(12, 101, 104, 100.5, 103.5))
	e2 := m428Tick(bars)
	for _, f := range m428FlatLevels(e2) {
		if f.Price == l.Price {
			t.Fatalf("a flat support traded through must be gone, still have %+v", f)
		}
	}
}

// TestM428R3BrokenLineIsDeleted — owner: "trendline gãy -> xoá". The valid
// support (return tape) is drawn and a location while unbroken; once a closed 5m
// candle sits wholly below it, it is NOT in the chart set (ChartTrendlines) and
// NOT a level. Production path: Evaluator.Tick. Mutant (skip dropDeadTrendlines)
// -> the dead line is still drawn / still in the set -> RED.
func TestM428R3BrokenLineIsDeleted(t *testing.T) {
	bars, mk := trendlineReturnTape()
	ctl := m428Tick(bars)
	var live int
	for _, tl := range ctl.State.ChartTrendlines {
		if tl.P1Idx == 8 && tl.ValidAt > 0 && !tl.Dead {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("control: the P1=8 support should be valid and unbroken, lines %+v", ctl.State.ChartTrendlines)
	}
	bars = append(bars, mk(14, 100.5, 101, 100, 100.4))
	for i := 15; i < 20; i++ { // the 09:15 5m bucket, wholly under the line (~99.6-100.4)
		bars = append(bars, mk(i, 97, 97.5, 96, 96.5))
	}
	bars = append(bars, mk(20, 100.4, 101, 100, 100.5))
	e := m428Tick(bars)
	for _, tl := range e.State.ChartTrendlines {
		if tl.P1Idx == 8 {
			t.Fatalf("the broken support is still drawn: %+v", tl)
		}
	}
	for _, l := range e.State.ChartLevels {
		if l.Kind == KindTrendline && strings.Contains(l.Key, "long") {
			t.Fatalf("the broken support is still a level: %+v", l)
		}
	}
}

// TestM428FridayFanCapAndFlatLevels — R5 + R2 + R3 on the real 10-23 tape,
// through Evaluator.Tick every 10 minutes from the 09:00 switch to the close:
//   - per side never more than 3 drawn intraday trendlines (valid + forming),
//     and no dead line is ever drawn (R3);
//   - at 13:20 CT the day's flat pairs are offered as support / resistance levels
//     (flat_support 29171.25, flat_resistance 29216.25) and none of them is a
//     trendline (R2).
//
// Mutants: lift the non-adjacent one-at-a-time gate (naFree/chain/standing) -> the
// per-side count exceeds 3 -> RED; skip dropDeadTrendlines -> a dead line is drawn -> RED.
func TestM428FridayFanCapAndFlatLevels(t *testing.T) {
	withFlatNear(t)
	all := m423Bars(t)
	maxSide := map[Side]int{}
	for h := 9; h <= 15; h++ {
		for m := 0; m < 60; m += 10 {
			if h == 15 && m > 50 {
				continue
			}
			bars, now := m423Upto(t, all, h, m)
			e := New(DefaultConfig())
			e.Cfg.Enabled = true
			e.Tick(bars, now)
			n := map[Side]int{}
			for _, tl := range e.State.ChartTrendlines {
				if tl.Dead {
					t.Fatalf("%02d:%02d: a dead (broken) line is still drawn: %+v", h, m, tl)
				}
				if tl.TFMin == 0 {
					n[tl.Side]++
				}
			}
			for sd, c := range n {
				if c > maxSide[sd] {
					maxSide[sd] = c
				}
				if c > 3 {
					t.Fatalf("%02d:%02d: %d concurrent %s intraday lines drawn, cap is 3 per side", h, m, c, sd)
				}
			}
		}
	}
	t.Logf("max concurrent intraday lines per side: %v", maxSide)
	bars, now := m423Upto(t, all, 13, 20)
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	e.Tick(bars, now)
	want := map[string]bool{"flat_support:29171.25": false, "flat_resistance:29216.25": false}
	for _, l := range m428FlatLevels(e) {
		for k := range want {
			if strings.HasSuffix(l.Key, k) {
				want[k] = true
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Fatalf("13:20 CT: %s missing from the level set (flat levels: %+v)", k, m428FlatLevels(e))
		}
	}
	// the flat pair 29216.25 / 29224.75-ish highs are no trendline: no drawn line joins two
	// highs within the near distance of each other.
	for _, tl := range e.State.ChartTrendlines {
		if tl.TFMin == 0 && (tl.P1Px-tl.P0Px < 1 && tl.P0Px-tl.P1Px < 1) {
			t.Fatalf("a flat line is drawn as a trendline: %+v", tl)
		}
	}
}

// m428ConcaveLows is a staircase of rising swing lows whose increments shrink
// (a concave curve), so EVERY earlier low joins every later one in an uncrossed
// support line — the worst case for a fan. Swing k sits at bar 6k+2.
func m428ConcaveLows(n int) []market.Kline {
	_, mk := trendlineBars()
	var out []market.Kline
	low := func(k int) float64 { return 100 + 20*(1-1/float64(k+1)) }
	for k := 0; k < n; k++ {
		for j := 0; j < 6; j++ {
			i := 6*k + j
			l := low(k+1) + 3
			if j == 2 {
				l = low(k)
			}
			out = append(out, mk(i, l+1, l+2, l, l+1))
		}
	}
	// two closing bars above the last swing so it is confirmed
	for j := 0; j < 3; j++ {
		i := 6*n + j
		l := low(n) + 3
		out = append(out, mk(i, l+1, l+2, l, l+1))
	}
	return out
}

// TestM428NonAdjacentCapIsOnePerSide — R5: however many earlier lows could join
// the newest one, at most ONE non-adjacent (a line that skips a swing between its
// anchors) support line is drawn at a time. Production path: TrendlinesBuild.
// Mutant (lift the one-at-a-time gate) -> every swing adds its own fan line -> RED.
func TestM428NonAdjacentCapIsOnePerSide(t *testing.T) {
	bars := m428ConcaveLows(9)
	now := time.UnixMilli(bars[len(bars)-1].OpenTime + 59_999).In(ctime())
	lines := TrendlinesBuild(bars, now)
	anchors := map[int64]bool{}
	for _, tl := range lines {
		anchors[tl.P0T], anchors[tl.P1T] = true, true
	}
	if len(anchors) < 8 {
		t.Fatalf("fixture: want >= 8 swing anchors, got %d (%d lines)", len(anchors), len(lines))
	}
	na := 0
	for _, tl := range lines {
		if tl.Dead {
			continue
		}
		for a := range anchors {
			if a > tl.P0T && a < tl.P1T {
				na++
				break
			}
		}
	}
	if na == 0 {
		t.Fatal("fixture: the concave tape must offer at least one non-adjacent line")
	}
	if na > 1 {
		t.Fatalf("%d non-adjacent support lines drawn at once, cap is ONE per side", na)
	}
}
