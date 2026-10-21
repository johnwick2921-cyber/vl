package trader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── m235 (owner #45 priority): mentor-arm placement must NOT be session-gated
// by the day-plan session LIST ──────────────────────────────────────────────
//
// Row 411 (SWING4H) was authored inside the NY session and refused at 15:20 CT
// by "outside all session windows (overnight/interim)" — the day-plan session
// list re-refusing a mentor arm at placement. The session gate governs the
// planner/AI arm path only; the mentor's own window + news gates (applied at
// AUTHORING) govern mentor arms. Every mentor placement refusal now logs once
// per (class, reason) change and ticks the per-session gate-block counter
// (bug-hunt #40: never silent).

// m235Rig is mentorLoopback + Day Plan ON with NY-only sessions (no shadow
// all-day TEST session): at 15:20 CT no session is active, so the session gate
// returns "outside all session windows" — the exact row-411 instant.
func m235Rig(t *testing.T) (at *AutoTrader, ledger *store.ArmedOrderStore) {
	t.Helper()
	at, _, ledger, _ = mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true, SessionsEnabled: []string{"NY"}}
	return at, ledger
}

// seedM235MentorRow authors a mentor-origin armed stop-entry row with the given
// condition (ISB = intraday, SWING4H = swing). Prices mirror the existing B3
// fixture (R:R 2.0).
func seedM235MentorRow(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario, condition string, expiryMs int64) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scenario, Side: "long", State: store.StateArmed,
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: condition, ExpiryMs: expiryMs,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1),
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
}

// TestM235SwingArmPlacedOutsideSessionWindows is the named RED: a SWING4H
// mentor arm at 15:20 CT (outside every session window, NY-only day plan) must
// be placed. MUTANT: drop the "outside all session windows" leg of
// mentorWaivesSessionBand → the arm is refused, never placed → RED.
func TestM235SwingArmPlacedOutsideSessionWindows(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, ledger := m235Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(15, 20) // NY ends 14:45 CT, ASIA opens 17:00 CT — no session
	seedM235MentorRow(t, at, ledger, "swing-411", "SWING4H", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	row := readArmRow(t, ledger, "swing-411")
	if row.SignalID == "" || store.IsTerminalArmState(row.State) {
		t.Fatalf("a SWING4H mentor arm must be placed at 15:20 CT despite the day-plan session list, got state=%q signal=%q", row.State, row.SignalID)
	}
}

// TestM235IntradayMentorArmStillCancelledAtEODFlat guards the narrowness of the
// waiver: an INTRADAY (ISB) mentor arm at 15:20 CT is still cancelled by the
// non-AI EOD-flat safety (B3 Q2). The waiver covers the session WINDOW family
// only, never the EOD-flat cancel.
func TestM235IntradayMentorArmStillCancelledAtEODFlat(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, ledger := m235Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(15, 20)
	seedM235MentorRow(t, at, ledger, "isb-eod", "ISB", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	row := readArmRow(t, ledger, "isb-eod")
	if row.State != store.StateCancelled {
		t.Fatalf("an INTRADAY mentor arm must still be cancelled by the EOD-flat safety at 15:20 CT, got state=%q signal=%q", row.State, row.SignalID)
	}
}

// TestM235PlannerArmKeepsItsOwnPathAt1520 pins the CTO's "planner arms keep
// their own path": in the SAME mentor+day-plan rig at 15:20 CT, the SWING4H
// mentor arm is placed while a PLANNER arm is NOT (the session-end cancel keeps
// it off the wire — the mentor pass places only isMentorArmOrigin rows).
func TestM235PlannerArmKeepsItsOwnPathAt1520(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, ledger := m235Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(15, 20)
	seedM235MentorRow(t, at, ledger, "swing-pin", "SWING4H", now.UnixMilli()+60_000)
	seedPlannerArmedRow(t, at, ledger, "planner-pin")

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	mentorRow := readArmRow(t, ledger, "swing-pin")
	plannerRow := readArmRow(t, ledger, "planner-pin")
	if mentorRow.SignalID == "" || store.IsTerminalArmState(mentorRow.State) {
		t.Fatalf("the SWING4H mentor arm must be placed, got state=%q signal=%q", mentorRow.State, mentorRow.SignalID)
	}
	if plannerRow.SignalID != "" {
		t.Fatalf("the planner arm must NOT be placed at 15:20 CT (no active session), got signal=%q state=%q", plannerRow.SignalID, plannerRow.State)
	}
}

// TestM235RefusedMentorArmLogsAndCounts pins the new observability on the
// KEPT session-risk refusal (the consecutive-loss breaker): the refusal must
// log once with the reason AND tick the per-session gate-block counter.
// MUTANT: drop the IncGateBlock in mentorOnlyPlacementPass → counter delta 0.
func TestM235RefusedMentorArmLogsAndCounts(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true, SessionsEnabled: []string{"NY"}}
	mentorWireSeams(t, at, ledger)
	logs := captureTraderLog(t)
	at.config.StrategyConfig.RiskControl.ConsecutiveLossHalt = store.IntPtr(1)
	now := b3Clock(10, 30) // inside NY, CME open — the session gate passes, the breaker fires
	neg := -52.0
	exit := now.Add(-10 * time.Minute)
	loss := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 99, RealizedPnL: neg, PnlCorrected: &neg,
		Status: "CLOSED", CloseReason: "sync",
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}
	if err := st.GormDB().Create(loss).Error; err != nil {
		t.Fatal(err)
	}
	seedM235MentorRow(t, at, ledger, "mentor-brk", "ISB", now.UnixMilli()+60_000)

	_, before := telemetry.GateBlockSnapshot()
	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})
	_, table := telemetry.GateBlockSnapshot()

	row := readArmRow(t, ledger, "mentor-brk")
	if row.SignalID != "" {
		t.Fatalf("mentor arm must be refused by the consecutive-loss breaker, got signal=%q", row.SignalID)
	}
	b := before[at.id]
	if got := table[at.id]; got["consecutive_loss"]-b["consecutive_loss"] != 1 {
		t.Fatalf("the refusal must tick the consecutive_loss gate-block counter once, got %v (before %v)", got, b)
	}
	if !strings.Contains(logs.String(), "🛑 mentor placement REFUSED (consecutive_loss)") {
		t.Fatalf("the refusal must log once with the reason; log:\n%s", logs.String())
	}
}

// TestM235StaleDataPlacementRefusalLogsAndCounts pins the new observability on
// the stale-data placement refusal: it must log once AND tick the stale_data
// gate-block counter (it previously refused silently). MUTANT: drop the
// IncGateBlock in the stale-data leg → counter delta 0.
func TestM235StaleDataPlacementRefusalLogsAndCounts(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, ledger := m235Rig(t)
	logs := captureTraderLog(t)
	now := b3Clock(9, 0) // CME open, inside NY
	mentorStaleBlockFeed(t, now, 3*time.Minute)
	seedM235MentorRow(t, at, ledger, "mentor-stale", "ISB", now.UnixMilli()+60_000)

	_, before := telemetry.GateBlockSnapshot()
	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})
	_, table := telemetry.GateBlockSnapshot()

	row := readArmRow(t, ledger, "mentor-stale")
	if row.SignalID != "" {
		t.Fatalf("mentor arm must be refused by the stale-data block, got signal=%q", row.SignalID)
	}
	b := before[at.id]
	if got := table[at.id]; got["stale_data"]-b["stale_data"] != 1 {
		t.Fatalf("the refusal must tick the stale_data gate-block counter once, got %v (before %v)", got, b)
	}
	if !strings.Contains(logs.String(), "🛑 mentor placement REFUSED (stale_data)") {
		t.Fatalf("the refusal must log once; log:\n%s", logs.String())
	}
}

// TestM235BreakerNotMaskedByWaivedBand is the m235c hole: at 15:20 CT the
// session gate returns "outside all session windows" (waivable), but a tripped
// breaker (ConsecutiveLossHalt=1 + one loss) must be re-adjudicated THROUGH the
// waived band and refuse as consecutive_loss, logged + counted. MUTANT: skip
// the re-adjudication in adjudicateWithBandWaiver → the verdict stays
// no_trade_band (waivable) → the arm is placed → RED.
func TestM235BreakerNotMaskedByWaivedBand(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true, SessionsEnabled: []string{"NY"}}
	mentorWireSeams(t, at, ledger)
	logs := captureTraderLog(t)
	at.config.StrategyConfig.RiskControl.ConsecutiveLossHalt = store.IntPtr(1)
	now := b3Clock(15, 20) // outside all session windows (NY ends 14:45)
	neg := -52.0
	exit := now.Add(-10 * time.Minute)
	loss := &store.TraderPosition{TraderID: at.id, Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 100, ExitPrice: 99, RealizedPnL: neg, PnlCorrected: &neg,
		Status: "CLOSED", CloseReason: "sync",
		EntryTime: exit.Add(-5 * time.Minute).UnixMilli(), ExitTime: exit.UnixMilli()}
	if err := st.GormDB().Create(loss).Error; err != nil {
		t.Fatal(err)
	}
	seedM235MentorRow(t, at, ledger, "swing-brk", "SWING4H", now.UnixMilli()+60_000)

	_, before := telemetry.GateBlockSnapshot()
	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})
	_, table := telemetry.GateBlockSnapshot()

	row := readArmRow(t, ledger, "swing-brk")
	if row.SignalID != "" {
		t.Fatalf("the breaker must refuse the mentor arm through the waived band, got signal=%q", row.SignalID)
	}
	b := before[at.id]
	if got := table[at.id]; got["consecutive_loss"]-b["consecutive_loss"] != 1 {
		t.Fatalf("the refusal must tick consecutive_loss once, got %v (before %v)", got, b)
	}
	if !strings.Contains(logs.String(), "🛑 mentor placement REFUSED (consecutive_loss)") {
		t.Fatalf("the refusal must log once with the reason; log:\n%s", logs.String())
	}
}

// TestM235RedNewsBlackoutNotWaivedThroughLunch pins the red-news T1 blackout
// through a waived band: at 13:00 CT (inside lunch) with a T1 event, the mentor
// arm is refused under no_trade_band with the red-news reason — never waived as
// lunch. MUTANT: move the T1 check back BELOW lunch in sessionGateDecision →
// the 13:00 minute reads 'lunch' (waivable) → the arm is placed → RED.
func TestM235RedNewsBlackoutNotWaivedThroughLunch(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true, SessionsEnabled: []string{"NY"}}
	mentorWireSeams(t, at, ledger)
	logs := captureTraderLog(t)
	// A T1 (red) print at 13:00 CT = 18:00 UTC on the test date, inside lunch.
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-09-23", Source: "forexfactory",
		EventsJSON: `[{"time":"2026-09-23T18:00:00Z","currency":"USD","title":"Synthetic FOMC","impact":"T1"}]`,
		CreatedAt:  time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	now := b3Clock(13, 0) // inside lunch 12:00–13:30, inside NY
	seedM235MentorRow(t, at, ledger, "swing-news", "SWING4H", now.UnixMilli()+60_000)

	_, before := telemetry.GateBlockSnapshot()
	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})
	_, table := telemetry.GateBlockSnapshot()

	row := readArmRow(t, ledger, "swing-news")
	if row.SignalID != "" {
		t.Fatalf("the T1 blackout must refuse the mentor arm through lunch, got signal=%q", row.SignalID)
	}
	b := before[at.id]
	if got := table[at.id]; got["no_trade_band"]-b["no_trade_band"] != 1 {
		t.Fatalf("the refusal must tick no_trade_band once, got %v (before %v)", got, b)
	}
	if !strings.Contains(logs.String(), "🛑 mentor placement REFUSED (no_trade_band)") || !strings.Contains(logs.String(), "red-news blackout") {
		t.Fatalf("the refusal must log once with the red-news reason; log:\n%s", logs.String())
	}
}
