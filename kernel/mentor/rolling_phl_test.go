package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// rollingFixture builds the D2.2 pullback-continuation tape + evaluator
// presets: an uptrend into an old HIGH at 130 (bar 2), then a three-candle
// pullback (bars 3–5) with NO level touch. The rolling path owns the entry:
// a buy stop rolled to EACH closed pullback candle's high, stop at that
// candle's low, target near the old high.
func rollingFixture() (*Evaluator, []market.Kline, int64) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // no EMA location line
	cfg.RoomMultiple = 0.05      // room rule effectively off (pin the rolling mechanics)
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.Exec2mAfter30m = false // 1m read
	cfg.LocTriggerFilter = false
	cfg.RollingPHL = true
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100.5),
		mk(1, 100.5, 102, 100, 101.5),
		mk(2, 101.5, 130, 98, 129), // the old high swing (H 130) and the structural low 98
		mk(3, 129, 128, 120, 121),  // pullback candle 1 (lower high, lower low)
		mk(4, 121, 122, 116, 117),  // pullback candle 2
		mk(5, 117, 118, 112, 113),  // pullback candle 3 (the newest closed)
	}
	now := bars[5].OpenTime + 59_999
	levels := []Level{
		{Key: "old-high:130", Kind: KindOldExtreme, Role: OldHigh, Price: 130},
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

func rollingPlaceEntries(ins []Intent) []Intent {
	var out []Intent
	for _, in := range ins {
		if in.Action == PlaceStopEntry && (in.Setup == "PHL" || in.Setup == "PLH") {
			out = append(out, in)
		}
	}
	return out
}

// TestRollingPHLPullbackFires — (A) pin: in a pullback with NO level touch the
// D2.2 rolling PHL fires: a buy stop at the newest closed candle's high + the
// course buffer, stop at that candle's low, target near the old high.
// Mutant: gate the rolling on a level touch (or drop the no-level path) → no
// entry on this touchless tape → RED.
func TestRollingPHLPullbackFires(t *testing.T) {
	e, bars, now := rollingFixture()
	ins := e.Tick(bars, now)
	places := rollingPlaceEntries(ins)
	if len(places) != 1 {
		t.Fatalf("rolling pullback emitted %d PHL/PLH entries, want exactly 1: %+v", len(places), places)
	}
	in := places[0]
	// entry = bar 5 high 118 + buffer 1.0 = 119; stop = bar 5 low 112
	if in.Side != SideLong || in.Price != 119 || in.Stop != 112 {
		t.Fatalf("rolling entry = %+v, want long 119 / stop 112", in)
	}
	if in.Target != 130 {
		t.Fatalf("target = %.2f, want the old high 130 (shy 0)", in.Target)
	}
	if e.State.RollingPHL.ArmID == "" {
		t.Fatal("the rolling leg must record its resting arm id")
	}
	if e.State.LevelArms[rollingLevelKey].ArmID != in.ArmID {
		t.Fatal("the rolling arm must rest under the rolling LevelArms key")
	}
}

// TestRollingPHLRollsToNextCandle — the headline: the stop RE-ARMS to each new
// closed pullback candle. Tick twice; the second (lower) candle cancels the
// first arm and places a NEW buy stop at the new candle's high.
// Mutant: drop the cancel+replace (keep only the first arm) → the second tick
// emits no new entry → RED.
func TestRollingPHLRollsToNextCandle(t *testing.T) {
	e, bars, _ := rollingFixture()
	_ = e.Tick(bars, bars[5].OpenTime+59_999)
	firstArm := e.State.RollingPHL.ArmID

	// A new pullback candle closes lower (high 114, low 108).
	t0 := bars[0].OpenTime
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	next := append(append([]market.Kline{}, bars...), mk(6, 113, 114, 108, 109))
	now := next[6].OpenTime + 59_999
	ins := e.Tick(next, now)
	places := rollingPlaceEntries(ins)
	if len(places) != 1 {
		t.Fatalf("second tick emitted %d entries, want 1 re-armed roll: %+v", len(places), places)
	}
	// entry = bar 6 high 114 + 1.0 = 115; stop = 108
	if places[0].Price != 115 || places[0].Stop != 108 {
		t.Fatalf("re-armed entry = %+v, want 115 / 108", places[0])
	}
	// the previous arm is cancelled
	var sawCancel bool
	for _, in := range ins {
		if in.Action == CancelArm && in.ArmID == firstArm {
			sawCancel = true
		}
	}
	if !sawCancel {
		t.Fatalf("the re-arm must cancel the previous rolling arm %s: %+v", firstArm, ins)
	}
}

// TestRollingPHLDoesNotFireOnLevelTouch — the m216 "never conflict" pin: a
// level touch-reject OWNS the entry (first-touch-fixed). The rolling path must
// emit NOTHING on that side. Mutant: drop the levelTouchRejectActive guard →
// a rolling entry doubles the level PHL → RED.
func TestRollingPHLDoesNotFireOnLevelTouch(t *testing.T) {
	// A support level touched and rejected from above (buy side) sits in
	// TouchReject; the rolling pullback is suppressed by the guard.
	e, bars, now := rollingFixture()
	e.State.Touches["support"] = Touch{
		LevelKey: "support", Outcome: TouchReject, ApproachedFrom: SideLong,
		RefBar: bars[5], PriceAtTouch: 112,
	}
	ins := e.Tick(bars, now)
	for _, in := range ins {
		if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "PHL/PLH rolling pullback") {
			t.Fatalf("a level touch must suppress the rolling entry; got %+v", in)
		}
	}
	if e.State.RollingPHL.ArmID != "" {
		t.Fatalf("the rolling state must reset under a level touch, got arm %s", e.State.RollingPHL.ArmID)
	}
}

// TestRollingPLHShortMirror — the D2.2 mirror: in a downtrend a bounce rolls a
// SELL stop under each closed bounce candle's low until a candle breaks the
// previous candle's low and fills it [D2.2 p1 @11:05–11:36 "Tôi ra một lệnh
// sell stop đã sẵn"].
func TestRollingPLHShortMirror(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.RoomMultiple = 0.05
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.Exec2mAfter30m = false
	cfg.LocTriggerFilter = false
	cfg.RollingPHL = true
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 150, 151, 149, 150.5),
		mk(1, 150.5, 151, 149, 150),
		mk(2, 150, 151, 120, 121), // the old low swing (L 120)
		mk(3, 121, 130, 122, 129), // bounce candle 1 (higher high, higher low)
		mk(4, 129, 132, 126, 131), // bounce candle 2
		mk(5, 131, 133, 128, 132), // bounce candle 3 (the newest closed)
	}
	now := bars[5].OpenTime + 59_999
	levels := []Level{{Key: "old-low:120", Kind: KindOldExtreme, Role: OldLow, Price: 120}}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 160, Low: 155, Drawn: true, Escaped: SideShort}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 140}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	ins := e.Tick(bars, now)
	places := rollingPlaceEntries(ins)
	if len(places) != 1 {
		t.Fatalf("rolling bounce emitted %d PLH entries, want 1: %+v", len(places), places)
	}
	// entry = bar 5 low 128 − buffer 1.0 = 127; stop = bar 5 high 133
	if places[0].Side != SideShort || places[0].Price != 127 || places[0].Stop != 133 {
		t.Fatalf("rolling short entry = %+v, want short 127 / stop 133", places[0])
	}
	if places[0].Target != 120 {
		t.Fatalf("target = %.2f, want the old low 120", places[0].Target)
	}
}
