package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestM472A7KeyLevel1HLastCandleEnds1515: the last RTH 1h key-level candle
// is 14:30-15:15 CT, not 14:30-15:00.
// [Oct D5.2 @ 02:14-02:40] frame
// /home/hoang/mm-course/oct/scenes/Ngày 5.2 Cách vẽ và trade tại major levels/s_00080.jpg
// (RTH shading ends at 15:15); D1.2 s_00665.jpg (report settings row 9).
func TestM472A7KeyLevel1HLastCandleEnds1515(t *testing.T) {
	loc := ctime()
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, loc)
	at := func(h, m int) int64 { return day.Add(time.Duration(h*60+m) * time.Minute).UnixMilli() }
	var bars []market.Kline
	for ms := at(8, 30); ms < at(15, 30); ms += 60_000 {
		p := 100.0
		hi := p + 1
		switch ms {
		case at(15, 10):
			hi = 150 // inside the 14:30 candle
		case at(15, 14):
			hi = 160 // last minute inside (15:15 end is exclusive)
		case at(15, 15):
			hi = 700 // first minute OUTSIDE
		case at(15, 20):
			hi = 900 // outside RTH candle
		}
		bars = append(bars, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: p, High: hi, Low: p - 1, Close: p})
	}
	got := keyLevel1HBars(bars)
	if len(got) == 0 {
		t.Fatal("no candles")
	}
	last := got[len(got)-1]
	if last.OpenTime != at(14, 30) {
		t.Fatalf("last candle opens %v, want 14:30", time.UnixMilli(last.OpenTime).In(loc))
	}
	if last.High != 160 {
		t.Fatalf("14:30 candle High=%v: want 160 (15:14 print inside, 15:15 print at 700 and 15:20 print at 900 outside)", last.High)
	}
	if c := keyLevel1HCandleCloseTime(last.OpenTime); c != at(15, 15) {
		t.Fatalf("scheduled close %v, want 15:15", time.UnixMilli(c).In(loc))
	}
}
