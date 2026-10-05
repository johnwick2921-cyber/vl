package mentor

import (
	"testing"

	"vl/market"
)

// D4.2-07: the 30m ISB box (same gate as the 15m box) + DAY-3 row 63
// escalation ladder (crossing ISBs make a TF invalid → read the next TF up).

func TestCrossingISBs(t *testing.T) {
	mk := func(o, h, l, c float64) market.Kline {
		return market.Kline{Open: o, High: h, Low: l, Close: c}
	}
	// Nested inside bars: A (red mother) ⊃ B (green, inside A) ⊃ C (inside B).
	// IsISB(A,B)=short, IsISB(B,C)=long → two crossing ISBs.
	crossing := []market.Kline{
		mk(110, 112, 100, 101), // A mother, red → short
		mk(101, 108, 103, 107), // B inside A, green → long
		mk(106, 107, 104, 105), // C inside B
	}
	if !crossingISBs(crossing, 3) {
		t.Fatal("two crossing ISBs within 3 buckets must read crossing")
	}
	// Same-direction: A (red mother) ⊃ B (red, inside A) ⊃ C (inside B) — both
	// ISBs short.
	same := []market.Kline{
		mk(110, 112, 100, 101),   // A mother, red
		mk(105, 108, 102, 104),   // B inside A, red → short
		mk(104, 107, 103, 103.5), // C inside B
	}
	if crossingISBs(same, 3) {
		t.Fatal("same-direction ISBs must NOT read crossing")
	}
	// Fewer than 2 ISBs → not crossing.
	if crossingISBs([]market.Kline{mk(110, 112, 100, 101), mk(101, 106, 100, 105)}, 3) {
		t.Fatal("a single ISB must NOT read crossing")
	}
	// Fewer than lookback buckets → not crossing.
	if crossingISBs(crossing[:2], 3) {
		t.Fatal("fewer than lookback buckets must NOT read crossing")
	}
}

// TestISB30mBoxBlocksCounterDirection — a LONG 1m ISB is refused while a SHORT
// 30m box stands (never trade against a 30m ISB inside its range). Mutant: drop
// the box30Blocked gate → RED.
func TestISB30mBoxBlocksCounterDirection(t *testing.T) {
	e, bars, now := mtfFixture(t, false) // long ISB
	e.State.ISBBox30m = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	if got := entryIntents(ins); len(got) != 0 {
		t.Fatalf("counter-direction ISB must be blocked by the 30m box, got %+v", got)
	}
	if e.State.Refusals["isb_box_blocked_30m"] == 0 {
		t.Fatalf("ledger = %v, want isb_box_blocked_30m counted", e.State.Refusals)
	}
}

// TestISB30mBoxAllowsSameDirection — a LONG 1m ISB fires while a LONG 30m box
// stands.
func TestISB30mBoxAllowsSameDirection(t *testing.T) {
	e, bars, now := mtfFixture(t, false) // long ISB
	e.State.ISBBox30m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	var longISB bool
	for _, in := range entryIntents(ins) {
		if in.Setup == "ISB" && in.Side == SideLong {
			longISB = true
		}
	}
	if !longISB {
		t.Fatalf("same-direction ISB must fire inside the 30m box; intents %+v refusals %v", ins, e.State.Refusals)
	}
}
