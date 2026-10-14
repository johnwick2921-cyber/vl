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

// trendlineReturnTape: trendlineBars + a LEAVE and a RETURN. Line value =
// 97 + 0.2·(i−2): bar 10 = 98.6 … bar 13 = 99.2. Bar 10 is neutral (low 99.5,
// off the line); bar 11 is a swing HIGH (102, above the line by 3.2 — price has
// LEFT it); bar 12 confirms it (lower high 101.6) without touching; bar 13
// comes BACK and touches (low 99.1 vs line 99.2) and closes above — the 3rd
// touch, a return [D3.2 p2 @03:45 "nó phải VỀ nó test lần thứ 3"]. The 102
// high sits in the still-forming 09:10 5m bucket, so no 5m trigger line forms;
// bar 13 closes at 101.2, above the 09:05 5m inside bar's range [98.2, 101],
// so the evaluator's 5m-ISB rest box does not swallow the PHL path.
func trendlineReturnTape() ([]market.Kline, func(i int, o, h, l, c float64) market.Kline) {
	bars, mk := trendlineBars()
	return append(bars,
		mk(10, 100, 101, 99.5, 100.5),      // neutral, off the line
		mk(11, 100.5, 102, 100, 101.8),     // swing HIGH @11 (102) — the leave
		mk(12, 101.5, 101.6, 100.2, 100.4), // confirms bar 11; no touch (line 99.0)
		mk(13, 100.4, 101.5, 99.1, 101.2),  // RETURN: touches the line (99.2), closes above
	), mk
}

// trendlineSittingTape: the same two points, but P1's OWN right side sits on
// the line — bar 9 confirms bar 8 (low 98.6 > 98.2) AND touches (line 98.4),
// bar 10 touches again (line 98.6). Price has not left the line: these are
// the swing's confirmation bars, not a return (the 10-13 probe: 55 of 120
// lines went VALID on the bar right after P1). origin/dev calls bar 9 the 3rd
// touch.
func trendlineSittingTape() ([]market.Kline, func(i int, o, h, l, c float64) market.Kline) {
	bars, mk := trendlineBars()
	bars[9] = mk(9, 98.5, 100.5, 98.6, 99.8) // confirms bar 8 AND touches the line
	return append(bars,
		mk(10, 99.8, 101.3, 98.8, 101.2), // touches again (line 98.6) — still sitting
	), mk
}

// trendlineMirror reflects a tape through 100 (p → 200−p): a support tape
// becomes the same resistance tape (rising lows → falling highs).
func trendlineMirror(bars []market.Kline) []market.Kline {
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		out[i] = market.Kline{OpenTime: b.OpenTime, CloseTime: b.CloseTime,
			Open: 200 - b.Open, High: 200 - b.Low, Low: 200 - b.High, Close: 200 - b.Close}
	}
	return out
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
// becomes VALID (a location) only on the 3rd touch, and that touch is a RETURN
// [D3.2 p2 @03:45 "VỀ nó test lần thứ 3"]: bar 11 leaves (a confirmed swing
// high off the line), bar 13 comes back and touches — ValidAt = 13. A
// preservation pin (green on origin/dev too: this tape's only touch is the
// return); the RED pins are the sitting tapes below.
func TestTrendlineThirdTouchValidates(t *testing.T) {
	bars, _ := trendlineReturnTape()
	now := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].ValidAt != 13 || tls[0].Dead {
		t.Fatalf("after the return = %+v, want ValidAt=13 Dead=false", tls)
	}
	// before the return (bar 12 the newest): left, not back yet → not valid.
	now12 := time.UnixMilli(bars[12].OpenTime + 59_999).In(ctime())
	if tls := TrendlinesBuild(bars[:13], now12); len(tls) != 1 || tls[0].ValidAt != 0 {
		t.Fatalf("left but not back yet = %+v, want ValidAt=0", tls)
	}
}

// TestTrendlineSittingOnLineIsNotThirdTouch — THE BUG (CTO probe 10-13, 1380
// real bars: 55 of 120 lines VALID on the bar right after P1, median +2): the
// bars that confirm P1 and still sit on the line are the swing being built,
// not a 3rd test. Bar 9 (P1's confirmation bar) and bar 10 both touch, price
// never left → NOT valid, NOT a location. RED on origin/dev (ValidAt = 9).
// Mutant (the scan restored to "first touch after P1Idx+1") → RED.
func TestTrendlineSittingOnLineIsNotThirdTouch(t *testing.T) {
	bars, _ := trendlineSittingTape()
	now := time.UnixMilli(bars[10].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].P0Idx != 2 || tls[0].P1Idx != 8 {
		t.Fatalf("lines = %+v, want the one support line P0=2 P1=8", tls)
	}
	if tls[0].ValidAt != 0 {
		t.Fatalf("ValidAt = %d — a touch by P1's own confirmation bars (price never left the line) was taken as the 3rd touch", tls[0].ValidAt)
	}
	if got := TrendlineLevels(tls, BoxesBuild(bars, DefaultBoxCfg(), now), bars); len(got) != 0 {
		t.Fatalf("a line price never left is not a location — levels=%+v", got)
	}
}

// TestTrendlineSittingThenReturnValidates — the SAME line as the sitting tape
// becomes valid once price LEAVES (bar 11, a confirmed swing high off the line)
// and comes BACK (bar 13). The sitting touches (9, 10) never count. RED on
// origin/dev (ValidAt = 9). Mirrored for a resistance line (falling highs, the
// leave is a confirmed swing LOW below it).
func TestTrendlineSittingThenReturnValidates(t *testing.T) {
	sit, mk := trendlineSittingTape()
	bars := append(sit,
		mk(11, 101.2, 102, 99.6, 101.8),    // swing HIGH @11 (102) — the leave; no touch (line 98.8)
		mk(12, 101.5, 101.6, 100.2, 100.4), // confirms bar 11; no touch (line 99.0)
		mk(13, 100.4, 101.5, 99.1, 101.2),  // RETURN: touches (line 99.2)
	)
	for _, tc := range []struct {
		name string
		bars []market.Kline
		side Side
	}{
		{"support", bars, SideLong},
		{"resistance (mirror)", trendlineMirror(bars), SideShort},
	} {
		now := time.UnixMilli(tc.bars[13].OpenTime + 59_999).In(ctime())
		tls := TrendlinesBuild(tc.bars, now)
		if len(tls) != 1 || tls[0].Side != tc.side || tls[0].P1Idx != 8 {
			t.Fatalf("%s: lines = %+v, want one %s line with P1=8", tc.name, tls, tc.side)
		}
		if tls[0].ValidAt != 13 || tls[0].Dead {
			t.Fatalf("%s: ValidAt=%d Dead=%v, want ValidAt=13 (the return after the leave), Dead=false", tc.name, tls[0].ValidAt, tls[0].Dead)
		}
		// one bar earlier the leave is not yet confirmed → still not valid.
		now11 := time.UnixMilli(tc.bars[11].OpenTime + 59_999).In(ctime())
		if tls := TrendlinesBuild(tc.bars[:12], now11); len(tls) != 1 || tls[0].ValidAt != 0 {
			t.Fatalf("%s: leave unconfirmed = %+v, want ValidAt=0", tc.name, tls)
		}
	}
}

// TestTrendlineSwingInsideTouchBandIsNotALeave — an opposite-role swing whose
// extreme is still inside the line's own touch band (trendlineTouchBandPts)
// never LEFT the line. Bar 11 is a swing high only 0.4 above the line (band
// 0.5); bars 12–13 keep touching → NOT valid. Mutant (drop the band from the
// leave test) → bar 12 becomes the "3rd touch" → RED.
func TestTrendlineSwingInsideTouchBandIsNotALeave(t *testing.T) {
	bars, mk := trendlineBars()
	bars[9] = mk(9, 98.7, 98.9, 98.6, 98.8) // confirms bar 8; touches (line 98.4)
	bars = append(bars,
		mk(10, 98.8, 98.9, 98.7, 98.8),   // touches (line 98.6)
		mk(11, 98.9, 99.2, 98.8, 99.0),   // swing HIGH @11 (99.2) — 0.4 over the line (98.8): inside the band
		mk(12, 99.0, 99.1, 98.9, 99.0),   // confirms bar 11; touches (line 99.0)
		mk(13, 99.0, 99.15, 98.95, 99.1), // touches (line 99.2)
	)
	now := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].Side != SideLong || tls[0].P1Idx != 8 {
		t.Fatalf("lines = %+v, want the one support line P1=8", tls)
	}
	if tls[0].ValidAt != 0 {
		t.Fatalf("ValidAt = %d — a swing high inside the touch band never left the line", tls[0].ValidAt)
	}
}

// TestTrendlineReturnThen5mCloseThroughKills — X9 @06:25 at the call site: the
// 5m close through still discards the line AFTER the return made it valid. The
// 09:10 bucket (bars 10–14) closes at 98.1, below the line at its open (98.6)
// → Dead, no longer valid, not a location. Green on origin/dev too (the kill
// is unchanged) — a preservation pin.
func TestTrendlineReturnThen5mCloseThroughKills(t *testing.T) {
	bars, mk := trendlineReturnTape()
	bars = append(bars, mk(14, 101.2, 101.3, 98.0, 98.1)) // bucket 10–14 closes 98.1
	// the 09:10 5m bucket closes at bars[14].OpenTime+59_999 — now is after it.
	now := time.UnixMilli(bars[14].OpenTime + 60_000).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || !tls[0].Dead || tls[0].ValidAt != 0 {
		t.Fatalf("after the 5m close through = %+v, want Dead=true ValidAt=0", tls)
	}
	if got := TrendlineLevels(tls, BoxesBuild(bars, DefaultBoxCfg(), now), bars); len(got) != 0 {
		t.Fatalf("a dead line is not a location — levels=%+v", got)
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
	bars, _ := trendlineReturnTape()
	now := bars[13].OpenTime + 59_999

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

	e := trendlineEval(bars, now, tl.key(), tl.priceAt(bars[13].OpenTime))
	e.Tick(bars, now)
	if e.State.Refusals["phl_no_old_extreme_on_side"] == 0 {
		t.Fatalf("valid trendline did not pass the location gate — refusals=%v", e.State.Refusals)
	}
}

// TestEvaluatorTrendlineSittingIsNotALocation — Tick-level pin of the bug: on
// the sitting tape (P1's own confirmation bars touch the line, price never
// left) the trendline is NOT in the level set, so the PHL path never reaches
// its target step — no phl_no_old_extreme_on_side refusal, the same proof
// TestEvaluatorTrendlineLocationPHL reads in reverse. RED on origin/dev (the
// line is VALID at bar 9 there and the refusal fires).
func TestEvaluatorTrendlineSittingIsNotALocation(t *testing.T) {
	bars, _ := trendlineSittingTape()
	now := bars[10].OpenTime + 59_999
	tls := TrendlinesBuild(bars, time.UnixMilli(now).In(ctime()))
	if len(tls) != 1 || tls[0].Side != SideLong {
		t.Fatalf("want the one support line, got %+v", tls)
	}
	e := trendlineEval(bars, now, tls[0].key(), tls[0].priceAt(bars[10].OpenTime))
	e.Tick(bars, now)
	if n := e.State.Refusals["phl_no_old_extreme_on_side"]; n != 0 {
		t.Fatalf("a line price never left reached the PHL path as a location (%d) — refusals=%v", n, e.State.Refusals)
	}
}

// TestTrendlineLevelsOnlyAfterThirdTouch — the "only after the 3rd test"
// half: before the return the line is NOT in the level set (two points, and
// the touches by P1's own confirmation bars, are not a location); after the
// leave and the return it is. Mutant (3rd-touch gate off) makes the first
// half FAIL.
func TestTrendlineLevelsOnlyAfterThirdTouch(t *testing.T) {
	sit, _ := trendlineSittingTape()
	before := time.UnixMilli(sit[10].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(sit, before)
	boxes := BoxesBuild(sit, DefaultBoxCfg(), before)
	if len(tls) != 1 || tls[0].ValidAt != 0 {
		t.Fatalf("want an unvalidated line, got %+v", tls)
	}
	if got := TrendlineLevels(tls, boxes, sit); len(got) != 0 {
		t.Fatalf("before the 3rd touch the line must not be a location — levels=%+v", got)
	}

	after, _ := trendlineReturnTape()
	afterNow := time.UnixMilli(after[13].OpenTime + 59_999).In(ctime())
	tls2 := TrendlinesBuild(after, afterNow)
	boxes2 := BoxesBuild(after, DefaultBoxCfg(), afterNow)
	if len(tls2) != 1 || tls2[0].ValidAt != 13 {
		t.Fatalf("want a validated line, got %+v", tls2)
	}
	if got := TrendlineLevels(tls2, boxes2, after); len(got) != 1 {
		t.Fatalf("after the 3rd touch the line must be a location — levels=%+v", got)
	}
}

// TestEvaluatorTrendlineInsideBoxBoxWins — CTO ruling 23:10:31Z: the band is
// the BOX ITSELF. A trendline whose 3rd touch sits INSIDE a live box's
// [Bottom, Top] is not a trendline location (the box governs). Same return
// tape but bar 8 has a wide green body -> box top 100 -> the bar-13 return at
// 99.2 is inside the box -> no PHL refusal.
func TestEvaluatorTrendlineInsideBoxBoxWins(t *testing.T) {
	bars, mk := trendlineReturnTape()
	bars[8] = mk(8, 100, 101, 98.2, 100.8) // wide body -> FTGL box [97, 100]
	now := bars[13].OpenTime + 59_999

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

	e := trendlineEval(bars, now, tl.key(), tl.priceAt(bars[13].OpenTime))
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

// TestLocationOfNamesTheTouchedPlace — the producer half of the location
// traceability: LocationOf reads the REAL keys the emit sites stamp as
// AnchorKey (eval.go: in.AnchorKey = lvl.Key; box_trade.go: the box key) —
// a trendline built by TrendlinesBuild names its kind AND side, a key level
// and a box name their kind, no anchor names nothing.
func TestLocationOfNamesTheTouchedPlace(t *testing.T) {
	bars, _ := trendlineReturnTape()
	now := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	for _, tc := range []struct {
		bars []market.Kline
		want string
	}{
		{bars, "trendline long"},
		{trendlineMirror(bars), "trendline short"},
	} {
		tls := TrendlinesBuild(tc.bars, now)
		lv := TrendlineLevels(tls, BoxesBuild(tc.bars, DefaultBoxCfg(), now), tc.bars)
		if len(lv) != 1 {
			t.Fatalf("want one trendline location, got %+v", lv)
		}
		if got := LocationOf(lv[0].Key); got != tc.want {
			t.Fatalf("LocationOf(%q) = %q, want %q", lv[0].Key, got, tc.want)
		}
	}
	if got := LocationOf(keyLevelKey(bars[0])); got != "key_level" {
		t.Fatalf("key level = %q, want key_level", got)
	}
	if bx := BoxesBuild(bars, DefaultBoxCfg(), now); len(bx) == 0 || LocationOf(bx[0].Key) != "ftgl" {
		t.Fatalf("box location = %+v, want an ftgl box", bx)
	}
	if got := LocationOf(""); got != "" {
		t.Fatalf("no anchor = %q, want empty", got)
	}
}

// supportLineP0P1 returns the support line joining bar 2 and bar 8 (the
// trendlineBars pair) if TrendlinesBuild reports it, else ok=false.
func supportLineP0P1(t *testing.T, bars []market.Kline, now time.Time) (Trendline, bool) {
	t.Helper()
	for _, tl := range TrendlinesBuild(bars, now) {
		if tl.Side == SideLong && tl.P0T == bars[2].OpenTime && tl.P1T == bars[8].OpenTime {
			return tl, true
		}
	}
	return Trendline{}, false
}

// TestTrendlineLeaveWaitsForTheConfirmingBarToClose — the leave is a CONFIRMED
// swing, and confirmed means its confirming bar has CLOSED. On the return tape
// at a moment INSIDE bar 12 (the bar that will confirm the bar-11 swing high,
// still forming): no leave yet, the line is not valid. One minute later (bar 12
// closed) the leave counts; the next touch (bar 13) validates.
//
// At the TrendlinesBuild call site the forming case cannot differ: every bar
// after a forming bar is also unclosed, so the touch scan finds nothing either
// way. The helper assertion is what pins the guard (CTO mutant M3: the
// closed-bar guard → `if false` returns 11 mid-bar → RED).
func TestTrendlineLeaveWaitsForTheConfirmingBarToClose(t *testing.T) {
	tape, _ := trendlineReturnTape()
	bars := tape[:13] // bars 0..12; bar 12 confirms the bar-11 swing high
	forming := bars[12].OpenTime + 30_000
	tl, ok := supportLineP0P1(t, bars, time.UnixMilli(forming).In(ctime()))
	if !ok || tl.ValidAt != 0 {
		t.Fatalf("mid-bar 12: line = %+v ok=%v, want the P0=2/P1=8 line, not valid", tl, ok)
	}
	if got := trendlineLeaveIdx(bars, swings3(bars), tl, forming); got != -1 {
		t.Fatalf("leave = %d while its confirming bar is still forming — want -1 (no leave yet)", got)
	}

	closed := bars[12].OpenTime + 59_999
	if got := trendlineLeaveIdx(bars, swings3(bars), tl, closed); got != 11 {
		t.Fatalf("leave = %d after bar 12 closed, want 11 (the bar-11 swing high)", got)
	}
	if tl, ok := supportLineP0P1(t, bars, time.UnixMilli(closed).In(ctime())); !ok || tl.ValidAt != 0 {
		t.Fatalf("left, no return yet: line = %+v ok=%v, want not valid", tl, ok)
	}
	if tl, ok := supportLineP0P1(t, tape, time.UnixMilli(tape[13].OpenTime+59_999).In(ctime())); !ok || tl.ValidAt != 13 {
		t.Fatalf("the next touch after the leave: line = %+v ok=%v, want ValidAt=13", tl, ok)
	}
}

// TestTrendlineUnconfirmedFractalIsNotALeave — a 3-bar fractal high above the
// support line that FAILS swingConfirmed (the next bar makes a HIGHER high) is
// not a leave. Bar 11 is a fractal high (102) that bar 12 exceeds (102.5); bar
// 12 swings down to touch the line (low 99.3 vs line 99.0). No confirmed swing
// high exists before that touch → NOT valid. Bar 13 then confirms the bar-12
// swing high (lower high 101.5) and touches → valid at 13. CTO mutant M1 (drop the swingConfirmed
// guard) takes bar 11 as the leave and calls bar 12 the 3rd touch → RED at the
// call site.
func TestTrendlineUnconfirmedFractalIsNotALeave(t *testing.T) {
	base, mk := trendlineBars()
	bars := append(base,
		mk(10, 100, 101, 99.2, 100.5),     // off the line (98.6 + band = 99.1)
		mk(11, 100.5, 102, 100, 101.8),    // fractal HIGH @11 (102)
		mk(12, 101.8, 102.5, 99.3, 100.0), // HIGHER high: bar 11 unconfirmed; touches the line (99.0); low not under bar 10's, so bar 12 is a swing HIGH, not a swing low
	)
	now12 := time.UnixMilli(bars[12].OpenTime + 59_999).In(ctime())
	tl, ok := supportLineP0P1(t, bars, now12)
	if !ok {
		t.Fatalf("want the P0=2/P1=8 support line, got %+v", TrendlinesBuild(bars, now12))
	}
	if tl.ValidAt != 0 {
		t.Fatalf("ValidAt = %d — an UNCONFIRMED fractal high was taken as the leave", tl.ValidAt)
	}

	bars = append(bars, mk(13, 100.0, 101.5, 99.1, 101.2)) // confirms bar 12; touches (99.2)
	now13 := time.UnixMilli(bars[13].OpenTime + 59_999).In(ctime())
	if tl, ok := supportLineP0P1(t, bars, now13); !ok || tl.ValidAt != 13 {
		t.Fatalf("after bar 13 confirms the bar-12 swing high: line = %+v ok=%v, want ValidAt=13", tl, ok)
	}
}

// TestTrendlineSameRoleSwingIsNotALeave — "left the line" is an OPPOSITE-role
// swing only. A support line is BUILT from lows — "một đáy thấp và một đáy cao
// hơn" [D2.3 p1 @00:00–00:34] — so a higher swing LOW above it is the next
// candidate POINT of a line, not a departure; price leaves a support line
// upward and turns back at a swing HIGH (the box re-read's "separated by a
// real swing of the other role", CTO-REREAD-1004 DAY-3 row 12). Here bar 12 is
// a confirmed swing low at 99.8, above the line (99.0) by more than the band,
// and no swing high forms → trendlineLeaveIdx = -1. Mirrored for resistance.
//
// At the TrendlinesBuild call site this cannot differ (CTO mutant M2 is
// equivalent there): a confirmed same-role swing after P1 re-pairs the line
// under the newest-pair rule (here 4 bars after P1 → no support line at all),
// so the P0/P1 line is never reported again for the leave to validate. The
// call-site half asserts only that no P0/P1 line goes valid on the later touch.
// The helper half pins the rule: M2 → returns 12 → RED.
func TestTrendlineSameRoleSwingIsNotALeave(t *testing.T) {
	base, mk := trendlineBars()
	bars := append(base,
		mk(10, 100, 101, 100, 100.5),
		mk(11, 100.5, 101, 100.3, 100.8),
		mk(12, 100.6, 100.9, 99.8, 100.7), // swing LOW @12 (99.8) — above the line (99.0) + band
		mk(13, 100.7, 101, 100.1, 100.6),  // confirms bar 12 (higher low); no swing high anywhere
		mk(14, 100.6, 100.8, 99.3, 100.5), // touches the P0/P1 line (99.4)
	)
	now := bars[14].OpenTime + 59_999
	tl := Trendline{Side: SideLong, P0Idx: 2, P0Px: 97, P0T: bars[2].OpenTime, P1Idx: 8, P1Px: 98.2, P1T: bars[8].OpenTime, FormedAt: 8}
	for _, tc := range []struct {
		name string
		bars []market.Kline
		tl   Trendline
	}{
		{"support", bars, tl},
		{"resistance (mirror)", trendlineMirror(bars), Trendline{Side: SideShort, P0Idx: 2, P0Px: 103, P0T: tl.P0T, P1Idx: 8, P1Px: 101.8, P1T: tl.P1T, FormedAt: 8}},
	} {
		if got := trendlineLeaveIdx(tc.bars, swings3(tc.bars), tc.tl, now); got != -1 {
			t.Fatalf("%s: leave = %d — a same-role swing beyond the line was taken as leaving it", tc.name, got)
		}
	}
	if tl, ok := supportLineP0P1(t, bars, time.UnixMilli(now).In(ctime())); ok && tl.ValidAt != 0 {
		t.Fatalf("the P0/P1 line went valid off a same-role swing: %+v", tl)
	}
}
