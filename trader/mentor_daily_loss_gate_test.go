package trader

import (
	"testing"
	"time"

	"vl/store"
)

// §5(a) — the daily loss limit is checked AT PLACEMENT, not only by the 60s
// sweep. A placement while the session-day's realized loss is already at/past
// the limit is refused and counted (daily_loss_refused).
//
// The pin drives the PRODUCTION readers the gate uses: deskGuardrail (the
// enforced daily-loss limit from the strategy's risk block) and
// deskRealizedToday (the corrected session-day P&L). A closed row with a
// corrected loss inside the session day trips the gate; a small loss does not.

func dailyLossNow() time.Time {
	// 10:30 CT on a weekday the CME is open — mid-session, far from the
	// 17:00 CT day roll.
	return time.Date(2026, 10, 2, 10, 30, 0, 0, chicagoLoc())
}

func seedClosedLoss(t *testing.T, st *store.Store, traderID string, now time.Time, corrected float64) {
	t.Helper()
	neg := corrected
	exit := now.Add(-5 * time.Minute)
	row := &store.TraderPosition{TraderID: traderID, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 99, RealizedPnL: neg, PnlCorrected: &neg,
		Status: "CLOSED", CloseReason: "sync",
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}
	if err := st.GormDB().Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

// TestMentorDailyLossGateRefusesAtLimit pins the refusal: a $150 realized loss
// against a $100 limit refuses and counts daily_loss_refused.
// MUTANT: drop the realized <= -limit comparison (always pass) → this test goes
// RED (no refusal).
func TestMentorDailyLossGateRefusesAtLimit(t *testing.T) {
	ResetMentorCountersForTest()
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	at, st := resetTrader(t, cfg)
	now := dailyLossNow()
	seedClosedLoss(t, st, at.id, now, -150)

	refuse, why := at.mentorDailyLossGate(now)
	if !refuse {
		t.Fatalf("a $150 loss against a $100 limit must refuse, got allow (%q)", why)
	}
	if snap := MentorCountSnapshot(); snap["daily_loss_refused"] != 1 {
		t.Fatalf("daily_loss_refused must be counted once, got %v", snap)
	}
}

// TestMentorDailyLossGateAllowsBelowLimit pins the pass: a $50 loss against a
// $100 limit is still inside the limit → allowed, nothing counted.
func TestMentorDailyLossGateAllowsBelowLimit(t *testing.T) {
	ResetMentorCountersForTest()
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{DailyLossLimitUSD: 100}}
	at, st := resetTrader(t, cfg)
	now := dailyLossNow()
	seedClosedLoss(t, st, at.id, now, -50)

	if refuse, why := at.mentorDailyLossGate(now); refuse {
		t.Fatalf("a $50 loss against a $100 limit must allow, got refuse (%q)", why)
	}
	if snap := MentorCountSnapshot(); snap["daily_loss_refused"] != 0 {
		t.Fatalf("daily_loss_refused must NOT be counted, got %v", snap)
	}
}

// TestMentorDailyLossGateUnconfiguredPasses pins the off case: the guardrails
// master OFF (even with a configured limit) means the guard is not enforced.
func TestMentorDailyLossGateUnconfiguredPasses(t *testing.T) {
	ResetMentorCountersForTest()
	off := false
	cfg := store.StrategyConfig{RiskControl: store.RiskControlConfig{
		GuardrailsEnabled: &off,
		DailyLossLimitUSD: 100,
	}}
	at, st := resetTrader(t, cfg)
	now := dailyLossNow()
	seedClosedLoss(t, st, at.id, now, -999)

	if refuse, why := at.mentorDailyLossGate(now); refuse {
		t.Fatalf("master OFF must allow, got refuse (%q)", why)
	}
}
