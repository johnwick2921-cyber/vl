package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// ftghFixture builds the 9-bar tape that draws exactly one FTGH [104, 106]
// (a clean double-top). Item 12: the partner is the LATER confirmed lower
// high (top 2 @5, body edge 104), never the earlier adjacent high. No escape
// candle here — the F2 Tick pins append their own bars after index 8, and a
// higher-high escape candle would become the extreme once confirmed. Used by
// the F2 Tick pins to put a real box on the tape.
func ftghFixture(mk func(int, float64, float64, float64, float64) market.Kline) []market.Kline {
	return []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 106, 100, 105), // top 1: the extreme high 106
		mk(3, 104, 105, 102, 104), // confirms top 1 (high 105 < 106)
		mk(4, 103, 104, 102, 103),
		mk(5, 104, 105.5, 102, 103.5),   // top 2: swing high 105.5, body edge 104
		mk(6, 103, 104.5, 102.5, 103.5), // confirms top 2
		mk(7, 103, 104, 102.5, 103),     // filler — no new swing high
		mk(8, 103, 104, 102.5, 103),     // filler — no new swing high
	}
}

func nearBoxEval(cfg Config, now int64) *Evaluator {
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.ORB = ORB{Day: dayStartCT(now), High: 110, Low: 80, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e
}

// TestNearBoxISBTickRefused — F2 (CTO 2026-10-04): an Evaluator.Tick pin for
// the ISB emit site. An ISB 10 pts under an FTGH with a 6 pt risk emits NO
// intent and counts near_box = 1. Mutant: deleting the ISB near-box call in
// eval.go must turn this RED (an ISB intent is emitted).
func TestNearBoxISBTickRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMALocationTFMinutes = 0 // keep the EMA-34 line out of the level set
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := append(ftghFixture(mk),
		mk(9, 90, 93, 89, 92),        // mother: green (90 < 92)
		mk(10, 91, 92.5, 89.5, 91.5), // inside: body [91, 91.5] in mother wick [89, 93]; range 3 → risk 6
	)
	now := bars[len(bars)-1].CloseTime + 1

	e := nearBoxEval(cfg, now)
	// Buy line well below the entry; LastBucket freezes it so TriggerTick does
	// not re-draw the line from the tape's lower low (the FTGH fixture + the
	// ISB pair break the 5m low, which would flip the trigger to SHORT and
	// refuse the LONG ISB as a trigger-side mismatch before near_box).
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 80, LastBucket: t0 + 5*60_000}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 80}}

	ints := e.Tick(bars, now)
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry && in.Setup == "ISB" {
			t.Fatalf("near-box ISB must not emit; got %+v; refusals=%v", in, e.State.Refusals)
		}
	}
	if got := e.State.Refusals["near_box"]; got != 1 {
		t.Fatalf("near_box refusal = %d, want 1; refusals=%v", got, e.State.Refusals)
	}
}

// TestNearBoxPHLTickRefused — F2 (CTO 2026-10-04): an Evaluator.Tick pin for
// the PHL/PLH emit site. A LONG PHL whose entry sits within NearBoxRoomMultiple
// × risk of a box edge above emits NO intent and counts near_box = 1. Mutant:
// deleting the PHL near-box call in eval.go must turn this RED.
func TestNearBoxPHLTickRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMALocationTFMinutes = 0
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := append(ftghFixture(mk),
		mk(9, 94, 95, 92, 93),  // pull-back above the key level
		mk(10, 93, 96, 90, 92), // touches L=90 from above, closes back above → LONG reject
	)
	now := bars[len(bars)-1].CloseTime + 1

	e := nearBoxEval(cfg, now)
	// A key level at 90 (the touch) and the FTGH's old high at 106 (the target
	// extreme). No trigger line: no trigger-retest level, no ISB direction.
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 90},
		{Key: "old-high:106", Kind: KindOldExtreme, Price: 106},
	}
	e.State.Trigger = TriggerLine{}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 80}}

	ints := e.Tick(bars, now)
	for _, in := range ints {
		if in.Action == PlaceStopEntry {
			t.Fatalf("near-box PHL must not emit; got %+v; refusals=%v", in, e.State.Refusals)
		}
	}
	if got := e.State.Refusals["near_box"]; got != 1 {
		t.Fatalf("near_box refusal = %d, want 1; refusals=%v", got, e.State.Refusals)
	}
}
