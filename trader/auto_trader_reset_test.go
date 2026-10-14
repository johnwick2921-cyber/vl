package trader

import (
	"path/filepath"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// P6 — the owner reset. Distinct from the re-read: it ABANDONS the chain
// (append-only — history and death reasons preserved), restores the FULL re-plan
// budget via a baseline marker, clears the NO-TRADE state, and writes a fresh
// plan through the normal read path with trigger_reason "owner reset". It never
// touches positions, brackets, guardrail counters or the daily cage.

func resetTrader(t *testing.T, cfg store.StrategyConfig) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "rst.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at := &AutoTrader{id: "trader-1", store: st, exchange: "ninjatrader"}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{} // planner falls back to the primary client
	// r22 RISK-GATES: the daily-loss latch is process-global per trader id; a
	// test that trips it must not leak a latch into the next "trader-1" test.
	// (Cleared at the END only — a test may arm the latch before the fixture.)
	t.Cleanup(func() { kernel.ClearDailyForceFlat("trader-1") })
	return at, st
}

func TestResetRefusesWhenDayPlanIsOff(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: false}})
	got := at.CanForceReset(time.Now())
	if got.Allowed || got.Reason == "" {
		t.Fatalf("day_plan off must refuse with a reason, got %+v", got)
	}
}

func TestResetRefusesOutsideAnySession(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	// 06:00 UTC = 01:00 CT — outside every session window (night).
	got := at.CanForceReset(time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC))
	if got.Allowed || got.Reason == "" {
		t.Fatalf("night must refuse with a reason, got %+v", got)
	}
}

func TestResetRefusesWithNoPlanYet(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	// NY window (08:30–14:45 CT) on a weekday the CME is open.
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, chicagoLoc())
	got := at.CanForceReset(now)
	if got.Allowed {
		t.Fatalf("no plan yet → nothing to abandon, must refuse, got %+v", got)
	}
	if got.Reason == "" {
		t.Fatal("the refusal must say why")
	}
}

func TestForceResetWritesFreshChainAndRestoresBudget(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	tradeDate := "2026-08-18"
	// Seed the exhausted chain: v1..v5 active + v6 NO-TRADE (cap 4).
	for i := 1; i <= 5; i++ {
		if _, err := st.Plan().AppendPlan(&store.PlanDB{
			PlanID: store.MakePlanID(tradeDate, "NY"), StrategyID: "trader-1",
			TradeDate: tradeDate, Session: "NY", Lifecycle: "active", Doc: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID(tradeDate, "NY"), StrategyID: "trader-1",
		TradeDate: tradeDate, Session: "NY", Lifecycle: "no_trade", Doc: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	// CLASS 35 — the budget is RECORDED: the six rows above spent nothing by
	// themselves, so record the four death re-plans that produced them.
	for i := 0; i < 4; i++ {
		if _, err := store.SpendReplan(st, "trader-1", tradeDate, "NY"); err != nil {
			t.Fatal(err)
		}
	}
	if b := store.GetReplanBudget(st, "trader-1", tradeDate, "NY", 4); b.May() {
		t.Fatalf("fixture: the original chain must be exhausted, got %+v", b)
	}

	now := time.Date(2026, 8, 18, 14, 0, 0, 0, chicagoLoc())
	// The planner returns a VALID plan → the reset writes an ACTIVE plan.
	at.mcpClient = &planClient{}
	gate, err := at.ForceReset(now)
	if err != nil {
		t.Fatalf("ForceReset: %v", err)
	}
	if !gate.Allowed {
		t.Fatalf("reset must be allowed mid-session, got %+v", gate)
	}

	// The marker records the seam at v7 (latest+1) → the new chain's first plan
	// is its v1, free, with the FULL budget restored.
	if got := store.GetResetBaseline(st, "trader-1", tradeDate, "NY"); got != 7 {
		t.Fatalf("baseline = %d, want 7", got)
	}
	// Budget restored: the counter is keyed under baseline 7, which starts at 0.
	if b := store.GetReplanBudget(st, "trader-1", tradeDate, "NY", 4); b.Used != 0 || b.Left() != 4 || !b.May() {
		t.Fatalf("budget after reset = %+v, want the full cap 4", b)
	}
	// The new row is an ACTIVE plan (NO-TRADE cleared), trigger "owner reset".
	row, _ := st.Plan().GetLatestPlanForSession(tradeDate, "NY")
	if row == nil || row.Lifecycle != "active" {
		t.Fatalf("the reset must write an ACTIVE plan, got %+v", row)
	}
	if row.TriggerReason != "owner_reset" {
		t.Fatalf("trigger_reason = %q, want owner_reset", row.TriggerReason)
	}
	// History preserved: the old chain still holds all six rows, NO-TRADE included.
	old, _ := st.Plan().GetPlan(store.MakePlanID(tradeDate, "NY"), 6)
	if old == nil || old.Lifecycle != "no_trade" {
		t.Fatalf("the abandoned chain's NO-TRADE marker must survive, got %+v", old)
	}
}

func TestForceResetFailsClosedOnBadRead(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	tradeDate := "2026-08-18"
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID(tradeDate, "NY"), StrategyID: "trader-1",
		TradeDate: tradeDate, Session: "NY", Lifecycle: "no_trade", Doc: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	// The planner call fails every attempt → the normal path writes a NO-TRADE
	// plan (fail-closed), never an error, never a mutation of anything else.
	at.mcpClient = &errorDecisionClient{}
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, chicagoLoc())
	gate, err := at.ForceReset(now)
	if err != nil {
		t.Fatalf("a failed read must not surface as a reset error: %v", err)
	}
	if !gate.Allowed {
		t.Fatalf("gate must have allowed the reset, got %+v", gate)
	}
	row, _ := st.Plan().GetLatestPlanForSession(tradeDate, "NY")
	if row == nil || row.Lifecycle != "no_trade" || row.TriggerReason != "planner_fail_closed" {
		t.Fatalf("bad read must fail-closed into a NO-TRADE plan, got %+v", row)
	}
}

// errorDecisionClient fails every call so the planner read fail-closes.
type errorDecisionClient struct{ fakeDecisionClient }

func (e *errorDecisionClient) CallWithMessages(string, string) (string, error) {
	return "", &timeoutErr2{}
}

// planClient returns a schema-valid plan so the read writes an ACTIVE row.
type planClient struct{ fakeDecisionClient }

func (p *planClient) CallWithMessages(_, user string) (string, error) {
	return mapCompliantPlanJSON(user), nil // W2 A4: a compliant model reads the MAP block
}

type timeoutErr2 struct{}

func (e *timeoutErr2) Error() string { return "timeout" }

func chicagoLoc() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic(err)
	}
	return loc
}

// UI-verification (2026-08-18) — the live finding: the owner's reset reported
// success while a concurrent death re-plan held the read claim, so the fresh
// plan was written by that OTHER read (wrong trigger_reason) with zero UI
// feedback. The reset now waits briefly for the claim and, if a concurrent read
// wins the whole window, says so in Note instead of lying.
func TestForceResetNotesWhenClaimHeld(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	old := resetClaimWaitMax
	resetClaimWaitMax = 10 * time.Millisecond
	defer func() { resetClaimWaitMax = old }()

	tradeDate := "2026-08-18"
	if _, err := at.store.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID(tradeDate, "NY"), StrategyID: "trader-1",
		TradeDate: tradeDate, Session: "NY", Lifecycle: "active", Doc: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	// A concurrent read holds the claim for the whole window.
	key := store.MakePlanIDForTrader("trader-1", tradeDate, "NY")
	claimPlannerRead(key)
	defer releasePlannerRead(key)

	now := time.Date(2026, 8, 18, 14, 0, 0, 0, chicagoLoc())
	gate, err := at.ForceReset(now)
	if err != nil {
		t.Fatalf("a claim-held reset must succeed honestly (budget still re-armed): %v", err)
	}
	if gate.Note == "" {
		t.Fatalf("claim-held reset must carry a Note, got %+v", gate)
	}
	if !at.PlannerReadInFlight(tradeDate, "NY") {
		t.Fatal("PlannerReadInFlight must report the held claim")
	}
}

// PlannerReadInFlight mirrors the claim map for the API's reading flag.
func TestPlannerReadInFlight(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}})
	if at.PlannerReadInFlight("2026-08-18", "NY") {
		t.Fatal("no claim held → must report idle")
	}
	key := store.MakePlanIDForTrader("trader-1", "2026-08-18", "NY")
	claimPlannerRead(key)
	if !at.PlannerReadInFlight("2026-08-18", "NY") {
		t.Fatal("held claim → must report in flight")
	}
	releasePlannerRead(key)
	if at.PlannerReadInFlight("2026-08-18", "NY") {
		t.Fatal("released claim → must report idle")
	}
}
