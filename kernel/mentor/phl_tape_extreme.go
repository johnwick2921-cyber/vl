package mentor

import (
	"fmt"

	"vl/market"
)

// PHL-1 (phl audit, 2026-11-10): the PHL/PLH "old high / old low" is the 1m
// swing the pullback started from — the extreme made minutes before the
// pullback, read on the trading chart — not a confirmed 5m/15m fractal pivot.
//
// Course: "Target của mình sẽ là ở gần đỉnh cũ" [OctD2.3-R30, Oct D2.3 @ 26:50];
// "Target về gần đáy cũ" [OctD2.3-R42 @ 41:23]; "mình đang entry ở cái target
// mà làm gì có target ... Tại nó không cấu tạo được một setup" [OctD2.3-R53 @
// 54:00]. In EVERY worked example the old extreme is the 1m swing made just
// before the pullback [A, frames + DB bars]:
//
//	WE6 Tue 10-27  old high 09:36 H 29,673.50 (red line 29,672.75→29,673.25 @ 26:23)
//	WE7 Fri 10-30  old low  13:21 L 28,288.75 (white dashed 28,288.00 @ 41:23)
//	WE9 Thu 10-29  old low  20:53 L 28,825.00 (green target 28,830.50 @ 56:41)
//	WE10 Thu 10-29 old low  19:48 L 28,958.75 (sell stop 28,980 filled 19:53 @ 1:07:47)
//
// The swing width k is NOT stated by the course. It ships as a knob
// (Config.PHLOldExtreme1mK, risk_control.mentor_tuning.phl_old_extreme_1m_k).
// Its default 4 is the ONLY width that reproduces all four worked examples on
// the real bars [A, TestPHL1CourseWorkedExamples]: k = 2 or 3 makes the 10-27
// 09:40 lower high 29,662.00 the old high (target 29,657 against a 12.5-pt
// risk — the 1:1 floor refuses the mentor's trade), and k = 5 confirms the
// 10-30 13:21 and 10-29 19:48 lows one candle AFTER the mentor's fill.
// k <= 0 restores the pre-PHL-1 source (the 5m/15m pivots of the level set).

// PHLOldExtreme1mKDefault is the default 1m swing width (see above).
const PHLOldExtreme1mKDefault = 4

// PHLOldExtreme1mKMin / Max bound the stored knob.
const (
	PHLOldExtreme1mKMin = 1
	PHLOldExtreme1mKMax = 10
)

// phlTapeOldExtreme returns the old extreme the pullback at bars[r] (the
// reference candle's LAST 1m bar) started from, read from the 1m tape:
//
//   - the candidate is a bar i <= r whose extreme beats each of the k bars to
//     its LEFT strictly (a new high / new low on the trading chart), and that
//     no later bar up to r traded beyond (a touch AT the price is not a
//     break — oldExtremeBroken's rule);
//   - the MOST RECENT such bar is the extreme the pullback started from. It is
//     also the nearest unbroken one: every older unbroken extreme lies beyond it;
//   - it is CONFIRMED once k bars stand to its right (the fractal's right
//     side IS the pullback). Fewer → pending=true, found=false: the old
//     extreme exists but is not read yet, and an OLDER, farther pivot is never
//     substituted (that was the stale far target of PHL-1);
//   - it must lie strictly beyond tref on the target side (a long's old high
//     above the reference candle's high, mirror short); one that does not is
//     no target (found=false, pending=false).
func phlTapeOldExtreme(bars []market.Kline, r int, tref float64, side Side, k int) (ex oldExtreme, found, pending bool) {
	if k <= 0 || r < 0 || r >= len(bars) {
		return oldExtreme{}, false, false
	}
	isHigh := side == SideLong
	px := func(i int) float64 {
		if isHigh {
			return bars[i].High
		}
		return bars[i].Low
	}
	beyond := func(a, b float64) bool { // a strictly beyond b on the extreme's side
		if isHigh {
			return a > b
		}
		return a < b
	}
	var after float64 // the most extreme price of bars (i, r]
	haveAfter := false
	for i := r; i >= k; i-- {
		p := px(i)
		// Unbroken: no bar of (i, r] traded strictly beyond it.
		if !haveAfter || !beyond(after, p) {
			left := true
			for j := i - k; j < i; j++ {
				if !beyond(p, px(j)) {
					left = false
					break
				}
			}
			if left {
				if r-i < k {
					return oldExtreme{}, false, true
				}
				if !beyond(p, tref) {
					return oldExtreme{}, false, false
				}
				role := OldLow
				if isHigh {
					role = OldHigh
				}
				return oldExtreme{
					level: Level{
						Key:    fmt.Sprintf("%s:%.2f", KindOldExtreme, p),
						Kind:   KindOldExtreme,
						Price:  p,
						AtTime: bars[i].CloseTime,
						Role:   role,
					},
					idx:    i,
					isHigh: isHigh,
				}, true, false
			}
		}
		if !haveAfter || beyond(p, after) {
			after = p
			haveAfter = true
		}
	}
	return oldExtreme{}, false, false
}

// rollingRecordOncePerCandle names a rolling-ladder event in the refusal
// ledger once per exec candle — the same key rollingSkipRung counts under, so
// a candle is never counted twice for one name (PHL-5: "counters record, never
// infer").
func (e *Evaluator) rollingRecordOncePerCandle(execCur market.Kline, name string) {
	e.refuseOncePerVisit(fmt.Sprintf("%s@%d", rollingLevelKey, execCur.CloseTime), name)
}

// rollingSilentCancelSeam restores the pre-PHL-5 silent cancels (no refusal
// recorded for "no old extreme on the side" / "a level touch owns the entry")
// for the frozen tick-identity goldens recorded before the phl audit.
var rollingSilentCancelSeam = false
