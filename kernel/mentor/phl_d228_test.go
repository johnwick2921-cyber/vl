package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// D2-28 [D2.2 p3 @02:30–04:18, @06:50]: the PHL "higher low" must validate the
// touch candle's low against the STRUCTURAL low LEFT of the old high — the low
// the leg to the old high started from ("Đối chiếu với những cái ĐÁY bên tay
// trái… đáy thấp → đáy cao hơn… SHIFT CẤU TRÚC"), not the previous pullback
// candle's 3-bar fractal low.
//
// The worked example [D2.2 p1 @06:50]: old high 29,431.75; the structural left
// low 29,335; a STEPPED pullback descends to a broken candle whose low 29,386
// is the lowest of the descent but still ABOVE 29,335. The PHL must emit. The
// buggy call site compares 29,386 against the previous descent bar (29,395 —
// a 3-bar fractal "swing low") and refuses it as phl_not_higher_low.
//
// This is the production call site (Evaluator.Tick).

func TestPHLHigherLowValidatesAgainstTheLeftLow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6
	cfg.EMALocationTFMinutes = 0 // isolate the higher-low check from the EMA-34 location target cap (B15)

	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 29386},                      // the support the broken candle touches
		{Key: "old-low:29335", Kind: KindOldExtreme, Price: 29335},        // the structural left low
		{Key: "old-high:29431.75", Kind: KindOldExtreme, Price: 29431.75}, // the old high
	}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}}

	bars := []market.Kline{
		rthBars(0, 29400, 29410, 29335, 29405),    // swing low (low 29335)
		rthBars(1, 29420, 29430, 29415, 29425),    // rise
		rthBars(2, 29425, 29431.75, 29420, 29428), // old high (high 29431.75)
		rthBars(3, 29410, 29415, 29400, 29408),    // stepped descent 1 (low 29400)
		rthBars(4, 29400, 29405, 29395, 29398),    // stepped descent 2 (low 29395)
		rthBars(5, 29395, 29397, 29386, 29392),    // broken candle touches L=29386, closes back above
	}
	now := bars[5].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

	ints := e.Tick(bars, now)
	for _, in := range ints {
		if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong {
			// The worked PHL must EMIT: the broken-candle low 29,386 is a
			// HIGHER low than the left low 29,335.
			return
		}
	}
	t.Fatalf("the @06:50 worked PHL must emit (higher low vs the left low); refusals=%v intents=%+v", e.State.Refusals, ints)
}
