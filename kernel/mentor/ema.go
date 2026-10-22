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

// ChartEMA is one point of the display-only EMA curve (m134): the EMA 9 and
// EMA 34 values at a 1m bar's open time.
type ChartEMA struct {
	Ms    int64   `json:"ms"`
	Ema9  float64 `json:"ema9"`
	Ema34 float64 `json:"ema34"`
}

// emaSeriesChart builds the per-bar EMA 9/34 curve for the chart (display only,
// m134). Seeds with the first bar's close — the same recurrence as emaValue —
// so the tail lands on the same values the EMA levels use. The series is only
// meaningful on the evaluator's tick timeframe (EMATFMinutes==1): it is the
// per-1m-bar EMA the levels read, so the chart snaps it onto the drawn bars.
// A non-positive period disables that curve (its values stay 0 across the whole
// series and the frontend drops the all-zero curve) — guarding against
// k = 2/(n+1) dividing by zero or emitting a nonsense value for a bad config.
func emaSeriesChart(bars []market.Kline, period9, period34 int) []ChartEMA {
	if len(bars) == 0 {
		return nil
	}
	k9, k34 := 0.0, 0.0
	if period9 > 0 {
		k9 = 2.0 / float64(period9+1)
	}
	if period34 > 0 {
		k34 = 2.0 / float64(period34+1)
	}
	e9, e34 := bars[0].Close, bars[0].Close
	if period9 <= 0 {
		e9 = 0
	}
	if period34 <= 0 {
		e34 = 0
	}
	out := make([]ChartEMA, 0, len(bars))
	out = append(out, ChartEMA{Ms: bars[0].OpenTime, Ema9: e9, Ema34: e34})
	for _, b := range bars[1:] {
		if period9 > 0 {
			e9 += k9 * (b.Close - e9)
		}
		if period34 > 0 {
			e34 += k34 * (b.Close - e34)
		}
		out = append(out, ChartEMA{Ms: b.OpenTime, Ema9: e9, Ema34: e34})
	}
	return out
}
