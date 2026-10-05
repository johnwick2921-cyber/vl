package mentor

import (
	"vl/kernel"
	"vl/market"
)

// R7 — the reverse ISB at EMA 9 (RULES-FIX-v3, a setup behind its OWN knob
// ISBReverseEMA9Enabled, default ON since the 2026-10-04 owner ruling) [D5.4]:
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
//   - the EXECUTOR adds a stop-entry wire offset on placement
//     (kernel.StopEntryOffsetTicks() × the MNQ tick, armed_executor.go
//     decideStopEntry), so the KERNEL-side entry here is the buffer MINUS that
//     offset — see reverseISBEntryPts. The wire result is exactly extreme ± 1.5;
//   - cancel if the next candle does not fill (the evaluator runs the same
//     ISB stacking arithmetic on the emitted arm);
//   - stop = the other side of the ISB candle plus the buffer (already ±
//     ISBBufferPts; it is a stop-loss, not an entry, so the wire offset does
//     not apply).
//
// The 1m is preferred but any TF pair that satisfies IsISB qualifies — the
// caller passes the timeframe it trades.

// reverseISBEntryPts is the KERNEL-side entry offset for the reverse ISB: the
// knob's buffer MINUS the stop-entry wire offset the executor re-adds at
// placement (kernel.StopEntryOffsetTicks() ticks × the MNQ tick). The executor's
// decideStopEntry pushes a long stop entry N ticks BEYOND the authored price, so
// to land ON THE WIRE at exactly the extreme ± ISBBufferPts the kernel must
// subtract that same offset here. Derived from the same sources the executor
// reads — never a hard-coded 0.5 (CTO ruling, trade #627/#628).
func reverseISBEntryPts(cfg Config) float64 {
	wire := float64(kernel.StopEntryOffsetTicks()) * market.FuturesTickSize("MNQ")
	return cfg.ISBBufferPts - wire
}

// ReverseISBAtEMA9 evaluates one candidate ISB candle pair against the 1m EMA 9
// value and the bigger trend. ok=false carries a cited reason (knob off, not an
// ISB, no EMA-9 touch, or the ISB already points WITH the trend).
func ReverseISBAtEMA9(prev, cur market.Kline, ema9 float64, trend Side, cfg Config) (Intent, bool, string) {
	if !cfg.ISBReverseEMA9Enabled {
		return Intent{}, false, "reverse ISB at EMA 9: knob off [D5.4]"
	}
	if !IsISB(prev, cur) {
		return Intent{}, false, "reverse ISB at EMA 9: not an ISB [D5.4]"
	}
	dir := ISBDirection(prev)
	if dir == trend {
		return Intent{}, false, "reverse ISB at EMA 9: the ISB already points with the trend — no reverse [D5.4]"
	}
	// Loose touch: the EMA 9 value sits inside the ISB candle's wick range.
	if ema9 < cur.Low || ema9 > cur.High {
		return Intent{}, false, "reverse ISB at EMA 9: price did not reach the EMA 9 [D5.4]"
	}
	entry := reverseISBEntryPts(cfg)
	if trend == SideLong {
		// ISB points short in an uptrend → LONG, buy stop at high + buffer (wire).
		return Intent{
			Action:   PlaceStopEntry,
			Setup:    "ISB",
			Side:     SideLong,
			Price:    cur.High + entry,
			Stop:     cur.Low - cfg.ISBBufferPts,
			RefBarMs: cur.CloseTime,
			Reason:   "reverse ISB at EMA 9: uptrend, ISB points short → long buy stop on the wire at the ISB high + buffer [D5.4, D1.4 p1 @ 22:26–23:26, D2.1 p1 @ 06:12]",
		}, true, ""
	}
	return Intent{
		Action:   PlaceStopEntry,
		Setup:    "ISB",
		Side:     SideShort,
		Price:    cur.Low - entry,
		Stop:     cur.High + cfg.ISBBufferPts,
		RefBarMs: cur.CloseTime,
		Reason:   "reverse ISB at EMA 9: downtrend, ISB points long → short sell stop on the wire below the ISB low − buffer [D5.4, D1.4 p1 @ 22:26–23:26, D2.1 p1 @ 06:12]",
	}, true, ""
}
