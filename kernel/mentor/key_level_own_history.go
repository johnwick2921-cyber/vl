package mentor

import (
	"sort"

	"vl/market"
)

// levels audit LL-4 — the key-level walk reads the CURRENT contract's own
// history where it has it.
//
// The mentor draws his key levels on the MNQZ2026 chart, scrolling back to the
// late-July lows [OctD5.2-R16 @06:18]: 28,154.50, 27,817.75, 27,997.75,
// 27,753.25 (frames s_00378, s_00384, s_00424). Those are MNQ 12-26's OWN
// prints, months before it became the front month. The stitch (RollStitcher)
// builds every pre-roll 1H RTH candle from the OLDER contract shifted by ONE
// roll gap (291.0 measured on the 09-11 evening), and the basis drifted: the
// 07-29 10:30 candle is GREEN on MNQ 12-26 (O 27,825.50 C 27,831.25 — a level,
// the mentor's 27,817.75) and RED on Sep+291 (no level at all); 07-29 13:30 is
// 28,060.00 native (mentor 28,062.25) vs 28,067.75 stitched; whole-candle drift
// reaches ~35 pts (07-31 08:30).
//
// OwnContractKeyLevel1H replaces each stitched 1H RTH candle with the newest
// contract's OWN candle wherever that contract's native bars cover the whole
// candle, in order of preference: 1m (every minute of the candle stored), 15m
// (every quarter), 30m (both halves; never the 14:30 candle, which ends at
// 15:15 [Oct D5.2 @ 02:14-02:40] — not a 30m boundary). Anything not fully
// covered keeps the stitched candle (the back-adjusted older contract), so the
// series never has a hole. Only OHLC change; the candle's open/close times are
// the stitched ones. Knob KeyLevelHistorySource ("own" default | "stitched").
// No number of ours: coverage is "every bar of the candle present".

// KeyLevelHistorySource knob values (levels audit LL-4).
const (
	KeyLevelHistoryOwn      = "own"
	KeyLevelHistoryStitched = "stitched"
)

// KeyLevelHistoryOwnOn reports whether the walk reads the current contract's
// own candles ("" reads as own — the default).
func KeyLevelHistoryOwnOn(cfg Config) bool {
	return cfg.KeyLevelHistorySource != KeyLevelHistoryStitched
}

// OwnContractKeyLevel1H returns candles with every candle OPENING before
// `before` (the instant the stitched series starts reading the newest contract
// itself — RollStitcher.NewestFrom) that the newest contract covers natively
// rebuilt from its own bars, and how many were replaced, per source ("1m",
// "15m", "30m"). The newest contract's own stretch is never touched. candles is
// not modified.
func OwnContractKeyLevel1H(candles []market.Kline, newest Contract1M, before int64) ([]market.Kline, map[string]int) {
	out := make([]market.Kline, len(candles))
	copy(out, candles)
	n := map[string]int{}
	if len(candles) == 0 {
		return out, n
	}
	for i, c := range candles {
		if c.OpenTime >= before {
			break // ascending: the rest is the newest contract's own stretch
		}
		end := keyLevel1HCandleCloseTime(c.OpenTime)
		if b, ok := ownCandle(newest.Bars, c.OpenTime, end, 60_000); ok {
			out[i] = withTimes(b, c)
			n["1m"]++
			continue
		}
		if b, ok := ownCandle(newest.Bars15m, c.OpenTime, end, 15*60_000); ok {
			out[i] = withTimes(b, c)
			n["15m"]++
			continue
		}
		if b, ok := ownCandle(newest.Bars30m, c.OpenTime, end, 30*60_000); ok {
			out[i] = withTimes(b, c)
			n["30m"]++
		}
	}
	return out, n
}

func withTimes(b, c market.Kline) market.Kline {
	b.OpenTime, b.CloseTime, b.Final = c.OpenTime, c.CloseTime, c.Final
	return b
}

// ownCandle aggregates bars (ascending, one timeframe of span ms) over
// [from, to) — ok only when the span divides the candle and EVERY bar of it is
// stored.
func ownCandle(bars []market.Kline, from, to, span int64) (market.Kline, bool) {
	if len(bars) == 0 || to <= from || (to-from)%span != 0 || from%span != 0 {
		return market.Kline{}, false
	}
	want := int((to - from) / span)
	i := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime >= from })
	var agg market.Kline
	got := 0
	for ; i < len(bars) && bars[i].OpenTime < to; i++ {
		b := bars[i]
		if b.OpenTime != from+int64(got)*span {
			return market.Kline{}, false // a missing bar (or an off-grid one)
		}
		if got == 0 {
			agg = b
		} else {
			if b.High > agg.High {
				agg.High = b.High
			}
			if b.Low < agg.Low {
				agg.Low = b.Low
			}
			agg.Close = b.Close
			agg.Volume += b.Volume
		}
		got++
	}
	return agg, got == want
}
