package mentor

import "testing"

// TestLimitsRealFillOnlyFeedsG1G2 is the FU-1 kernel pin: with RealFillOnly the
// simulated candle-touch fill never fires; only RecordFill (the broker's real
// fill) registers the G1 leg and opens the G2 loss trade. A never-placed order
// can then never phantom-fill and spend the budget. MUTANT: drop the realFill
// skip in simulate → the candle-touch registers (Long != nil BEFORE RecordFill)
// → RED.
func TestLimitsRealFillOnlyFeedsG1G2(t *testing.T) {
	cfg := limitsCfg()
	cfg.RealFillOnly = true

	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000
	in := limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)
	in.ArmID = "lvl-7"

	// Placement registers a real-fill pend.
	if out := applyAtCfg(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg); len(out) != 1 {
		t.Fatalf("placement: want 1, got %d", len(out))
	}
	// Candle 2 touches the entry (high 93 >= 90): a real-fill-only evaluator
	// must NOT fill it — the sim is no longer the fill source.
	applyAtCfg(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(91, 93, 90, 92, 2), 2, cfg)
	if l.Long != nil {
		t.Fatalf("real-fill-only: a candle touch must NOT fill (leg created by the sim)")
	}
	// The REAL fill registers exactly once.
	l.RecordFill("sig-1", "lvl-7", 90, 93, nil, nil)
	if l.Long == nil || l.Long.Entries != 1 || !l.Long.PHLFilled {
		t.Fatalf("real fill: want one PHL leg entry, got %+v", l.Long)
	}
	// Dedupe: the same receipt (a partial-then-full / retransmit) is a no-op
	// and must NOT count a spurious refusal.
	l.RecordFill("sig-1", "lvl-7", 90, 93, nil, nil)
	if l.Long.Entries != 1 {
		t.Fatalf("dedupe: a repeated receipt must not double-register, entries=%d", l.Long.Entries)
	}
	if l.Refusals["record_fill_no_pend"] != 0 {
		t.Fatalf("dedupe: a repeated receipt must not count a no-pend refusal, refusals=%v", l.Refusals)
	}
}

// TestLimitsRecordFillNoPend is the FU-1 fail-closed pin: a receipt whose pend
// is gone (expired, cancelled, or a foreign arm) is counted and never
// fabricated. MUTANT: fabricate a fill when the pend is missing → Long != nil.
func TestLimitsRecordFillNoPend(t *testing.T) {
	cfg := limitsCfg()
	cfg.RealFillOnly = true
	var l Limits
	l.RecordFill("sig-x", "no-such-arm", 92, 93, nil, nil)
	if l.Long != nil {
		t.Fatalf("a receipt with no pend must never fabricate a leg")
	}
	if l.Refusals["record_fill_no_pend"] != 1 {
		t.Fatalf("a missing-pend receipt must be counted, got refusals=%v", l.Refusals)
	}
}

// TestLimitsRecordFillLossBoxesOnStopOut pins that a real fill still feeds the
// G2 loss box through the existing open-trade simulation: fill, then a
// stop-out candle → one loss at the place, leg stopped.
func TestLimitsRecordFillLossBoxesOnStopOut(t *testing.T) {
	cfg := limitsCfg()
	cfg.RealFillOnly = true
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000
	in := limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)
	in.ArmID = "lvl-8"
	applyAtCfg(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	l.RecordFill("sig-2", "lvl-8", 90, 93, nil, nil)
	// A stop-out candle (low 87 <= stop 88) after the fill → one loss.
	applyAtCfg(&l, nil, limitsK(91, 93, 90, 92, 2), limitsK(89, 90, 87, 88, 3), 3, cfg)
	if l.Long == nil || !l.Long.Stopped {
		t.Fatalf("a real fill then a stop-out must close the leg, got %+v", l.Long)
	}
	if n := len(l.Places); n != 1 {
		t.Fatalf("the stop-out must box the place once, got %d places", n)
	}
}

// TestLimitsRecordFillSameCandleStopOut is the FU-1 P2-3 pin: a late drain
// misses the fill candle, so when the fill candle's low/high already crossed
// the stop, RecordFill must register the G2 loss immediately — not wait for a
// later candle that may never touch the stop. MUTANT: remove the stopOutOn
// check in fill → no loss registered (Places empty) → RED.
func TestLimitsRecordFillSameCandleStopOut(t *testing.T) {
	cfg := limitsCfg()
	cfg.RealFillOnly = true
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000
	in := limitsPHLAt(90, 88, 99, exp, "key_level:97", 97)
	in.ArmID = "lvl-9"
	applyAtCfg(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	// The fill candle's low (87) already crossed the stop (88).
	l.RecordFill("sig-3", "lvl-9", 87, 93, nil, nil)
	if l.Long == nil || l.Long.Entries != 1 || !l.Long.Stopped {
		t.Fatalf("a same-candle stop-out must register AND close the leg, got %+v", l.Long)
	}
	if n := len(l.Places); n != 1 {
		t.Fatalf("a same-candle stop-out must box the place once, got %d", n)
	}
}

// TestLimitsRecordFillEmptyReceipt is the FU-1 P2-5 pin: an empty receipt id is
// counted (record_fill_no_receipt) before any lookup, never a leg.
func TestLimitsRecordFillEmptyReceipt(t *testing.T) {
	var l Limits
	l.RecordFill("", "lvl-x", 90, 93, nil, nil)
	if l.Long != nil {
		t.Fatalf("an empty receipt must not register a leg")
	}
	if l.Counters["record_fill_no_receipt"] != 1 {
		t.Fatalf("an empty receipt must be counted, got %v", l.Counters)
	}
}

// TestLimitsRecordFillFromRow is the FU-1 P1-2 pin: a real fill with NO pend (a
// restart dropped the in-memory pends) registers from the armed ROW — the leg
// extreme resolves via the old-extreme fallback against the supplied levels.
// MUTANT: drop the row fallback branch in RecordFill → no leg → RED.
func TestLimitsRecordFillFromRow(t *testing.T) {
	var l Limits
	l.RecordFill("sig-r", "lvl-r", 90, 93, &FillRow{
		Side: SideLong, Entry: 90, Stop: 88, Target: 99, ISB: false,
	}, limitsLvls)
	if l.Long == nil || l.Long.Entries != 1 || !l.Long.PHLFilled {
		t.Fatalf("a row fallback must register the leg, got %+v", l.Long)
	}
	if l.Counters["record_fill_from_row"] != 1 {
		t.Fatalf("the row fallback must be counted, got %v", l.Counters)
	}
	if l.Refusals["record_fill_no_pend"] != 0 {
		t.Fatalf("a row fallback must not count a no-pend refusal, got %v", l.Refusals)
	}
}
