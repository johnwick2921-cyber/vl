package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestM473B4FirstRTH5mCandleIsNeverTheInsideBar — October course D4.3-R58
// [Oct D4.3 @ 57:20-57:35] "cai nen dau tien no khong phai la cai nen INSABA...
// cai nen tiep theo nay la cai nen INSABA": the 08:30 CT 5m candle is never
// itself the inside bar; the next 5m candle inside it is. Frames:
// scenes/Ngày 4.3 Multi Time Frame/s_03442.jpg, s_03450.jpg, s_03455.jpg
// (Mon 02 Nov 2026 5m; box 28,727.50 / 28,639.75). Driven through Tick with the
// production clock (now = the bar's OpenTime).
func TestM473B4FirstRTH5mCandleIsNeverTheInsideBar(t *testing.T) {
	ct := ctime()
	at := func(h, m int) int64 { return time.Date(2026, 11, 2, h, m, 0, 0, ct).UnixMilli() }
	// five 1m bars forming one 5m candle: o first, c last, h/l on inner bars.
	bucket := func(h, m int, o, hi, lo, c float64) []market.Kline {
		var out []market.Kline
		mid := (o + c) / 2
		for i := 0; i < 5; i++ {
			ot := at(h, m+i)
			k := market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: mid, High: mid + 1, Low: mid - 1, Close: mid}
			out = append(out, k)
		}
		out[0].Open = o
		out[0].Low = lo
		out[1].High = hi
		out[4].Close = c
		return out
	}
	var bars []market.Kline
	bars = append(bars, bucket(8, 25, 28650, 28727.50, 28639.75, 28720)...) // green mother (Globex)
	bars = append(bars, bucket(8, 30, 28680, 28710, 28670, 28700)...)       // first RTH candle: body inside 08:25
	bars = append(bars, bucket(8, 35, 28685, 28702, 28680, 28695)...)       // inside 08:30

	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	feed := func(from, to int) {
		for i := from; i < to; i++ {
			e.Tick(bars[:i+1], bars[i].OpenTime)
		}
	}
	// 08:30 candle closed (first 1m of 08:35 is in): NOT an ISB box.
	feed(0, 11)
	if e.State.ISBBox != nil {
		t.Fatalf("the 08:30 CT 5m candle was boxed as an inside bar: %+v", e.State.ISBBox)
	}
	// 08:35 candle closed (08:40 bar present): ISB box with the 08:35 extremes.
	bars = append(bars, bucket(8, 40, 28690, 28698, 28688, 28692)[0])
	feed(11, len(bars))
	b := e.State.ISBBox
	if b == nil {
		t.Fatal("the 08:35 candle inside the 08:30 candle must box")
	}
	if b.High != 28702 || b.Low != 28680 || b.AtTime != at(8, 35) {
		t.Fatalf("box must carry the 08:35 extremes, got %+v", b)
	}
}
