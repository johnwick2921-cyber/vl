package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// plan-lifecycle wave (2026-08-27) — trader-side: dormant/re-arm, budget,
// reasoning wire routing.

func TestExecPlanReasoningWireDefaults(t *testing.T) {
	t.Setenv("AI_EXEC_REASONING", "")
	t.Setenv("AI_PLAN_REASONING", "")
	m, e := execReasoningWire()
	if m != "enabled" || e != "low" {
		t.Fatalf("exec default = %s/%s, want enabled/low (fast)", m, e)
	}
	pm, pe := planReasoningWire()
	if pm != "enabled" || pe != "max" {
		t.Fatalf("plan default = %s/%s, want enabled/max", pm, pe)
	}
}

func TestExecPlanReasoningWireOff(t *testing.T) {
	t.Setenv("AI_EXEC_REASONING", "off")
	m, e := execReasoningWire()
	if m != "disabled" || e != "" {
		t.Fatalf("exec off = %s/%q, want disabled/\"\"", m, e)
	}
}

func TestReasoningWireUnknownKnobFallsBack(t *testing.T) {
	m, e, l := reasoningWire("nonsense", "fast")
	if m != "enabled" || e != "low" || l == "" {
		t.Fatalf("unknown knob must fall back to the default: %s/%s/%s", m, e, l)
	}
}

func TestUpdatePlanLifecycleDormantRearmRoundTrip(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	td := "2026-08-18"
	pid := store.MakePlanIDForTrader(at.id, td, "NY")
	v, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: "{}"})
	if err != nil || v != 1 {
		t.Fatalf("seed plan: v=%d err=%v", v, err)
	}
	if err := st.Plan().UpdatePlanLifecycle(pid, 1, "dormant", "dormant:flip-condition: test"); err != nil {
		t.Fatalf("dormant write: %v", err)
	}
	row, err := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if err != nil || row == nil || row.Lifecycle != "dormant" || row.Version != 1 {
		t.Fatalf("after dormant: %+v err=%v", row, err)
	}
	if err := st.Plan().UpdatePlanLifecycle(pid, 1, "active", "rearmed:test"); err != nil {
		t.Fatalf("rearm write: %v", err)
	}
	row, _ = st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if row.Lifecycle != "active" || row.Version != 1 {
		t.Fatalf("after rearm: lifecycle=%s version=%d (same version required)", row.Lifecycle, row.Version)
	}
}

// barsAt installs a deterministic FuturesBarsProvider for the test window.
func barsAt(bars []market.Kline) {
	market.FuturesBarsProvider = func(symbol string, tf string, count int) []market.Kline {
		return bars
	}
	traderTestBarsInstalled = true
}

var traderTestBarsInstalled = false

func TestFlipDeathMarksDormantAndSkipsBudget(t *testing.T) {
	t.Setenv("FLIP_ATR_BUFFER", "0")
	t.Setenv("DORMANT_MIN_HOLD_MIN", "0")
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: 4, SessionsEnabled: []string{"NY"}}}
	at, st := resetTrader(t, cfg)
	// synthetic now inside the NY window (08:30–14:45 CT): 14:00 UTC = 09:00 CT.
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	td := "2026-08-18"
	pid := store.MakePlanIDForTrader(at.id, td, "NY")
	birth := now.Add(-40 * time.Minute)
	doc := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", FlipCondition: "flips short on 2x5m below 100"}, FlipStructured: &kernel.PlanCondition{Price: 100, Side: "below", Rule: "2x5m"}}
	blob, _ := json.Marshal(doc)
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: "NY", StrategyID: at.id, Lifecycle: "active", Doc: string(blob), CreatedAt: birth}); err != nil {
		t.Fatal(err)
	}
	// bars: 14 flat minutes at 100 then 10 minutes closing at 96, ending ~6 min
	// before the synthetic now (fresh for the G7 staleness gate).
	t0 := birth.Add(10 * time.Minute).UnixMilli()
	bars := make([]market.Kline, 0, 30)
	for i := 0; i < 14; i++ {
		ot := t0 + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 60_000 - 1, Open: 100, High: 100, Low: 100, Close: 100})
	}
	for i := 0; i < 10; i++ { // two 5m buckets closing below the line
		ot := t0 + int64(14+i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 60_000 - 1, Open: 100, High: 100, Low: 96, Close: 96})
	}
	barsAt(bars)
	defer func() { market.FuturesBarsProvider = nil; traderTestBarsInstalled = false }()

	at.maybeRunSessionReadsAt(now)

	row, err := st.Plan().GetLatestPlanForTraderSession(td, "NY", at.id)
	if err != nil || row == nil {
		t.Fatalf("read back: %v", err)
	}
	if row.Lifecycle != "dormant" {
		t.Fatalf("flip death must mark DORMANT, got lifecycle=%s trigger=%s", row.Lifecycle, row.TriggerReason)
	}
	if row.Version != 1 {
		t.Fatalf("dormant must NOT write a new version (got v%d)", row.Version)
	}
	if !strings.HasPrefix(row.TriggerReason, "dormant:flip:") {
		t.Fatalf("trigger marker wrong: %q", row.TriggerReason)
	}
	// budget untouched: no no_trade row anywhere in the chain.
	rows, _ := st.Plan().ListVersionsForTrader(td, "NY", at.id)
	for _, r := range rows {
		if r.Lifecycle == "no_trade" {
			t.Fatalf("terminal no_trade must never appear from a flip death")
		}
	}
}

func TestDormantPlanBlocksEntriesGate(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC) // NY window
	testNow = func() time.Time { return now }
	defer func() { testNow = nil }()
	td := "2026-08-18"
	pid := store.MakePlanIDForTrader(at.id, td, "NY")
	if _, err := st.Plan().AppendPlan(&store.PlanDB{PlanID: pid, TradeDate: td, Session: "NY", StrategyID: at.id, Lifecycle: "dormant", TriggerReason: "dormant:flip-condition: 2x5m close below 29212.50", Doc: "{}"}); err != nil {
		t.Fatal(err)
	}
	reason := at.executorPlanDeadReason()
	if !strings.Contains(reason, "dormant") || !strings.Contains(reason, "refused") {
		t.Fatalf("dormant must block entries with an honest reason, got %q", reason)
	}
	// the gate verdict itself must block opens and pass management actions.
	if blocked, _ := kernel.ExecutorPlanDeadVerdict("open_long", reason); !blocked {
		t.Fatalf("open_long must be blocked while dormant")
	}
	if blocked, _ := kernel.ExecutorPlanDeadVerdict("close_long", reason); blocked {
		t.Fatalf("position management must NOT be blocked while dormant")
	}
}
