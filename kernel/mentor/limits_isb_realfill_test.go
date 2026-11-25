package mentor

import "testing"

// #5 — a REAL ISB fill never registered a G1 leg: RecordFill passed nil levels,
// the ISB pend carries no legExt (Apply sets it only on the PHL branch), so
// registerLeg's old-extreme fallback got nil and returned without a leg. A real
// ISB stop-out then never set Leg.Stopped, and the next same-side ISB/PHL was
// never refused by the leg budget. The fix passes the evaluator's levels into
// fill so the ISB pend resolves its leg extreme like the sim path does.
// MUTANT: restore `l.fill(p, lo, hi, nil)` in RecordFill → the ISB real fill
// creates no leg, the stop-out leaves Long.Stopped false, and the second ISB is
// emitted → RED.
func TestLimitsRealISBFillRegistersLeg(t *testing.T) {
	cfg := limitsCfg() // G1 leg budget ON
	cfg.RealFillOnly = true
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// ISB 1 placed (entry 92, stop 89) — an ISB pend (legExt 0).
	in := limitsISB(92, 89, 98, exp)
	in.ArmID = "isb-1"
	if out := applyAtCfg(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg); len(out) != 1 {
		t.Fatalf("ISB placement: want 1, got %d (refusals=%v)", len(out), l.Refusals)
	}

	// The REAL fill (with levels — old_extreme:100) must register the leg.
	l.RecordFill("sig-1", "isb-1", 90, 93, nil, limitsLvls)
	if l.Long == nil || l.Long.Entries != 1 || l.Long.Extreme != 100 {
		t.Fatalf("real ISB fill must register the long leg at extreme 100, got %+v", l.Long)
	}

	// A stop-out candle (low 88 <= stop 89) closes the leg.
	applyAtCfg(&l, nil, limitsK(91, 93, 90, 92, 2), limitsK(89, 90, 87, 88, 3), 3, cfg)
	if l.Long == nil || !l.Long.Stopped {
		t.Fatalf("a real ISB fill then a stop-out must set Leg.Stopped, got %+v", l.Long)
	}

	// The next same-side ISB is refused by the leg budget.
	out := applyAtCfg(&l, []Intent{limitsISB(95, 92, 101, exp)}, limitsK(89, 90, 87, 88, 3), limitsK(94, 96, 93, 95, 4), 4, cfg)
	if len(out) != 0 {
		t.Fatalf("second same-side ISB after a stopped leg: want 0, got %d (refusals=%v)", len(out), l.Refusals)
	}
	if l.Refusals["leg_budget_stopped"] == 0 {
		t.Fatalf("the second ISB must be refused with leg_budget_stopped, refusals=%v", l.Refusals)
	}
}
