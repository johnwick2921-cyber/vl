package mentor

import "vl/market"

// R7 — the reverse ISB at EMA 9 (RULES-FIX-v3, a setup behind its OWN knob
// ISBReverseEMA9Enabled, default OFF) [D5.4]:
//
//   - price must actually reach EMA 9 (a loose touch is OK);
//   - in an uptrend, an ISB that points SHORT at EMA 9 → go LONG with a buy
//     stop above that ISB candle's high; downtrend mirror: sell stop below low;
//   - cancel if the next candle does not fill (the evaluator runs the same
//     ISB stacking arithmetic on the emitted arm);
//   - stop = the other side of the ISB candle plus the buffer (the method does
//     not say; the knob ISBBufferPts supplies it).
//
// The 1m is preferred but any TF pair that satisfies IsISB qualifies — the
// caller passes the timeframe it trades.

// ReverseISBAtEMA9 evaluates one candidate ISB candle pair against the 1m EMA 9
// value and the bigger trend. ok=false carries a cited reason (knob off, not an
// ISB, no EMA-9 touch, or the ISB already points WITH the trend).
func ReverseISBAtEMA9(prev, cur market.Kline, ema9 float64, trend Side, cfg Config) (Intent, bool, string) {
	if !cfg.ISBReverseEMA9Enabled {
		return Intent{}, false, "reverse ISB at EMA 9: knob off [D5.4; L4]"
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
	if trend == SideLong {
		// ISB points short in an uptrend → LONG, buy stop above its high.
		return Intent{
			Action: PlaceStopEntry,
			Setup:  "ISB",
			Side:   SideLong,
			Price:  cur.High,
			Stop:   cur.Low - cfg.ISBBufferPts,
			Reason: "reverse ISB at EMA 9: uptrend, ISB points short → long buy stop above the ISB high [D5.4]",
		}, true, ""
	}
	return Intent{
		Action: PlaceStopEntry,
		Setup:  "ISB",
		Side:   SideShort,
		Price:  cur.Low,
		Stop:   cur.High + cfg.ISBBufferPts,
		Reason: "reverse ISB at EMA 9: downtrend, ISB points long → short sell stop below the ISB low [D5.4]",
	}, true, ""
}
