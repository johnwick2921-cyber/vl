package trader

import (
	"encoding/json"
	"fmt"
	"nofx/store"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
)

// Gate-level pin: the production recorder and gate resolver must agree on
// which version/anchor a timestamp describes. Both versions reuse S1.
func TestPlanLivenessVersionAnchorPin(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	now, _ := time.Parse(time.RFC3339, "2026-09-08T10:00:00-05:00")
	plan := &kernel.ActivePlan{PlanID: "2026-09-08:NY:test", Session: "NY", Version: 1}
	var tape []market.Kline
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return tape }
	t.Cleanup(func() {
		market.FuturesBarsProvider = old
		kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{})
	})
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
	for version, anchor := range []float64{29687.5, 29753.25} {
		plan.Version = version + 1
		stamp := now.Add(time.Duration(version) * time.Hour)
		plan.BirthMs = stamp.Add(-30 * time.Minute).UnixMilli()
		plan.Doc = kernel.PlanDoc{Levels: []kernel.PlanLevel{{Price: anchor, Label: "ONH", Grade: "A"}}, Scenarios: []kernel.PlanScenario{{ID: "S1", Direction: "short", Condition: "reject", Trigger: fmt.Sprintf("reject %.2f", anchor)}}}
		tape = barsClosingAboveSince(stamp.Add(-25*time.Minute), anchor, 20)
		at.recordScenarioStateAt(stamp)
		got, ok := at.scenarioInvalidationResolverClock(plan, func() time.Time { return stamp })("S1")
		if !ok || !got.Invalidated || got.Anchor != anchor || got.AtCT != kernel.FormatCT(stamp) {
			t.Fatalf("v%d S1 must retain its own anchor %.2f and death time %s; got %+v evaluable=%v", plan.Version, anchor, kernel.FormatCT(stamp), got, ok)
		}
	}
}

// Live replay: plans row 265, ASIA v2 S1; bars row 451050 and its five
// constituent minute rows 451031/451034/451037/451039/451051. No legacy stamp.
func TestPlanLivenessBornDeadWritePin(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	now, _ := time.Parse(time.RFC3339, "2026-09-07T22:03:44-05:00")
	start := time.UnixMilli(1788836100000)
	closes := []float64{29668.75, 29668.5, 29668.5, 29665.25, 29661.5}
	var tape []market.Kline
	for i, c := range closes {
		o := start.Add(time.Duration(i) * time.Minute)
		tape = append(tape, market.Kline{OpenTime: o.UnixMilli(), CloseTime: o.Add(time.Minute).UnixMilli(), Open: c, High: c + 1, Low: c - 1, Close: c})
	}
	old := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline { return tape }
	t.Cleanup(func() { market.FuturesBarsProvider = old })
	bad := strings.Replace(validTraderPlanJSON, "2x5m<15470", "5m close below 29664.50 (SWG-H·15m) kills the setup", 1)
	// W2 A1: the repair must be INSIDE the grammar — "the auction changes
	// character" (the pre-W2 re-author here) is now a grammar refusal. A
	// conformant, unbreached line on this tape (closes 29661.50–29668.75).
	good := strings.Replace(validTraderPlanJSON, "2x5m<15470", "5m close below 29600.00", 1)
	calls := 0
	ver, lc, err := at.runPlannerReadCoreWithFactsGradesClock(func() time.Time { return now }, "ASIA", "2026-09-07", "", "model", "hash", "", "", "", "", kernel.PlanFacts{}, nil, nil, nil, true, func(string) (string, error) {
		calls++
		if calls == 1 {
			return bad, nil
		}
		return good, nil
	})
	if err != nil || ver != 1 || lc != "active" || calls != 2 {
		t.Fatalf("born-dead candidate must retry before publication; unsupported repair accepted: version=%d lifecycle=%s calls=%d err=%v", ver, lc, calls, err)
	}
	row, _ := at.store.Plan().GetLatestPlanForSession("2026-09-07", "ASIA")
	if row == nil || strings.Contains(row.Doc, "29664.50 (SWG-H") {
		t.Fatalf("published born-dead candidate: %+v", row)
	}
}

func TestPlanLivenessExhaustionWarningOncePerVersion(t *testing.T) {
	for _, hour := range []string{"08:00:00", "23:45:00"} {
		t.Run(hour, func(t *testing.T) {
			at := plannerTestTrader(t)
			at.config.NinjaTraderSymbol = "MNQ"
			now, _ := time.Parse(time.RFC3339, "2026-09-08T"+hour+"-05:00")
			plan := &kernel.ActivePlan{PlanID: "2026-09-08:ASIA:test", Session: "ASIA", Version: 1, BirthMs: now.Add(-30 * time.Minute).UnixMilli(), Doc: kernel.PlanDoc{Levels: []kernel.PlanLevel{{Price: 30000, Label: "ONH", Grade: "A"}}, Scenarios: []kernel.PlanScenario{{ID: "S1", Direction: "short", Condition: "reject", Trigger: "reject 30000"}}}}
			old := market.FuturesBarsProvider
			market.FuturesBarsProvider = func(string, string, int) []market.Kline {
				return barsClosingAboveSince(now.Add(-25*time.Minute), 30000, 20)
			}
			kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
			t.Cleanup(func() {
				market.FuturesBarsProvider = old
				kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{})
			})
			at.lastPlannerWakeAt = now.Add(-4 * time.Minute)
			for i := 0; i < 3; i++ {
				at.recordScenarioStateAt(now.Add(time.Duration(i) * time.Minute))
			}
			counts, err := at.store.PlanLivenessCounts()
			if err != nil || counts.ExhaustionWarnings != 1 {
				t.Fatalf("exhaustion must record one warning per version despite repeated cycles and 26m throttle remaining: counts=%+v err=%v", counts, err)
			}
			if !at.lastPlannerWakeAt.Equal(now.Add(-4 * time.Minute)) {
				t.Fatal("warn-only exhaustion changed the planner wake clock")
			}
			plan.Version++
			at.recordScenarioStateAt(now.Add(4 * time.Minute))
			counts, _ = at.store.PlanLivenessCounts()
			if counts.ExhaustionWarnings != 2 {
				t.Fatalf("new version must get its own warning: %+v", counts)
			}
		})
	}
}

func TestPlanLivenessBootAndDeskUseRecordedFacts(t *testing.T) {
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	now, _ := time.Parse(time.RFC3339, "2026-09-08T10:00:00-05:00")
	plan := &kernel.ActivePlan{PlanID: "2026-09-08:NY:test", Session: "NY", Version: 2, Doc: kernel.PlanDoc{Scenarios: []kernel.PlanScenario{{ID: "S1"}}}}
	kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{ActivePlan: func(string) *kernel.ActivePlan { return plan }})
	t.Cleanup(func() { kernel.SetTraderPlanProviders(at.id, kernel.TraderPlanProviders{}) })
	meta, _ := json.Marshal(map[string]any{"observed_at": now})
	at.store.SetSystemConfig(store.ScenarioMetaKey(at.id, plan.PlanID, plan.Version), string(meta))
	at.store.SetSystemConfig(store.ScenarioStatusKey(at.id, plan.PlanID, plan.Version), `{"S1":"invalidated"}`)
	at.store.RecordPlanLivenessEvent(store.LivenessExhaustionWarning, "test-version", now, "test")
	line := at.deskPlanner(now)
	if !strings.Contains(line.Text, "tradeable 0/1") || !strings.Contains(line.Text, "EXHAUSTED") || line.State != "warn" {
		t.Fatalf("desk did not read liveness: %+v", line)
	}
	at.store.RecordPlanLivenessEvent(store.LivenessAuthoredGrammarRefusal, "test-grammar", now, "test")
	got := PlanLivenessBootLine(at.store)
	if !strings.Contains(got, "exhausted-warnings=1") {
		t.Fatalf("boot did not read counter: %s", got)
	}
	// W2 A1 — the policy is READ from the kernel's one source, and the grammar
	// refusals are their own recorded count; the old count is tape-only now.
	if !strings.Contains(got, "invalidation: "+kernel.AuthoredInvalidationPolicy()) || kernel.AuthoredInvalidationPolicy() != "enforced (grammar)" ||
		!strings.Contains(got, "grammar refusals=1") || !strings.Contains(got, "authored UNKNOWN(tape; pre-W2 events mix grammar+tape)=0") {
		t.Fatalf("boot line must read the invalidation policy and the grammar-refusal count: %s", got)
	}
	if got := PlanLivenessBootLine(nil); !strings.Contains(got, "UNKNOWN") || !strings.Contains(got, "invalidation: enforced (grammar)") {
		t.Fatalf("unavailable store invented counts (or dropped the code-known policy): %s", got)
	}
}
