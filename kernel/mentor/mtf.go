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
// the 15m ISB, the 5m ISB and the entry side align (MTFAligned) AND the 5m
// trigger agrees with that side. Fail-closed: a silent trigger (empty
// direction) can never agree, and a trigger opposite the entry refuses.
func MTFConfluence(side Side, trig TriggerLine, entryPrice float64, bars5m, bars15m []market.Kline) bool {
	if !MTFAligned(bars5m, bars15m, side) {
		return false
	}
	if ok, trigSide, _ := TriggerVerdict(trig, entryPrice); !ok || trigSide == "" || trigSide != side {
		return false
	}
	return true
}
