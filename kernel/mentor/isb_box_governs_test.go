package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestISBBoxSkipsTriggerPriceFilter pins D4.1-23 — inside a standing 5m ISB
// box ONLY a same-direction ISB trades, and the box (not the 5m trigger line)
// governs: "CHỈ ĐƯỢC ĐÁNH INSIDE BAR CÙNG CHIỀU, Ở KHUNG GIỜ NHỎ HƠN… KÊU
// ĐÁNH LÊN THÌ ĐÁNH LÊN" [D4.1 p2 @04:06–06:45]; METHOD §5.2 "do not use the
// trigger line while it stands". Without the fix a same-direction ISB BELOW a
// 5m buy line inside a LONG box is refused isb_trigger_side. Mutant: run the
// trigger filter even while the box stands → the box case starts refusing
// isb_trigger_side → RED.
func TestISBBoxSkipsTriggerPriceFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	// Flat lead-in, then a LONG 1m ISB: candle-1 green (range 94–99), candle-2
	// body [96.5, 97.5] inside it. The close 97.5 sits BELOW the seeded 5m buy
	// line at 100, so the trigger price-side filter would refuse the pair.
	bars := []market.Kline{
		mk(0, 95, 96, 94, 95),
		mk(1, 95, 96, 94, 95),
		mk(2, 95, 96, 94, 95),
		mk(3, 95, 96, 94, 95),
		mk(4, 95, 96, 94, 95),
		mk(5, 95, 96, 94, 95),
		mk(6, 95, 96, 94, 95),
		mk(7, 95, 96, 94, 95),
		mk(8, 95, 99, 94, 98),         // green candle-1 (ISB direction long)
		mk(9, 97.5, 98.5, 96.5, 97.5), // ISB candle-2, body inside candle-1's range
	}
	now := bars[9].OpenTime + 59_999
	seed := func(box *ISBBox) *Evaluator {
		e := New(cfg)
		// LastBucket pins the trigger so TriggerTick leaves the seeded buy line
		// in place (no bucket is newer than it).
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 100, LastBucket: 1 << 62}
		e.State.ISBBox = box
		return e
	}

	// CONTROL: no box stands → the trigger price-side filter refuses the ISB.
	eNoBox := seed(nil)
	eNoBox.Tick(bars, now)
	if eNoBox.State.Refusals["isb_trigger_side"] != 1 {
		t.Fatalf("control: without the box the below-the-line ISB must be refused isb_trigger_side, ledger=%v", eNoBox.State.Refusals)
	}

	// A LONG box stands: the box governs — the trigger price filter is skipped,
	// and the same-direction ISB is not refused by it.
	eBox := seed(&ISBBox{High: 110, Low: 90, Dir: SideLong, AtTime: bars[0].OpenTime})
	eBox.Tick(bars, now)
	if eBox.State.Refusals["isb_trigger_side"] != 0 {
		t.Fatalf("with the box standing the trigger price filter must be skipped, got isb_trigger_side=%d (ledger=%v)", eBox.State.Refusals["isb_trigger_side"], eBox.State.Refusals)
	}
}
