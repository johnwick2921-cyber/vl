package mentor

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// TestBarsTF4HParityWithPlannerTable pins the ONE-4h-anchor contract: the
// mentor's barsTF(bars, 240) (which the HTF gate + SWING4H read) and the
// planner's kernel.Aggregate4HCT produce IDENTICAL 4h candles on the same 1m
// input — same OpenTime, same OHLC, same volume. If a future edit re-copies the
// anchor into either side instead of sharing kernel.FourHBucketStart, this goes
// RED.
func TestBarsTF4HParityWithPlannerTable(t *testing.T) {
	loc := ctime()
	from := time.Date(2026, time.August, 17, 16, 0, 0, 0, loc)
	var bars []market.Kline
	for i, cur := 0, from; !cur.After(time.Date(2026, time.August, 17, 22, 59, 0, 0, loc)); i, cur = i+1, cur.Add(time.Minute) {
		c := float64(1000 + i)
		bars = append(bars, market.Kline{
			OpenTime: cur.UnixMilli(), CloseTime: cur.Add(time.Minute - 1).UnixMilli(),
			Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 1,
		})
	}

	mentor4h := barsTF(bars, 240)
	planner4h := kernel.Aggregate4HCT(bars)
	if len(mentor4h) != len(planner4h) {
		t.Fatalf("bar counts differ: mentor %d vs planner %d", len(mentor4h), len(planner4h))
	}
	for i := range mentor4h {
		a, b := mentor4h[i], planner4h[i]
		if a.OpenTime != b.OpenTime || a.Open != b.Open || a.High != b.High ||
			a.Low != b.Low || a.Close != b.Close || a.Volume != b.Volume {
			t.Fatalf("4h candle %d differs: mentor {%d %.2f %.2f %.2f %.2f v%.2f} vs planner {%d %.2f %.2f %.2f %.2f v%.2f}",
				i, a.OpenTime, a.Open, a.High, a.Low, a.Close, a.Volume,
				b.OpenTime, b.Open, b.High, b.Low, b.Close, b.Volume)
		}
	}
}
