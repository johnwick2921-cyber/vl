package kernel

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// nativeDailyBar builds a native daily Kline for trading date td (00:00 CT):
// open-stamped (td-1) 00:00 CT with CloseTime = open + 24h − 1 — exactly what
// the cache's openStampBars + barsToKlines produce (the #15 stamp convention).
func nativeDailyBar(td time.Time, o, h, l, c float64) market.Kline {
	open := time.Date(td.Year(), td.Month(), td.Day()-1, 0, 0, 0, 0, CTLocation())
	return market.Kline{
		OpenTime:  open.UnixMilli(),
		CloseTime: open.Add(24*time.Hour).UnixMilli() - 1,
		Open:      o, High: h, Low: l, Close: c,
	}
}

// TestDetectHTFLevelsDailyDropsFormingSession pins #15 (m220): the daily HTF
// pass must NOT read the still-forming daily bar as CLOSED. A native daily bar
// for trading date D is open-stamped (D-1) 00:00 CT, so its CloseTime
// ((D-1) 23:59:59.999) is ~16h before the real 16:00 CT session close. Before
// the fix the forming bar passed closedBars from D 00:00 CT onward and daily
// levels were seated off a half-built candle. After the fix the forming session
// is dropped until it actually closes.
func TestDetectHTFLevelsDailyDropsFormingSession(t *testing.T) {
	// 08:00 CT on the forming trading day (its session runs 17:00 CT the prior
	// day → 16:00 CT today): the newest daily bar is still forming.
	now := time.Date(2026, 10, 21, 8, 0, 0, 0, CTLocation())
	tradingDates := []string{"2026-10-14", "2026-10-15", "2026-10-16", "2026-10-19", "2026-10-20", "2026-10-21"}
	bars := make([]market.Kline, 0, len(tradingDates))
	for _, d := range tradingDates {
		td, _ := time.ParseInLocation("2006-01-02", d, CTLocation())
		bars = append(bars, nativeDailyBar(td, 100, 101, 99, 100.5))
	}
	fetch := func(tf string, count int) []market.Kline {
		if tf != "1d" {
			return nil
		}
		return bars
	}
	_, rep := DetectHTFLevelsExport(fetch, []string{"1d"}, "MNQ", now)
	if rep == nil {
		t.Fatal("nil report")
	}
	// 5 of the 6 daily bars are complete sessions; the 6th (10-21) is forming
	// and must be dropped. Before the fix this read bars=6.
	if got := rep.Resolved["1d"]; !strings.HasPrefix(got, "bars=5 ") {
		t.Fatalf("daily detection fed the forming bar: %q (want bars=5)", got)
	}
	if rep.Skipped["1d"] != "" {
		t.Fatalf("daily detection unexpectedly skipped: %q", rep.Skipped["1d"])
	}
}
