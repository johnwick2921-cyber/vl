package mentor

import (
	"vl/market"
)

// ── MTF alignment → mode C (D4.2-05 / D4.2-06) ─────────────────────────────
// "Khung 15 phút đang là inside bar LONG… khung 5 phút CŨNG… khung 1 phút —
// TÔI CŨNG VÔ INSIDE BAR CHO LỆNH LONG" [D4.2 p1 @12:16–12:47]. When the 15m
// ISB, the 5m ISB and the entry all point the SAME way, many things resonate
// and mode C applies ("cộng hưởng quá nhiều thứ… khung 15 phút nó cũng kêu
// mình làm vậy, khung 5 phút nó cũng kêu mình làm vậy" [D4.2 p1 @14:57]).
// This is the same read ISBConflictVerdict uses (CLOSED buckets only, the
// candle-1 colour) — but it reports ALIGNMENT, not the opposite-direction
// veto.

// MTFAligned reports whether the latest CLOSED 5m ISB pair and the latest
// CLOSED 15m ISB pair both stand and both point `side` (15m dir = 5m dir =
// entry side). Both directions are the candle-1 colour (ISBDirection); a doji
// candle 1 is no ISB at all, so it can never align. Fewer than 2 bars on
// either TF → false (fail-closed).
func MTFAligned(bars5m, bars15m []market.Kline, side Side) bool {
	if side != SideLong && side != SideShort {
		return false
	}
	if len(bars5m) < 2 || len(bars15m) < 2 {
		return false
	}
	p5, c5 := bars5m[len(bars5m)-2], bars5m[len(bars5m)-1]
	p15, c15 := bars15m[len(bars15m)-2], bars15m[len(bars15m)-1]
	if !IsISB(p5, c5) || !IsISB(p15, c15) {
		return false
	}
	return ISBDirection(p5) == side && ISBDirection(p15) == side
}

// MTFConfluence is D4.2-06 part 1: mode C also fires on timeframe agreement —
// the 15m ISB, the 5m ISB and the entry side align (MTFAligned), nothing else.
//
// C52 W1 B9 (owner order 2026-10-26 "align everything to the course"): D4.2
// p1 @12:16–12:47 / @15:16–15:36 says the 15m, 5m and 1m inside bars agreeing
// is the whole reason to hold ("cộng hưởng quá nhiều thứ… khung 15 phút nó
// cũng kêu mình làm vậy, khung 5 phút nó cũng kêu mình làm vậy") and
// @14:30–14:52 says opposite higher frames = no trade. Neither D4.2 part
// mentions the 5m trigger line. The trigger conjunct that used to sit here was
// borrowed from the BOX confluence of D3.4 p3 @07:38 (ConfluenceVerdict), a
// different setup; it is dropped so the MTF rule is the D4.2 rule. An
// opposite trigger is still refused where it always was (the entry filters),
// not here.
func MTFConfluence(side Side, bars5m, bars15m []market.Kline) bool {
	return MTFAligned(bars5m, bars15m, side)
}
