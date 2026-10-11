package trader

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"vl/store"
)

// P5 (ledger-close 2026-08-19) — 402 outage alerting. The 08-18 incident: 139
// dead cycles, 4.3 hours, zero alerts.

func ai402Trader(t *testing.T) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "a402.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at := &AutoTrader{id: "t402", store: st, exchange: "ninjatrader"}
	at.config.Exchange = "ninjatrader"
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	return at, st
}

func alertCount(t *testing.T, st *store.Store, kind string, ackedToo bool) (n, unacked int) {
	t.Helper()
	alerts, _ := st.Alert().List("t402", 200)
	for _, a := range alerts {
		if a.Kind != kind {
			continue
		}
		n++
		if !a.Acked {
			unacked++
		}
	}
	_ = ackedToo
	return n, unacked
}

func TestClassifyAIError(t *testing.T) {
	deepseek402 := errors.New(`Failed to get AI decision: AI API call failed: API returned error (status 402): {"error":{"message":"Insufficient Balance"}}`)
	if got := classifyAIError(deepseek402); got != "ai_payment_402" {
		t.Fatalf("the live 402 text must classify as ai_payment_402, got %q", got)
	}
	if got := classifyAIError(errors.New("context deadline exceeded")); got != "ai_call_failed" {
		t.Fatalf("a timeout must classify as ai_call_failed, got %q", got)
	}
	if got := classifyAIError(nil); got != "" {
		t.Fatalf("nil error must classify empty, got %q", got)
	}
}

// A 139-burst raises exactly ONE P0 banner, and the first success clears it.
func Test402BurstOneAlertThenAutoClear(t *testing.T) {
	at, st := ai402Trader(t)
	now := time.Now()
	err402 := errors.New("API returned error (status 402): Insufficient Balance")

	for i := 0; i < 139; i++ {
		at.on402Failure(now.Add(time.Duration(i)*3*time.Minute), err402)
	}
	n, unacked := alertCount(t, st, "ai-payment", true)
	if n != 1 || unacked != 1 {
		t.Fatalf("139-burst must raise exactly one unacked P0, got n=%d unacked=%d", n, unacked)
	}

	// First success → outage cleared, banner auto-acked.
	at.onAISuccess(now.Add(5 * time.Hour))
	if at.ai402OutageStartMs.Load() != 0 {
		t.Fatal("success must clear the outage latch")
	}
	if _, unacked := alertCount(t, st, "ai-payment", true); unacked != 0 {
		t.Fatal("success must auto-ack the 402 banner")
	}

	// A NEW outage (later) re-alerts with a fresh event id.
	at.on402Failure(now.Add(24*time.Hour), err402)
	if n, unacked := alertCount(t, st, "ai-payment", true); n != 2 || unacked != 1 {
		t.Fatalf("a new outage must raise a NEW banner, got n=%d unacked=%d", n, unacked)
	}
}

// A success with no latched outage is a no-op (no spurious acks/logs).
func TestOnAISuccessNoOutageNoOp(t *testing.T) {
	at, _ := ai402Trader(t)
	at.onAISuccess(time.Now())
	if at.ai402OutageStartMs.Load() != 0 {
		t.Fatal("no-outage success must stay clear")
	}
}

// AI_BALANCE_WARN default-off contract.
func TestAIBalanceWarnDefaultOff(t *testing.T) {
	if _, on := aiBalanceWarnThreshold(); on {
		t.Fatal("AI_BALANCE_WARN must default OFF")
	}
	t.Setenv("AI_BALANCE_WARN", "5.0")
	if th, on := aiBalanceWarnThreshold(); !on || th != 5.0 {
		t.Fatalf("AI_BALANCE_WARN=5.0 must arm the poll, got %v %v", th, on)
	}
	t.Setenv("AI_BALANCE_WARN", "junk")
	if _, on := aiBalanceWarnThreshold(); on {
		t.Fatal("malformed AI_BALANCE_WARN must stay OFF")
	}
}

// ── F5 (REL17, 2026-10-11) — 402 banner clears restart-safe + planner/watcher ─

type ai402Err struct{}

func (e *ai402Err) Error() string {
	return "API returned error (status 402): Insufficient Balance"
}

// ai402PlannerClient fails every planner call with a 402.
type ai402PlannerClient struct{ fakeDecisionClient }

func (c *ai402PlannerClient) CallWithMessages(string, string) (string, error) {
	return "", &ai402Err{}
}

func alertsForTrader(t *testing.T, st *store.Store, traderID, kind string) (n, unacked int) {
	t.Helper()
	alerts, err := st.Alert().List(traderID, 200)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range alerts {
		if a.Kind != kind {
			continue
		}
		n++
		if !a.Acked {
			unacked++
		}
	}
	return n, unacked
}

// (b) restart-safe: a FRESH process (no in-memory latch) still acks a banner a
// previous process raised. The in-memory event id is gone after a restart, so
// the clear must ack by (trader, kind), not by event id.
func TestOnAISuccessRestartSafeAcksOpenBanner(t *testing.T) {
	at, st := ai402Trader(t)
	if _, err := st.Alert().Emit(&store.AlertDB{
		TraderID: at.id, Level: "P0", Kind: "ai-payment",
		EventID: "ai402:2026-10-07T10:40", Title: "AI CREDIT EXHAUSTED",
		Body: "stale from a previous process", CreatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	at.onAISuccess(time.Now())
	if _, unacked := alertsForTrader(t, st, at.id, "ai-payment"); unacked != 0 {
		t.Fatal("restart-safe success must ack the stale open banner (latch was 0)")
	}
}

// (c) a planner 402 raises the same P0 outage banner (the planner path was
// silent before REL17).
func TestPlanner402RaisesOutageBanner(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	at.mcpClient = &ai402PlannerClient{}
	at.runPlannerReadWithTriggerClaimedCtx(time.Now(), "NY", "2026-08-18", "owner_reread", "", nil, true)
	n, _ := alertsForTrader(t, st, at.id, "ai-payment")
	if n == 0 {
		t.Fatal("a planner 402 must raise the ai-payment P0 banner")
	}
}

// (a) a successful planner read clears a latched 402 banner (the planner is the
// one DeepSeek call mentor mode still makes).
func TestPlannerSuccessClearsBanner(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	at.on402Failure(time.Now(), &ai402Err{})
	if _, unacked := alertsForTrader(t, st, at.id, "ai-payment"); unacked != 1 {
		t.Fatalf("fixture: the 402 must raise one unacked banner, got %d", unacked)
	}
	at.mcpClient = &planClient{}
	at.runPlannerReadWithTriggerClaimedCtx(time.Now(), "NY", "2026-08-18", "owner_reread", "", nil, true)
	if _, unacked := alertsForTrader(t, st, at.id, "ai-payment"); unacked != 0 {
		t.Fatal("a successful planner read must clear the latched 402 banner")
	}
}

// A non-402 planner error must not raise the ai-payment banner.
func TestPlannerNon402ErrorNoBanner(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	at.mcpClient = &errorDecisionClient{} // timeout, not 402
	at.runPlannerReadWithTriggerClaimedCtx(time.Now(), "NY", "2026-08-18", "owner_reread", "", nil, true)
	if n, _ := alertsForTrader(t, st, at.id, "ai-payment"); n != 0 {
		t.Fatalf("a non-402 planner error must not raise an ai-payment banner, got %d", n)
	}
}
