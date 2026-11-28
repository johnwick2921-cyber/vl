package mentor

import (
	"testing"

	"vl/market"
)

// TestM401FrameSwitchResetsEMATouchState is the DS-103 review pin for the
// 08:59→09:00 frame switch (item 1 of the m401 hard review). The level KEY
// (KindEMA34 = "ema34") is constant while its PRICE jumps from the 1m EMA34 to
// the 2m EMA34 at the switch. A stale wrong-way touch / ISB-only invalidation
// recorded against the 1m price must NOT suppress the freshly-priced 2m level:
// a frame switch must start a fresh visit for that level.
//
// The test drives the production Tick path with a warm evaluator, plants the
// stale state exactly as a wrong-way first touch at 08:58 would leave it
// (TouchWrongWay + ISBOnly["ema34"]), then ticks across 09:00 and asserts the
// invalidation is cleared while the level's price moved to the 2m value.
func TestM401FrameSwitchResetsEMATouchState(t *testing.T) {
	cfg := m401Cfg(true)
	cfg.TouchBandPts = 5 // wide band: the bars keep "touching" across the switch
	tape := m401Tape()
	e := New(cfg)
	first := 5 * 60 // seed through 08:00 CT
	Seed(e, tape[:first], tape[first-1].CloseTime+1)
	// Warm the 2m EMA so the schedule can select it after the switch.
	// A SMALL jump (1 pt) is the realistic 1m↔2m EMA34 difference at the switch.
	e.State.EMA2mWarmed = true
	e.State.EMA34x2 = e.State.EMA34 + 1
	e.State.EMA9x2 = e.State.EMA9 + 1

	// Tick to 08:59 (1m period) and plant the stale wrong-way invalidation as
	// a first touch on the 1m EMA34 would have left it.
	upto := barIndexAt(tape, auditMs(2026, 10, 21, 8, 59, 0))
	for n := first; n <= upto; n++ {
		if n < 2 {
			continue
		}
		e.Tick(tape[:n], BarCloseInstant(tape[n-1]))
	}
	plantedPx := e.State.EMA34
	e.State.Touches[string(KindEMA34)] = Touch{Outcome: TouchWrongWay, ApproachedFrom: SideShort, PriceAtTouch: plantedPx}
	e.State.ISBOnly[string(KindEMA34)] = true

	// Tick across 09:00 into the 2m period.
	upto = barIndexAt(tape, auditMs(2026, 10, 21, 9, 1, 0))
	for n := barIndexAt(tape, auditMs(2026, 10, 21, 8, 59, 0)) + 1; n <= upto; n++ {
		e.Tick(tape[:n], BarCloseInstant(tape[n-1]))
	}

	// The level's price must now be the 2m EMA (the switch happened).
	ema34Lvl := 0.0
	for _, l := range e.State.ChartLevels {
		if l.Kind == KindEMA34 {
			ema34Lvl = l.Price
		}
	}
	if ema34Lvl == 0 || ema34Lvl != e.State.EMA34x2 {
		t.Fatalf("after the switch the KindEMA34 price = %.6f, want the 2m EMA %.6f — the frame did not switch", ema34Lvl, e.State.EMA34x2)
	}

	// The bug under review: the invalidation recorded against the 1m price
	// must NOT carry over to the freshly-priced 2m level.
	if e.State.ISBOnly[string(KindEMA34)] {
		t.Fatalf("frame switch carried the stale ISB-only invalidation over to the 2m EMA price (fresh visit not started)")
	}
	// A FRESH visit may record its own touch on the new line (the wide band keeps
	// price touching it); what must not survive is the planted 1m-line touch.
	if tr := e.State.Touches[string(KindEMA34)]; tr.Outcome == TouchWrongWay && tr.PriceAtTouch != e.State.EMA34x2 && tr.ApproachedFrom == SideShort && tr.PriceAtTouch == plantedPx {
		t.Fatalf("frame switch carried the stale touch over to the 2m EMA price: %+v", tr)
	}
}

// barIndexAt returns the slice index i+1 for the bar OPENING at want (i.e. the
// exclusive prefix length that includes that bar).
func barIndexAt(bars []market.Kline, want int64) int {
	for i, b := range bars {
		if b.OpenTime == want {
			return i + 1
		}
	}
	return -1
}
