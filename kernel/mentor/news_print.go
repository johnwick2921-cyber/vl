package mentor

import (
	"sync"
	"time"

	"vl/market"
)

// ── NEWS 07:30 CT — the print candle must not move the HTF trigger lines ────
// [D4.4 p1 @18:13, @22:15]. The 07:30 BLS print (CPI/PPI/Unemployment) is a
// news spike, not a real break: the 1h/4h trigger lines ignore every 1m bar
// whose OPEN falls inside the print window (07:20–07:35 CT — the print minute
// with its −10m/+5m skirt, the same window the trader's news hold uses). The
// evaluator is pure: this is a TIME-ONLY rule (the calendar and the print-day
// test live in the trader); a 07:20–07:35 CT break is skipped every day, on
// the grounds that the 07:30 minute is the BLS minute and a scheduled release
// must never redraw the higher-timeframe direction.

// newsPrintMinuteCache keys the per-minute verdict by the bar's UTC minute
// (the same pattern as fourHBucketCache): the per-bar cost is a map lookup
// after the first pass; time.Date runs once per unique minute.
var newsPrintMinuteCache sync.Map // utcMinute -> bool

// newsPrintWindowOpen reports whether a 1m bar's OPEN (real-UTC ms) falls
// inside the 07:20–07:35 CT print window.
func newsPrintWindowOpen(openMs int64) bool {
	key := openMs / 60_000
	if v, ok := newsPrintMinuteCache.Load(key); ok {
		return v.(bool)
	}
	ct := time.UnixMilli(openMs).In(ctime())
	in := ct.Hour() == 7 && ct.Minute() >= 20 && ct.Minute() < 35
	newsPrintMinuteCache.Store(key, in)
	return in
}

// htfFeedBars returns the 1m feed for the HTF trigger lines: every bar EXCEPT
// the print-window bars (item 18 part 1). It allocates only when a print bar
// is actually present — a non-print-day tape returns the input slice untouched.
func htfFeedBars(bars []market.Kline) []market.Kline {
	drop := false
	for i := range bars {
		if newsPrintWindowOpen(bars[i].OpenTime) {
			drop = true
			break
		}
	}
	if !drop {
		return bars
	}
	out := make([]market.Kline, 0, len(bars))
	for i := range bars {
		if !newsPrintWindowOpen(bars[i].OpenTime) {
			out = append(out, bars[i])
		}
	}
	return out
}
