package trader

import (
	"testing"
	"time"

	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── B3 (release #3b): mentor arms must NOT depend on the AI day plan ─────────
//
// Pins at the production call site (maybeManageArmedOrdersAtOpts / mentorEvalOnce
// → mentorPlaceNow). The rig is a mentor-mode loopback with the day plan ON and
// an all-day TEST session so the pass computes "no active plan" (no AI plan
// provider registered) and reaches the mentor-only placement path.

// mentorB3Rig is mentorLoopback + day plan ON + an all-day TEST session.
func mentorB3Rig(t *testing.T) (at *AutoTrader, st *store.Store, ledger *store.ArmedOrderStore, frames chan ntwire.FrameType) {
	t.Helper()
	at, st, ledger, frames = mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true}
	shadowEnableTestSession(t, st)
	return at, st, ledger, frames
}

// b3Clock is a fixed instant on Wed 2026-09-23 (a CME-open day).
func b3Clock(h, m int) time.Time {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CDT", -5*60*60)
	}
	return time.Date(2026, 9, 23, h, m, 0, 0, loc)
}

// seedMentorArmedRow authors a mentor-origin armed stop-entry row (never placed).
func seedMentorArmedRow(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario string, expiryMs int64) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scenario, Side: "long", State: store.StateArmed,
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: "ISB", ExpiryMs: expiryMs,
		Origin: store.ArmOriginMentor,
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
}

// seedPlannerArmedRow authors a planner-origin armed row (never placed).
func seedPlannerArmedRow(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario string) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "planner-1", Version: 1, Session: "NY",
		Scenario: scenario, Side: "long", State: store.StateArmed,
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry",
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
}

func readArmRow(t *testing.T, ledger *store.ArmedOrderStore, scenario string) store.ArmedOrderDB {
	t.Helper()
	var row store.ArmedOrderDB
	if err := ledger.DB().Where("scenario = ?", scenario).First(&row).Error; err != nil {
		t.Fatalf("row %q missing: %v", scenario, err)
	}
	return row
}

// (a) 18:00 CT, no AI plan, mentor ON → the mentor row is placed and NOT
// cancelled; a planner row in the same pass IS cancelled.
func TestB3NoAIPlanPlacesMentorNotCancelled(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(18, 0)
	seedMentorArmedRow(t, at, ledger, "mentor-a", now.UnixMilli()+60_000)
	seedPlannerArmedRow(t, at, ledger, "planner-a")

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	mentorRow := readArmRow(t, ledger, "mentor-a")
	plannerRow := readArmRow(t, ledger, "planner-a")
	if mentorRow.SignalID == "" || store.IsTerminalArmState(mentorRow.State) {
		t.Fatalf("mentor row must be placed and NOT cancelled, got state=%q signal=%q", mentorRow.State, mentorRow.SignalID)
	}
	if plannerRow.State != store.StateCancelled {
		t.Fatalf("planner row must be cancelled in the same pass, got state=%q", plannerRow.State)
	}
}

// (b) lunch band → the mentor row is placed; the planner row is NOT placed.
func TestB3LunchBandPlacesMentorNotPlanner(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(12, 15) // the AI lunch band 12:00–13:30 CT
	seedMentorArmedRow(t, at, ledger, "mentor-b", now.UnixMilli()+60_000)
	seedPlannerArmedRow(t, at, ledger, "planner-b")

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	mentorRow := readArmRow(t, ledger, "mentor-b")
	plannerRow := readArmRow(t, ledger, "planner-b")
	if mentorRow.SignalID == "" || store.IsTerminalArmState(mentorRow.State) {
		t.Fatalf("mentor row must be placed despite the lunch band, got state=%q signal=%q", mentorRow.State, mentorRow.SignalID)
	}
	if plannerRow.SignalID != "" {
		t.Fatalf("planner row must NOT be placed, got signal=%q state=%q", plannerRow.SignalID, plannerRow.State)
	}
}

// (c) consecutive-loss breaker → the mentor row is REFUSED.
func TestB3ConsecutiveLossRefusesMentor(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	at.config.StrategyConfig.RiskControl.ConsecutiveLossHalt = store.IntPtr(1)
	now := b3Clock(10, 30) // not lunch, CME open
	neg := -52.0
	exit := now.Add(-10 * time.Minute)
	loss := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 99, RealizedPnL: neg, PnlCorrected: &neg,
		Status: "CLOSED", CloseReason: "sync",
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}
	if err := st.GormDB().Create(loss).Error; err != nil {
		t.Fatal(err)
	}
	seedMentorArmedRow(t, at, ledger, "mentor-c", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	mentorRow := readArmRow(t, ledger, "mentor-c")
	if mentorRow.SignalID != "" {
		t.Fatalf("mentor row must be REFUSED by the consecutive-loss breaker, got signal=%q state=%q", mentorRow.SignalID, mentorRow.State)
	}
}

// (d) the account/SIM gate still refuses a mentor placement (the SIM-only
// predicate is not weakened by the mentor-only placement path).
func TestB3AccountGateStillRefusesMentor(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	// Rebind the bound account to a NON-SIM account: the SIM gate must refuse.
	at.armedTrader().GetServer().SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: false}}, "Sim101")
	now := b3Clock(10, 30)
	seedMentorArmedRow(t, at, ledger, "mentor-d", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	mentorRow := readArmRow(t, ledger, "mentor-d")
	if mentorRow.SignalID != "" {
		t.Fatalf("mentor row must be REFUSED by the account/SIM gate, got signal=%q state=%q", mentorRow.SignalID, mentorRow.State)
	}
}

// (e) the mentor arm is placed on the authoring event, without the 2-min scan:
// mentorEvalOnce runs the mentor-only pass immediately (B3 item 4).
func TestB3MentorPlacedOnAuthoringEvent(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(10, 30)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	seedMentorArmedRow(t, at, ledger, "mentor-e", now.UnixMilli()+60_000)

	// The authoring event: a NEW 1m close. No maybeManageArmedOrdersAt call from
	// the test — the event placement (B3 item 4) must reach the wire on its own.
	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1)})

	mentorRow := readArmRow(t, ledger, "mentor-e")
	if mentorRow.SignalID == "" || store.IsTerminalArmState(mentorRow.State) {
		t.Fatalf("mentor arm must be placed on the authoring event, got state=%q signal=%q", mentorRow.State, mentorRow.SignalID)
	}
}
