package mentor

// D2-27 (item 27) — G1 leg budget fixes:
//
// X15-4 [X15 @02:23–02:46]: a new leg's candidates are ISB (on the break) →
// PHL/PLH (on the backtest) → ISB; take at most 2 of 3. A PHL/PLH must be
// allowed as the 2nd entry AFTER an ISB (today it is refused with
// leg_budget_second_phl); a 2nd PHL/PLH and any 3rd entry stay refused.
//
// X15-5 [X15 @00:49, @01:18]: the two slots are the ORDERS YOU TOOK — the
// budget must count real FILLS, not emitted intents. A cancelled arm
// (CancelArm) must drop its pend so it never phantom-fills later; a
// zero-expiry level/box pend (B6, ExpiryMs 0) rests exactly one candle live
// (the trader's N12 next-candle default) and must not fill after that.

import "testing"

// TestLimitsPHLAllowedAfterISB pins X15-4 at Limits.Apply: an ISB fills
// first, then a PHL placed on the fill tick is ALLOWED; the PHL then fills
// and a 2nd PHL (and any 3rd entry) is refused.
// MUTANT: keep legVerdict's PHL branch at `leg.Entries >= 1` → the PHL
// after the ISB is refused (out2 == 0) → RED.
func TestLimitsPHLAllowedAfterISB(t *testing.T) {
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// ISB placed on candle 1 (not yet filled).
	if out := applyAt(&l, []Intent{limitsISB(92, 89, 98, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1); len(out) != 1 {
		t.Fatalf("ISB placement: want 1, got %d", len(out))
	}
	// Candle 2 fills the ISB (high 93 >= 92); the PHL placed on the SAME
	// tick must be allowed (ISB → PHL = candidates 1 and 2).
	out2 := applyAt(&l, []Intent{limitsPHL(90, 88, 99, exp)}, limitsK(93, 94, 92, 93, 1), limitsK(91, 93, 90, 92, 2), 2)
	if len(out2) != 1 {
		t.Fatalf("PHL after ISB fill: want 1 entry, got %d (refusals=%v)", len(out2), l.Refusals)
	}
	if l.Long == nil || l.Long.Entries != 1 || l.Long.PHLFilled {
		t.Fatalf("leg after ISB fill: want entries=1 PHLFilled=false, got %+v", l.Long)
	}
	// Candle 3 fills the PHL (high 95 >= 90); a 2nd PHL on the same tick is
	// refused (at most ONE PHL per leg).
	out3 := applyAt(&l, []Intent{limitsPHL(91, 89, 99, exp)}, limitsK(91, 93, 90, 92, 2), limitsK(94, 95, 93, 94, 3), 3)
	if len(out3) != 0 {
		t.Fatalf("second PHL: want 0, got %d (refusals=%v)", len(out3), l.Refusals)
	}
	if l.Long == nil || l.Long.Entries != 2 || !l.Long.PHLFilled {
		t.Fatalf("leg after PHL fill: want entries=2 PHLFilled=true, got %+v", l.Long)
	}
	// The budget is full: a 3rd entry (ISB) is refused.
	out4 := applyAt(&l, []Intent{limitsISB(93, 90, 99, exp)}, limitsK(94, 95, 93, 94, 3), limitsK(92, 94, 91, 93, 4), 4)
	if len(out4) != 0 {
		t.Fatalf("third entry: want 0, got %d", len(out4))
	}
}

// TestLimitsCancelArmDropsPend pins X15-5 (cancel) at Limits.Apply: a PHL
// cancelled by its ArmID before it fills must never register a later fill.
// MUTANT: drop the dropPend call in the CancelArm branch → the cancelled
// PHL fills on candle 3 (Long != nil) → RED.
func TestLimitsCancelArmDropsPend(t *testing.T) {
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	in := limitsPHL(90, 88, 99, exp)
	in.ArmID = "lvl-1"
	if out := applyAt(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1); len(out) != 1 {
		t.Fatalf("PHL placement: want 1, got %d", len(out))
	}
	// Candle 2 does NOT reach the entry; the arm is cancelled.
	applyAt(&l, []Intent{{Action: CancelArm, ArmID: "lvl-1"}}, limitsK(93, 94, 92, 93, 1), limitsK(85, 89, 84, 86, 2), 2)
	// Candle 3 reaches the entry — the cancelled order must NOT fill.
	applyAt(&l, nil, limitsK(85, 89, 84, 86, 2), limitsK(94, 95, 93, 94, 3), 3)
	if l.Long != nil {
		t.Fatalf("cancelled PHL must not register a fill, got %+v", l.Long)
	}
}

// TestLimitsZeroExpiryPendExpiresAfterOneCandle pins X15-5 (live expiry) at
// Limits.Apply: a zero-expiry level/box PHL (B6) rests exactly ONE candle
// live (the trader's N12 next-candle default); after that it must not fill.
// MUTANT: keep the `p.expiry != 0 && now >= p.expiry` guard only → the
// zero-expiry PHL rests forever and fills on candle 3 → RED.
func TestLimitsZeroExpiryPendExpiresAfterOneCandle(t *testing.T) {
	var l Limits
	prev := limitsK(94, 96, 94, 95, 0)

	in := limitsPHL(90, 88, 99, 0) // zero expiry (the level-order shape)
	in.ArmID = "lvl-1"
	if out := applyAt(&l, []Intent{in}, prev, limitsK(93, 94, 92, 93, 1), 1); len(out) != 1 {
		t.Fatalf("PHL placement: want 1, got %d", len(out))
	}
	// Candle 2 does NOT reach the entry → the zero-expiry order expires at
	// candle 2's close.
	applyAt(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(85, 89, 84, 86, 2), 2)
	// Candle 3 DOES reach the entry — but the order already expired.
	applyAt(&l, nil, limitsK(85, 89, 84, 86, 2), limitsK(94, 95, 93, 94, 3), 3)
	if l.Long != nil {
		t.Fatalf("expired zero-expiry PHL must not register a fill, got %+v", l.Long)
	}
}
