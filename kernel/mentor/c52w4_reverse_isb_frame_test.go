package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// C52-W4 (A5 part 1): the reverse ISB at EMA 9 reads the EMA 9 on the frame the
// ISB pair is read on. "anh trade khung nào thì anh bật khung đó, khung M1 cũng
// được, khung M2 cũng được" [Ngày 5.4 ISB đảo chiều part1.srt @00:08:15–00:08:40].
// Before: the raw 1m EMA 9 was tested against 2m candles. The 5m evening path
// is untouched (owner Q9 unanswered); the touch tolerance is parked (mentor Q).

// c52w4Fixture is a 10:00 CT (2m frame) uptrend ramp of 1m bars whose last two
// CLOSED 2m candles form a SHORT-pointing ISB (red mother, inside daughter).
// The 1m EMA 9 and the 2m EMA 9 differ on a ramp (the 2m line lags more), so
// the `cur` range [curLow, curHigh] can contain one and not the other.
func c52w4Fixture(t *testing.T, curLow, curHigh float64) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Exec2mAfter30m = true
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = true
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	cfg.RoomMultiple = 0.5
	t0 := time.Date(2026, time.September, 15, 10, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	var bars []market.Kline
	// 60 one-minute bars ramping 100 -> 159 (+1 per minute).
	for i := 0; i < 60; i++ {
		c := 100 + float64(i)
		bars = append(bars, mk(i, c-0.5, c+0.25, c-0.75, c))
	}
	// mother 2m candle (minutes 60,61): O160 H165 L115 C118, red and wide.
	// Daughter 2m (62,63): inside it, spanning [curLow, curHigh].
	mid := (curLow + curHigh) / 2
	bars = append(bars,
		mk(60, 160, 165, 118, 120),
		mk(61, 120, 122, 115, 118),
		mk(62, mid, curHigh, curLow, mid),
		mk(63, mid, curHigh, curLow, mid),
	)
	now := BarCloseInstant(bars[len(bars)-1])
	levels := []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 90},
		{Key: "K-above", Kind: KindKeyLevel, Price: 200},
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	// Pin the 5m trigger LONG: the wide red mother would flip a live trigger to
	// short, and this fixture isolates the EMA-frame read, not the trigger.
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 101, LastBucket: math.MaxInt64}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

func c52w4Reverse(ins []Intent) *Intent {
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && (in.Setup == "ISB" || in.Setup == "PHL") && in.Side == SideLong {
			return in
		}
	}
	return nil
}

// c52w4EMAs returns the 1m and the 2m EMA 9 of the tape (the oracle for the
// fixture premises).
func c52w4EMAs(bars []market.Kline, now int64) (e1, e2 float64) {
	return emaValue(bars, 9), emaValue(closedBucketsTF(bars, 2, now), 9)
}

// TestC52W4ReverseISBUsesThe2mEMA9 - the 2m EMA 9 sits inside the 2m ISB
// candle, the 1m EMA 9 does NOT. Production path: Evaluator.Tick. Before the
// fix the 1m EMA 9 decided and the reverse ISB was dropped. Mutant: restore
// `emaValue(bars, period)` for revEma9 -> RED.
func TestC52W4ReverseISBUsesThe2mEMA9(t *testing.T) {
	e, bars, now := c52w4Fixture(t, 144.4, 148)
	e1m, e2m := c52w4EMAs(bars, now)
	if !(e1m < 144.4 && e2m >= 144.4 && e2m <= 148) {
		t.Fatalf("fixture premise broken: ema9(1m)=%.3f must be below 144.4, ema9(2m)=%.3f inside [144.4,148]", e1m, e2m)
	}
	ins := e.Tick(bars, now)
	r := c52w4Reverse(ins)
	if r == nil {
		t.Fatalf("the 2m EMA 9 (%.3f) is inside the 2m ISB candle: the reverse ISB must fire LONG; intents %+v refusals %v", e2m, ins, e.State.Refusals)
	}
	if r.ExecTFMin != 2 {
		t.Fatalf("reverse ISB ExecTFMin = %d, want 2", r.ExecTFMin)
	}
}

// TestC52W4ReverseISBRefusesWhenOnly1mEMA9Touches - the converse: the 1m EMA 9
// sits inside the 2m candle but the 2m EMA 9 does NOT. The 2m line is the one
// the trader watches, so no reverse ISB. Mutant: 1m EMA -> RED (it fired).
func TestC52W4ReverseISBRefusesWhenOnly1mEMA9Touches(t *testing.T) {
	e, bars, now := c52w4Fixture(t, 137, 141.5)
	e1m, e2m := c52w4EMAs(bars, now)
	if !(e1m >= 137 && e1m <= 141.5 && e2m > 141.5) {
		t.Fatalf("fixture premise broken: ema9(1m)=%.3f inside [137,141.5], ema9(2m)=%.3f above", e1m, e2m)
	}
	ins := e.Tick(bars, now)
	if r := c52w4Reverse(ins); r != nil {
		t.Fatalf("the 2m EMA 9 (%.3f) is outside the 2m ISB candle: no reverse ISB; got %+v", e2m, r)
	}
}

// TestC52W4ReverseISBPrefersTheDrawn2mLine - when the incremental 2m EMA 9 has
// folded exactly through execCur (watermark == execCur.CloseTime) it IS the
// number tested (the same line the chart and the EMA 9 level show).
func TestC52W4ReverseISBPrefersTheDrawn2mLine(t *testing.T) {
	e, bars, now := c52w4Fixture(t, 144.4, 148)
	cb := closedBucketsTF(bars, 2, now)
	e.State.EMA2mWarmed = true
	e.State.EMA9x2 = 100 // far from the candle: would refuse if it is the number tested
	e.State.Seed2mWatermark = cb[len(cb)-1].CloseTime
	if r := c52w4Reverse(e.Tick(bars, now)); r != nil {
		t.Fatalf("State.EMA9x2=100 is the aligned 2m line and is outside the candle: must refuse, got %+v", r)
	}
	// a stale watermark -> the incremental line is NOT in step with execCur, so
	// the closed-2m-bucket EMA is used instead (inside the candle -> fires).
	e2, bars2, now2 := c52w4Fixture(t, 144.4, 148)
	e2.State.EMA2mWarmed = true
	e2.State.EMA9x2 = 100
	e2.State.Seed2mWatermark = 1
	if c52w4Reverse(e2.Tick(bars2, now2)) == nil {
		t.Fatalf("a stale 2m watermark must fall back to the closed-bucket 2m EMA 9 and fire")
	}
}

// TestC52W4FrameHelperKeeps1mAnd5m - the 1m exec frame and the 5m evening exec
// frame keep the 1m EMA 9 (the 5m path waits for owner Q9); only 2m changes.
func TestC52W4FrameHelperKeeps1mAnd5m(t *testing.T) {
	e, bars, now := c52w4Fixture(t, 144.4, 148)
	e1m, e2m := c52w4EMAs(bars, now)
	last := bars[len(bars)-1]
	for _, tf := range []int{1, 5} {
		if got := e.reverseISBEMA9(bars, last, tf, now); math.Abs(got-e1m) > 1e-9 {
			t.Fatalf("exec frame %dm: got %.4f, want the 1m EMA 9 %.4f", tf, got, e1m)
		}
	}
	if got := e.reverseISBEMA9(bars, last, 2, now); math.Abs(got-e2m) > 1e-9 {
		t.Fatalf("exec frame 2m: got %.4f, want the 2m EMA 9 %.4f", got, e2m)
	}
}
