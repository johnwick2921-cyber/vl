package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// ── item 21 (D4.1-06) — the ISB "in range" size cut is wider than
// midRangeBoxed: inside the standing 5m ISB box, between two key levels closer
// than the ping-pong minimum, and (once built) a 15m ISB range. "Khi trade isb
// in-range bắt buộc giảm size" [D4.1 p1 written rule 3]. ───────────────────────

func TestISBInRangeInsideStandingBox(t *testing.T) {
	box := &ISBBox{High: 100, Low: 90}
	if !isbInRange(95, nil, nil, box, 50) {
		t.Fatal("an ISB candle inside the standing 5m box must be in-range")
	}
	if !isbInRange(90, nil, nil, box, 50) || !isbInRange(100, nil, nil, box, 50) {
		t.Fatal("an ISB candle ON the box edge must be in-range")
	}
	if isbInRange(105, nil, nil, box, 50) {
		t.Fatal("an ISB candle above the box must NOT be in-range")
	}
	if isbInRange(95, nil, nil, nil, 50) {
		t.Fatal("no standing box → no box in-range")
	}
}

func TestISBInRangeNarrowKeyLevels(t *testing.T) {
	// price 100 between two key levels 30 pts apart (< 50) → in-range.
	narrow := []Level{{Key: "k1", Kind: KindKeyLevel, Price: 90}, {Key: "k2", Kind: KindKeyLevel, Price: 120}}
	if !isbInRange(100, narrow, nil, nil, 50) {
		t.Fatal("between two key levels closer than the ping-pong minimum must be in-range")
	}
	// 60 pts apart (>= 50) → not in-range.
	wide := []Level{{Key: "k1", Kind: KindKeyLevel, Price: 80}, {Key: "k2", Kind: KindKeyLevel, Price: 140}}
	if isbInRange(100, wide, nil, nil, 50) {
		t.Fatal("between two key levels >= the ping-pong minimum must NOT be in-range")
	}
	// a non-key level never forms the narrow-range pair.
	nonKey := []Level{{Key: "ema34", Kind: KindEMA34, Price: 90}, {Key: "k2", Kind: KindKeyLevel, Price: 120}}
	if isbInRange(100, nonKey, nil, nil, 50) {
		t.Fatal("a non-key level must not form the narrow-range pair")
	}
	// ping-pong min disabled (0) → never narrow-range.
	if inNarrowRange(100, narrow, 0) {
		t.Fatal("ping-pong min 0 must disable the narrow-range condition")
	}
}

// The production resolver: an ISB inside the standing 5m box carries isb_in_range
// even with no boxes and no old extreme.
func TestISBFlagsForWidensInRange(t *testing.T) {
	e := New(DefaultConfig())
	e.State.ISBBox = &ISBBox{High: 100, Low: 90}
	cur := market.Kline{High: 96, Low: 94, Close: 95}
	if f := e.isbFlagsFor(cur, nil, nil); f != FlagISBInRange {
		t.Fatalf("ISB inside the 5m box must carry isb_in_range, got %q", f)
	}
	// outside the box, no old extreme → no flags.
	e.State.ISBBox = &ISBBox{High: 100, Low: 90}
	cur2 := market.Kline{High: 106, Low: 104, Close: 105}
	if f := e.isbFlagsFor(cur2, nil, nil); f != "" {
		t.Fatalf("outside the box with no other range the flags must be empty, got %q", f)
	}
}

// TestISBInRangeFlagOnTick — item 21 (D4.1-06) Tick-level pin at the emit call
// site (`chosen.Flag = e.isbFlagsFor(cur, levels, boxes)`): an ISB whose candle
// sits inside the standing 5m ISB box emits with isb_in_range. The unit pin
// (TestISBFlagsForWidensInRange) exercises the resolver directly; this pin
// catches a revert of the CALL SITE to the old `isbFlags`. Mutant
// (`chosen.Flag = isbFlags(cur, levels, boxes)`) → RED: the ISB emits with an
// empty flag.
func TestISBInRangeFlagOnTick(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	box := ISBBox{High: 103, Low: 97, Dir: SideLong, AtTime: auditMs(2026, 9, 15, 9, 0, 0)}
	// a head bar with a high close so the EMA 34 target sits well beyond the
	// entry (the setup must carry a target: §6).
	head := rthBars(0, 121, 121.5, 120.5, 120)
	prev := rthBars(1, 98, 102.9, 97.5, 99) // green candle-1 → long, matches box.Dir
	cur := rthBars(2, 99.5, 101, 98, 100)   // the ISB: close 100 inside [97, 103]
	if !IsISB(prev, cur) || ISBDirection(prev) != SideLong {
		t.Fatal("fixture: green candle-1 ISB")
	}
	bars := []market.Kline{head, prev, cur}
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ISBBox = &box
	e.State.ORB = ORB{Day: dayStartCT(auditMs(2026, 9, 15, 9, 0, 0)), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(auditMs(2026, 9, 15, 9, 0, 0)).In(ctime())), Verdict: DayTrade}
	ins := e.Tick(bars, cur.CloseTime+1)
	for _, in := range ins {
		if in.Action == PlaceStopLimitEntry && in.Setup == "ISB" {
			if !HasFlag(in.Flag, FlagISBInRange) {
				t.Fatalf("an ISB inside the standing 5m box must emit with isb_in_range, got flag %q: %+v", in.Flag, in)
			}
			return
		}
	}
	t.Fatalf("the same-direction ISB must emit inside the box; got %+v", ins)
}
