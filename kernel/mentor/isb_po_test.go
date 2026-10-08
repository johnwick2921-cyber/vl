package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// isbFixture is the standard 3-bar ISB fixture (09:00–09:02 CT, RTH): the head
// bar keeps the EMA 34 target well BEYOND the entry so the E-2 1:1 floor does
// not fire. prev is green (candle-1 colour → long), cur's body is inside prev.
func isbFixture() []market.Kline {
	return []market.Kline{
		rthBars(0, 121, 121.5, 120.5, 120),
		rthBars(1, 99, 106, 98.5, 106),
		rthBars(2, 101, 102.1, 100.9, 102),
	}
}

// newISBEval returns an evaluator whose trigger, HTF, ORB and day latch all
// allow the standard long ISB through to the emit/target stage.
func newISBEval(cfg Config) *Evaluator {
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	bars := isbFixture()
	now := bars[len(bars)-1].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e
}

// TestDojiCandle1IsNotISB — RULING P3 (CTO 2026-10-03T15:12Z): a doji candle 1
// is NO ISB; the old Close >= Open read traded dojis long.
func TestDojiCandle1IsNotISB(t *testing.T) {
	doji := market.Kline{Open: 100, High: 110, Low: 90, Close: 100}
	cur := market.Kline{Open: 100, High: 103, Low: 97, Close: 101}
	if IsISB(doji, cur) {
		t.Fatal("a doji candle 1 must not form an ISB")
	}
	if dir := ISBDirection(doji); dir != "" {
		t.Fatalf("a doji has no ISB direction, got %q", dir)
	}
	if _, _, ok, _ := ISBStopLimitOrder(doji, cur, DefaultConfig()); ok {
		t.Fatal("a doji candle 1 must not produce an ISB order")
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	e := newISBEval(cfg)
	bars := []market.Kline{
		rthBars(0, 100, 110, 90, 100), // doji candle 1
		rthBars(1, 100, 103, 97, 101), // body inside — but candle 1 is a doji
	}
	ints := e.Tick(bars, bars[1].CloseTime+1)
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry {
			t.Fatalf("a doji candle 1 must not trade; got %+v", in)
		}
	}
	for k := range e.State.Refusals {
		if k[:4] == "isb_" {
			t.Fatalf("no ISB stage may fire on a doji pair; ledger = %v", e.State.Refusals)
		}
	}
}

// TestISBMissingTargetSkipsOnlyThatISB — CTO E-1: a missing target refuses
// THAT ISB with a named refusal and the tick CONTINUES (the old code returned
// out, aborting PHL/box/swing/stacking for the minute).
func TestISBMissingTargetSkipsOnlyThatISB(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	bars := []market.Kline{
		rthBars(1, 99, 106, 98.5, 100), // green candle 1; close 100 keeps every EMA below the entry
		rthBars(2, 101, 102.1, 100.9, 102),
	}
	now := bars[1].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	// Seeded with a level set that has NOTHING beyond the long entry: the EMA
	// lines are pinned at 0 (below), and the seed watermarks skip the walks.
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	// A live short arm whose range contains cur's body: the stacking loop
	// (AFTER the ISB block) must extend it — proof the tick did not abort.
	e.State.ISBArms = map[string]ISBArm{
		"arm-1": {FirstBar: market.Kline{High: 120, Low: 80}, Inside: 1, Side: SideShort},
	}

	ints := e.Tick(bars, now)
	if e.State.Refusals["isb_missing_target"] == 0 {
		t.Fatalf("the missing-target ISB must be refused by name; ledger = %v", e.State.Refusals)
	}
	found := false
	for _, in := range ints {
		if in.Action == ExtendArm && in.ArmID == "arm-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the tick aborted at the missing-target ISB — the stacking ExtendArm never fired; intents = %+v", ints)
	}
}

// TestISBTargetBelowFloorRefused — CTO E-2: target = next level beyond with NO
// 1:1 floor; |target−entry| < |entry−stop| is refused by name ("the target is
// never smaller than the stop" [D1.2 p1 @ 07:48]) and no arm is placed.
func TestISBTargetBelowFloorRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	bars := []market.Kline{
		rthBars(1, 99, 106, 98.5, 106),
		rthBars(2, 101, 102.1, 100.9, 102),
	}
	now := bars[1].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	// entry = 102.1 + 1.5 = 103.6, stop = 100.9 − 1.5 = 99.4 → risk 4.2. Pin
	// the EMA 34 at 106: target = 106, |106 − 103.6| = 2.4 < 4.2 → sub-1:1.
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 106
	e.State.EMA9 = 0

	ints := e.Tick(bars, now)
	if e.State.Refusals["rr_floor"] == 0 {
		t.Fatalf("a sub-1:1 ISB target must be refused by name; ledger = %v", e.State.Refusals)
	}
	if len(e.State.ISBArms) != 0 {
		t.Fatalf("no arm may be placed on a sub-1:1 target; arms = %v", e.State.ISBArms)
	}
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry {
			t.Fatalf("no ISB entry may survive the target floor; got %+v", in)
		}
	}
}

// TestTargetFloorOK is the pure geometry of the D1.2 floor.
func TestTargetFloorOK(t *testing.T) {
	if !targetFloorOK(100, 95, 105) {
		t.Fatal("a 1:1 target must pass")
	}
	if targetFloorOK(100, 95, 104) {
		t.Fatal("a sub-1:1 target must fail")
	}
	if !targetFloorOK(100, 105, 95) { // short side, symmetric
		t.Fatal("the floor is |distance| based on both sides")
	}
	if targetFloorOK(100, 105, 96) {
		t.Fatal("a sub-1:1 short target must fail")
	}
}

// TestISBSilentDropsAllNamed — CTO E-4: the twenties skip, the HTF block, the
// R5 box block, the HTF side mismatch and the DayOff branch all record a named
// refusal instead of dropping silently.
func TestISBSilentDropsAllNamed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false // these pins exercise the direction gate itself (all-day)

	t.Run("twenties", func(t *testing.T) {
		e := newISBEval(cfg)
		bars := []market.Kline{
			rthBars(0, 121, 121.5, 120.5, 120),
			rthBars(1, 95, 120, 90, 115),  // green candle 1, wide
			rthBars(2, 100, 115, 96, 101), // ISB; stop = 19 + 3 = 22 pts
		}
		if _, ok, _ := ISBStopVerdict(bars[2], cfg); ok {
			t.Fatal("fixture: the stop must sit in the twenties")
		}
		e.Tick(bars, bars[2].CloseTime+1)
		if e.State.Refusals["isb_stop_twenties"] == 0 {
			t.Fatalf("twenties skip must be refused by name; ledger = %v", e.State.Refusals)
		}
	})

	t.Run("htf-blocked", func(t *testing.T) {
		e := newISBEval(cfg)
		e.State.HTF = HTF{} // no 4h trigger → the read never starts
		bars := isbFixture()
		e.Tick(bars, bars[2].CloseTime+1)
		if e.State.Refusals["isb_htf_blocked"] == 0 {
			t.Fatalf("HTF block must be refused by name; ledger = %v", e.State.Refusals)
		}
	})

	t.Run("box-blocked", func(t *testing.T) {
		e := newISBEval(cfg)
		e.State.ISBBox = &ISBBox{High: 200, Low: 50, Dir: SideShort, AtTime: auditMs(2026, 9, 15, 9, 0, 0)}
		bars := isbFixture()
		e.Tick(bars, bars[2].CloseTime+1)
		if e.State.Refusals["isb_box_blocked"] == 0 {
			t.Fatalf("R5 box block must be refused by name; ledger = %v", e.State.Refusals)
		}
	})

	t.Run("htf-side-mismatch", func(t *testing.T) {
		e := newISBEval(cfg)
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 90}} // long ISB vs short 4h
		bars := isbFixture()
		e.Tick(bars, bars[2].CloseTime+1)
		if e.State.Refusals["isb_htf_side_mismatch"] == 0 {
			t.Fatalf("side mismatch must be refused by name; ledger = %v", e.State.Refusals)
		}
	})

	t.Run("day-off", func(t *testing.T) {
		e := newISBEval(cfg)
		bars := isbFixture()
		now := bars[2].CloseTime + 1
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayOff}
		e.Tick(bars, now)
		if e.State.Refusals["isb_day_off"] == 0 {
			t.Fatalf("DayOff must be refused by name; ledger = %v", e.State.Refusals)
		}
	})
}

// TestISBSpentDayTargetCap — B9 [D5.1 p1 @16:24, @19:11–20:07]: on a spent day
// the ISB target obeys the cap ("15 điểm bán, 10 điểm bán") like every other
// setup — the next-level-beyond target is pulled down to entry+15 when it is
// farther, and the 1:1 floor is re-checked after the cap.
func TestISBSpentDayTargetCap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // this pin is about the cap value, not the room rule
	cfg.DayGateSpentPts = 300
	cfg.DayGateTargetCapPts = 15
	e := newISBEval(cfg)
	bars := isbFixture()
	now := bars[len(bars)-1].CloseTime + 1
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DaySpent}

	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		if ins[i].Action == PlaceStopLimitEntry {
			isb = &ins[i]
			break
		}
	}
	if isb == nil {
		t.Fatalf("the long ISB did not emit on a spent day: %v (refusals %v)", ins, e.State.Refusals)
	}
	if isb.Target == 0 {
		t.Fatal("ISB emitted without a target")
	}
	if abs(isb.Target-isb.Price) > cfg.DayGateTargetCapPts+1e-9 {
		t.Fatalf("spent-day ISB target = %.2f, %.2f pts from entry — must be capped at %.0f",
			isb.Target, abs(isb.Target-isb.Price), cfg.DayGateTargetCapPts)
	}
}
