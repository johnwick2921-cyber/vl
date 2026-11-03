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
// measures 61.50 pts (the 1m chart 09:44–09:59 — 15 closed 1m candles) and a
// 33.75 / 34.50-pt candle inside it ("30-40 điểm"). So over the last
// smallRangeBars closed 1m candles, if the span (max High − min Low) is <=
// smallRangeMaxSpanPts AND the largest 1m candle range is >=
// smallRangeBigCandlePts, the window is a too-small range: every intraday
// entry priced inside [min Low, max High] is refused
// range_too_small_big_candles (break_lock.go courseRangeFilter) until a
// closed 1m candle CLOSES outside it — the break test of D4.1-R34 [D4.1 p1
// @20:36–21:03]. Knob risk_control.mentor_tuning.mentor_small_range_guard
// (default ON); OFF never builds the state.
const (
	smallRangeBars         = 15    // 09:44–09:59 on the 1m chart [D4.2 p2 @06:16–06:30]
	smallRangeMaxSpanPts   = 61.50 // the measured range [D4.2 p2 @06:16–06:30]
	smallRangeBigCandlePts = 30.00 // "cây nến vừa vậy 30-40 điểm" (measured 33.75 / 34.50)
)

// SmallRange is one standing too-small range.
type SmallRange struct {
	Lo float64 `json:"lo"` // min Low of the window
	Hi float64 `json:"hi"` // max High of the window
	At int64   `json:"at"` // the OpenTime of the 1m candle that latched it
}

// smallRangeWindow reads the last smallRangeBars closed 1m candles: ok when
// they span <= smallRangeMaxSpanPts with one candle >= smallRangeBigCandlePts.
func smallRangeWindow(bars []market.Kline) (lo, hi float64, ok bool) {
	if len(bars) < smallRangeBars {
		return 0, 0, false
	}
	w := bars[len(bars)-smallRangeBars:]
	lo, hi = w[0].Low, w[0].High
	big := 0.0
	for _, b := range w {
		if b.Low < lo {
			lo = b.Low
		}
		if b.High > hi {
			hi = b.High
		}
		if r := b.High - b.Low; r > big {
			big = r
		}
	}
	if hi-lo > smallRangeMaxSpanPts || big < smallRangeBigCandlePts {
		return 0, 0, false
	}
	return lo, hi, true
}

// advanceSmallRange steps the guard on the newest closed 1m candle: a close
// outside the standing range clears it (counted range_small_cleared); then
// the window is re-read and a qualifying window latches (counted
// range_small_set when none stood) or moves the standing range to it.
func (e *Evaluator) advanceSmallRange(bars []market.Kline) {
	if !e.Cfg.SmallRangeGuard || len(bars) == 0 {
		e.State.SmallRange = nil
		return
	}
	cur := bars[len(bars)-1]
	if r := e.State.SmallRange; r != nil && (cur.Close < r.Lo || cur.Close > r.Hi) {
		e.State.SmallRange = nil
		e.count("range_small_cleared")
	}
	lo, hi, ok := smallRangeWindow(bars)
	if !ok {
		return
	}
	if r := e.State.SmallRange; r != nil {
		if r.Lo != lo || r.Hi != hi {
			e.State.SmallRange = &SmallRange{Lo: lo, Hi: hi, At: cur.OpenTime}
		}
		return
	}
	e.State.SmallRange = &SmallRange{Lo: lo, Hi: hi, At: cur.OpenTime}
	e.count("range_small_set")
	e.note("too-small range %.2f–%.2f (%.2f pts over %d 1m candles, a candle >= %.2f pts) — no entry inside until a 1m close outside [D4.2-R33 p2 @06:16–06:30]",
		lo, hi, hi-lo, smallRangeBars, smallRangeBigCandlePts)
}

// smallRangeRefuses reports whether an entry is priced inside the standing
// too-small range.
func smallRangeRefuses(r *SmallRange, in Intent) bool {
	return r != nil && in.Price >= r.Lo && in.Price <= r.Hi
}
