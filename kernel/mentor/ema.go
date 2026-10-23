package mentor

import (
	"sort"

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
	return EMALevelsAt(bars, cfg, 0)
}

// emaScheduleTF is the M401 EXECUTION-FRAME schedule for the EMA lines: the
// timeframe the EMA 9/34 levels and the EMA34 location line are computed on at
// the instant `now` (the just-closed 1m bar's close instant). With
// Exec2mAfter30m ON and the TF knob left at its default (<= 1), it is the SAME
// frame the named setups read — exec2mReads2m, ONE clock rule: 1m during
// 08:30 <= t < 09:00 CT, 2m at every other minute. The evening-slow 5m branch
// of the setup read is deliberately NOT mirrored (tf is 1 or 2 only). An
// explicit knobTF > 1 stays an experimentation override and wins; knob OFF (or
// now == 0, the legacy no-clock callers) returns knobTF unchanged.
//
// Course basis (owner ordered, CTO override 2026-10-23), raw transcripts:
//   - "Em lúc nào cũng prefer trade khung 2 phút … Trong 30 phút đầu giờ em
//     vẫn sẽ trade khung 1 phút … Nhưng sau 30 phút em sẽ chuyển qua khung 2
//     phút em trade, khi nến đã bắt đầu ổn định rồi" [extra/transcripts/Routine
//     trước khi trade/part1.srt @00:00:34–00:01:14];
//   - "Nó phải đóng qua EMA 34 … cây nến 2 phút … Nó bóng above cái EMA 34 …
//     Mình sẽ vô lon" — the ENTRY condition is read against the EMA34 on the
//     2-minute candles he trades [extra/transcripts/Cách né trap & Setup gồng
//     lời/part1.srt @00:02:05–00:02:48];
//   - "anh TRADE KHUNG NÀO thì anh BẬT KHUNG ĐÓ" [D5.4 @08:15].
//
// The 2m line is used only once it has formed: at least EMAPeriod34 closed 2m
// candles (the callers fall back to the 1m line below that).
func emaScheduleTF(cfg Config, knobTF int, now int64) int {
	if !cfg.Exec2mAfter30m || now == 0 || knobTF > 1 {
		return knobTF
	}
	if exec2mReads2m(now) {
		return 2
	}
	return 1
}

// EMALevelsAt is EMALevels on the execution-frame schedule (emaScheduleTF).
// now == 0 means no clock: the configured EMATFMinutes is used as-is.
func EMALevelsAt(bars []market.Kline, cfg Config, now int64) []Level {
	if !cfg.Enabled || len(bars) == 0 || cfg.EMAPeriod34 <= 0 && cfg.EMAPeriod9 <= 0 {
		return nil
	}
	tfMin := emaScheduleTF(cfg, cfg.EMATFMinutes, now)
	var tf []market.Kline
	if tfMin != cfg.EMATFMinutes {
		// Scheduled frame: CLOSED candles only (the forming bucket is dropped).
		tf = closedBucketsTF(bars, tfMin, now)
		if len(tf) == 0 || len(tf) < cfg.EMAPeriod34 {
			// Fewer closed 2m candles than the EMA 34 period: the line has not
			// formed on that frame (a bare tape, never a live one) - the 1m line
			// stands rather than an EMA seeded off a couple of buckets.
			tf = barsTF(bars, cfg.EMATFMinutes)
		}
	} else {
		tf = barsTF(bars, tfMin)
	}
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

// EMAOnTF computes the standard EMA (seeded with the first close, k = 2/(n+1))
// over the bars aggregated to tfMin-minute CLOSED candles. It returns 0 when
// there are no closed candles or period <= 0. Used by the X8 trap-dodge exit to
// read the EMA34 on the 2m chart the mentor actually trades ("mình trade khung 2
// phút") — NOT the evaluator's 1m EMA34 (State.EMA34), which was the m308 bug.
func EMAOnTF(bars []market.Kline, tfMin, period int) float64 {
	if len(bars) == 0 || period <= 0 {
		return 0
	}
	tf := closedBucketsTF(bars, tfMin, BarCloseInstant(bars[len(bars)-1]))
	if len(tf) == 0 {
		return 0
	}
	return emaValue(tf, period)
}

// ChartEMA is one point of the display-only EMA curve (m134): the EMA 9 and
// EMA 34 values at a 1m bar's open time.
type ChartEMA struct {
	Ms    int64   `json:"ms"`
	Ema9  float64 `json:"ema9"`
	Ema34 float64 `json:"ema34"`
	// TFMin is the timeframe the point was computed on: 1 or 2 (M401). Ms is the
	// open time of that frame's bar.
	TFMin int `json:"tf_min"`
}

// emaSeriesChart builds the per-bar EMA 9/34 curve for the chart (display only,
// m134). Seeds with the first bar's close — the same recurrence as emaValue —
// so the tail lands on the same values the EMA levels use. The series is only
// meaningful on the evaluator's tick timeframe (EMATFMinutes==1): it is the
// per-1m-bar EMA the levels read, so the chart snaps it onto the drawn bars.
// A non-positive period disables that curve (its values stay 0 across the whole
// series and the frontend drops the all-zero curve) — guarding against
// k = 2/(n+1) dividing by zero or emitting a nonsense value for a bad config.
//
// m327 (c): the first `warmup` points are DROPPED (the unconverged head) — an
// EMA seeded at the slice head needs ~6×period bars before the seed's error
// decays below 0.01 pts, so the head used to be up to 6.9 pts off a
// long-seeded EMA. Every EMITTED point is now converged (within 0.01 of an EMA
// seeded from long history); the curve simply starts `warmup` bars in.
func emaSeriesChart(bars []market.Kline, period9, period34 int) []ChartEMA {
	return emaSeriesTF(bars, period9, period34, 1)
}

// emaSeriesChartSched is the M401 chart curve: the EMA the engine ACTUALLY used
// at each point. With Exec2mAfter30m ON, points in a 1m period (08:30 <= t <
// 09:00 CT) are per-1m-bar EMA values (tf_min 1) and points in a 2m period are
// one per CLOSED 2m bar, ms = that 2m bar's open, 2m EMA values (tf_min 2). The
// period of a point is judged by exec2mReads2m at the bar's CLOSE instant — the
// same predicate and the same instant Tick judges the level frame on. Sorted by
// ms. Each series keeps its own m327 warmup. Knob OFF: the 1m curve, unchanged.
func emaSeriesChartSched(bars []market.Kline, cfg Config) []ChartEMA {
	one := emaSeriesTF(bars, cfg.EMAPeriod9, cfg.EMAPeriod34, 1)
	if !cfg.Exec2mAfter30m || cfg.EMATFMinutes > 1 || len(bars) == 0 {
		return one
	}
	out := make([]ChartEMA, 0, len(one))
	for _, p := range one {
		if !exec2mReads2m(p.Ms + 60_000) { // 1m bar close instant
			out = append(out, p)
		}
	}
	two := emaSeriesTF(closedBucketsTF(bars, 2, BarCloseInstant(bars[len(bars)-1])), cfg.EMAPeriod9, cfg.EMAPeriod34, 2)
	for _, p := range two {
		if exec2mReads2m(p.Ms + 120_000) { // 2m bar close instant
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ms < out[j].Ms })
	return out
}

// emaSeriesTF is the per-bar EMA 9/34 curve over bars that are ALREADY on the
// tfMin frame (tf_min is stamped on every point).
func emaSeriesTF(bars []market.Kline, period9, period34, tfMin int) []ChartEMA {
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
	warmup := emaWarmupBars(period9, period34)
	capHint := len(bars) - warmup
	if capHint < 0 {
		capHint = 0 // the whole window is unconverged head — emit nothing
	}
	out := make([]ChartEMA, 0, capHint)
	for i := range bars {
		if i > 0 {
			if period9 > 0 {
				e9 += k9 * (bars[i].Close - e9)
			}
			if period34 > 0 {
				e34 += k34 * (bars[i].Close - e34)
			}
		}
		if i >= warmup {
			out = append(out, ChartEMA{Ms: bars[i].OpenTime, Ema9: e9, Ema34: e34, TFMin: tfMin})
		}
	}
	return out
}

// emaWarmupBars returns the number of head bars to DROP so every emitted EMA
// point is converged (m327 c): 6×period decays the seed's weight to < 1e-5,
// so even a 1000-pt seed error shrinks below 0.01 pts. The max over the enabled
// periods wins — a disabled period contributes 0.
func emaWarmupBars(period9, period34 int) int {
	w := 0
	if period9 > 0 && 6*period9 > w {
		w = 6 * period9
	}
	if period34 > 0 && 6*period34 > w {
		w = 6 * period34
	}
	return w
}
