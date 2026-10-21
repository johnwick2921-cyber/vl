package mentor

import (
	"vl/market"
)

// R7 — the reverse ISB at EMA 9 (RULES-FIX-v3, a setup behind its OWN knob
// ISBReverseEMA9Enabled, default ON since the 2026-10-04 owner ruling) [D5.4 p1 @01:26–01:30]:
//
//   - price must actually reach EMA 9 (a loose touch is OK);
//   - in an uptrend, an ISB that points SHORT at EMA 9 → go LONG with a buy
//     stop that lands ON THE WIRE at the ISB high + the buffer; downtrend
//     mirror: sell stop on the wire at low − the buffer;
//   - the entry buffer is MANDATORY on the 1m — "1 đến 1.5 điểm cho entry và
//     stoploss … bắt buộc … anh chị sẽ không thua, tại vì nó không có fill"
//     [D1.4 p1 @ 22:26–23:26]; the mentor's number is 1.5 pts — "1 điểm rưỡi
//     đi… đừng 1 điểm" [D2.1 p1 @ 06:12]. Without it the arm parks at the ISB's
//     exact extreme and fills on a 1-pt wick — trade #627 (R7, D5.4);
//   - the entry authored here IS the wire trigger: the executor adds NO
//     offset to a mentor row (trader/armed_executor.go stopEntryWireOffset —
//     the planner's E7 stop-entry offset is planner-only since rel22 P2-1), so
//     the entry is the extreme ± ISBBufferPts exactly, like the regular ISB.
//     (Until rel22 this file pre-subtracted the executor's 0.5 — a second
//     definition of the same number; trade #627/#628 ruling kept, one source.)
//   - cancel if the next candle does not fill (the evaluator runs the same
//     ISB stacking arithmetic on the emitted arm);
//   - stop = the other side of the ISB candle plus the buffer (already ±
//     ISBBufferPts; it is a stop-loss, not an entry, so the wire offset does
//     not apply).
//
// The 1m is preferred but any TF pair that satisfies IsISB qualifies — the
// caller passes the timeframe it trades.

// ReverseISBAtEMA9 evaluates one candidate ISB candle pair against the 1m EMA 9
// value and the bigger trend. ok=false carries a cited reason (knob off, not an
// ISB, no EMA-9 touch, or the ISB already points WITH the trend).
func ReverseISBAtEMA9(prev, cur market.Kline, ema9 float64, trend Side, cfg Config) (Intent, bool, string) {
	if !cfg.ISBReverseEMA9Enabled {
		return Intent{}, false, "reverse ISB at EMA 9: knob off [D5.4 p1 @01:26–01:30]"
	}
	if !IsISB(prev, cur) {
		return Intent{}, false, "reverse ISB at EMA 9: not an ISB [D5.4 p1 @01:26–01:30]"
	}
	if trend == "" {
		// B6 (L14): with no trend direction the reverse ISB used to fall
		// through to SHORT unconditionally. Fail closed [D5.4: "đánh theo xu
		// hướng" — the trend must exist].
		return Intent{}, false, "reverse ISB at EMA 9: no trend direction — fail-closed [D5.4 p1 @04:16–04:20, B6]"
	}
	dir := ISBDirection(prev)
	if dir == trend {
		return Intent{}, false, "reverse ISB at EMA 9: the ISB already points with the trend — no reverse [D5.4 p1 @01:26–01:30]"
	}
	// Loose touch: the EMA 9 value sits inside the ISB candle's wick range.
	if ema9 < cur.Low || ema9 > cur.High {
		return Intent{}, false, "reverse ISB at EMA 9: price did not reach the EMA 9 [D5.4 p1 @01:26–01:30]"
	}
	entry := cfg.ISBBufferPts // the course's 1.5 [D2.1 p1 @ 06:12]; the wire adds nothing (rel22 P2-1)
	if trend == SideLong {
		// ISB points short in an uptrend → LONG, buy stop at high + buffer (wire).
		return Intent{
			Action:          PlaceStopEntry,
			Setup:           "ISB",
			Side:            SideLong,
			Price:           cur.High + entry,
			Stop:            cur.Low - cfg.ISBBufferPts,
			RefBarMs:        cur.CloseTime,
			EntryCandleHigh: cur.High, // release #31 rule 5
			EntryCandleLow:  cur.Low,
			Reason:          "reverse ISB at EMA 9: uptrend, ISB points short → long buy stop on the wire at the ISB high + buffer [D5.4, D1.4 p1 @ 22:26–23:26, D2.1 p1 @ 06:12]",
		}, true, ""
	}
	return Intent{
		Action:          PlaceStopEntry,
		Setup:           "ISB",
		Side:            SideShort,
		Price:           cur.Low - entry,
		Stop:            cur.High + cfg.ISBBufferPts,
		RefBarMs:        cur.CloseTime,
		EntryCandleHigh: cur.High, // release #31 rule 5
		EntryCandleLow:  cur.Low,
		Reason:          "reverse ISB at EMA 9: downtrend, ISB points long → short sell stop on the wire below the ISB low − buffer [D5.4, D1.4 p1 @ 22:26–23:26, D2.1 p1 @ 06:12]",
	}, true, ""
}
