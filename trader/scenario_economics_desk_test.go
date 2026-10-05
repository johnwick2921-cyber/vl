package trader

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"vl/kernel"
)

func TestScenarioEconomicsOnProductionDesk(t *testing.T) {
	now := time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC)
	b, err := os.ReadFile("../kernel/testdata/scenario-economics/plan-265.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc kernel.PlanDoc
	json.Unmarshal(b, &doc)
	s := doc.Scenarios[1]
	p, r1, r2 := 29671.42, 6.92/24.5, 56.75/24.5
	s.Economics = &kernel.ScenarioEconomics{FirstObstacle: &kernel.ScenarioObstacle{Price: &p, Level: "reference", Family: "reference", Response: "pass_through"}, RToObstacle: &r1, RToArmTarget: &r2}
	doc.Scenarios = []kernel.PlanScenario{s}
	plan := &kernel.ActivePlan{Doc: doc, PlanID: "2026-09-08:NY:econ-test", Session: "NY", Version: 2, BirthMs: now.Add(-time.Minute).UnixMilli()}
	kernel.SetTraderPlanProviders("econ-desk", kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
	defer kernel.SetTraderPlanProviders("econ-desk", kernel.TraderPlanProviders{})
	at := &AutoTrader{id: "econ-desk", store: deskStore(t), config: AutoTraderConfig{NinjaTraderSymbol: "MNQ"}}
	strip := at.DeskStripAt(now)
	found := false
	for _, l := range strip.Lines {
		if l.Key != "scenarios" {
			continue
		}
		found = true
		if !strings.Contains(l.Text, "obstacle R=0.282449") || !strings.Contains(l.Text, "arm R=2.316327") {
			t.Fatalf("Rs absent on production desk: %s", l.Text)
		}
	}
	if !found {
		t.Fatal("economics helper is not wired into DeskStripAt")
	}
	plan.Doc.Scenarios[0].Economics = nil
	for _, l := range at.DeskStripAt(now).Lines {
		if l.Key == "scenarios" && (l.State != "unknown" || !strings.Contains(l.Text, "arm R=UNKNOWN")) {
			t.Fatalf("legacy inferred: %+v", l)
		}
	}
}

func TestScenarioEconomicsWriteRefusesThenPersistsContract(t *testing.T) {
	at := plannerTestTrader(t)
	// W-WRITE-TIME-FEASIBILITY: the reject arm has no frozen zone map (geometry
	// gate would refuse it). Knob OFF pins the economics contract flow
	// byte-identical to pre-wave (spec d).
	feasOff := false
	at.config.StrategyConfig.DayPlan.WriteTimeFeasibility = &feasOff
	now := time.Date(2026, 9, 8, 19, 0, 0, 0, time.UTC)
	good := class39LegsPlanJSON("15550")
	bad := strings.Replace(good, `"target_chain": [15550, 15620]`, `"target_chain": [15620]`, 1)
	if bad == good {
		t.Fatal("fixture mutation missed actual path")
	}
	calls := 0
	before := kernel.ScenarioEconomicsCounters()
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(func() time.Time { return now }, "NY", "2026-09-08", "owner_reset", "model", "hash", "", "", "", "", kernel.PlanFacts{}, nil, nil, nil, true, func(string) (string, error) {
		calls++
		if calls == 1 {
			return bad, nil
		}
		return good, nil
	})
	if err != nil || lc != "active" || calls != 2 {
		t.Fatalf("write must reject off-path attempt then accept repaired contract: lc=%s calls=%d err=%v", lc, calls, err)
	}
	row, err := at.store.Plan().GetLatestPlanForSession("2026-09-08", "NY")
	if err != nil || row == nil {
		t.Fatal(err)
	}
	var doc kernel.PlanDoc
	if err = json.Unmarshal([]byte(row.Doc), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Scenarios[0].Economics == nil || doc.Scenarios[0].Economics.Version != kernel.ScenarioEconomicsContractVersion {
		t.Fatal("new contract was not persisted by the existing plan writer")
	}
	after := kernel.ScenarioEconomicsCounters()
	if after.Contradictions != before.Contradictions+1 {
		t.Fatalf("refusal counter mismatch: %+v -> %+v", before, after)
	}
}
