package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"vl/kernel"
	"vl/market"
	"vl/store"
)

// These pins enter the existing production arm cycle. JSON knob input lets the
// pre-change tree compile and demonstrate behavioral RED before implementation.
func structuralFixture(t *testing.T, entry, lo, hi, target, authored, buffer, cost float64, missing bool, mirror ...bool) (armPathGolden, *AutoTrader, *store.Store) {
	t.Helper()
	id := "entry-zone"
	p := func(v float64) *float64 { return &v }
	d := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long"}, IdentityLevels: []kernel.PlanLevel{{ID: &id, Price: entry, Label: "PDL", Lo: p(lo), Hi: p(hi)}}, Levels: []kernel.PlanLevel{{ID: &id, Price: entry, Label: "PDL", Lo: p(lo), Hi: p(hi)}}, Scenarios: []kernel.PlanScenario{{ID: "S1", LevelID: &id, Condition: "reject", Direction: "long", Quality: "A", Trigger: "support rejection", Invalid: "below zone", Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: entry, Side: "above"}, Arm: &kernel.PlanArmSpec{Enabled: true, Entry: entry, Stop: authored, Target: target}, Economics: &kernel.ScenarioEconomics{FirstObstacle: &kernel.ScenarioObstacle{Price: p(target), Level: "resistance"}}}}}
	if !missing {
		d.Zones = &kernel.LevelZoneMap{At: time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation()), ATR5m: 20, Zones: []kernel.LevelZone{{Anchor: entry, Lo: p(lo), Hi: p(hi), Sources: []kernel.ZoneSource{{Price: entry, Label: "PDL", TF: "1m"}}}, {Anchor: target + 2, Lo: p(target), Hi: p(target + 4), Sources: []kernel.ZoneSource{{Price: target + 2, Label: "ONH", TF: "1m"}}}}}
	}
	identity := structuralTestIdentity(entry, "PDL")
	id = *identity.ID
	d.IdentityLevels = []kernel.PlanLevel{identity}
	d.Levels[0].ID = identity.ID
	b, _ := json.Marshal(d)
	if len(mirror) > 0 && mirror[0] {
		d.Bias.Direction = "short"
		d.Scenarios[0].Direction = "short"
		d.Scenarios[0].Confirm.Side = "below"
		if d.Zones != nil {
			d.Zones.Zones[1].Lo = p(target - 4)
			d.Zones.Zones[1].Hi = p(target)
		}
		b, _ = json.Marshal(d)
	}
	oneSetupFixtureDocOverride = string(b)
	mutate := func(c *store.StrategyConfig) {
		oneSetupOff(c) // isolate geometry from the independently tested selection checks
		raw, _ := json.Marshal(c.DayPlan)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		m["structural_stop"] = map[string]any{"buffer_points": buffer, "round_trip_cost_points": cost}
		raw, _ = json.Marshal(m)
		_ = json.Unmarshal(raw, c.DayPlan)

	}
	hook := func(at *AutoTrader, _ *store.Store, _ string) {
		at.oneSetupFactsForTest = func(now time.Time) oneSetupTestFacts {
			return oneSetupTestFacts{Price: entry, BandPts: 100, Candidates: []kernel.MapCandidate{{ID: &id, Identity: d.IdentityLevels[0], Price: entry, Names: []string{"PDL"}, Grade: "A"}}, Permission: map[string]kernel.FadeVerdict{"S1": {Evaluated: true, Permitted: true}}}
		}
		originalTape := market.FuturesBarsProvider
		market.FuturesBarsProvider = func(_ string, _ string, _ int) []market.Kline {
			bars := originalTape("MNQ", "1m", 80)
			for i := range bars {
				bars[i].Open = entry
				bars[i].Close = entry
				bars[i].High = entry + 10
				bars[i].Low = entry - 10
			}
			return bars
		}
	}
	g, at, st, _ := driveOneSetupArmPath(t, mutate, hook)
	return g, at, st
}

func TestStructuralStopF1F2ProductionArmCycle(t *testing.T) {
	g, _, _ := structuralFixture(t, 29010, 29000, 29010, 29120, 28960, 5, 2, false)
	if len(g.Rows) != 1 || g.Rows[0]["stop"] != 28995.0 {
		t.Fatalf("F1/F2: structural stop 28995 must stand despite authored/ATR floors; rows=%+v", g.Rows)
	}
}

func TestStructuralStopF1F2ShortProductionArmCycle(t *testing.T) {
	g, _, _ := structuralFixture(t, 29000, 29000, 29010, 28890, 29050, 5, 2, false, true)
	if len(g.Rows) != 1 || g.Rows[0]["stop"] != 29015.0 {
		t.Fatalf("short structural stop must be 29015, rows=%+v", g.Rows)
	}
}

func TestStructuralStopF5FirstTargetIsNeverSkipped(t *testing.T) {
	p := func(x float64) *float64 { return &x }
	id := "entry"
	z := func(a, lo, hi float64) kernel.LevelZone {
		return kernel.LevelZone{Anchor: a, Lo: p(lo), Hi: p(hi), Sources: []kernel.ZoneSource{{Price: a, Label: "level", TF: "1m"}}}
	}
	identity := structuralTestIdentity(100, "level")
	id = *identity.ID
	doc := kernel.PlanDoc{IdentityLevels: []kernel.PlanLevel{identity}, Zones: &kernel.LevelZoneMap{Zones: []kernel.LevelZone{z(100, 99, 101), z(104, 104, 105), z(150, 150, 151)}}}
	sc := kernel.PlanScenario{ID: "S1", LevelID: &id, Direction: "long"}
	policy := store.StructuralStopPolicy{BufferPoints: 5, BufferKnown: true, CostPoints: 2, CostKnown: true, MinRR: 2}
	r := ComposeLevelFadeGeometry(&doc, sc, kernel.PlanArmLeg{Entry: 100}, policy, 20, .25, 2)
	if r.Reason != "rr" || r.Stop == nil || *r.Stop != 94 || r.Target == nil || *r.Target != 104 {
		t.Fatalf("freeze nearest target and structural stop; refuse without repair: %+v", r)
	}
}

func structuralRecord(t *testing.T, st *store.Store) map[string]any {
	t.Helper()
	var rows []struct{ Value string }
	if err := st.GormDB().Raw("SELECT value FROM system_config WHERE key LIKE ? ORDER BY key DESC", "structural_geometry:%").Scan(&rows).Error; err != nil || len(rows) == 0 {
		t.Fatalf("missing durable geometry/refusal record: n=%d err=%v", len(rows), err)
	}
	var r map[string]any
	if err := json.Unmarshal([]byte(rows[0].Value), &r); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestStructuralStopF3FallbackRefusesMissingProvenance(t *testing.T) {
	g, _, st := structuralFixture(t, 29010, 29000, 29010, 29120, 28960, 5, 2, true)
	r := structuralRecord(t, st)
	if len(g.Rows) != 0 || r["stop_source"] != "atr_fallback" || r["stop"] != 28980.0 || r["quantity"] != float64(0) || r["reason"] != "no_provenance" {
		t.Fatalf("F3 fallback calculation cannot authorize absent structure: rows=%+v record=%+v", g.Rows, r)
	}
}
func TestStructuralStopF4F5Refuses23For4WithoutMoving(t *testing.T) {
	g, _, st := structuralFixture(t, 29010, 28992, 29010, 29014, 28987, 5, 2, false)
	r := structuralRecord(t, st)
	if len(g.Rows) != 0 || r["quantity"] != float64(0) || r["reason"] != "rr" || r["stop"] != 28987.0 || r["target"] != 29014.0 {
		t.Fatalf("F4/F5 refuse unchanged 23-for-4; rows=%+v record=%+v", g.Rows, r)
	}
}
func TestStructuralStopF6NetGainBeforeRR(t *testing.T) {
	g, _, st := structuralFixture(t, 29010, 29009.75, 29010, 29012, 29009.5, .25, 2, false)
	r := structuralRecord(t, st)
	if len(g.Rows) != 0 || r["quantity"] != float64(0) || !strings.Contains(r["reason"].(string), "net") {
		t.Fatalf("F6 nonpositive net gain must refuse despite 4R; rows=%+v record=%+v", g.Rows, r)
	}
}

func TestStructuralStopF6BothFailNetReasonComesFirst(t *testing.T) {
	_, _, st := structuralFixture(t, 29010, 29006, 29010, 29012, 29005, 1, 2, false)
	r := structuralRecord(t, st)
	if r["reason"] != "net_nonpositive" {
		t.Fatalf("net check must precede RR: %+v", r)
	}
}

func TestStructuralGeometryReportsContractRiskWithoutPerTradeCap(t *testing.T) {
	z := func(lo, hi float64) kernel.LevelZone {
		return kernel.LevelZone{Lo: geometryNumber(lo), Hi: geometryNumber(hi), Sources: []kernel.ZoneSource{{Label: "fixture"}}}
	}
	zones := []kernel.LevelZone{z(95, 100), z(120, 121)}
	p := store.StructuralStopPolicy{BufferPoints: 1, BufferKnown: true, CostPoints: 2, CostKnown: true, MinRR: 2}
	for _, v := range []struct{ pointValue, loss float64 }{{2, 16}, {20, 160}} {
		r := ComposeFrozenLevelFadeGeometry(zones, 0, "long", 100, p, 20, .25, v.pointValue)
		if r.Reason != "" || r.Stop == nil || *r.Stop != 94 || r.Target == nil || *r.Target != 120 || r.LossUSD == nil || *r.LossUSD != v.loss || r.RiskCapUSD != nil {
			t.Fatalf("automatic structural prices and contract exposure must not require a per-trade cap: %+v", r)
		}
	}
}

func TestStructuralStopProductionCycleStillRefusesDailyLossTrip(t *testing.T) {
	kernel.SetDailyForceFlat("trader-1", "daily loss limit hit (realized today=-492.00, limit=-450.00)")
	t.Cleanup(func() { kernel.ClearDailyForceFlat("trader-1") })
	g, _, st := structuralFixture(t, 29010, 29000, 29010, 29120, 28960, 5, 2, false)
	r := structuralRecord(t, st)
	if len(g.Rows) != 0 || r["quantity"] != float64(0) || r["reason"] != "entry_gate" || !strings.Contains(r["detail"].(string), "daily_force_flat") {
		t.Fatalf("the production arm path must retain daily-loss refusal: rows=%+v record=%+v", g.Rows, r)
	}
	if r["stop"] != 28995.0 || r["target"] != 29120.0 {
		t.Fatalf("daily refusal must not repair structural prices: %+v", r)
	}
}
