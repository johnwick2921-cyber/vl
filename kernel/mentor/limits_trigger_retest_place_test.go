package mentor

import "testing"

// #48 — G2 place for a trigger-line retest must be keyed BY PRICE, not by the
// constant "trigger_retest" key. Each position the 5m trigger line holds is its
// own support/resistance line (visitKey, r22 P1-2, D3.4 p2 @20:27), so losses
// at different line positions must not merge into one place (which would make
// two losses anywhere on the line turn the WHOLE line off for the day).
// MUTANT: drop the KindTriggerRetest case in placeID → the two different-line
// losses merge into one "trigger_retest" place → OffDay → RED.
func TestLimitsTriggerRetestPlaceKeyedByPrice(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg() // G2 only
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// Loss 1 at the trigger line position 100.
	applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "trigger_retest", 100)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg) // fill
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg) // stop → loss 1 at 100

	// Loss 2 at a DIFFERENT line position 150 — a distinct place.
	// The swing break frees the 100 place; place at 150, fill, stop.
	if out := applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "trigger_retest", 150)}, limitsK(70, 72, 59, 70, 3), limitsK(107, 108.5, 106, 108, 4), 4, cfg); len(out) != 1 {
		t.Fatalf("re-entry at 150: want 1 entry, got %d (refusals=%v)", len(out), l.Refusals)
	}
	applyAtG2(&l, nil, limitsK(107, 108.5, 106, 108, 4), limitsK(89, 91, 88.5, 89, 5), 5, cfg) // fill
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 5), limitsK(70, 72, 59, 70, 6), 6, cfg)       // stop → loss 2 at 150

	// The two places are distinct: each has ONE loss, neither is OffDay.
	p100 := l.Places["trigger_retest@100.00"]
	p150 := l.Places["trigger_retest@150.00"]
	if p100 == nil || p100.Losses != 1 || p100.OffDay {
		t.Fatalf("place at 100: %+v, want Losses=1 OffDay=false", p100)
	}
	if p150 == nil || p150.Losses != 1 || p150.OffDay {
		t.Fatalf("place at 150: %+v, want Losses=1 OffDay=false", p150)
	}
	if l.Places["trigger_retest"] != nil {
		t.Fatalf("the constant trigger_retest place must not exist, got %+v", l.Places["trigger_retest"])
	}
}

// TestLimitsTriggerRetestSamePriceTwoLossesOffDay pins the price key's own
// escalation: two losses at the SAME line position do turn that position off.
func TestLimitsTriggerRetestSamePriceTwoLossesOffDay(t *testing.T) {
	var l Limits
	cfg := limitsCfgNoLeg()
	prev := limitsK(94, 96, 94, 95, 0)
	exp := limitsNow(60) + 86400_000

	// Two losses at the same position 100.
	applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "trigger_retest", 100)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
	applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(89, 91, 88.5, 89, 2), 2, cfg)
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 2), limitsK(70, 72, 59, 70, 3), 3, cfg)
	if out := applyAtG2(&l, []Intent{limitsPHLAt(90, 88, 99, exp, "trigger_retest", 100)}, limitsK(70, 72, 59, 70, 3), limitsK(107, 108.5, 106, 108, 4), 4, cfg); len(out) != 1 {
		t.Fatalf("re-entry at 100: want 1, got %d", len(out))
	}
	applyAtG2(&l, nil, limitsK(107, 108.5, 106, 108, 4), limitsK(89, 91, 88.5, 89, 5), 5, cfg)
	applyAtG2(&l, nil, limitsK(89, 91, 88.5, 89, 5), limitsK(70, 72, 59, 70, 6), 6, cfg)
	if p := l.Places["trigger_retest@100.00"]; p == nil || !p.OffDay || p.Losses != 2 {
		t.Fatalf("same-price place: %+v, want Losses=2 OffDay=true", p)
	}
}
