package mentor

import (
	"testing"

	"vl/market"
)

// Release #52 W5 — swing 4h ISB alignment with the course.
//
// A6 [D5.2 p2 @20:41-21:04, p1 @19:51-20:31]: once the first touching candle
// closed through, the level is INVALID — "chỉ đi kiếm Insaba", never a normal
// reject / PHL / PLH at it again. An ISB fire must not erase that marker.
//
// A7 [D1.4 p1 @09:20-09:36, D5.2 p1 @21:25-21:40, p2 @10:23-10:51]: candle 1's
// colour sets the ISB side (red short, green long), and the swing pair is
// "close back, THEN wait one more 5m beat for the inside bar".
//
// Every test drives SwingTick, the production entry the Evaluator calls
// (eval.go runSwing); the tape is the shared swingTape (line 10168.16).

func w5Entries(out []Intent) (entries, cancels int) {
	for _, in := range out {
		switch in.Action {
		case PlaceStopEntry:
			entries++
		case CancelArm:
			cancels++
		}
	}
	return entries, cancels
}

// shortISBTape: approach from BELOW the line (short side). The through candle
// closes above; candle 1 closes back below; the ISB follows.
func w5ShortTape(t *testing.T, candle1, isb market.Kline) []market.Kline {
	t.Helper()
	return swingTape(t, []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),     // prev: below the line
		mk5m(t, 15, 5, 5, 10155, 10220, 10150, 10220), // touches, closes THROUGH (above)
		candle1,
		isb,
	})
}

// A7: the close-back bar may not be its own inside bar — the pair needs one
// more 5m beat. Old code fired on the close-back bar (inside the through
// candle's range); the course waits for candle 1 to close back first.
func TestSwing4hW5ISBNotOnTheCloseBackBar(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
		mk5m(t, 15, 5, 5, 10155, 10220, 10150, 10220),  // through
		mk5m(t, 15, 5, 10, 10175, 10180, 10150, 10160), // closes back AND sits inside the through candle (red)
	}
	s := &SwingState{}
	out := SwingTick(s, swingTape(t, cur), cfg, cur[2].OpenTime+60_000)
	if len(out) != 0 {
		t.Fatalf("the close-back bar fired as its own inside bar one beat early: %+v", out)
	}
	// ...and the NEXT beat, an inside bar of that red close-back candle, fires.
	cur = append(cur, mk5m(t, 15, 5, 15, 10165, 10167, 10150, 10160))
	out = SwingTick(s, swingTape(t, cur), cfg, cur[3].OpenTime+60_000)
	if e, _ := w5Entries(out); e != 1 || out[0].Side != SideShort {
		t.Fatalf("want one SHORT ISB entry on the beat after the close-back, got %+v", out)
	}
}

// A7: a GREEN candle 1 is a LONG ISB by the course; at a resistance-reject
// (short) swing it is the wrong colour pair and must not enter.
func TestSwing4hW5ISBWrongColourCandle1Refused(t *testing.T) {
	cfg := DefaultSwingCfg()
	green := mk5m(t, 15, 5, 10, 10152, 10180, 10150, 10160) // green, closes back below the line
	isb := mk5m(t, 15, 5, 15, 10155, 10158, 10152, 10157)   // inside the green candle
	s := &SwingState{}
	out := SwingTick(s, w5ShortTape(t, green, isb), cfg, isb.OpenTime+60_000)
	if len(out) != 0 {
		t.Fatalf("a GREEN candle 1 must not give a SHORT swing ISB: %+v", out)
	}
	// mirror: support (long) swing, RED candle 1 -> refused
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10220, 10190, 10210),  // prev above the line
		mk5m(t, 15, 5, 5, 10200, 10200, 10140, 10150),  // touches, closes THROUGH (below)
		mk5m(t, 15, 5, 10, 10190, 10195, 10140, 10180), // RED? open 10190 close 10180: red, closes back above the line
		mk5m(t, 15, 5, 15, 10185, 10190, 10170, 10182), // inside bar of the red candle
	}
	s2 := &SwingState{}
	out = SwingTick(s2, swingTape(t, cur), cfg, cur[3].OpenTime+60_000)
	if len(out) != 0 {
		t.Fatalf("a RED candle 1 must not give a LONG swing ISB: %+v", out)
	}
}

// A6: after the ISB fired, the level stays INVALID — a later wick-touch that
// closes back must NOT arm an ordinary reject (nor cancel the resting ISB
// order) inside the same 4h candle. Mutant: restore `s.FirstTouch = nil` on
// the fire -> the 05:20 touch arms a reject + supersede-cancel -> RED.
func TestSwing4hW5ISBFireKeepsThroughLatched(t *testing.T) {
	cfg := DefaultSwingCfg()
	red := mk5m(t, 15, 5, 10, 10175, 10180, 10150, 10160)
	isb := mk5m(t, 15, 5, 15, 10165, 10167, 10150, 10160)
	bars := w5ShortTape(t, red, isb)
	// a later wick-touch of the line that closes back below (a normal reject
	// shape) — price never trades through the 10150 ISB entry.
	touch := mk5m(t, 15, 5, 20, 10160, 10172, 10155, 10158)
	bars = append(bars, touch)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, touch.OpenTime+60_000)
	entries, cancels := w5Entries(out)
	if entries != 1 || cancels != 0 {
		t.Fatalf("want exactly the ISB entry and no reject re-arm / cancel after it; entries=%d cancels=%d %+v", entries, cancels, out)
	}
	if s.FirstTouch == nil || !s.FirstTouch.Through || !s.FirstTouch.ISBFired {
		t.Fatalf("the ISB fire must keep Through latched and mark ISBFired: %+v", s.FirstTouch)
	}
}

// A6: the ISB does not re-arm each bar — a second red close-back + inside-bar
// pair in the same invalid 4h candle enters nothing more. Mutant: drop the
// !ISBFired term -> a second entry -> RED.
func TestSwing4hW5ISBDoesNotRearm(t *testing.T) {
	cfg := DefaultSwingCfg()
	red := mk5m(t, 15, 5, 10, 10175, 10180, 10150, 10160)
	isb := mk5m(t, 15, 5, 15, 10165, 10167, 10150, 10160)
	bars := w5ShortTape(t, red, isb)
	bars = append(bars,
		mk5m(t, 15, 5, 20, 10175, 10180, 10152, 10158), // another red close-back candle
		mk5m(t, 15, 5, 25, 10165, 10167, 10153, 10159), // and its inside bar
	)
	s := &SwingState{}
	out := SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+60_000)
	if entries, _ := w5Entries(out); entries != 1 {
		t.Fatalf("the ISB must fire once per invalid level, got %d entries: %+v", entries, out)
	}
}

// A7: candle 1 must itself have CLOSED BACK across the line. A red candle that
// still closes beyond the line (above, for a short) is not the close-back
// candle, even if the next bar is inside it and closes back. Mutant: drop the
// closedBack(prev) term -> the ISB fires off a candle that never closed back
// -> RED.
func TestSwing4hW5ISBCandle1MustCloseBack(t *testing.T) {
	cfg := DefaultSwingCfg()
	red := mk5m(t, 15, 5, 10, 10200, 10205, 10150, 10180) // RED but closes ABOVE the 10168.16 line
	isb := mk5m(t, 15, 5, 15, 10175, 10176, 10160, 10162) // inside red; its own close is back below the line
	s := &SwingState{}
	out := SwingTick(s, w5ShortTape(t, red, isb), cfg, isb.OpenTime+60_000)
	if len(out) != 0 {
		t.Fatalf("candle 1 never closed back across the line; no ISB may fire: %+v", out)
	}
}
