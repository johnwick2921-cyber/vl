package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// D4.3 wick microscalp (advanced, knob OFF by default): 2+ consecutive CLOSED 5m
// candles rejecting with wicks the SAME way put a bounded target at their far
// wick [D4.3 @00:00–03:20, @09:27–09:46, @13:22–13:44].

func TestWickMicroscalpLevel(t *testing.T) {
	mk := func(o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: 1000, CloseTime: 1000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// Long: two lower-wick candles (low < body) → buy pressure; target = the
	// far (ceiling) wick = max high.
	got, ok := wickMicroscalpLevel([]market.Kline{
		mk(100, 110, 95, 102),
		mk(102, 110, 97, 104),
	}, SideLong)
	if !ok || got.Price != 110 || got.Kind != KindWickMicroscalp {
		t.Fatalf("long lower-wick run = (%+v, %v), want (Price 110, true)", got, ok)
	}
	// Short: two upper-wick candles (high > body) → sell pressure; target = the
	// far (floor) wick = min low.
	got, ok = wickMicroscalpLevel([]market.Kline{
		mk(100, 110, 95, 98),
		mk(98, 110, 93, 96),
	}, SideShort)
	if !ok || got.Price != 93 {
		t.Fatalf("short upper-wick run = (%+v, %v), want (Price 93, true)", got, ok)
	}
	// One candle is not enough ("2–3 consecutive").
	if _, ok := wickMicroscalpLevel([]market.Kline{mk(100, 110, 95, 102)}, SideLong); ok {
		t.Fatal("a single rejecting candle must NOT fire")
	}
	// Wicks against the trend are unreadable (a lower wick alone in a
	// downtrend must not read as short pressure).
	if _, ok := wickMicroscalpLevel([]market.Kline{
		mk(100, 102, 95, 102), mk(102, 104, 97, 104), // lower wick only, no upper wick
	}, SideShort); ok {
		t.Fatal("lower wicks in a downtrend must NOT fire")
	}
	// No wick at all (low == body low and high == body high).
	if _, ok := wickMicroscalpLevel([]market.Kline{
		mk(100, 102, 100, 102), mk(102, 104, 102, 104),
	}, SideLong); ok {
		t.Fatal("no rejecting wick must NOT fire")
	}
}

func wickFixture(t *testing.T) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	cfg.WickMicroscalpEnabled = true
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// Two closed 5m buckets with LOWER wicks (lows dip below the body) and a
	// flat ceiling high of 110 (so 110 is NOT a 1m fractal swing high — the wick
	// level is the ONLY obstacle between the entry and the next key level 120).
	bars := []market.Kline{
		// 5m candle A: open 100, high 110, low 95, close 102 (lower wick)
		mk(0, 100, 110, 95, 100),
		mk(1, 100, 110, 96, 101),
		mk(2, 101, 110, 97, 101.5),
		mk(3, 101.5, 110, 98, 102),
		mk(4, 102, 110, 99, 102),
		// 5m candle B: open 102, high 110, low 97, close 104 (lower wick, higher low)
		mk(5, 102, 110, 97, 103),
		mk(6, 103, 110, 98, 103.5),
		mk(7, 103.5, 110, 99, 104),
		mk(8, 104, 110, 100, 103.5),
		mk(9, 103.5, 110, 101, 104),
		// ISB pair (green candle 1 → long)
		mk(10, 99.5, 106.5, 99.5, 106),
		mk(11, 102, 104.5, 100.5, 104),
	}
	now := bars[len(bars)-1].CloseTime
	levels := []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 90},
		{Key: "K-above", Kind: KindKeyLevel, Price: 120},
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 85, Low: 80, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 101}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

// TestISBTargetCanBeTheWickMicroscalp — the D4.3 pressure read (2 lower-wick 5m
// candles) puts the bounded wick target at 110, nearer than the next key level
// 120. Mutant: drop the wick level → RED (target 120).
func TestISBTargetCanBeTheWickMicroscalp(t *testing.T) {
	e, bars, now := wickFixture(t)
	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			isb = in
			break
		}
	}
	if isb == nil {
		t.Fatalf("the long ISB must fire; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 110 {
		t.Fatalf("ISB target = %.2f, want 110 (the wick microscalp); intents %+v", isb.Target, ins)
	}
}

// TestWickMicroscalpOffLeavesTargetAtNextLevel — with the knob OFF the wick
// level is absent and the target is the next key level.
func TestWickMicroscalpOffLeavesTargetAtNextLevel(t *testing.T) {
	e, bars, now := wickFixture(t)
	e.Cfg.WickMicroscalpEnabled = false
	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			isb = in
			break
		}
	}
	if isb == nil {
		t.Fatalf("the long ISB must fire; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 120 {
		t.Fatalf("knob-off ISB target = %.2f, want 120 (next key level); intents %+v", isb.Target, ins)
	}
}

// TestWickMicroscalpLevelNeverBecomesALocation — the wick is target-only.
func TestWickMicroscalpLevelNeverBecomesALocation(t *testing.T) {
	if levelIsLocation(Level{Key: "wick_microscalp", Kind: KindWickMicroscalp, Price: 110}, nil) {
		t.Fatal("the wick-microscalp level must never be a PHL/PLH location")
	}
}
