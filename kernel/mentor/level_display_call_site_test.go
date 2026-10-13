package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// ── OLD-EXTREME-CLEANUP — CALL-SITE PINS (canon class 53). ────────────────
// The DisplayLevels fold must NOT touch trading. These two tests drive the real
// evaluator Tick path with an old extreme that the DRAW set folds away, and
// assert the TRADING readers (the ISB size-3 flag at eval.go:1285 and the
// target ladder at eval.go:1254) still read the RAW old extreme. The mutant —
// feeding DisplayLevels(...) into those call sites — makes both go RED.

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
	e.State.SeedLevels = []Level{{Key: "key:" + fmtPriceKey(keyPrice), Kind: KindKeyLevel, Price: keyPrice, AtTime: 1}}
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

// TestISBFlagReadsRawOldExtremeCallSite — an ISB whose candle sits at an old
// extreme that the DRAW set folds into a key level must still carry the
// isb_at_old_extreme size-3 flag. Mutant (feed DisplayLevels into isbFlagsFor
// at eval.go:1285) → the folded old extreme is gone → the flag is empty → RED.
func TestISBFlagReadsRawOldExtremeCallSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	swing := swingHighTape5m(101.5)          // swing high 101.5, within 20 of key 85
	prev := rthBars(26, 98, 103, 97.5, 101.5) // green mother (body 98→101.5)
	cur := rthBars(27, 100.5, 102, 99, 100)  // inside bar, touches 101.5 (99..102)
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
				t.Fatalf("ISB at the folded old extreme must carry isb_at_old_extreme, got flag %q (intent %+v)", in.Flag, in)
			}
			return
		}
	}
	t.Fatalf("no ISB intent emitted: %+v", intents)
}

// TestTargetReadsRawOldExtremeCallSite — the ISB target ladder (nextLevelBeyondRoom
// at eval.go:1254) must use the RAW nearest obstacle: an old extreme folded into
// a key for the DRAW is still the target. Mutant (feed DisplayLevels) → the
// target jumps to the farther key → RED.
func TestTargetReadsRawOldExtremeCallSite(t *testing.T) {
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
				t.Fatalf("target = %.2f, want 125 (the RAW old extreme, the nearer obstacle)", in.Target)
			}
			return
		}
	}
	t.Fatalf("no ISB intent emitted: %+v", intents)
}
