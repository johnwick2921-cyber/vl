package mentor

import (
	"testing"

	"vl/market"
)

// ── release #31 rule 5 — the ORB-edge ISB candle fraction ───────────────────
//
// "80% 90% là cái nến nó nằm ở ngoài… anh chị có quyền take risk… điểm entry…
// nằm gần cái resistance quá thì thôi bỏ đi" [extra "Routine trước khi trade"
// part1 @07:46–09:04]. The measure is the fraction of the ISB candle (high−low)
// OUTSIDE the ORB on the trade side — NOT a points distance (that is CE-7
// orb_edge_veto_pts). Default ON at 0.80.

// TestRelease31Defaults — rule 4 and rule 5 ship ON in DefaultConfig.
func TestRelease31Defaults(t *testing.T) {
	d := DefaultConfig()
	if !d.Exec2mAfter30m {
		t.Fatal("rule 4: exec_2m_after_30m must default ON (1m for the first 30 min, then 2m)")
	}
	if d.OrbISBFractionOutside != 0.80 {
		t.Fatalf("rule 5: orb_isb_fraction_outside must default 0.80, got %v", d.OrbISBFractionOutside)
	}
}

// TestOrbISBFractionOutside — the pure measure: the fraction of the candle's
// range outside the ORB on the trade side. Long reads above the high, short
// below the low.
func TestOrbISBFractionOutside(t *testing.T) {
	orb := ORB{High: 100, Low: 90}
	cases := []struct {
		side Side
		hi   float64
		lo   float64
		want float64
	}{
		{SideLong, 110, 105, 1.0}, // wholly above the high
		{SideLong, 105, 95, 0.5},  // half above the high (5 of 10)
		{SideLong, 101, 99, 0.5},  // 1 of 2 above
		{SideLong, 100, 90, 0.0},  // wholly inside
		{SideShort, 90, 85, 1.0},  // wholly below the low
		{SideShort, 105, 95, 0.0}, // wholly above the low
		{SideShort, 95, 85, 0.5},  // half below the low (5 of 10)
		{SideShort, 93, 89, 0.25}, // 1 of 4 below the low
	}
	for _, c := range cases {
		if got := orbISBFractionOutside(orb, c.side, c.hi, c.lo); got != c.want {
			t.Fatalf("%s hi %.0f lo %.0f: fraction = %.4f, want %.4f", c.side, c.hi, c.lo, got, c.want)
		}
	}
	if got := orbISBFractionOutside(orb, SideLong, 90, 100); got != 0 {
		t.Fatalf("an unmeasured candle (hi <= lo) must read 0, got %v", got)
	}
}

// TestOrbISBMostlyInsideGate — the production gate call site (orbGateFilter):
// an ISB whose candle is mostly INSIDE the ORB on the trade side is refused
// orb_isb_mostly_inside; mostly outside passes; the fraction knob scales it and
// 0 turns the rule off. Only Setup=="ISB" intents with a stamped candle are
// measured (a PHL is not).
func TestOrbISBMostlyInsideGate(t *testing.T) {
	day := auditMs(2026, 9, 15, 9, 0, 0)
	orb := ORB{Day: dayStartCT(day), High: 100, Low: 90, Drawn: true, Escaped: SideLong}
	cfg := DefaultConfig()
	cfg.Enabled = true

	isb := func(hi, lo float64) Intent {
		return Intent{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideLong, Price: hi + 1, EntryCandleHigh: hi, EntryCandleLow: lo}
	}
	phl := func() Intent {
		return Intent{Action: PlaceStopEntry, Setup: "PHL", Side: SideLong, Price: 101, Reason: "PHL"}
	}

	// 50% outside (hi 102, lo 98 → (102-100)/(102-98) = 0.5) < 0.80 → refused.
	got, refused := orbGateFilter([]Intent{isb(102, 98)}, orb, day, cfg)
	if len(got) != 0 || len(refused) != 1 || refused[0] != "orb_isb_mostly_inside" {
		t.Fatalf("mostly-inside ISB must be refused orb_isb_mostly_inside; got %+v refused %v", got, refused)
	}

	// 100% outside (hi 110, lo 105) ≥ 0.80 → passes.
	got, refused = orbGateFilter([]Intent{isb(110, 105)}, orb, day, cfg)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("mostly-outside ISB must pass; got %+v refused %v", got, refused)
	}

	// The fraction knob scales: 0.40 admits the 50%-outside candle.
	cfg40 := DefaultConfig()
	cfg40.Enabled = true
	cfg40.OrbISBFractionOutside = 0.40
	got, refused = orbGateFilter([]Intent{isb(102, 98)}, orb, day, cfg40)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("fraction 0.40 must admit a 50%%-outside ISB; got %+v refused %v", got, refused)
	}

	// Knob <= 0 turns the rule off.
	cfgOff := DefaultConfig()
	cfgOff.Enabled = true
	cfgOff.OrbISBFractionOutside = 0
	got, refused = orbGateFilter([]Intent{isb(102, 98)}, orb, day, cfgOff)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("knob 0 must turn the rule off; got %+v refused %v", got, refused)
	}

	// A PHL (no stamped candle) is not measured by rule 5.
	got, refused = orbGateFilter([]Intent{phl()}, orb, day, cfg)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("a PHL must not be measured by rule 5; got %+v refused %v", got, refused)
	}

	// M565-3: rule 5 is scoped to Setup=="ISB" — a non-ISB entry that CARRIES a
	// candle (hi 102 lo 98 = mostly inside) must still pass; dropping the
	// Setup=="ISB" guard would refuse it. (A PHL never stamps one in production
	// today; the guard is what keeps it that way.)
	phlWithCandle := Intent{Action: PlaceStopEntry, Setup: "PHL", Side: SideLong, Price: 101, EntryCandleHigh: 102, EntryCandleLow: 98, Reason: "PHL"}
	got, refused = orbGateFilter([]Intent{phlWithCandle}, orb, day, cfg)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("rule 5 must not measure a non-ISB setup even with a stamped candle; got %+v refused %v", got, refused)
	}

	// M565-1: the EXACT 80% boundary passes (the rule is strictly <, not <=).
	// hi 104 lo 99, ORB high 100 → (104-100)/(104-99) = 0.80 = the knob.
	got, refused = orbGateFilter([]Intent{isb(104, 99)}, orb, day, cfg)
	if len(got) != 1 || len(refused) != 0 {
		t.Fatalf("an ISB at exactly 80%% outside must pass (the rule is <, not <=); got %+v refused %v", got, refused)
	}

	// Short mirror: below the low, hi 93 lo 88 → (90-88)/(93-88) = 0.4 < 0.80.
	orbS := ORB{Day: dayStartCT(day), High: 100, Low: 90, Drawn: true, Escaped: SideShort}
	shortISB := Intent{Action: PlaceStopLimitEntry, Setup: "ISB", Side: SideShort, Price: 88, EntryCandleHigh: 93, EntryCandleLow: 88}
	got, refused = orbGateFilter([]Intent{shortISB}, orbS, day, cfg)
	if len(got) != 0 || len(refused) != 1 || refused[0] != "orb_isb_mostly_inside" {
		t.Fatalf("short mostly-inside ISB must be refused; got %+v refused %v", got, refused)
	}
}

// TestISBOrdersStampTheCandle — the production ISB emit stamps the entry candle
// high/low, so orbGateFilter can measure rule 5.
func TestISBOrdersStampTheCandle(t *testing.T) {
	cfg := DefaultConfig()
	long, short := ISBOrders(market.Kline{High: 110, Low: 90}, cfg)
	if long.EntryCandleHigh != 110 || long.EntryCandleLow != 90 {
		t.Fatalf("long ISB must stamp the candle high/low, got %v/%v", long.EntryCandleHigh, long.EntryCandleLow)
	}
	if short.EntryCandleHigh != 110 || short.EntryCandleLow != 90 {
		t.Fatalf("short ISB must stamp the candle high/low, got %v/%v", short.EntryCandleHigh, short.EntryCandleLow)
	}
}
