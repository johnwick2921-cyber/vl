package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

func trendlineMk(t0 int64) func(i int, o, h, l, c float64) market.Kline {
	return func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
}

// trendlineBars: two STRUCTURAL swing lows — bar 2 (97) and bar 8 (98.2), both
// two-sided confirmed, 6 bars apart. Bar 9 confirms bar 8 WITHOUT touching the
// line (line at bar 9 = 98.4, bar 9 low 99.0), so the line EXISTS but is not
// yet valid. Bar 8's tight red body keeps the FTGL box top at 98.3 so a later
// touch above 98.3 is OUTSIDE the box. Highs are capped at 101 so no 5m trigger
// line forms.
func trendlineBars() ([]market.Kline, func(i int, o, h, l, c float64) market.Kline) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := trendlineMk(t0)
	return []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 97, 100.4), // swing low @2 (97)
		mk(3, 100, 101, 98, 100.6), // confirms bar 2
		mk(4, 100, 101, 98.5, 100.8),
		mk(5, 100, 101, 98, 100.9),
		mk(6, 100, 101, 98.6, 100.5),
		mk(7, 100, 101, 98.9, 100.7),
		mk(8, 98.6, 101, 98.2, 98.3), // swing low @8 (98.2) — P1, tight body
		mk(9, 99.5, 101, 99.0, 99.7), // confirms bar 8; does NOT touch the line
	}, mk
}

// TestTrendlineBuildRisingLows — D2.3 + X9 slide 27 + CTO ruling 23:10:31Z: the
// line joins two CONFIRMED two-sided structural lows with the second HIGHER,
// at least 5 bars apart. It EXISTS at 2 points (ValidAt == 0 before the 3rd
// touch).
func TestTrendlineBuildRisingLows(t *testing.T) {
	bars, _ := trendlineBars()
	now := time.UnixMilli(bars[9].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 {
		t.Fatalf("trendlines = %+v, want exactly 1 support line", tls)
	}
	tl := tls[0]
	if tl.Side != SideLong || tl.P0Idx != 2 || tl.P1Idx != 8 || tl.P0Px != 97 || tl.P1Px != 98.2 {
		t.Fatalf("support line = %+v, want P0=(2,97) P1=(8,98.2)", tl)
	}
	if tl.ValidAt != 0 || tl.Dead {
		t.Fatalf("line valid/dead = %d/%v, want 0/false before the 3rd touch", tl.ValidAt, tl.Dead)
	}
}

// TestTrendlineThirdTouchValidates — DAY-3 row 27 [p2 @03:45–04:07]: the line
// becomes VALID (a location) only after the 3rd touch. Bar 10 touches the line
// (line at bar 10 = 98.6, low 98.4) — ValidAt = 10.
func TestTrendlineThirdTouchValidates(t *testing.T) {
	bars, mk := trendlineBars()
	bars = append(bars, mk(10, 100, 101.5, 98.4, 101.2))
	now := time.UnixMilli(bars[10].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].ValidAt != 10 || tls[0].Dead {
		t.Fatalf("after the 3rd touch = %+v, want ValidAt=10 Dead=false", tls)
	}
}

// TestTrendlineRefusesHorizontal — D2.3 p1 @06:44–07:18: a flat line is never
// a trendline. Two equal structural lows -> no line.
func TestTrendlineRefusesHorizontal(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := trendlineMk(t0)
	bars := []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 97, 100.4), // swing low @2 (97)
		mk(3, 100, 101, 98, 100.6), // confirms bar 2
		mk(4, 100, 101, 98.5, 100.8),
		mk(5, 100, 101, 98, 100.9),
		mk(6, 100, 101, 98.6, 100.5),
		mk(7, 100, 101, 98.9, 100.7),
		mk(8, 100, 101, 97, 100.8), // swing low @8 (97) — equal to bar 2
		mk(9, 100, 101, 98, 100.9), // confirms bar 8
	}
	now := time.UnixMilli(bars[9].OpenTime + 59_999).In(ctime())
	if tls := TrendlinesBuild(bars, now); len(tls) != 0 {
		t.Fatalf("horizontal line = %+v, want none (never horizontal)", tls)
	}
}

// TestTrendlineFractalNoiseDrawsNoLine — CTO ruling 23:10:31Z: a raw 3-bar
// fractal is NOT a structural point. Bar 8 is a higher low fractal but is the
// LAST bar (unconfirmed), so the only structural low is bar 2 — one point, no
// line. Mutant (raw fractals instead of structural) draws a line -> RED.
func TestTrendlineFractalNoiseDrawsNoLine(t *testing.T) {
	bars, _ := trendlineBars()
	bars = bars[:9] // drop bar 9 — bar 8 is now the last (unconfirmed) bar
	now := time.UnixMilli(bars[8].OpenTime + 59_999).In(ctime())
	if tls := TrendlinesBuild(bars, now); len(tls) != 0 {
		t.Fatalf("raw unconfirmed fractal drew a line = %+v, want none", tls)
	}
}

// TestTrendlineBrokenBy5mClose — X9 @06:25: a 5m candle CLOSING through the
// line discards it. Direct struct test: support broken by a close below the
// line at the bucket's open time.
func TestTrendlineBrokenBy5mClose(t *testing.T) {
	tl := Trendline{Side: SideLong, P0Idx: 2, P0Px: 97, P0T: 2 * 60_000, P1Idx: 8, P1Px: 98.2, P1T: 8 * 60_000}
	if got := tl.priceAt(10 * 60_000); math.Abs(got-98.6) > 0.0001 {
		t.Fatalf("priceAt = %.4f, want 98.6", got)
	}
	if tl.brokenBy5m(market.Kline{OpenTime: 10 * 60_000, Close: 98.7}) {
		t.Fatal("close ABOVE the support line is not a break")
	}
	if !tl.brokenBy5m(market.Kline{OpenTime: 10 * 60_000, Close: 98.5}) {
		t.Fatal("close BELOW the support line must break it")
	}
}

// TestTrendlineNeverTarget — the CTO's rule: a trendline is a LOCATION only,
// never a target. nextLevelBeyond must skip it.
func TestTrendlineNeverTarget(t *testing.T) {
	levels := []Level{
		{Key: "trendline:long:2:8", Kind: KindTrendline, Price: 98.6},
		{Key: "old-high:110", Kind: KindOldExtreme, Price: 110},
	}
	if got := nextLevelBeyond(levels, 100, SideLong); got != 110 {
		t.Fatalf("target = %.1f, want 110 (the trendline is not a target)", got)
	}
}

// trendlineEval presets an evaluator for the trendline tape, with a reject
// touch pre-classified at the trendline's key (the trendline itself is built
// from the tape by TrendlinesBuild inside Tick).
func trendlineEval(bars []market.Kline, now int64, key string, priceAt float64) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.RoomMultiple = 0.05
	cfg.PHLMinCandlesFromExtreme = 0
	cfg.PHLTargetShyPts = 0
	cfg.LocTriggerFilter = false
	cfg.NearBoxRoomMultiple = 0
	cfg.PingPongCandleMaxPts = 0
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{{Key: "old-high:110", Kind: KindOldExtreme, Price: 110}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Touches = map[string]Touch{
		key: {LevelKey: key, Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: bars[len(bars)-1], PriceAtTouch: priceAt},
	}
	return e
}

// TestEvaluatorTrendlineLocationPHL — Tick-level pin: after the 3rd touch the
// trendline enters the level set as a LOCATION (outside the box), so the PHL
// path runs and refuses with phl_no_old_extreme_on_side (no target-side old
// extreme on this tape) — the refusal PROVES the location gate passed, because
// a non-location level is skipped silently before that step.
func TestEvaluatorTrendlineLocationPHL(t *testing.T) {
	bars, mk := trendlineBars()
	bars = append(bars, mk(10, 100, 101.5, 98.4, 101.2))
	now := bars[10].OpenTime + 59_999

	tls := TrendlinesBuild(bars, time.UnixMilli(now).In(ctime()))
	var tl Trendline
	for _, x := range tls {
		if x.Side == SideLong && x.ValidAt != 0 {
			tl = x
		}
	}
	if tl.ValidAt == 0 {
		t.Fatalf("no valid support trendline in %+v", tls)
	}

	e := trendlineEval(bars, now, tl.key(), tl.priceAt(bars[10].OpenTime))
	e.Tick(bars, now)
	if e.State.Refusals["phl_no_old_extreme_on_side"] == 0 {
		t.Fatalf("valid trendline did not pass the location gate — refusals=%v", e.State.Refusals)
	}
}

// TestTrendlineLevelsOnlyAfterThirdTouch — the "only after the 3rd test"
// half: before the 3rd touch the line is NOT in the level set (the 2nd touch
// is not a location); after the 3rd touch it is. Mutant (3rd-touch gate off)
// makes the first half FAIL.
func TestTrendlineLevelsOnlyAfterThirdTouch(t *testing.T) {
	bars, mk := trendlineBars()
	before := time.UnixMilli(bars[9].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, before)
	boxes := BoxesBuild(bars, DefaultBoxCfg(), before)
	if len(tls) != 1 || tls[0].ValidAt != 0 {
		t.Fatalf("want an unvalidated line, got %+v", tls)
	}
	if got := TrendlineLevels(tls, boxes, bars); len(got) != 0 {
		t.Fatalf("before the 3rd touch the line must not be a location — levels=%+v", got)
	}

	after := append(bars, mk(10, 100, 101.5, 98.4, 101.2))
	afterNow := time.UnixMilli(after[10].OpenTime + 59_999).In(ctime())
	tls2 := TrendlinesBuild(after, afterNow)
	boxes2 := BoxesBuild(after, DefaultBoxCfg(), afterNow)
	if len(tls2) != 1 || tls2[0].ValidAt != 10 {
		t.Fatalf("want a validated line, got %+v", tls2)
	}
	if got := TrendlineLevels(tls2, boxes2, after); len(got) != 1 {
		t.Fatalf("after the 3rd touch the line must be a location — levels=%+v", got)
	}
}

// TestEvaluatorTrendlineInsideBoxBoxWins — CTO ruling 23:10:31Z: the band is
// the BOX ITSELF. A trendline whose 3rd touch sits INSIDE a live box's
// [Bottom, Top] is not a trendline location (the box governs). Same tape but
// bar 8 has a wide green body -> box top 100 -> the bar-10 touch at 98.6 is
// inside the box -> no PHL refusal.
func TestEvaluatorTrendlineInsideBoxBoxWins(t *testing.T) {
	bars, mk := trendlineBars()
	bars[8] = mk(8, 100, 101, 98.2, 100.8) // wide body -> FTGL box [97, 100]
	bars = append(bars, mk(10, 100, 101.5, 98.4, 101.2))
	now := bars[10].OpenTime + 59_999

	tls := TrendlinesBuild(bars, time.UnixMilli(now).In(ctime()))
	var tl Trendline
	for _, x := range tls {
		if x.Side == SideLong && x.ValidAt != 0 {
			tl = x
		}
	}
	if tl.ValidAt == 0 {
		t.Fatalf("no valid support trendline in %+v", tls)
	}

	e := trendlineEval(bars, now, tl.key(), tl.priceAt(bars[10].OpenTime))
	e.Tick(bars, now)
	if e.State.Refusals["phl_no_old_extreme_on_side"] != 0 {
		t.Fatalf("trendline inside the box must not be a location — refusals=%v", e.State.Refusals)
	}
}

// TestTrendlineMinBarSpacing — CTO ruling 23:10:31Z: the two points must be at
// least 5 bars apart. Two CONFIRMED structural lows 3 bars apart draw NO line;
// the same pair 5 bars apart draws one. Mutant (the < 5 guard becomes < 0)
// makes the 3-bars-apart case draw a line -> RED.
func TestTrendlineMinBarSpacing(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := trendlineMk(t0)

	// 3 bars apart: swing lows at bar 2 (97) and bar 5 (97.5), both confirmed.
	three := []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 97, 100.4), // swing low @2 (97)
		mk(3, 100, 101, 98, 100.6), // confirms bar 2
		mk(4, 100, 101, 98.2, 100.8),
		mk(5, 100, 101, 97.5, 100.9), // swing low @5 (97.5) — 3 bars after bar 2
		mk(6, 100, 101, 98, 100.9),   // confirms bar 5
	}
	now3 := time.UnixMilli(three[6].OpenTime + 59_999).In(ctime())
	if tls := TrendlinesBuild(three, now3); len(tls) != 0 {
		t.Fatalf("3-bars-apart lows drew a line = %+v, want none (>= 5 bars apart)", tls)
	}

	// 5 bars apart: swing lows at bar 2 (97) and bar 7 (97.5), both confirmed.
	five := []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 97, 100.4), // swing low @2 (97)
		mk(3, 100, 101, 98, 100.6), // confirms bar 2
		mk(4, 100, 101, 98.2, 100.8),
		mk(5, 100, 101, 98.3, 100.9),
		mk(6, 100, 101, 98.4, 100.9),
		mk(7, 100, 101, 97.5, 100.9), // swing low @7 (97.5) — 5 bars after bar 2
		mk(8, 100, 101, 98, 100.9),   // confirms bar 7
	}
	now5 := time.UnixMilli(five[8].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(five, now5)
	if len(tls) != 1 || tls[0].P0Idx != 2 || tls[0].P1Idx != 7 {
		t.Fatalf("5-bars-apart lows = %+v, want one line P0=(2,97) P1=(7,97.5)", tls)
	}
}
