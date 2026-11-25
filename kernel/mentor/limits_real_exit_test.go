package mentor

import "testing"

// #47 — a broker exit must close the matching Limits.openTrade, so a later
// deep retrace through the ORIGINAL stop is not booked as a phantom stop-out.
// Only an exit at the ORIGINAL stop ("sl") counts as a loss; a break-even,
// trailed stop, partial, manual, news or EOD flat simply drops the trade.
// MUTANT: drop the RealExit call (or make it a no-op) → the BE-exited trade
// stays open and the retrace books Leg.Stopped + a blocked place → RED.
func TestLimitsRealExitDropsOpenTrade(t *testing.T) {
	cfg := limitsCfg()
	cfg.RealFillOnly = true
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000
	in := limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)
	in.ArmID = "lvl-9"
	if out := applyAtCfg(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg); len(out) != 1 {
		t.Fatalf("placement: want 1, got %d", len(out))
	}
	l.RecordFill("sig-3", "lvl-9", 90, 93, nil, nil)
	if l.Long == nil {
		t.Fatal("the real fill must register the long leg")
	}

	// The broker exits at break-even (NOT the original stop).
	l.RealExit("sig-3", false)

	// A later candle retraces through the ORIGINAL stop 88: no phantom loss.
	applyAtCfg(&l, nil, limitsK(91, 93, 90, 92, 2), limitsK(89, 90, 87, 88, 3), 3, cfg)
	if l.Long != nil && l.Long.Stopped {
		t.Fatalf("a BE exit must not phantom-book a stop-out, got %+v", l.Long)
	}
	if p := l.Places["key_level:97"]; p != nil && p.Blocked {
		t.Fatalf("a BE exit must not block the place, got %+v", p)
	}
}

// TestLimitsRealExitAtOriginalStopCountsLoss pins the wasStop half: the broker
// closes at the ORIGINAL stop, so the loss IS registered.
func TestLimitsRealExitAtOriginalStopCountsLoss(t *testing.T) {
	cfg := limitsCfg()
	cfg.RealFillOnly = true
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000
	in := limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)
	in.ArmID = "lvl-10"
	applyAtCfg(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	l.RecordFill("sig-4", "lvl-10", 90, 93, nil, nil)

	l.RealExit("sig-4", true) // the original stop filled
	if l.Long == nil || !l.Long.Stopped {
		t.Fatalf("an original-stop exit must register the loss, got %+v", l.Long)
	}
	if p := l.Places["key_level:97"]; p == nil || p.Losses != 1 || !p.Blocked {
		t.Fatalf("an original-stop exit must block the place with one loss, got %+v", p)
	}
}
