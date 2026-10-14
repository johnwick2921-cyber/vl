package mentor

import (
	"fmt"
	"testing"
	"time"

	"vl/market"
)

// ── OLD EXTREMES — CALL-SITE PINS (canon class 53, r22 P1). ─────────────────
// These drive the real evaluator Tick path and assert the TRADING readers (the
// ISB size-3 flag via isbFlagsFor and the ISB target ladder via
// nextLevelBeyondRoom) read the ONE nearest UNBROKEN old high: an ISB at it is
// flagged and targets it; a pivot already traded through is neither (the
// pre-r22 12-pivot list kept it). Mutant (return every pivot from
// swingOldExtremeLevels) → TestISBFlagIgnoresABrokenOldHighCallSite goes RED.

// swingHighTape5m returns 25 closed 1m bars (five 5m buckets) with a swing HIGH
// at the middle bucket (the detector's 2*k+1=5 floor), so SwingPointLevels
// emits exactly one 5m swing high at `swingHigh`.
func swingHighTape5m(swingHigh float64) []market.Kline {
	// Lows are strictly INCREASING so the 5m trigger (seeded long) never flips
	// short; the middle bucket still carries the swing HIGH.
	buckets := []struct{ h, l, c float64 }{
		{swingHigh - 6, swingHigh - 10, swingHigh - 8},
		{swingHigh - 3, swingHigh - 8, swingHigh - 4},
		{swingHigh, swingHigh - 6, swingHigh - 1},
		{swingHigh - 2, swingHigh - 5, swingHigh - 3},
		{swingHigh - 2, swingHigh - 4, swingHigh - 4},
	}
	var out []market.Kline
	for bi, b := range buckets {
		for mi := 0; mi < 5; mi++ {
			i := bi*5 + mi
			h, l := b.c, b.c
			if mi == 0 {
				h = b.h
			}
			if mi == 1 {
				l = b.l
			}
			out = append(out, rthBars(i, b.c, h, l, b.c))
		}
	}
	return out
}

// seededMentorEval builds a seeded evaluator: a key level at keyPrice, frozen
// EMAs at emaPrice, and the trigger/HTF/ORB/day state the ISB path needs.
func seededMentorEval(cfg Config, keyPrice, emaPrice float64, bars []market.Kline, now int64) *Evaluator {
	// Disable the EMA-location line: the tape's incremental EMA34 would sit
	// between the entry and the old extreme and steal the target (this test
	// pins the OLD-EXTREME read, not the EMA-location ladder).
	cfg.EMALocationTFMinutes = 0
	e := New(cfg)
	e.seeded = true
	e.State.SeedLevels = []Level{{Key: fmt.Sprintf("key:%.2f", keyPrice), Kind: KindKeyLevel, Price: keyPrice, AtTime: 1}}
	e.State.EMA34 = emaPrice
	e.State.EMA9 = emaPrice
	// Freeze the incremental EMA so it stays at the injected price (the ISB's
	// far target), independent of the tape closes.
	e.State.Seed1mWatermark = bars[len(bars)-1].CloseTime
	trig := TriggerLine{Dir: SideLong, Price: 50}
	// Freeze the 5m trigger line at the injected long state: skip the whole
	// tape so the ISB's own low break never flips it short (the I1 trigger-side
	// gate would refuse a long ISB under a short line — not what this test is
	// about). Price 50 sits below every entry so TriggerVerdict passes.
	trig.LastBucket = bars[len(bars)-1].OpenTime
	e.State.Trigger = trig
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 50}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 50, Low: 45, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e
}

// TestISBFlagAtTheNearestUnbrokenOldHighCallSite — an ISB whose candle sits
// within ±2 of the nearest unbroken old high (101.5; the mother candle tops out
// AT it, no candle traded above it) carries the isb_at_old_extreme size-3 flag
// [D4.1 p1 @08:05 written rule 2].
func TestISBFlagAtTheNearestUnbrokenOldHighCallSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	swing := swingHighTape5m(101.5)            // swing high 101.5
	prev := rthBars(26, 98, 101.5, 97.5, 101.5) // green mother, tops out AT the old high
	cur := rthBars(27, 100.5, 102, 99, 100)    // inside bar, touches 101.5 (99..102)
	if !IsISB(prev, cur) {
		t.Fatal("fixture: must be an ISB")
	}
	bars := append(append(swing, prev), cur)
	now := cur.CloseTime + 1
	e := seededMentorEval(cfg, 85, 125, bars, now)

	intents := e.Tick(bars, now)
	for _, in := range intents {
		if in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry {
			if !HasFlag(in.Flag, FlagISBAtOldExtreme) {
				t.Fatalf("ISB at the nearest unbroken old high must carry isb_at_old_extreme, got flag %q (intent %+v)", in.Flag, in)
			}
			return
		}
	}
	t.Fatalf("no ISB intent emitted: %+v", intents)
}

// TestISBFlagIgnoresABrokenOldHighCallSite — the same ISB, but the mother
// candle traded ABOVE the pivot high (103 > 101.5) before the inside bar: the
// pivot is no longer an old high, so the inside bar is not "at an old high" and
// is not size-cut. The pre-r22 12-pivot list still flagged it.
func TestISBFlagIgnoresABrokenOldHighCallSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	swing := swingHighTape5m(101.5)
	prev := rthBars(26, 98, 103, 97.5, 101.5) // the mother wicks to 103 — breaks 101.5
	cur := rthBars(27, 100.5, 102, 99, 100)
	if !IsISB(prev, cur) {
		t.Fatal("fixture: must be an ISB")
	}
	bars := append(append(swing, prev), cur)
	now := cur.CloseTime + 1
	e := seededMentorEval(cfg, 85, 125, bars, now)

	intents := e.Tick(bars, now)
	for _, in := range intents {
		if in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry {
			if HasFlag(in.Flag, FlagISBAtOldExtreme) {
				t.Fatalf("ISB next to a BROKEN pivot high must not carry isb_at_old_extreme (flag %q)", in.Flag)
			}
			return
		}
	}
	t.Fatalf("no ISB intent emitted: %+v", intents)
}

// TestTargetIsTheNearestUnbrokenOldHighCallSite — the ISB target ladder
// (nextLevelBeyondRoom) takes the nearest unbroken old high (125) as the first
// obstacle, ahead of the key level at 130 [§6 "nearest old high → EMA 34 → the
// next level", D3.3 p1 @04:11].
func TestTargetIsTheNearestUnbrokenOldHighCallSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	swing := swingHighTape5m(125) // swing high 125, within 20 of key 130
	prev := rthBars(26, 105, 109, 104.5, 108) // green mother
	cur := rthBars(27, 107, 108.5, 106.5, 107) // inside bar (entry 110 / stop 105)
	if !IsISB(prev, cur) {
		t.Fatal("fixture: must be an ISB")
	}
	bars := append(append(swing, prev), cur)
	now := cur.CloseTime + 1
	e := seededMentorEval(cfg, 130, 140, bars, now)

	intents := e.Tick(bars, now)
	for _, in := range intents {
		if in.Action == PlaceStopLimitEntry || in.Action == PlaceStopEntry {
			if in.Target != 125 {
				t.Fatalf("target = %.2f, want 125 (the nearest unbroken old high, the nearer obstacle)", in.Target)
			}
			return
		}
	}
	t.Fatalf("no ISB intent emitted: %+v", intents)
}
