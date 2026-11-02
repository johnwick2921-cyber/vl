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

// MTFConfluenceStanding is D4.2-R17 (m465 P1, release #60): mode C holds while
// the 15m AND the 5m both say `side` — "tại sao hồi sáng em lại không move stop
// loss mà em đặt stop loss ở dưới để cho nó chạy, tại vì đây nó cộng hưởng quá
// nhiều thứ... Khung 15 phút nó cũng kêu mình làm vậy, khung 5 phút cũng kêu
// mình làm vậy" [D4.2 p1 @14:57–15:37]; WE-2: the 15m box 29,079.25/28,985.00,
// the 5m ISB box 29,048.50/29,017.25, the 1m entry ~29,040, ran ~+100.
//
// It reads the STANDING boxes, not the latest candle pairs (MTFAligned lost the
// confluence one 5m candle after the 5m ISB while both boxes still stood):
//
//   - box15: a CLOSED (non-provisional) 15m ISB box with Dir == side. A
//     provisional box (forming15mISB) never counts — it only ever refuses; the
//     hold is a stronger claim than a refusal.
//   - the 5m agrees, read exactly as fifteenMinISBGate's require_5m_isb does:
//     a STANDING 5m box with Dir == side, OR the last 5m box — drawn at or after
//     the 15m inside candle opened — pointed `side` and escaped `side`.
func MTFConfluenceStanding(side Side, box5, box15 *ISBBox, last5mAt int64, last5mDir, last5mEscape Side) bool {
	if side != SideLong && side != SideShort {
		return false
	}
	if box15 == nil || box15.Provisional || box15.Dir != side {
		return false
	}
	if box5 != nil {
		return box5.Dir == side
	}
	return last5mAt >= box15.AtTime && last5mAt > 0 && last5mDir == side && last5mEscape == side
}

// mtfConfluence is mode C's MTF read for the evaluator, by the knob
// mentor_mtf_confluence_source (risk_control.mentor_tuning.mtf_confluence_source):
// "standing_boxes" (default) = MTFConfluenceStanding; "last_pair" = the
// pre-#60 MTFConfluence over the latest closed 5m/15m pairs.
func (e *Evaluator) mtfConfluence(side Side, bars5m, bars15m []market.Kline) bool {
	if e.Cfg.MTFConfluenceSource == MTFConfluenceLastPair {
		return MTFConfluence(side, bars5m, bars15m)
	}
	return MTFConfluenceStanding(side, e.State.ISBBox, e.State.ISBBox15m, e.State.LastISBBoxAt, e.State.LastISBBoxDir, e.State.LastISBBoxEscape)
}
