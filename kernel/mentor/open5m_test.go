package mentor

import (
	"testing"

	"vl/market"
)

// TestOpen5mRangeBreak pins the D4.4 p2 open-window rule at the production
// filter: in the open window (08:30–09:30 CT) a with-4h entry is allowed only
// after a 5m candle has CLOSED outside the opening 5m range (the first 5m
// candle 08:30–08:35) in the 4h's direction.
// MUTANT: drop the open5mRangeFilter call in Tick (or return ints,nil from it)
// → the pre-break long EMITS → RED.
func TestOpen5mRangeBreak(t *testing.T) {
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}

	// The opening 5m range: 08:30–08:34, high 100.5, low 99.0.
	openBars := []market.Kline{
		mk(8, 30, 100.0, 100.5, 99.5, 100.2),
		mk(8, 31, 100.2, 100.4, 99.6, 100.1),
		mk(8, 32, 100.1, 100.3, 99.2, 100.0),
		mk(8, 33, 100.0, 100.2, 99.0, 99.8),
		mk(8, 34, 99.8, 100.1, 99.4, 100.0),
	}
	// The first 5m bucket after the opening one: 08:35–08:39, closes 08:40 at
	// 100.0 — INSIDE the range (no break).
	inside := []market.Kline{
		mk(8, 35, 100.0, 100.2, 99.8, 100.0),
		mk(8, 36, 100.0, 100.1, 99.7, 99.9),
		mk(8, 37, 99.9, 100.0, 99.6, 99.8),
		mk(8, 38, 99.8, 100.0, 99.7, 99.9),
		mk(8, 39, 99.9, 100.1, 99.8, 100.0),
	}
	bars := append(append([]market.Kline{}, openBars...), inside...)
	now := auditMs(2026, 9, 15, 9, 0, 0) // 09:00 CT — open window

	h := HTF{FourH: TriggerLine{Dir: SideLong, Price: 100.0}}

	longEntry := func() Intent {
		return Intent{Action: PlaceStopEntry, Side: SideLong, Reason: "PHL", Price: 100.6, Stop: 99.5, Target: 102.0}
	}

	// Sanity: the range is drawn and not yet broken.
	high, low, drawn := open5mRange(bars, now)
	if !drawn || high != 100.5 || low != 99.0 {
		t.Fatalf("open5mRange = %v/%v drawn=%v, want 100.5/99.0 true", high, low, drawn)
	}
	if bl, bs := open5mBroken(bars, now, high, low); bl || bs {
		t.Fatalf("open5mBroken before any break = long %v short %v, want false false", bl, bs)
	}

	// A with-4h long entry before the 5m break is REFUSED.
	kept, refused := open5mRangeFilter([]Intent{longEntry()}, h, bars, now)
	if len(kept) != 0 || len(refused) != 1 || refused[0] != "open_5m_range_not_broken" {
		t.Fatalf("pre-break: kept=%d refused=%v, want 0 kept / [open_5m_range_not_broken]", len(kept), refused)
	}

	// A 5m candle closes above the range in the 4h direction → break → allow.
	breakBars := []market.Kline{
		mk(8, 40, 100.0, 100.7, 99.9, 100.6),
		mk(8, 41, 100.6, 100.8, 100.4, 100.7),
		mk(8, 42, 100.7, 100.9, 100.5, 100.8),
		mk(8, 43, 100.8, 101.0, 100.6, 100.9),
		mk(8, 44, 100.9, 101.2, 100.8, 101.0), // 5m close 101.0 > 100.5 → brokeLong
	}
	bars = append(bars, breakBars...)
	now2 := auditMs(2026, 9, 15, 9, 10, 0) // 09:10 CT — still the open window
	if bl, bs := open5mBroken(bars, now2, high, low); !bl || bs {
		t.Fatalf("open5mBroken after the break = long %v short %v, want true false", bl, bs)
	}
	kept, refused = open5mRangeFilter([]Intent{longEntry()}, h, bars, now2)
	if len(kept) != 1 || len(refused) != 0 {
		t.Fatalf("post-break: kept=%d refused=%v, want 1 kept / none", len(kept), refused)
	}

	// The swing is exempt, and a SHORT entry (off the 4h long) is left to the
	// HTF gate, not this filter.
	swing := Intent{Action: PlaceStopEntry, Side: SideLong, Reason: "swing", Price: 100.6}
	short := Intent{Action: PlaceStopEntry, Side: SideShort, Reason: "PHL", Price: 99.0}
	kept2, _ := open5mRangeFilter([]Intent{swing, short}, h, bars, now2)
	if len(kept2) != 2 {
		t.Fatalf("swing + off-4h must pass through: kept=%d, want 2", len(kept2))
	}

	// Outside the open window the filter does nothing.
	after := auditMs(2026, 9, 15, 10, 0, 0)
	kept3, _ := open5mRangeFilter([]Intent{longEntry()}, h, bars, after)
	if len(kept3) != 1 {
		t.Fatalf("outside the open window the filter must not gate: kept=%d, want 1", len(kept3))
	}
}
