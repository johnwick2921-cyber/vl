package pep

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// eDay is a style-E fixture: a flat 25000 tape from 06:00, the two-minute box
// 08:30+08:31 spanning 24990..25020 (size 30), then the tape jumps to 25100 from
// 08:32 so the EMA9 on 2m candles climbs 25000 -> 25020 -> 25036.
func eDay(t testing.TB, date string, over map[string]ohlc, skip ...string) []market.Kline {
	o := map[string]ohlc{
		"08:30": {25000, 25020, 24990, 25000},
		"08:31": {25000, 25015, 24992, 25000},
	}
	for m := ctms(t, date, "08:32"); m < ctms(t, date, "15:10"); m += minuteMs {
		loc, _ := CTLocation()
		o[time.UnixMilli(m).In(loc).Format("15:04")] = flat(25100)
	}
	for k, v := range over {
		o[k] = v
	}
	return session(t, date, "06:00", "15:10", 25000, o, skip...)
}

// STYLE E — the EMA9 on 2m candles crossing the box HIGH from <= to >.
// 2m candles are CT-even: 08:32-08:34 closes 25100 -> EMA 25020 (== high, NOT
// above); 08:34-08:36 -> EMA 25036 (> high): the signal is the candle that
// closes at 08:36, entered at that candle's close.
func TestE_EMACrossUp(t *testing.T) {
	bars := eDay(t, dayCST, nil)
	// 08:34 candle close: EMA == box high, not a cross
	r := Evaluate(bars, nowAfter(t, dayCST, "08:33"), cfgE(), State{})
	wantRefusal(t, r, CodeNoCrossYet, SevWait)
	near(t, "ema at the 08:34 candle", r.Diag.LastEMA, 25020)
	r = Evaluate(bars, nowAfter(t, dayCST, "08:35"), cfgE(), State{})
	in := mustOne(t, r)
	if in.Side != SideLong || in.Style != StyleE {
		t.Fatalf("intent %+v", in)
	}
	near(t, "ema at the 08:36 candle", r.Diag.LastEMA, 25036)
	near(t, "entry = the candle close", in.Entry, 25100)
	near(t, "box (2m ORB)", in.BoxSize, 30)
	near(t, "sl = half box 15", in.StopDist, 15)
	near(t, "stop", in.Stop, 25085)
	near(t, "tp = full box", in.TargetDist, 30)
	near(t, "target", in.Target, 25130)
	if in.SignalClose != ctms(t, dayCST, "08:36")-1 {
		t.Fatalf("signal close %d", in.SignalClose)
	}
}

func TestE_EMACrossDown(t *testing.T) {
	over := map[string]ohlc{}
	for _, hhmm := range []string{"08:32", "08:33", "08:34", "08:35", "08:36", "08:37", "08:38", "08:39"} {
		over[hhmm] = flat(24900)
	}
	bars := eDay(t, dayCST, over)
	// EMA: 25000 -> 24980 (== box low 24990? no: 24980 < 24990 -> already crossed)
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:33"), cfgE(), State{}))
	if in.Side != SideShort {
		t.Fatalf("side %s", in.Side)
	}
	near(t, "entry", in.Entry, 24900)
	near(t, "stop = entry + 15", in.Stop, 24915)
	near(t, "target = entry - 30", in.Target, 24870)
}

// The stop is half the box ROUNDED OUTWARD to a tick: box 36.75 -> 18.375 -> 18.5
// (the PDF's 2025-10-23 style-E stop is 18.5 = 222/12).
func TestE_StopRoundsOutward(t *testing.T) {
	bars := eDay(t, dayCST, map[string]ohlc{"08:30": {25000, 25020, 24983.25, 25000}, "08:31": {25000, 25010, 24990, 25000}})
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:35"), cfgE(), State{}))
	near(t, "box", in.BoxSize, 36.75)
	near(t, "stopDist 36.75/2=18.375 -> 18.5 (outward)", in.StopDist, 18.5)
	near(t, "targetDist", in.TargetDist, 36.75)
}

// OWNER-SELECTABLE: the candle CLOSE crossing the edge instead of the EMA.
// Candle 08:32-08:34 closes 25100 (> high 25020) from a previous close 25000.
func TestE_CloseSourceSignalsEarlier(t *testing.T) {
	bars := eDay(t, dayCST, nil)
	c := cfgE()
	c.E.Source = SourceClose
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:33"), c, State{}))
	if in.Side != SideLong || in.SignalClose != ctms(t, dayCST, "08:34")-1 {
		t.Fatalf("intent %+v", in)
	}
	// the EMA source does NOT signal there
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:33"), cfgE(), State{}), CodeNoCrossYet, SevWait)
}

// The EMA needs warming: with too few candles it fails closed (named), never
// guesses from a seed.
func TestE_NotWarmFailsClosed(t *testing.T) {
	bars := session(t, dayCST, "08:00", "15:10", 25000, map[string]ohlc{
		"08:30": {25000, 25020, 24990, 25000}, "08:31": {25000, 25015, 24992, 25000},
	})
	for m := ctms(t, dayCST, "08:32"); m < ctms(t, dayCST, "15:10"); m += minuteMs {
		loc, _ := CTLocation()
		bars[(m-ctms(t, dayCST, "08:00"))/minuteMs] = kl(m, flat(25100))
		_ = loc
	}
	// 08:00..08:36 = 18 candles < the default 27
	r := Evaluate(bars, nowAfter(t, dayCST, "08:35"), cfgE(), State{})
	wantRefusal(t, r, CodeEMANotWarm, SevBlock)
	// with a smaller owner-set warm-up it signals
	c := cfgE()
	c.E.WarmupBars = 12
	mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:35"), c, State{}))
}

// A missing minute inside the 2m box fails closed (box = 08:30+08:31).
func TestE_MissingBoxMinute(t *testing.T) {
	bars := eDay(t, dayCST, nil, "08:31")
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:35"), cfgE(), State{}), CodeBoxMissingMin, SevBlock)
}

// A candle with a missing 1m bar is not a candle: it is skipped, no cross is
// inferred across the hole, and the diag counts it.
func TestE_PartialCandleSkippedNoCrossAcrossHole(t *testing.T) {
	bars := eDay(t, dayCST, nil, "08:35") // 08:34-08:36 candle is incomplete
	r := Evaluate(bars, nowAfter(t, dayCST, "08:37"), cfgE(), State{})
	if r.Diag.PartialCandles < 1 {
		t.Fatalf("partial candles = %d", r.Diag.PartialCandles)
	}
	// the cross would have been the 08:36 candle; with it gone the next candle
	// (08:36-08:38) has a non-adjacent predecessor -> no inference
	wantRefusal(t, r, CodeNoCrossYet, SevWait)
}

// THE EMA IS CARRIED ACROSS SESSIONS: it equals an independent recurrence over
// EVERY complete candle from the first bar, not one reset at today's open.
func TestE_EMAIsNeverResetDaily(t *testing.T) {
	prev := session(t, "2026-01-09", "08:00", "15:00", 24000, nil)
	today := eDay(t, dayCST, nil)
	// today's tape starts at 06:00 (eDay); keep only 08:00 so little of today precedes the box
	var cut []market.Kline
	for _, b := range today {
		if b.OpenTime >= ctms(t, dayCST, "08:00") {
			cut = append(cut, b)
		}
	}
	bars := join(prev, cut)
	c := cfgE()
	c.E.WarmupBars = 5
	now := nowAfter(t, dayCST, "08:35")
	r := Evaluate(bars, now, c, State{})

	// independent recurrence, hand-rolled over EVERY complete 2m candle of the
	// whole series
	ms := int64(2) * minuteMs
	closes := map[int64]float64{}
	count := map[int64]int{}
	var order []int64
	for _, b := range bars {
		if b.CloseTime >= now {
			break
		}
		bs := (b.OpenTime / ms) * ms
		if count[bs] == 0 {
			order = append(order, bs)
		}
		count[bs]++
		closes[bs] = b.Close
	}
	var ema float64
	first := true
	for _, bs := range order {
		if count[bs] != 2 || bs+ms > now {
			continue
		}
		if first {
			ema, first = closes[bs], false
			continue
		}
		ema += (2.0 / 10.0) * (closes[bs] - ema)
	}
	near(t, "carried-across-sessions EMA", r.Diag.LastEMA, ema)

	// and it is NOT what a daily reset would give (seeded at today's 08:00)
	todayBars := cut
	ema2, first2 := 0.0, true
	for i := 0; i+1 < len(todayBars); i += 2 {
		if todayBars[i].CloseTime+1+minuteMs > now {
			break
		}
		cl := todayBars[i+1].Close
		if first2 {
			ema2, first2 = cl, false
		} else {
			ema2 += 0.2 * (cl - ema2)
		}
	}
	if math.Abs(ema2-ema) < 1 {
		t.Fatalf("fixture is not discriminating: reset %v vs carried %v", ema2, ema)
	}
}

// One trade a day: after the entry is recorded the same signal does not repeat.
func TestE_OneTradeADay(t *testing.T) {
	bars := eDay(t, dayCST, nil)
	now := nowAfter(t, dayCST, "08:35")
	mustOne(t, Evaluate(bars, now, cfgE(), State{}))
	wantRefusal(t, Evaluate(bars, now, cfgE(), State{Day: dayCST, Trades: 1}), CodeMaxTrades, SevWait)
}

// A signal the bot never saw in time is not entered late, and the day does not
// slide onto the NEXT cross: with max 1 trade the first slot is the first cross.
func TestE_StaleSignalBlocks(t *testing.T) {
	bars := eDay(t, dayCST, nil)
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:41"), cfgE(), State{}), CodeStaleSignal, SevBlock)
}

func TestE_BoxStartsAt0830CTAcrossDST(t *testing.T) {
	for _, tc := range []struct{ date, utc string }{{"2026-03-09", "13:30"}, {"2025-11-03", "14:30"}} {
		bars := eDay(t, tc.date, nil)
		r := Evaluate(bars, nowAfter(t, tc.date, "08:35"), cfgE(), State{})
		mustOne(t, r)
		if got := time.UnixMilli(r.Box.Start).UTC().Format("15:04"); got != tc.utc {
			t.Errorf("%s: box starts %s UTC, want %s", tc.date, got, tc.utc)
		}
	}
}

// Per-style blocks: switching style never reads the other style's numbers.
func TestStylesKeepSeparateNumbers(t *testing.T) {
	c := DefaultConfig()
	c.A.SLFraction, c.E.SLFraction = 0.9, 0.4
	c.Style = StyleA
	near(t, "A sl", c.SLFraction(), 0.9)
	c.Style = StyleE
	near(t, "E sl", c.SLFraction(), 0.4)
	c.A.BoxMinutes, c.E.BoxMinutes = 7, 3
	c.Style = StyleA
	if c.BoxMinutes() != 7 {
		t.Fatal("A box minutes")
	}
	c.Style = StyleE
	if c.BoxMinutes() != 3 {
		t.Fatal("E box minutes")
	}
}

// --- boundary pins (review round 2: three evaluator mutants had survived) ---

// STALE-FEED BOUNDARY: the newest closed 1m bar may be exactly SignalMaxAgeMin
// old (lag == limit is fresh); one millisecond more is stale. Kills lag >= maxAge.
func TestEvaluate_StaleFeedBoundaryIsExact(t *testing.T) {
	bars := eDay(t, dayCST, nil)
	var upto []market.Kline // newest bar opens 08:36 -> closes at 08:37:00.000
	for _, b := range bars {
		if b.OpenTime < ctms(t, dayCST, "08:37") {
			upto = append(upto, b)
		}
	}
	c := cfgE()
	edge := ctms(t, dayCST, "08:37") + int64(c.SignalMaxAgeMin)*minuteMs
	for _, ms := range []int64{0, 1} {
		r := Evaluate(upto, edge+ms, c, State{})
		stale := false
		for _, f := range r.Refusals {
			if f.Code == CodeBarsStale {
				stale = true
			}
		}
		if (ms == 0) == stale {
			t.Fatalf("lag = limit + %dms: stale=%v (refusals %+v)", ms, stale, r.Refusals)
		}
	}
}

// WARM-UP BOUNDARY: with WarmupBars=N the first candle allowed to signal is the
// one at 0-based index N (N complete candles precede it). The cross candle here
// (the 08:34-08:36 candle) is index 17 of a tape that starts at 08:00, so N=17
// signals and N=18 is EMA-not-warm. Kills i < warm-1.
func TestE_WarmupBoundaryIsExact(t *testing.T) {
	bars := session(t, dayCST, "08:00", "15:10", 25000, map[string]ohlc{
		"08:30": {25000, 25020, 24990, 25000}, "08:31": {25000, 25015, 24992, 25000},
	})
	for i := range bars {
		if bars[i].OpenTime >= ctms(t, dayCST, "08:32") {
			bars[i] = kl(bars[i].OpenTime, flat(25100))
		}
	}
	now := nowAfter(t, dayCST, "08:35")
	c := cfgE()
	c.E.WarmupBars = 17
	mustOne(t, Evaluate(bars, now, c, State{}))
	c.E.WarmupBars = 18
	wantRefusal(t, Evaluate(bars, now, c, State{}), CodeEMANotWarm, SevBlock)
}

// SHORT-SIDE EQUALITY (spec: "from >= box low to < box low"): a reference value
// EXACTLY on the box low followed by a close below it is a sell. Source `close`
// is used so the previous reference is exactly the low, with no EMA arithmetic.
// Kills pr > box.Low (the long-side mirror is pinned by TestE_EMACrossUp).
func TestE_ShortCrossFromExactlyTheBoxLow(t *testing.T) {
	bars := eDay(t, dayCST, map[string]ohlc{
		"08:32": flat(24990), "08:33": flat(24990), // candle closing 08:34: close == box low 24990
		"08:34": flat(24980), "08:35": flat(24980), // next candle closes 24980 < low
	})
	c := cfgE()
	c.E.Source = SourceClose
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:35"), c, State{}))
	if in.Side != SideShort {
		t.Fatalf("side %s, want short", in.Side)
	}
	near(t, "entry = the signal candle close", in.Entry, 24980)
}
