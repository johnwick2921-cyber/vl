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

func trendlineBars() ([]market.Kline, func(i int, o, h, l, c float64) market.Kline) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := trendlineMk(t0)
	return []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 98, 100.4), // swing low @2 (98)
		mk(3, 100, 101, 98.5, 100.6),
		mk(4, 100, 101, 97, 100.8), // swing low @4 (97) — the extreme
		mk(5, 100, 101, 98, 100.9), // confirms bar 4; highs capped at 101 so no 5m trigger line forms
		mk(6, 101, 101, 98.6, 100.5),
		mk(7, 100, 101, 98.9, 100.7),
		mk(8, 100, 101, 98.2, 100.8), // swing low @8 (98.2) — P1, the higher low
	}, mk
}

// TestTrendlineBuildRisingLows — D2.3 + X9 slide 27: a support trendline is
// two lows with the second HIGHER; it EXISTS at 2 points (ValidAt == 0 — no
// 3rd touch yet). The line is drawn through the lowest confirmed swing low
// and the nearest later higher low.
func TestTrendlineBuildRisingLows(t *testing.T) {
	bars, _ := trendlineBars()
	now := time.UnixMilli(bars[8].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 {
		t.Fatalf("trendlines = %+v, want exactly 1 support line", tls)
	}
	tl := tls[0]
	if tl.Side != SideLong || tl.P0Idx != 4 || tl.P1Idx != 8 || tl.P0Px != 97 || tl.P1Px != 98.2 {
		t.Fatalf("support line = %+v, want P0=(4,97) P1=(8,98.2)", tl)
	}
	if tl.ValidAt != 0 || tl.Dead {
		t.Fatalf("line valid/dead = %d/%v, want 0/false before the 3rd touch", tl.ValidAt, tl.Dead)
	}
}

// TestTrendlineThirdTouchValidates — DAY-3 row 27 [p2 @03:45–04:07]: the line
// becomes VALID (a location) only after the 3rd touch. Bar 9 touches the
// line (low 98.4 reaches 98.5) — ValidAt = 9.
func TestTrendlineThirdTouchValidates(t *testing.T) {
	bars, mk := trendlineBars()
	bars = append(bars, mk(9, 100, 101, 98.4, 99.5))
	now := time.UnixMilli(bars[9].OpenTime + 59_999).In(ctime())
	tls := TrendlinesBuild(bars, now)
	if len(tls) != 1 || tls[0].ValidAt != 9 || tls[0].Dead {
		t.Fatalf("after the 3rd touch = %+v, want ValidAt=9 Dead=false", tls)
	}
}

// TestTrendlineRefusesHorizontal — D2.3 p1 @06:44–07:18: a flat line is
// never a trendline. Equal lows -> no line.
func TestTrendlineRefusesHorizontal(t *testing.T) {
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := trendlineMk(t0)
	bars := []market.Kline{
		mk(0, 100, 101, 100, 100.5),
		mk(1, 100, 101, 99, 100.2),
		mk(2, 100, 101, 98, 100.4), // swing low @2 (98)
		mk(3, 100, 101, 98.5, 100.6),
		mk(4, 100, 101, 98, 100.8), // swing low @4 (98) — equal to bar 2
		mk(5, 100, 101, 98.5, 100.9),
	}
	now := time.UnixMilli(bars[5].OpenTime + 59_999).In(ctime())
	if tls := TrendlinesBuild(bars, now); len(tls) != 0 {
		t.Fatalf("horizontal line = %+v, want none (never horizontal)", tls)
	}
}

// TestTrendlineBrokenBy5mClose — X9 @06:25: a 5m candle CLOSING through the
// line discards it. Direct struct test: support broken by a close below the
// line at the bucket's open time.
func TestTrendlineBrokenBy5mClose(t *testing.T) {
	tl := Trendline{Side: SideLong, P0Idx: 4, P0Px: 97, P0T: 4 * 60_000, P1Idx: 8, P1Px: 98.2, P1T: 8 * 60_000}
	if got := tl.priceAt(9 * 60_000); got != 98.5 {
		t.Fatalf("priceAt = %.2f, want 98.5", got)
	}
	if tl.brokenBy5m(market.Kline{OpenTime: 9 * 60_000, Close: 98.6}) {
		t.Fatal("close ABOVE the support line is not a break")
	}
	if !tl.brokenBy5m(market.Kline{OpenTime: 9 * 60_000, Close: 98.4}) {
		t.Fatal("close BELOW the support line must break it")
	}
}

// TestTrendlineNeverTarget — the CTO's rule: a trendline is a LOCATION only,
// never a target. nextLevelBeyond must skip it.
func TestTrendlineNeverTarget(t *testing.T) {
	levels := []Level{
		{Key: "trendline:long:4:8", Kind: KindTrendline, Price: 98.5},
		{Key: "old-high:110", Kind: KindOldExtreme, Price: 110},
	}
	if got := nextLevelBeyond(levels, 100, SideLong); got != 110 {
		t.Fatalf("target = %.1f, want 110 (the trendline at 98.5 is not a target)", got)
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
// trendline enters the level set as a LOCATION, so the PHL path runs (and, on
// this tape with no target-side old extreme, refuses with
// phl_no_old_extreme_on_side — the refusal PROVES the location gate passed,
// because a non-location level is skipped silently before that step).
func TestEvaluatorTrendlineLocationPHL(t *testing.T) {
	bars, mk := trendlineBars()
	bars = append(bars, mk(9, 100, 101, 98.4, 99.5))
	now := bars[9].OpenTime + 59_999

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

	e := trendlineEval(bars, now, tl.key(), tl.priceAt(bars[9].OpenTime))
	e.Tick(bars, now)
	if e.State.Refusals["phl_no_old_extreme_on_side"] == 0 {
		t.Fatalf("valid trendline did not pass the location gate — refusals=%v", e.State.Refusals)
	}
}

// TestEvaluatorTrendlineNotLocationBeforeThirdTouch — the "only after the 3rd
// test" half: before the 3rd touch the trendline is NOT in the level set, so
// the same pre-set reject touch is silently skipped (no PHL refusal; the tape
// draws no 5m trigger line, so the trigger-retest location cannot pollute).
func TestEvaluatorTrendlineNotLocationBeforeThirdTouch(t *testing.T) {
	bars, _ := trendlineBars() // 9 bars — no 3rd touch yet
	now := bars[8].OpenTime + 59_999
	tls := TrendlinesBuild(bars, time.UnixMilli(now).In(ctime()))
	if len(tls) != 1 || tls[0].ValidAt != 0 {
		t.Fatalf("want an unvalidated line, got %+v", tls)
	}
	e := trendlineEval(bars, now, tls[0].key(), tls[0].priceAt(bars[8].OpenTime))
	e.Tick(bars, now)
	if e.State.Refusals["phl_no_old_extreme_on_side"] != 0 {
		t.Fatalf("unvalidated trendline should be skipped (not a location) — refusals=%v", e.State.Refusals)
	}
}
