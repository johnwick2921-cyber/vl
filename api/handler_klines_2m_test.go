package api

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// m421a C4 — the chart's 2m candles are aggregated from 1m on EVEN CT minutes
// (kernel/mentor bucketOpen epoch floor), through the production entry
// getKlinesFromNinjaTrader. The 16:00-17:00 CT break yields no bucket and the
// last forming bucket is served with Final=false.
func TestKlines2mAggregatesFrom1mAcrossBreakAndMarksForming(t *testing.T) {
	loc := kernel.CTLocation()
	at := func(h, m int) int64 { return time.Date(2026, 10, 20, h, m, 0, 0, loc).UnixMilli() }
	one := func(ms int64, o, h, l, c, v float64) market.Kline {
		return market.Kline{OpenTime: ms, Open: o, High: h, Low: l, Close: c, Volume: v}
	}
	forming := time.Now().Truncate(2 * time.Minute) // current even-minute bucket start
	raw := []market.Kline{
		one(at(15, 57), 1, 2, 0, 1.5, 1), // odd minute: belongs to the 15:56 bucket
		one(at(15, 58), 2, 5, 1, 4, 2),
		one(at(15, 59), 4, 6, 3, 5, 3),
		// 16:00-17:00 CT break: no bars
		one(at(17, 0), 10, 11, 9, 10.5, 4),
		one(at(17, 1), 10.5, 12, 8, 11, 5),
		one(forming.UnixMilli(), 20, 21, 19, 20.5, 6), // still forming
	}
	prev := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = prev }()
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		if tf != "1m" {
			t.Fatalf("2m must be built from the 1m ring, provider asked for %q", tf)
		}
		return raw
	}
	s := &Server{}
	got, _ := s.getKlinesFromNinjaTrader("MNQ", "2m", 100)
	span := int64(120_000)
	want := []market.Kline{
		{OpenTime: at(15, 56), CloseTime: at(15, 56) + span - 1, Open: 1, High: 2, Low: 0, Close: 1.5, Volume: 1, Final: true},
		{OpenTime: at(15, 58), CloseTime: at(15, 58) + span - 1, Open: 2, High: 6, Low: 1, Close: 5, Volume: 5, Final: true},
		{OpenTime: at(17, 0), CloseTime: at(17, 0) + span - 1, Open: 10, High: 12, Low: 8, Close: 11, Volume: 9, Final: true},
		{OpenTime: forming.UnixMilli(), CloseTime: forming.UnixMilli() + span - 1, Open: 20, High: 21, Low: 19, Close: 20.5, Volume: 6, Final: false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d candles, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candle %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	capped, _ := s.getKlinesFromNinjaTrader("MNQ", "2m", 2)
	if len(capped) != 2 || capped[1].Final {
		t.Fatalf("limit must keep the newest 2 with the forming one last: %+v", capped)
	}
}
