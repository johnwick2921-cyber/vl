package mentor

import (
	"testing"

	"vl/market"
)

// TestTickFreezesHTFDuringThePrintWindow (item 18, D4.4-03): while the
// evaluator's `now` is inside [HTFFreezeFrom, HTFFreezeTo), the 4h/1h advance is
// SKIPPED — a break made by the 07:30 print candle never moves a line ("kệ nó"
// [D4.4 p1 @18:13, @22:15]).
func TestTickFreezesHTFDuringThePrintWindow(t *testing.T) {
	// Two 1h buckets: the 11:00 bar breaks the 09:00 bucket's high → the 1h line
	// would fire LONG.
	bars := []market.Kline{
		rthBars(0, 100, 100, 99, 99.5),     // 09:00, 1h bucket A (high 100)
		rthBars(120, 101, 106, 100.5, 105), // 11:00, 1h bucket B (breaks high 100)
	}
	now := bars[len(bars)-1].CloseTime + 1

	// Without a freeze the 1h line fires long.
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.Tick(bars, now)
	if e.State.HTF.OneH.Dir != SideLong {
		t.Fatalf("without a freeze the 1h line must fire long, got %q", e.State.HTF.OneH.Dir)
	}

	// Frozen: the same bars, with `now` inside the print freeze window, leave the
	// 1h line untouched.
	cfg2 := DefaultConfig()
	cfg2.Enabled = true
	cfg2.HTFFreezeFrom = now
	cfg2.HTFFreezeTo = now + 5*60_000
	e2 := New(cfg2)
	e2.Tick(bars, now)
	if e2.State.HTF.OneH.Dir != "" {
		t.Fatalf("during the print freeze the 1h line must not move, got %q", e2.State.HTF.OneH.Dir)
	}

	// Outside the freeze (now after To) the break applies again.
	cfg3 := DefaultConfig()
	cfg3.Enabled = true
	cfg3.HTFFreezeFrom = now - 10*60_000
	cfg3.HTFFreezeTo = now
	e3 := New(cfg3)
	e3.Tick(bars, now)
	if e3.State.HTF.OneH.Dir != SideLong {
		t.Fatalf("after the freeze window the 1h line must fire long, got %q", e3.State.HTF.OneH.Dir)
	}
}
