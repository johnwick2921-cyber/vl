package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestISBNotLocationGated — OWNER RULING 2026-10-03 ("exactly like he said"):
// the ISB is NOT location-gated — "inside bar lúc nào cũng có thể take risk…
// trong range, trên range, ngoài range, dưới range" [D4.1 p1 @ 05:15]. A
// mid-air ISB (no key level, no EMA touch, no trigger retest — LocationVerdict
// would refuse) still emits when the remaining conditions hold. Mutant
// (restore the location gate on the ISB path) → RED.
func TestISBNotLocationGated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// a head bar with a high close so the EMA 34 target sits well BEYOND the
	// entry: the E-2 floor ("the target is never smaller than the stop"
	// [D1.2 p1 @ 07:48]) refuses a sub-1:1 target.
	head := rthBars(0, 121, 121.5, 120.5, 120)
	prev := rthBars(1, 99, 106, 98.5, 106)
	cur := rthBars(2, 101, 102.1, 100.9, 102)
	bars := []market.Kline{head, prev, cur}
	if !IsISB(prev, cur) {
		t.Fatal("fixture: must be an ISB")
	}
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	// the ORB gate is default-ON: preset a drawn + escaped-long opening range
	// (these tests are about the location rule, not the ORB).
	e.State.ORB = ORB{Day: dayStartCT(cur.CloseTime + 1), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	now := cur.CloseTime + 1
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	intents := e.Tick(bars, now)
	// sanity: this ISB has NO location — the old gate would refuse it here.
	levels := Levels(bars, cfg, now)
	if ok, _ := LocationVerdict(cur, levels, e.State.Trigger, bars, cfg); ok {
		t.Fatalf("fixture must be mid-air (no location); got %v", ok)
	}
	for _, in := range intents {
		if in.Action == PlaceStopLimitEntry {
			return // the ISB emitted without any location — ruling honored
		}
	}
	t.Fatalf("a mid-air ISB must emit (no location gate); got %+v", intents)
}

// TestISBBoxGatesTheEvaluator — R5: while the 5m-ISB rest box stands, only a
// 1m ISB in the SAME direction as the 5m ISB may arm inside it; a 1m BODY
// closing outside deletes the box.
func TestISBBoxGatesTheEvaluator(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // item 25: this fixture pins the R5 box gate, not the room rule
	// a box standing with Dir long
	box := ISBBox{High: 103, Low: 97, Dir: SideLong, AtTime: auditMs(2026, 9, 15, 9, 0, 0)}

	newE := func(esc Side) *Evaluator {
		e := New(cfg)
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
		e.State.ISBBox = &box
		e.State.ORB = ORB{Day: dayStartCT(auditMs(2026, 9, 15, 9, 0, 0)), High: 90, Low: 85, Drawn: true, Escaped: esc}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(auditMs(2026, 9, 15, 9, 0, 0)).In(ctime())), Verdict: DayTrade}
		return e
	}
	tick := func(e *Evaluator, bars []market.Kline) []Intent {
		return e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	}
	// a head bar with a high close so the EMA 34 target sits BEYOND the entry
	// price (the setup must carry a target: §6 "TARGET LÀ VỀ NHỮNG LEVEL KẾ
	// TIẾP").
	head := rthBars(0, 121, 121.5, 120.5, 120)

	t.Run("same-direction ISB allowed", func(t *testing.T) {
		// candle-1 green → ISB direction long == box.Dir
		prev := rthBars(1, 98, 102.9, 97.5, 99)
		cur := rthBars(2, 99.5, 101, 98, 100)
		if !IsISB(prev, cur) || ISBDirection(prev) != SideLong {
			t.Fatal("fixture: green candle-1 ISB")
		}
		ints := tick(newE(SideLong), []market.Kline{head, prev, cur})
		for _, in := range ints {
			if in.Action == PlaceStopLimitEntry {
				return
			}
		}
		t.Fatalf("a same-direction 1m ISB must arm inside the box: %+v", ints)
	})
	t.Run("opposite-direction ISB refused", func(t *testing.T) {
		// candle-1 red → ISB direction short, against the box. The HTF is set
		// SHORT so the R5 box gate is the ONLY blocker — removing it must let
		// the entry through (the mutant goes RED on this subtest).
		prev := rthBars(1, 100, 102.9, 97.5, 99)
		cur := rthBars(2, 99.5, 101, 98, 100)
		if ISBDirection(prev) != SideShort {
			t.Fatal("fixture: red candle-1")
		}
		e := newE(SideShort)
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 90}}
		ints := tick(e, []market.Kline{head, prev, cur})
		for _, in := range ints {
			if in.Action == PlaceStopLimitEntry {
				t.Fatalf("an opposite-direction ISB must not arm inside the box: %+v", in)
			}
		}
	})
	t.Run("1m body close outside deletes the box", func(t *testing.T) {
		// cur closes with its BODY above the box top (103) → escape → delete
		prev := rthBars(1, 98, 102.9, 97.5, 99)
		cur := rthBars(2, 104.5, 105, 104, 105)
		e := newE(SideLong)
		tick(e, []market.Kline{head, prev, cur})
		if e.State.ISBBox != nil {
			t.Fatalf("a 1m BODY close outside must delete the box; box=%+v", e.State.ISBBox)
		}
	})
}

// TestMidRangeBoxed — MID-RANGE ban via boxes (CTO 1791003862333): between an
// FTGL below and an FTGH above there is NO PHL, NO PLH, regardless of width.
func TestMidRangeBoxed(t *testing.T) {
	boxes := []Box{{Kind: FTGL, Top: 90, Bottom: 80}, {Kind: FTGH, Top: 120, Bottom: 110}}
	if !midRangeBoxed(boxes, 100) {
		t.Fatal("price between a floor box and a ceiling box is mid-range")
	}
	if midRangeBoxed(boxes, 85) || midRangeBoxed(boxes, 115) {
		t.Fatal("outside the two boxes is not mid-range")
	}
	if midRangeBoxed([]Box{{Kind: FTGL, Top: 90, Bottom: 80}}, 100) {
		t.Fatal("one box alone cannot make a mid-range")
	}
}

// TestTouchesOldExtremeFlag — an ISB at an old high/low is flagged
// "isb_at_old_extreme" for the injector's size tier (written rule 2, D4.1 p1).
func TestTouchesOldExtremeFlag(t *testing.T) {
	cur := market.Kline{High: 101, Low: 99}
	if !touchesOldExtreme(cur, []Level{{Kind: KindOldExtreme, Price: 100.5}}) {
		t.Fatal("a candle whose range reaches an old extreme ±2 must flag")
	}
	if touchesOldExtreme(cur, []Level{{Kind: KindOldExtreme, Price: 104}}) {
		t.Fatal("an old extreme 3 pts away must not flag")
	}
	if touchesOldExtreme(cur, []Level{{Kind: KindKeyLevel, Price: 100}}) {
		t.Fatal("only OLD EXTREMES flag, not key levels")
	}
}

// TestISBFlags — the ISB size flags: rule 2 (at an old high/low) and rule 3
// (in a range — the same mid-range test as the PHL/PLH ban) [D4.1 p1
// @ 08:05/09:40].
func TestISBFlags(t *testing.T) {
	cur := market.Kline{High: 101, Low: 99, Close: 100}
	extreme := []Level{{Kind: KindOldExtreme, Price: 100.5}}
	rangeBoxes := []Box{{Kind: FTGL, Top: 90, Bottom: 80}, {Kind: FTGH, Top: 120, Bottom: 110}}
	if f := isbFlags(cur, nil, nil); f != "" {
		t.Fatalf("no flags expected, got %q", f)
	}
	if f := isbFlags(cur, extreme, nil); f != "isb_at_old_extreme" {
		t.Fatalf("flags = %q, want isb_at_old_extreme", f)
	}
	if f := isbFlags(cur, nil, rangeBoxes); f != "isb_in_range" {
		t.Fatalf("flags = %q, want isb_in_range", f)
	}
	if f := isbFlags(cur, extreme, rangeBoxes); f != "isb_at_old_extreme|isb_in_range" {
		t.Fatalf("flags = %q, want both joined", f)
	}
}

// The flag strings are the contract with the trader's sizing call site
// (trader.mentorSizeFor reads them through HasFlag): a rename on one side only
// silently turns the written size cuts off again.
func TestISBFlagContractWithTrader(t *testing.T) {
	if FlagISBAtOldExtreme != "isb_at_old_extreme" || FlagISBInRange != "isb_in_range" {
		t.Fatalf("flag strings changed: %q %q", FlagISBAtOldExtreme, FlagISBInRange)
	}
	both := FlagISBAtOldExtreme + "|" + FlagISBInRange
	for _, tc := range []struct {
		flag, want string
		has        bool
	}{
		{both, FlagISBAtOldExtreme, true},
		{both, FlagISBInRange, true},
		{FlagISBInRange, FlagISBAtOldExtreme, false},
		{"", FlagISBInRange, false},
		{"x_isb_in_range", FlagISBInRange, false}, // exact match only
	} {
		if got := HasFlag(tc.flag, tc.want); got != tc.has {
			t.Fatalf("HasFlag(%q,%q) = %v, want %v", tc.flag, tc.want, got, tc.has)
		}
	}
}
