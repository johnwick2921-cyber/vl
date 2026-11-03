package mentor

import "vl/market"

// ── The TOO-SMALL RANGE guard (m466 P1, release #61: G33) ───────────────────
//
// D4.2-R33 [D4.2 p2 @06:16–06:30]: "từ đây trên đây có 50-60 điểm, mà cây nến
// vừa vậy 30-40 điểm… bên trong một cái range nó quá nhỏ đi, đừng bao giờ đụng
// tới". R31 [@05:03–05:37] is the same idea for the ping-pong box (box_trade.go,
// unchanged); this is the GENERAL rule — any range too small for its candles.
//
// Pinned definition = the worked example (no number is ours): the video
// measures 61.50 pts (the 1m chart 09:44–09:59 — 15 closed 1m candles) and TWO
// candles inside it, 33.75 and 34.50 pts ("cây nến vừa vậy 30-40 điểm" — the
// candles ARE that size). So over the last smallRangeBars closed 1m candles,
// if the span (max High − min Low) is <= smallRangeMaxSpanPts AND at least
// smallRangeBigCandles of them range >= smallRangeBigCandlePts, the window is
// a too-small range: every intraday entry priced inside [min Low, max High] is
// refused range_too_small_big_candles (break_lock.go courseRangeFilter) until
// a closed 1m candle CLOSES outside it — the break test of D4.1-R34 [D4.1 p1
// @20:36–21:03]. Knob risk_control.mentor_tuning.mentor_small_range_guard
// (default ON); OFF never builds the state.
//
// Release #61 review round (two lenses' P2 + one P3, fix/m466-checklist-gaps):
//
//   - ONE big candle is not the rule. On 10-21 18:01–18:15 a 46.5-pt chop of
//     5–18-pt candles had a single 45.25-pt wick at 18:10 and the guard
//     refused the 18:15 ISB short (target reached 18:21). D4.2-R31 [@05:03–
//     05:37] calls teens-sized candles in a 50-pt range NORMAL; R33's example
//     has two 30-40 pt candles, so two are required (smallRangeBigCandles —
//     the video's count; how many the mentor means is a mentor question).
//   - The guard ENDS on the break close. Before, the clear was followed on the
//     same tick by a re-read of the window that now held the breakout candle,
//     so a still-small window re-latched a WIDENED range around the breakout
//     and refused the break-direction entry. Now a new range needs a window
//     built wholly from candles AFTER the break candle (SmallRangeBreakAt).
//   - A standing range is the box it latched; it does not slide with the
//     window (the old slide re-latched on every shift: range_small_set 426 =
//     range_small_cleared 426 over the month's replay).
//   - Review round 2: the window never latches on a candle that itself
//     closes outside the 14 before it — that candle is the break, not part of
//     a range, and is recorded as the break (smallRangeWindow breakout).
const (
	smallRangeBars         = 15    // 09:44–09:59 on the 1m chart [D4.2 p2 @06:16–06:30]
	smallRangeMaxSpanPts   = 61.50 // the measured range [D4.2 p2 @06:16–06:30]
	smallRangeBigCandlePts = 30.00 // "cây nến vừa vậy 30-40 điểm" (measured 33.75 / 34.50)
	smallRangeBigCandles   = 2     // the two candles the video measures (33.75 and 34.50)
)

// SmallRange is one standing too-small range.
type SmallRange struct {
	Lo float64 `json:"lo"` // min Low of the window
	Hi float64 `json:"hi"` // max High of the window
	At int64   `json:"at"` // the OpenTime of the 1m candle that latched it
}

// smallRangeWindow reads the last smallRangeBars closed 1m candles: ok when
// they span <= smallRangeMaxSpanPts with at least smallRangeBigCandles
// candles >= smallRangeBigCandlePts. breakout reports a window that WOULD
// qualify but whose newest candle closes outside the range of the candles
// before it (release #61 review round 2, mtf-loss lens P2): that candle is
// the D4.1-R34 break [D4.1 p1 @20:36–21:03], not part of a range — the
// guard treats it exactly like the close that ends a standing range. Without
// this a tight chop plus one >= 30-pt breakout candle (the second big
// candle) latched on the breakout and refused the pullback setups
// D4.1-R31/R33 [D4.1 p1 @18:29–20:33] say to wait for after a break.
func smallRangeWindow(bars []market.Kline) (lo, hi float64, ok, breakout bool) {
	if len(bars) < smallRangeBars {
		return 0, 0, false, false
	}
	w := bars[len(bars)-smallRangeBars:]
	lo, hi = w[0].Low, w[0].High
	plo, phi := lo, hi // the range of every candle but the newest
	big := 0
	for i, b := range w {
		if i == len(w)-1 {
			plo, phi = lo, hi
		}
		if b.Low < lo {
			lo = b.Low
		}
		if b.High > hi {
			hi = b.High
		}
		if b.High-b.Low >= smallRangeBigCandlePts {
			big++
		}
	}
	if hi-lo > smallRangeMaxSpanPts || big < smallRangeBigCandles {
		return 0, 0, false, false
	}
	if c := w[len(w)-1].Close; c < plo || c > phi {
		return 0, 0, false, true
	}
	return lo, hi, true, false
}

// advanceSmallRange steps the guard on the newest closed 1m candle. A standing
// range is kept as latched until a 1m close outside it, which clears it
// (counted range_small_cleared) and records the break candle — nothing more
// happens on that tick. With no range standing, a qualifying window latches
// (counted range_small_set) only when every candle in it opened AFTER the last
// break candle; a window that would qualify but whose newest candle is itself
// the break (smallRangeWindow breakout) latches nothing and records that
// candle as the break (counted range_small_breakout).
func (e *Evaluator) advanceSmallRange(bars []market.Kline) {
	if !e.Cfg.SmallRangeGuard || len(bars) == 0 {
		e.State.SmallRange, e.State.SmallRangeBreakAt = nil, 0
		return
	}
	cur := bars[len(bars)-1]
	if r := e.State.SmallRange; r != nil {
		if cur.Close < r.Lo || cur.Close > r.Hi {
			e.State.SmallRange = nil
			e.State.SmallRangeBreakAt = cur.OpenTime
			e.count("range_small_cleared")
			e.note("too-small range %.2f–%.2f ended: the 1m close %.2f at %s CT is outside it [D4.1-R34 p1 @20:36–21:03]",
				r.Lo, r.Hi, cur.Close, swingCT(cur.OpenTime))
		}
		return
	}
	lo, hi, ok, breakout := smallRangeWindow(bars)
	if !ok && !breakout || bars[len(bars)-smallRangeBars].OpenTime <= e.State.SmallRangeBreakAt {
		return
	}
	if breakout {
		e.State.SmallRangeBreakAt = cur.OpenTime
		e.count("range_small_breakout")
		e.note("no too-small range: the 1m close %.2f at %s CT breaks the window it would close — a break, not a range [D4.1-R34 p1 @20:36–21:03]",
			cur.Close, swingCT(cur.OpenTime))
		return
	}
	e.State.SmallRange = &SmallRange{Lo: lo, Hi: hi, At: cur.OpenTime}
	e.count("range_small_set")
	e.note("too-small range %.2f–%.2f (%.2f pts over %d 1m candles, %d+ candles >= %.2f pts) — no entry inside until a 1m close outside [D4.2-R33 p2 @06:16–06:30]",
		lo, hi, hi-lo, smallRangeBars, smallRangeBigCandles, smallRangeBigCandlePts)
}

// smallRangeRefuses reports whether an entry is priced inside the standing
// too-small range.
func smallRangeRefuses(r *SmallRange, in Intent) bool {
	return r != nil && in.Price >= r.Lo && in.Price <= r.Hi
}
