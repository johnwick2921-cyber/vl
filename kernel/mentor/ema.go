package mentor

import (
	"vl/market"
)

// EMALevels promotes EMA 34 and EMA 9 to levels (§8: "EMA 34 on the 1-hour or
// 4-hour is the most valid level there is" [D5.2 p1 @ 03:36]; §10: the reverse
// inside bar lives at EMA 9 [D5.4 @ 00:01]; §11: exactly two indicators).
//
// The evaluator needs a price the per-1m-close touch rules can act on, so the
// level line is the LATEST EMA value on the TF given by EMATFMinutes
// (default 1m — the frame the evaluator ticks on; the 4h-EMA swing setup is
// out of P2 scope per PLAN v1 and stays a knob of the TF).
func EMALevels(bars []market.Kline, cfg Config) []Level {
	if !cfg.Enabled || len(bars) == 0 || cfg.EMAPeriod34 <= 0 && cfg.EMAPeriod9 <= 0 {
		return nil
	}
	tf := barsTF(bars, cfg.EMATFMinutes)
	if len(tf) == 0 {
		return nil
	}
	last := tf[len(tf)-1]
	var out []Level
	if cfg.EMAPeriod34 > 0 {
		v := emaValue(tf, cfg.EMAPeriod34)
		out = append(out, Level{
			// STABLE key: the EMA line moves every bar, but it is ONE line —
			// keying by value would fragment the touch state (each bar a new
			// level) and break the invalid-level rule.
			Key:    string(KindEMA34),
			Kind:   KindEMA34,
			Price:  v,
			AtTime: last.OpenTime,
		})
	}
	if cfg.EMAPeriod9 > 0 {
		v := emaValue(tf, cfg.EMAPeriod9)
		out = append(out, Level{
			Key:    string(KindEMA9),
			Kind:   KindEMA9,
			Price:  v,
			AtTime: last.OpenTime,
		})
	}
	return out
}

// emaValue is the standard EMA seeded with the first close (k = 2/(n+1)) —
// the same definition the bot's prompt context uses for ema_periods
// (DS-106 §1: ema_periods knob, engine_analysis.go EMA rows).
func emaValue(bars []market.Kline, period int) float64 {
	k := 2.0 / float64(period+1)
	ema := bars[0].Close
	for _, b := range bars[1:] {
		ema = ema + k*(b.Close-ema)
	}
	return ema
}
