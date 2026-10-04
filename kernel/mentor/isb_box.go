package mentor

import "vl/market"

// R5 — the 5m ISB rest box (RULES-FIX-v3): box the latest 5m inside-bar candle
// and extend it right. While it stands, use the box, not the trigger line
// [D3.4 p2 @ 00:14–13:00; D4.1 p1 @ 21:38].
//
// Nothing trades inside it except a 1m ISB in the SAME direction as the 5m ISB
// (the R1 direction: the colour of the 5m candle BEFORE the inside bar).
// Escape = a 1m candle closes with its BODY outside the box; then trade the
// escape direction (ISB or PHL/PLH — the evaluator's choice) and DELETE the
// box [D3.4 p2 @ 12:02]. No per-touch weight change belongs here: a level only
// ever gets WEAKER with each test (deck slide 17; CTO 2026-10-03).

// ISBBox is one standing 5m ISB rest box.
type ISBBox struct {
	High   float64 // the 5m ISB candle's wick high (the box top)
	Low    float64 // the 5m ISB candle's wick low (the box bottom)
	Dir    Side    // the 5m ISB's R1 direction (candle-1 colour) [D1.4 p1 @ 09:20–10:20]
	AtTime int64   // the 5m ISB candle's open time
}

// ISBBoxFrom5m reports whether (prev5m, cur5m) form a 5m ISB and, if so, boxes
// cur5m (the inside-bar candle itself, wicks included) [D3.4 p2 @ 00:14].
func ISBBoxFrom5m(prev5m, cur5m market.Kline) (ISBBox, bool) {
	if !IsISB(prev5m, cur5m) {
		return ISBBox{}, false
	}
	return ISBBox{
		High:   cur5m.High,
		Low:    cur5m.Low,
		Dir:    ISBDirection(prev5m),
		AtTime: cur5m.OpenTime,
	}, true
}

// ISBBoxAllows answers the R5 trade rule for a candidate 1m ISB while the box
// stands: allowed only when the pair IS an ISB AND its R1 direction equals the
// 5m ISB's direction [D3.4 p2 @ 00:14–13:00].
func ISBBoxAllows(box ISBBox, prev1m, cur1m market.Kline) (allowed bool, reason string) {
	if !IsISB(prev1m, cur1m) {
		return false, "R5 box: not a 1m ISB [D3.4 p2 @ 00:14]"
	}
	if ISBDirection(prev1m) != box.Dir {
		return false, "R5 box: a 1m ISB inside the box must match the 5m ISB direction [D3.4 p2 @ 00:14–13:00]"
	}
	return true, ""
}

// ISBBoxEscape reports whether bar1m closes with its BODY outside the box and,
// if so, the escape direction (above → long, below → short). On escape the box
// is deleted [D3.4 p2 @ 12:02].
func ISBBoxEscape(box ISBBox, bar1m market.Kline) (escaped bool, dir Side) {
	if bar1m.Close > box.High {
		return true, SideLong
	}
	if bar1m.Close < box.Low {
		return true, SideShort
	}
	return false, ""
}

// mtfConflict is the D4.2-03 PERSISTENT 15m/5m conflict: BOTH the 5m and the
// 15m ISB boxes stand with OPPOSITE directions → "làm ơn đừng trade luôn" — no
// trade at all until one of the two boxes escapes [D4.2 p1 @13:59–14:53,
// @07:37–08:00]. Unlike the old one-shot pair veto it is a standing STATE, so
// it refuses every setup (ISB, PHL/PLH, box returns) for as long as the two
// boxes disagree.
func mtfConflict(box5, box15 *ISBBox) bool {
	return box5 != nil && box15 != nil && box5.Dir != box15.Dir
}

// crossingISBs reports whether the last `lookback` CLOSED buckets hold 2 ISBs
// with OPPOSITE directions — the choppy read that makes that TF invalid and
// escalates the read one TF up [D3.4 p2 @17:07–17:49]. Conservative reading
// (mentor question open, Q10): lookback 3 buckets, 2 crossing ISBs.
func crossingISBs(buckets []market.Kline, lookback int) bool {
	if len(buckets) < lookback {
		return false
	}
	window := buckets[len(buckets)-lookback:]
	var dirs []Side
	for i := 1; i < len(window); i++ {
		if IsISB(window[i-1], window[i]) {
			dirs = append(dirs, ISBDirection(window[i-1]))
		}
	}
	if len(dirs) < 2 {
		return false
	}
	return dirs[0] != dirs[len(dirs)-1]
}
