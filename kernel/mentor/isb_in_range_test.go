package mentor

import (
	"testing"

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
