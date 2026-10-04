package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// TestMentorEvalOnceTicksAtTheBarCloseInstant pins the PRODUCTION call site
// (mentorEvalOnce): the evaluator's clock is the instant the last bar closed.
// With the bar's OpenTime as the clock, the ORB escape (orb.go: the last candle
// must have closed) could never latch and the ORB gate refused every entry.
func TestMentorEvalOnceTicksAtTheBarCloseInstant(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	old := market.FuturesBarsProvider
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline { return nil }

	loc := kernel.CTLocation()
	day := time.Date(2026, time.October, 1, 0, 0, 0, 0, loc).UnixMilli()
	bar := func(h, m int, o, hi, lo, c float64) market.Kline {
		ot := day + int64(h*60+m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: hi, Low: lo, Close: c, Final: true}
	}
	var bars []market.Kline
	for m := 6*60 + 30; m < 8*60+30; m++ {
		bars = append(bars, bar(m/60, m%60, 100, 101, 99, 100))
	}
	bars = append(bars,
		bar(8, 30, 100, 101, 99, 100),
		bar(8, 31, 100, 101, 99.5, 100),
		bar(8, 32, 100, 105.5, 100, 105), // the first 1m close outside the 08:30–08:31 box
		bar(8, 33, 105, 106, 104, 105),
	)
	escaped := ""
	for i := 2; i <= len(bars); i++ {
		at.mentorEvalOnce(bars[:i])
		if at.mentorEval.State.ORB.Escaped != "" && escaped == "" {
			escaped = time.UnixMilli(bars[i-1].OpenTime).In(loc).Format("15:04")
		}
	}
	if escaped != "08:32" {
		t.Fatalf("ORB escape latched on the eval of the bar opened %q, want 08:32 (a production tick at the bar's close instant)", escaped)
	}
}
