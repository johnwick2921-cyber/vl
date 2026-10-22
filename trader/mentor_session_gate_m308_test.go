package trader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── m308 (owner #45 P1): the m235 mentor waiver must never mask the HARD
// windows ─────────────────────────────────────────────────────────────────────
//
// The m235 waiver (mentorWaivesSessionBand) ignores the day-plan session LIST
// (outside-session, session-off, sessions_enabled, trade cap, lunch, first-5m),
// but the red-news T1 blackout and the force-flat windows are KEPT mentor
// safeties. m308 found three masks that let a waived band swallow a hard window:
//
//   - sessionEntryBlockedT1At returned early on "not runnable" and "trade cap"
//     BEFORE it read the T1 windows, and sessionGateDecision returned "outside
//     all session windows" before InT1Blackout — so a mentor arm could land
//     inside an FOMC T1 blackout while the session was off/capped/between.
//   - sessionRiskGateAt called forceFlatWindowAt only when the band was OPEN —
//     so a mentor arm could land inside the T1 lead or after the EOD flat while
//     the session list blocked.
//
// The fix evaluates the T1 blackout and the force-flat windows INDEPENDENTLY of
// the band reason; they always refuse. Pins below drive the mentor placement
// pass (maybeManageArmedOrdersAtOpts → mentorOnlyPlacementPass →
// sessionRiskGateAt), the same chokepoint m235 tested.

// TestM308RedNewsBlackoutNotWaivedOutsideSessionWindow pins the first mask: at
// 13:00 CT with an FOMC T1 event, NO session active (NY's window ends 11:00) —
// the band would read "outside all session windows" (waivable), but the mentor
// arm must be REFUSED under no_trade_band with the red-news reason. MUTANT:
// move the T1 read back BELOW the session checks in sessionEntryBlockedT1At
// (and keep currentT1Windows returning nil without an active session) → the arm
// is waived and placed → RED.
func TestM308RedNewsBlackoutNotWaivedOutsideSessionWindow(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.config.StrategyConfig.DayPlan = &store.DayPlanConfig{PlanEnabled: true, SessionsEnabled: []string{"NY"}}
	// NY ends 11:00 CT so 13:00 CT is "outside all session windows".
	b3SessionRegistry(t, st, "NY", "08:30", "11:00")
	mentorWireSeams(t, at, ledger)
	logs := captureTraderLog(t)
	// A T1 (red) print at 13:00 CT = 18:00 UTC on the test date (CDT, UTC-5).
	if _, err := st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
		TradeDate: "2026-09-23", Source: "forexfactory",
		EventsJSON: `[{"time":"2026-09-23T18:00:00Z","currency":"USD","title":"Synthetic FOMC","impact":"T1"}]`,
		CreatedAt:  time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	now := b3Clock(13, 0) // outside NY (ended 11:00), inside the FOMC blackout
	seedM235MentorRow(t, at, ledger, "swing-news-out", "SWING4H", now.UnixMilli()+60_000)

	_, before := telemetry.GateBlockSnapshot()
	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})
	_, table := telemetry.GateBlockSnapshot()

	row := readArmRow(t, ledger, "swing-news-out")
	if row.SignalID != "" {
		t.Fatalf("the T1 blackout must refuse the mentor arm outside the session window, got signal=%q state=%q", row.SignalID, row.State)
	}
	b := before[at.id]
	if got := table[at.id]; got["no_trade_band"]-b["no_trade_band"] != 1 {
		t.Fatalf("the refusal must tick no_trade_band once, got %v (before %v)", got, b)
	}
	if !strings.Contains(logs.String(), "🛑 mentor placement REFUSED (no_trade_band)") || !strings.Contains(logs.String(), "red-news blackout") {
		t.Fatalf("the refusal must log once with the red-news reason; log:\n%s", logs.String())
	}
}

// TestM308EODForceFlatNotWaivedThroughTradeCap pins the second m308 mask AND
// the m314 intraday side: inside NY (active session), the per-session trade cap
// is reached (a WAIVABLE band reason) while the in-session EOD flat
// (eod_flat_offset_min=15 → 14:30) is already past at 14:40 CT — an INTRADAY
// (ISB) mentor arm must be REFUSED under force_flat_window, never waived as
// "trade cap reached". MUTANT (m308): gate forceFlatWindowAt back behind
// `!blocked` in sessionRiskGateAt → the trade-cap band is waived and the arm is
// placed → RED.
func TestM308EODForceFlatNotWaivedThroughTradeCap(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, ledger := m235Rig(t)
	flat15, cap0 := 15, 0
	at.config.StrategyConfig.DayPlan.Sessions = []store.DayPlanSessionOverride{
		{Session: "NY", MaxTrades: &cap0, EODFlatOffsetMin: &flat15},
	}
	mentorWireSeams(t, at, ledger)
	logs := captureTraderLog(t)
	now := b3Clock(14, 40) // inside NY (ends 14:45), past the 14:30 EOD flat
	seedM235MentorRow(t, at, ledger, "isb-eod", "ISB", now.UnixMilli()+60_000)

	_, before := telemetry.GateBlockSnapshot()
	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})
	_, table := telemetry.GateBlockSnapshot()

	row := readArmRow(t, ledger, "isb-eod")
	if row.SignalID != "" {
		t.Fatalf("the EOD force-flat must refuse the intraday mentor arm through the waived trade cap, got signal=%q state=%q", row.SignalID, row.State)
	}
	b := before[at.id]
	if got := table[at.id]; got["force_flat_window"]-b["force_flat_window"] != 1 {
		t.Fatalf("the refusal must tick force_flat_window once, got %v (before %v)", got, b)
	}
	if !strings.Contains(logs.String(), "EOD flat") {
		t.Fatalf("the refusal must log the EOD flat reason; log:\n%s", logs.String())
	}
}

// TestM308SwingArmPlacedPastEODFlat pins the m314 swing exemption: at the SAME
// instant as the intraday pin above (14:40 CT, past the 14:30 EOD flat, trade
// cap reached), a SWING4H mentor arm is PLACED — it waives both the trade-cap
// band and the in-session EOD flat (the course holds the swing by the 4h,
// D5.2). MUTANT: drop the swing exemption (isEODFlatWindowRisk) → the swing is
// refused force_flat_window, never placed → RED.
func TestM308SwingArmPlacedPastEODFlat(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, ledger := m235Rig(t)
	flat15, cap0 := 15, 0
	at.config.StrategyConfig.DayPlan.Sessions = []store.DayPlanSessionOverride{
		{Session: "NY", MaxTrades: &cap0, EODFlatOffsetMin: &flat15},
	}
	mentorWireSeams(t, at, ledger)
	now := b3Clock(14, 40) // inside NY (ends 14:45), past the 14:30 EOD flat
	seedM235MentorRow(t, at, ledger, "swing-eod", "SWING4H", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	row := readArmRow(t, ledger, "swing-eod")
	if row.SignalID == "" || store.IsTerminalArmState(row.State) {
		t.Fatalf("a SWING4H mentor arm must be placed past the EOD flat (held by the 4h), got state=%q signal=%q", row.State, row.SignalID)
	}
}
