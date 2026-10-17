package trader

import (
	"os"
	"strings"
	"testing"
	"time"

	"nofx/store"
)

// ── W-ONE-BUTTON M2 site 5 — planner claims are refused while held ─────────
//
// A planner read is a 5–20 minute AI call; the installation gate waits for
// planner_in_flight to reach zero, so a hold must stop NEW reads from being
// claimed or the gate can starve. Refused BEFORE the claim: no plan row, no
// budget spent, no fail-closed NO-TRADE marker.

func TestPlannerReadRefusedWhileHeld(t *testing.T) {
	dir := withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	setHold(t, dir, "job-planner")
	before := gateBlocks(at.id, "maintenance_hold")

	for i := 0; i < 2; i++ {
		if at.runPlannerReadWithTriggerClaimedCtx("NY", "2026-08-18", "owner_reread", "", nil, true) {
			t.Fatal("held: the planner read must be refused")
		}
	}
	if got := gateBlocks(at.id, "maintenance_hold"); got != before+1 {
		t.Fatalf("want exactly one maintenance_hold block for one refused read key, got %d", got-before)
	}
	if row, _ := st.Plan().GetLatestPlanForTraderSession("2026-08-18", "NY", at.id); row != nil {
		t.Fatalf("held: no plan row may be written (not even a fail-closed marker): %+v", row)
	}
	key := store.MakePlanIDForTrader(at.id, "2026-08-18", "NY")
	if !claimPlannerRead(key) {
		t.Fatal("a refused read must not leave the claim held")
	}
	releasePlannerRead(key)
}

// Owner reset and owner re-read refuse UP FRONT while held — before the reset
// abandons the chain, and with a reason that names the update instead of the
// misleading "a concurrent re-plan was already writing".
func TestOwnerResetAndRereadRefusedWhileHeld(t *testing.T) {
	dir := withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	tradeDate := "2026-08-18"
	if _, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID: store.MakePlanID(tradeDate, "NY"), StrategyID: at.id,
		TradeDate: tradeDate, Session: "NY", Lifecycle: "active", Doc: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, chicagoLoc())
	if !at.CanForceReset(now).Allowed || !at.CanForceReread(now).Allowed {
		t.Fatal("fixture: reset and re-read must be allowed without a hold")
	}
	setHold(t, dir, "job-owner")

	for name, got := range map[string]struct {
		allowed bool
		reason  string
	}{
		"CanForceReset":  {at.CanForceReset(now).Allowed, at.CanForceReset(now).Reason},
		"CanForceReread": {at.CanForceReread(now).Allowed, at.CanForceReread(now).Reason},
	} {
		if got.allowed || !strings.Contains(got.reason, "update is in progress") {
			t.Fatalf("%s held: want a refusal naming the update, got allowed=%v reason=%q", name, got.allowed, got.reason)
		}
	}
	if _, err := at.ForceReset(now); err == nil {
		t.Fatal("ForceReset must refuse while held")
	}
	row, _ := st.Plan().GetLatestPlanForTraderSession(tradeDate, "NY", at.id)
	if row == nil || row.Lifecycle != "active" {
		t.Fatalf("a refused reset must not abandon the chain: %+v", row)
	}
}

// The weekly read claims asynchronously; its hold check must precede the claim.
func TestWeeklyReadChecksTheHoldBeforeItsClaim(t *testing.T) {
	b, err := os.ReadFile("auto_trader_weekly.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "func (at *AutoTrader) maybeRunWeeklyRead(")
	j := strings.Index(src[i:], "\n}\n")
	body := src[i : i+j]
	held := strings.Index(body, "at.refusePlannerClaimWhileHeld(")
	claim := strings.Index(body, "claimWeeklyRead(")
	if held < 0 || claim < 0 || held > claim {
		t.Fatalf("maybeRunWeeklyRead must call refusePlannerClaimWhileHeld before claimWeeklyRead (held=%d claim=%d)", held, claim)
	}
}

// M2.1 (review 3 F8): configured data dir, NO hold file — the planner read is
// claimed and runs exactly as before.
func TestPlannerReadRunsWhenConfiguredAndNoHoldFile(t *testing.T) {
	withMaintenanceDir(t) // configured, no hold file
	at, _ := resetTrader(t, store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true, ReplanCap: store.IntPtr(4)}})
	before := gateBlocks(at.id, "maintenance_hold")
	if !at.runPlannerReadWithTriggerClaimedCtx("NY", "2026-08-18", "owner_reread", "", nil, true) {
		t.Fatal("configured + no hold file: the planner read must run (it did before the hold existed)")
	}
	if gateBlocks(at.id, "maintenance_hold") != before {
		t.Fatal("no hold file: nothing may be counted as a maintenance refusal")
	}
}
