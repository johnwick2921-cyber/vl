package mentor

import (
	"testing"

	"vl/market"
)

// x5_10Bars is the 4-bar fixture for the X5-10 pin (09:00–09:03 CT, RTH).
// On the 1m chart the last two bars form a long ISB (bar 2 green candle 1,
// bar 3's body inside bar 2's range); on the 2m chart candle 1 (bars 0–1)
// is a doji — open == close — so the 2m read is NO ISB at all. The head
// bars keep the EMA 34 target well above the entry (same trick as isbFixture).
func x5_10Bars() []market.Kline {
	return []market.Kline{
		rthBars(0, 121, 122, 120, 120.5),
		rthBars(1, 121, 121.5, 120, 121), // 2m candle 1: open 121 == close 121 (doji)
		rthBars(2, 99, 106, 98.5, 106),   // green candle 1 (1m)
		rthBars(3, 101, 102.1, 100.9, 102),
	}
}

// x5_10Emits counts the ISB entry intents a Tick leaves.
func x5_10Emits(e *Evaluator, bars []market.Kline, now int64) int {
	n := 0
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopLimitEntry && in.Setup == "ISB" {
			n++
		}
	}
	return n
}

// TestX5_10Exec2mAfter30mSwitchesISBReadTo2m — X5-10 (optional, default OFF):
// after the first 30 minutes of RTH (09:00 CT) the ISB entry is read on the
// 2m chart instead of the 1m ("sau 30 phút em sẽ chuyển qua khung 2 phút"
// [X5 @00:41–01:17; X11 @17:06–17:32]). The pin drives Tick at the production
// call site: the 1m read emits the long ISB, the 2m read (doji candle 1) does
// not, and the switch only applies at/after 09:00 CT.
func TestX5_10Exec2mAfter30mSwitchesISBReadTo2m(t *testing.T) {
	bars := x5_10Bars()
	now := bars[len(bars)-1].CloseTime + 1 // 09:03 CT

	// OFF (default): the 1m read emits the ISB.
	cfgOff := DefaultConfig()
	cfgOff.Enabled = true
	if got := x5_10Emits(newISBEval(cfgOff), bars, now); got != 1 {
		t.Fatalf("knob OFF (1m read) must emit the ISB, got %d", got)
	}

	// ON + after the first 30 min of RTH: the 2m read drops the ISB.
	cfgOn := DefaultConfig()
	cfgOn.Enabled = true
	cfgOn.Exec2mAfter30m = true
	if got := x5_10Emits(newISBEval(cfgOn), bars, now); got != 0 {
		t.Fatalf("knob ON (2m read) must drop the ISB, got %d", got)
	}

	// ON + BEFORE the first 30 min of RTH (08:50–08:53 CT): still the 1m read.
	pre := []market.Kline{
		barAt(auditMs(2026, 9, 15, 8, 50, 0), 121, 122, 120, 120.5),
		barAt(auditMs(2026, 9, 15, 8, 51, 0), 121, 121.5, 120, 121),
		barAt(auditMs(2026, 9, 15, 8, 52, 0), 99, 106, 98.5, 106),
		barAt(auditMs(2026, 9, 15, 8, 53, 0), 101, 102.1, 100.9, 102),
	}
	preNow := pre[len(pre)-1].CloseTime + 1 // 08:53 CT
	if got := x5_10Emits(newISBEval(cfgOn), pre, preNow); got != 1 {
		t.Fatalf("knob ON before 09:00 CT must keep the 1m read, got %d", got)
	}
}

func barAt(openMs int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: openMs, CloseTime: openMs + 59_999, Open: o, High: h, Low: l, Close: c}
}

// TestX5_10Exec2mReadsClosedBucketsNotTheFormingOne — P1: on a 1m close that
// OPENS a 2m bucket, the read must be the previous two CLOSED 2m candles, not
// the half-formed one (barsTF would flush the partial bucket and the ISB would
// read a half-built candle). The fixture ends at 09:04 — the FIRST minute of
// the [09:04,09:05] bucket. The closed 2m read is (doji A, B) → no ISB; the
// barsTF read would be (B, half-formed 09:04) → ISB.
func TestX5_10Exec2mReadsClosedBucketsNotTheFormingOne(t *testing.T) {
	bars := []market.Kline{
		rthBars(0, 140, 141, 139, 140), // bucket A open 140
		rthBars(1, 140, 141, 139, 140), // bucket A close 140 → 2m doji
		rthBars(2, 99, 106, 98.5, 106), // bucket B (green)
		rthBars(3, 101, 105, 100, 104), // bucket B close 104
		rthBars(4, 103, 104, 102, 103), // 09:04 — first minute of the next 2m bucket
	}
	now := bars[len(bars)-1].CloseTime + 1 // 09:04 CT
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = true
	if got := x5_10Emits(newISBEval(cfg), bars, now); got != 0 {
		t.Fatalf("the 2m read must use CLOSED buckets (candle 1 is a doji), got %d ISB", got)
	}
}
