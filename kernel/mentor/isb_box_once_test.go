package mentor

import (
	"testing"

	"vl/market"
)

// TestEscapedISBBoxIsNotRebuiltFromTheSamePair — CTO parity ruling 2026-10-04:
// after a 1m close escapes the 5m ISB box, the same closed pair is still the
// latest one until the next 5m candle closes. The box must stay gone; before
// the fix the very next Tick rebuilt it and it flapped on and off. Driven
// through Tick with the production clock (now = the bar's OpenTime,
// trader/mentor_tick.go mentorEvalOnce).
func TestEscapedISBBoxIsNotRebuiltFromTheSamePair(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: int64(i) * 60_000, CloseTime: int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		// bucket A (minutes 0–4): a green mother candle, range 97–106.
		mk(0, 99, 106, 97, 105),
		mk(1, 100, 105.5, 99.5, 104),
		mk(2, 101, 104.5, 100, 103),
		mk(3, 102, 104, 101, 103.5),
		mk(4, 101, 104, 100.5, 105),
		// bucket B (minutes 5–9): its body sits inside A's range → 5m ISB.
		mk(5, 101, 104, 100.5, 102),
		mk(6, 101, 104, 100.5, 102),
		mk(7, 101, 104, 100.5, 102),
		mk(8, 101, 104, 100.5, 102),
		mk(9, 101, 104, 100.5, 102),
	}
	e := New(cfg)
	tick := func(b market.Kline) {
		bars = append(bars, b)
		e.Tick(bars, b.OpenTime)
	}
	tick(mk(10, 102, 103, 101, 102)) // B has closed → the box stands
	if e.State.ISBBox == nil {
		t.Fatal("the 5m ISB box must stand once its candle has closed")
	}
	tick(mk(11, 101, 101, 98, 99)) // a 1m close below the box low 100.5 → escape
	if e.State.ISBBox != nil {
		t.Fatalf("a close below the box must delete it: %+v", e.State.ISBBox)
	}
	tick(mk(12, 99, 99.5, 98, 99)) // same latest closed pair (A, B) — still gone
	if e.State.ISBBox != nil {
		t.Fatalf("the escaped box was rebuilt from the same pair: %+v", e.State.ISBBox)
	}
	tick(mk(13, 99, 99.5, 98, 99))
	if e.State.ISBBox != nil {
		t.Fatalf("the escaped box was rebuilt from the same pair: %+v", e.State.ISBBox)
	}
}
