package kernel

import (
	"strings"
	"testing"
)

// The pure freshness rule (the arm gate and the grader both call it).
func TestConfirmFreshAgeDistanceAndUnknown(t *testing.T) {
	t.Setenv("STALE_CONFIRM_ATR", "")
	sc := PlanScenario{ID: "S1", Condition: "hold", Direction: "long",
		Confirm:   &PlanConfirm{Rule: "time_hold", RefPrice: 100, Side: "above"},
		Economics: &ScenarioEconomics{Version: 1, EntryZone: []float64{100.5, 102}}}
	v := ConfirmVerdict{Met: true, EventMs: 1_000_000, EventKnown: true, RefPrice: 100}
	in := ConfirmFreshInput{NowMs: 1_000_000 + 29*60_000, Price: 103, ATR5m: 2, MaxAgeMs: 30 * 60_000}
	if ok, class, why := ConfirmFresh(sc, v, nil, 0, in); !ok {
		t.Fatalf("29 min old, 1 pt above the zone (< 2×ATR 4): fresh, got %s %s", class, why)
	}
	in.NowMs = 1_000_000 + 31*60_000
	if ok, class, _ := ConfirmFresh(sc, v, nil, 0, in); ok || class != "age" {
		t.Fatalf("31 min after the EVENT: stale by age, got ok=%v %s", ok, class)
	}
	in.NowMs = 1_000_000 + 60_000
	in.Price = 106.5 // 4.5 pts above the zone high 102 > 2×ATR5m 4
	if ok, class, why := ConfirmFresh(sc, v, nil, 0, in); ok || class != "distance" || !strings.Contains(why, "zone 100.00–102.00") {
		t.Fatalf("price 2×ATR away from the touch band: stale by distance, got ok=%v %s %q", ok, class, why)
	}
	in.Price = 101 // inside the band: distance 0
	if ok, _, _ := ConfirmFresh(sc, v, nil, 0, in); !ok {
		t.Fatal("inside the touch band is distance 0")
	}
	in.ATR5m = 0
	in.Price = 500
	if ok, _, _ := ConfirmFresh(sc, v, nil, 0, in); !ok {
		t.Fatal("no ATR5m: the distance leg fails open (the annotation's rule); age still binds")
	}
	v.EventKnown = false
	if ok, class, _ := ConfirmFresh(sc, v, nil, 0, in); ok || class != "event_unknown" {
		t.Fatalf("a MET verdict without an event instant cannot be aged: UNKNOWN does not satisfy, got ok=%v %s", ok, class)
	}
}

// N5 — an empty tape is UNKNOWN for BOTH gates (the #515 sweep gate and the
// touch gate): refusal missing_tape, never a market claim.
func TestConfirmGatesEmptyTapeIsUnknown(t *testing.T) {
	for _, cond := range []string{"reclaim", "hold"} {
		sc := PlanScenario{ID: "S1", Condition: cond, Direction: "long", Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 100, Side: "above"}}
		v := EvaluateScenarioConfirm(sc, nil, 1_000_000, 2_000_000)
		if v.Met || v.Outcome != "UNKNOWN" || v.Refusal != ConfirmRefusalMissingTape || VerdictWaitsForSweep(v) || VerdictWaitsForTouch(v) || !VerdictMissingTape(v) {
			t.Fatalf("%s on an empty tape: UNKNOWN/missing_tape, not a hold reason: %+v", cond, v)
		}
	}
}
