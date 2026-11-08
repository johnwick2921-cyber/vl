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
// (Mon 02 Nov 2026 5m). The PRICES here are synthetic: the on-screen
// 28,727.50 / 28,639.75 is the R59 head-to-toe MERGED box of several candles at
// 11:05 (left edge on the 08:30 candle; the 08:25 candle in the frame is RED),
// which this wave does not implement. What the test pins is the R58 rule:
// 08:30 is not the inside bar, the next candle inside it is (the box then
// carries the 08:35 candle's extremes - a reading of "bọc cái nến này lại", not
// a frame-pinned number). Driven through Tick with the production clock (now =
// the bar's OpenTime).
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
	bars = append(bars, bucket(8, 25, 28650, 28727.50, 28639.75, 28720)...) // synthetic mother (Globex)
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

// TestM473B4OpenRuleFollowsTheCalendarAndWidth — the exemption is the 08:30 CT
// 5m bucket on a day the CME is open then (a half-day open too: Fri 27 Nov 2026
// opens 08:30 and closes early), never a 15m bucket, and never 08:35.
func TestM473B4OpenRuleFollowsTheCalendarAndWidth(t *testing.T) {
	ct := ctime()
	ms := func(y int, mo time.Month, d, h, m int) int64 { return time.Date(y, mo, d, h, m, 0, 0, ct).UnixMilli() }
	if !isFirstRTH5m(ms(2026, 11, 27, 8, 30)) {
		t.Fatal("half-day open 08:30 must be the first RTH 5m candle")
	}
	if isFirstRTH5m(ms(2026, 11, 2, 8, 35)) || isFirstRTH5m(ms(2026, 11, 2, 8, 25)) {
		t.Fatal("only the 08:30 bucket is the first RTH candle")
	}
	if isFirstRTH5m(ms(2026, 12, 25, 8, 30)) {
		t.Fatal("08:30 on a closed CME day is not an RTH open")
	}
	k := func(o int64, op, hi, lo, c float64) market.Kline {
		return market.Kline{OpenTime: o, CloseTime: o + 299_999, Open: op, High: hi, Low: lo, Close: c}
	}
	mother := k(ms(2026, 11, 2, 8, 25), 28650, 28727.5, 28639.75, 28640) // red mother
	inside := k(ms(2026, 11, 2, 8, 30), 28680, 28710, 28670, 28700)
	if IsISB(mother, inside) == false {
		t.Fatal("setup: plain IsISB sees 08:30 inside 08:25")
	}
	if isISBFirstRTHAware(mother, inside) {
		t.Fatal("08:30 inside 08:25 must NOT read as an ISB (R58)")
	}
	// a 15m pair opening 08:30 is judged by plain IsISB
	m15 := k(ms(2026, 11, 2, 8, 15), 28650, 28727.5, 28639.75, 28640)
	i15 := k(ms(2026, 11, 2, 8, 30), 28680, 28710, 28670, 28700)
	if !isISBFirstRTHAware(m15, i15) {
		t.Fatal("a 15m bucket is not exempt")
	}
	// 08:30 as MOTHER: 08:35 inside it is a real ISB
	in35 := k(ms(2026, 11, 2, 8, 35), 28690, 28702, 28680, 28695)
	if !isISBFirstRTHAware(k(ms(2026, 11, 2, 8, 30), 28680, 28710, 28670, 28660), in35) {
		t.Fatal("08:30 stays usable as the mother candle")
	}
	// crossingISBs: (08:25,08:30) phantom, (08:30,08:35) real, colours differ:
	// without the exemption the phantom flips direction and reads as crossing.
	b30 := k(ms(2026, 11, 2, 8, 30), 28680, 28710, 28670, 28660) // red 08:30
	if crossingISBs([]market.Kline{mother, b30, in35}, 3) {
		t.Fatal("phantom 08:30 ISB must not make the 5m read cross")
	}
	// MTFAligned / ISBConflictVerdict ignore the phantom pair too.
	g15a := k(ms(2026, 11, 2, 8, 15), 28600, 28800, 28600, 28790)
	g15b := k(ms(2026, 11, 2, 8, 30), 28650, 28750, 28650, 28700)
	if MTFAligned([]market.Kline{mother, inside}, []market.Kline{g15a, g15b}, SideShort) || ISBConflictVerdict([]market.Kline{mother, inside}, []market.Kline{g15a, g15b}) {
		t.Fatal("phantom 08:30 5m ISB must not align or conflict")
	}
}
