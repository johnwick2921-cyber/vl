package mentor

import (
	"testing"

	"vl/market"
)

// R5 tests — 00-METHOD §2.1 / RULES-FIX-v3: "A 5-minute ISB means REST. Draw a
// BOX around it, extend right" [D3.4 p2 @ 00:14]; nothing trades inside except
// a 1m ISB in the SAME direction as the 5m ISB [D3.4 p2 @ 00:14–13:00]; escape
// = a 1m candle closes with its BODY outside the box, then the box is deleted
// [D3.4 p2 @ 12:02].

func TestISBBoxFormsFromThe5mISBCandle(t *testing.T) {
	// green candle 1, its inside bar is candle 2 (red body inside the full
	// range) → box wraps candle 2's wicks, direction LONG (candle-1 colour).
	prev := market.Kline{Open: 100, Close: 106, High: 107, Low: 99}
	cur := market.Kline{Open: 104, Close: 102, High: 106.5, Low: 100.5, OpenTime: 999}
	box, ok := ISBBoxFrom5m(prev, cur)
	if !ok {
		t.Fatal("a 5m ISB must form a box [D3.4 p2 @ 00:14]")
	}
	if box.High != 106.5 || box.Low != 100.5 || box.Dir != SideLong {
		t.Fatalf("box = %+v, want {106.5 100.5 long}", box)
	}
	// not an ISB → no box
	notISB := market.Kline{Open: 104, Close: 108, High: 108.5, Low: 103}
	if _, ok := ISBBoxFrom5m(prev, notISB); ok {
		t.Fatal("no ISB, no box")
	}
}

func TestISBBoxAllowsOnlySameDirection1mISB(t *testing.T) {
	box := ISBBox{High: 110, Low: 90, Dir: SideLong, AtTime: 1}
	// 1m ISB whose candle 1 is green → allowed
	greenPrev := market.Kline{Open: 100, Close: 105, High: 108, Low: 95}
	inside := market.Kline{Open: 101, Close: 104, High: 108, Low: 95}
	if allowed, _ := ISBBoxAllows(box, greenPrev, inside); !allowed {
		t.Fatal("a same-direction 1m ISB trades inside the box [D3.4 p2 @ 00:14–13:00]")
	}
	// candle 1 red → the 1m ISB points short → banned inside a long box
	redPrev := market.Kline{Open: 105, Close: 100, High: 108, Low: 95}
	if allowed, reason := ISBBoxAllows(box, redPrev, inside); allowed || reason == "" {
		t.Fatalf("an opposite-direction 1m ISB must be banned inside the box; got allowed=%v reason=%q", allowed, reason)
	}
	// not an ISB at all → not allowed
	if allowed, _ := ISBBoxAllows(box, greenPrev, market.Kline{Open: 101, Close: 110, High: 110.5, Low: 100}); allowed {
		t.Fatal("a non-ISB pair is not the box exception")
	}
}

func TestISBBoxEscapeDeletesTheBox(t *testing.T) {
	box := ISBBox{High: 110, Low: 90, Dir: SideShort}
	// body (close) above the box top → escape long, box deleted [D3.4 p2 @ 12:02]
	if escaped, dir := ISBBoxEscape(box, market.Kline{Close: 111}); !escaped || dir != SideLong {
		t.Fatalf("body close above the box must escape long; escaped=%v dir=%q", escaped, dir)
	}
	if escaped, dir := ISBBoxEscape(box, market.Kline{Close: 89}); !escaped || dir != SideShort {
		t.Fatalf("body close below the box must escape short; escaped=%v dir=%q", escaped, dir)
	}
	// close inside → the box stands
	if escaped, _ := ISBBoxEscape(box, market.Kline{Close: 100}); escaped {
		t.Fatal("a close inside the box is not an escape")
	}
	// a WICK outside is not an escape — the BODY must close out [D3.4 p2 @ 12:02]
	if escaped, _ := ISBBoxEscape(box, market.Kline{Close: 109, High: 115, Low: 88}); escaped {
		t.Fatal("a wick outside with the body inside is not an escape")
	}
}
