package kernel

import (
	"strings"
	"testing"
	"time"

	"vl/market"
	"vl/store"
	"vl/telemetry"
)

// pinDecisionCycleNow pins ShouldSkipDecisionCycle's clock to an in-session
// CME-open instant (a Wednesday 10:00 CT) so the HOLD-path assertions below
// never depend on the wall clock (they fail with cme_closed on weekends and
// during the 16:00–17:00 CT break).
func pinDecisionCycleNow(t *testing.T) {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load Chicago tz: %v", err)
	}
	decisionCycleNow = func() time.Time {
		return time.Date(2026, 9, 30, 10, 0, 0, 0, loc) // Wednesday, in session
	}
	t.Cleanup(func() { decisionCycleNow = time.Now })
}

// TestExecHoldPathsCountGateBlocks pins P2-7 at the PRODUCTION call site:
// GetFullDecisionWithStrategy's two HOLD branches
// (executor_plan_gate, schema_parse_failed) must land in the gate-block
// table that /api/risk/gate-blocks serves — a cycle-holding gate that is not
// counted is invisible to the operator.
func TestExecHoldPathsCountGateBlocks(t *testing.T) {
	pinDecisionCycleNow(t)

	trader := "p27-holder"
	// The active symbol defaults to MNQ with no positions/candidates; supply its
	// market data so the m150-5 fail-closed gate (market_data_unavailable) does
	// NOT fire first — these HOLD branches exercise the AI call paths.
	ctx := &Context{
		TraderID: trader,
		Account:  AccountInfo{TotalEquity: 60000},
		MarketDataMap: map[string]*market.Data{
			"MNQ": {Symbol: "MNQ", CurrentPrice: 30000},
		},
	}
	engine := &StrategyEngine{config: &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{MaxPositions: 1},
	}}

	// Case 1 — schema_parse_failed: three garbage replies exhaust the retry
	// loop; the cycle HOLDs and the counter must record the class.
	garbage := &mockAIClient{replies: []string{"{not json", "still not json", "nope"}}
	fd, err := GetFullDecisionWithStrategy(ctx, garbage, engine, "futures")
	if err != nil {
		t.Fatalf("a parse HOLD must not error: %v", err)
	}
	if fd == nil || fd.SkipReason != "schema_parse_failed" {
		t.Fatalf("want SkipReason schema_parse_failed, got %+v", fd)
	}
	if !gateBlockCountHas(t, trader, "schema_parse_failed") {
		t.Fatalf("schema_parse_failed HOLD not counted in gate-block table")
	}

	// Case 2 — executor_plan_gate: a VALID open decision against a dead plan
	// must HOLD with the typed refusal and count its class. The take_profit is
	// ≥3× the risk so the R:R gate (now reachable with market data present)
	// passes first and the plan gate is what refuses.
	ctx.ExecutorPlanDead = "plan machine-dead (test)"
	valid := &mockAIClient{replies: []string{
		`{"action":"open_long","symbol":"MNQ","entry":30000,"stop_loss":29900,"take_profit":30400,"reasoning":"test","leverage":1,"confidence":80,"position_size_usd":60000}`,
	}}
	fd2, err2 := GetFullDecisionWithStrategy(ctx, valid, engine, "futures")
	if err2 != nil {
		t.Fatalf("a gate HOLD must not error: %v", err2)
	}
	if fd2 == nil || !strings.HasPrefix(fd2.SkipReason, "executor_plan_gate:") {
		t.Fatalf("want SkipReason executor_plan_gate:…, got %+v", fd2)
	}
	if !gateBlockCountHas(t, trader, "executor_plan_gate") {
		t.Fatalf("executor_plan_gate HOLD not counted in gate-block table")
	}
}

func gateBlockCountHas(t *testing.T, trader, class string) bool {
	t.Helper()
	_, table := telemetry.GateBlockSnapshot()
	if table == nil {
		return false
	}
	n := table[trader][class]
	return n > 0
}
