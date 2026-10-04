package mentor

import (
	"testing"

	"vl/market"
)

// TestTriggerDoesNotFireBeforeTheBreakMinute — replay-audit look-ahead (a): the
// Go evaluator registers a 5m break only from the minute it happens. A forming
// 5m bucket that has not broken anything draws no line; the line appears only
// once the breaking minute arrives.
func TestTriggerDoesNotFireBeforeTheBreakMinute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: int64(i) * 60_000, CloseTime: int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	// bucket 0 (closed): high 100. Bucket 5 is FORMING with one bar whose high
	// (99) has not broken bucket 0's high — no line may exist yet.
	bars := []market.Kline{
		mk(5, 99, 100, 96, 98), // bucket 5 (minutes 5–9)
		mk(10, 98, 99, 97, 98), // bucket 10 forming (minutes 10–14)
	}
	got := TriggerTick(TriggerLine{}, barsTF(bars, 5), 5, cfg)
	if got.Dir != "" {
		t.Fatalf("no line may exist before the break minute: %+v", got)
	}
	// the next 1m bar of the SAME forming bucket breaks bucket 0's high → the
	// line registers NOW, at the break minute (line = the broken high 100).
	bars = append(bars, mk(11, 98, 103, 97, 101))
	got = TriggerTick(got, barsTF(bars, 5), 5, cfg)
	if got.Dir != SideLong || got.Price != 100 {
		t.Fatalf("the line must register at the break minute: %+v, want long @ 100", got)
	}
}

// TestISBBoxFormedOnlyAfterClose — replay-audit look-ahead (b): the 5m ISB rest
// box exists only after that 5m candle has CLOSED. The evaluator builds it from
// the CLOSED 5m buckets, never from the forming one.
func TestISBBoxFormedOnlyAfterClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: int64(i) * 60_000, CloseTime: int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	// bucket A (5 bars) is a big mother candle; bucket B is forming with a
	// body-inside shape vs A — but B has NOT closed yet.
	bars := []market.Kline{
		mk(0, 99, 106, 97, 105),
		mk(1, 100, 105.5, 99.5, 104),
		mk(2, 101, 104.5, 100, 103),
		mk(3, 102, 104, 101, 103.5),
		mk(4, 101, 104, 100.5, 102.5),
		// forming bucket B: inside A's range
		mk(5, 101, 104, 100.5, 102),
		mk(6, 101, 104, 100.5, 102),
	}
	e := New(cfg)
	now := bars[len(bars)-1].CloseTime + 1
	e.Tick(bars, now)
	if e.State.ISBBox != nil {
		t.Fatalf("the box must not exist while its 5m candle is still forming: %+v", e.State.ISBBox)
	}
	// the rest of bucket B (minutes 7–9) closes it at 10:00 → the box forms.
	for i := 7; i <= 9; i++ {
		bars = append(bars, mk(i, 101, 104, 100.5, 102))
	}
	now = bars[len(bars)-1].CloseTime + 1 // 10:00 — bucket B has CLOSED
	e.Tick(bars, now)
	if e.State.ISBBox == nil {
		t.Fatal("the box must form once the 5m candle has CLOSED")
	}
}
