package kernel

import (
	"encoding/json"
	"os"
	"testing"
)

// N6 (rescan 10-14) — flip coherence judges the WIRE price of a market_in_zone
// arm. The real ASIA 10-13 v3 doc (plans row, zone_map dropped): flip "2x5m
// close below 30798.92 → short"; S1 long at VWAP authored 30798.92 (ON the
// line), zone 30798.67–30798.92 — the limit went out at 30798.75 (row 355),
// strictly past the line. Before N6 only S2 (authored 30769.75) was flagged.
func TestFlipCoherenceJudgesTheZoneWireEdge650(t *testing.T) {
	raw, err := os.ReadFile("testdata/planner_touch_plans_1013_1014.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx map[string]struct {
		Doc PlanDoc `json:"doc"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	doc := fx["asia_v3"].Doc
	got := map[string]float64{}
	for _, is := range FlipIncoherentScenarios(&doc) {
		got[is.Scenario] = is.Entry
	}
	if e, ok := got["S1"]; !ok || e != 30798.75 {
		t.Fatalf("S1's wire limit 30798.75 is strictly past the flip line 30798.92 — incoherent: %+v", got)
	}
	if _, ok := got["S2"]; !ok {
		t.Fatalf("S2 (authored 30769.75) stays incoherent: %+v", got)
	}
	// A short's wire edge is the zone LOW rounded UP.
	sc := PlanScenario{Direction: "short", Arm: &PlanArmSpec{Enabled: true, Policy: EntryPolicyMarketInZone},
		Economics: &ScenarioEconomics{EntryZone: []float64{100.1, 101}}}
	if e, ok := zoneWireEdge(sc, "short"); !ok || e != 100.25 {
		t.Fatalf("short wire edge = zone low rounded up to the tick: %v %v", e, ok)
	}
	sc.Arm.Policy = ""
	if _, ok := zoneWireEdge(sc, "short"); ok {
		t.Fatal("a legacy arm has no zone wire edge — its authored entry is the wire price")
	}
}
