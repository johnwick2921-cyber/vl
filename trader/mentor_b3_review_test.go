package trader

import (
	"encoding/json"
	"testing"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── B3 (release #3b) — the CTO review's five fixes (P0 + N4/N5/N6/N15) ────────
//
// P0: with an ACTIVE AI plan + one planner row + one mentor row, only the mentor
//     row is placed (the planner row stays unplaced, fail-closed).
// N4: EOD flat keeps a SWING4H mentor arm (and position) — only intraday rows die.
// N5: a mentor arm waives ONLY the lunch / first-5m bands — red-news, the breaker,
//     the cap, force-flat, outside-session all still refuse.
// N6: a mentor arm is exempt from the per-session last-entry cutoff; the only hard
//     cutoff is the CME daily halt at 15:45 CT.
// N15: EOD flat still runs with mentor mode ON and the day plan OFF.

// b3SessionRegistry writes a custom session registry so an EOD-flat test can
// land in the "no active session" branch deterministically (the shared TEST
// session spans 00:00–23:59, so its own flat can never be "past").
func b3SessionRegistry(t *testing.T, st *store.Store, name, startCT, endCT string) {
	t.Helper()
	reg := kernel.SessionRegistry{Sessions: []kernel.SessionDef{
		{Name: name, WindowStartCT: startCT, WindowEndCT: endCT, ReadCT: "00:00", FlatCT: endCT, Enabled: true},
	}}
	blob, _ := json.Marshal(reg)
	if err := st.SetSystemConfig(kernel.SessionRegistryConfigKey, string(blob)); err != nil {
		t.Fatal(err)
	}
}

// seedSwingMentorArmedRow authors a SWING4H mentor-origin armed row (never placed).
func seedSwingMentorArmedRow(t *testing.T, at *AutoTrader, ledger *store.ArmedOrderStore, scenario string, expiryMs int64) {
	t.Helper()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: scenario, Side: "long", State: store.StateArmed,
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: "SWING4H", ExpiryMs: expiryMs,
		Origin: store.ArmOriginMentor,
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
}

// ── P0: an active AI plan must NOT author or place a planner row through the
// mentor-only pass. The positive half (mentor row placed) is in (a) of
// mentor_dayplan_independence_test.go; this is the FAIL-CLOSED half: with ONLY
// a planner row resting, the mentor-only pass must leave it unplaced. ─────────
func TestB3MentorPassRefusesPlannerRow(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(10, 30) // CME open, no band
	// An ACTIVE AI plan: reason == "" (the plan-lifecycle block does NOT cancel
	// the planner row), so the mentor-only pass is the only thing that could
	// touch it — and it must leave it unplaced (fail-closed).
	blob, _ := json.Marshal(zoneDoc())
	shadowPlanAtTime(t, at, st, string(blob), now)
	seedPlannerArmedRow(t, at, ledger, "planner-p0")

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	plannerRow := readArmRow(t, ledger, "planner-p0")
	if plannerRow.SignalID != "" {
		t.Fatalf("planner row must NOT be placed through the mentor path, got signal=%q state=%q", plannerRow.SignalID, plannerRow.State)
	}
	if plannerRow.State == store.StateCancelled {
		t.Fatalf("planner row must stay unplaced (NOT cancelled — the plan is active), got state=%q", plannerRow.State)
	}
}

// ── N5: mentorWaivesSessionBand — ONLY lunch / first-5m. ──────────────────────
func TestB3MentorWaivesOnlyBandReasons(t *testing.T) {
	cases := []struct {
		name  string
		risk  sessionRiskVerdict
		waive bool
	}{
		{"lunch", sessionRiskVerdict{Refuse: true, Class: "no_trade_band", Reason: "lunch no-trade window (12:00-13:30)"}, true},
		{"first5m", sessionRiskVerdict{Refuse: true, Class: "no_trade_band", Reason: "TEST first-5m no-trade window"}, true},
		{"red_news", sessionRiskVerdict{Refuse: true, Class: "no_trade_band", Reason: "🔴 red-news blackout: FOMC"}, false},
		{"consecutive_loss", sessionRiskVerdict{Refuse: true, Class: "consecutive_loss", Reason: "consecutive losses"}, false},
		{"force_flat", sessionRiskVerdict{Refuse: true, Class: "force_flat_window", Reason: "force-flat window"}, false},
		{"outside_session", sessionRiskVerdict{Refuse: true, Class: "no_trade_band", Reason: "outside all session windows"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mentorWaivesSessionBand(c.risk); got != c.waive {
				t.Fatalf("mentorWaivesSessionBand(%+v) = %v, want %v", c.risk, got, c.waive)
			}
		})
	}
}

// ── N6: mentorPastDailyHaltCutoff — the only hard cutoff is 15:45 CT. ─────────
func TestB3MentorDailyHaltCutoff(t *testing.T) {
	cases := []struct {
		h, m  int
		block bool
	}{
		{14, 40, false},
		{15, 44, false},
		{15, 45, true},
		{15, 59, true},
	}
	for _, c := range cases {
		at := &AutoTrader{}
		got := false
		if _, blocked := at.mentorPastDailyHaltCutoff(b3Clock(c.h, c.m)); blocked {
			got = true
		}
		if got != c.block {
			t.Fatalf("mentorPastDailyHaltCutoff(%02d:%02d) = %v, want %v", c.h, c.m, got, c.block)
		}
	}
}

// ── N4: the EOD flat keeps a SWING4H mentor arm; intraday + planner die. ──────
func TestB3EODFlatKeepsSwingArm(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, st, ledger, _ := mentorB3Rig(t)
	// A session that ENDED before the test instant → "no active session" → the
	// EOD-flat cancel runs unconditionally (the same body every close runs).
	b3SessionRegistry(t, st, "TEST", "00:00", "10:00")
	now := b3Clock(11, 0)
	seedSwingMentorArmedRow(t, at, ledger, "swing-n4", now.UnixMilli()+60_000)
	seedMentorArmedRow(t, at, ledger, "intraday-n4", now.UnixMilli()+60_000)
	seedPlannerArmedRow(t, at, ledger, "planner-n4")

	if !at.enforceEODFlatAt(now) {
		t.Fatalf("EOD flat must act (cancel intraday + planner arms)")
	}

	swing := readArmRow(t, ledger, "swing-n4")
	if store.IsTerminalArmState(swing.State) {
		t.Fatalf("SWING4H mentor arm must survive the EOD flat, got state=%q", swing.State)
	}
	intraday := readArmRow(t, ledger, "intraday-n4")
	if intraday.State != store.StateCancelled {
		t.Fatalf("intraday mentor arm must be cancelled, got state=%q", intraday.State)
	}
	planner := readArmRow(t, ledger, "planner-n4")
	if planner.State != store.StateCancelled {
		t.Fatalf("planner arm must be cancelled, got state=%q", planner.State)
	}
}

// ── N4: isSwingPosition — the position-exemption predicate. ───────────────────
func TestB3IsSwingPosition(t *testing.T) {
	if isSwingPosition(nil) {
		t.Fatal("nil position must not be a swing position")
	}
	if !isSwingPosition(&store.TraderPosition{CitedScenarioID: "swing-1734567890"}) {
		t.Fatal("swing-… must be a swing position")
	}
	if !isSwingPosition(&store.TraderPosition{CitedScenarioID: "SWING-1734567890"}) {
		t.Fatal("SWING-… must be a swing position (case-insensitive)")
	}
	if isSwingPosition(&store.TraderPosition{CitedScenarioID: "isb-N"}) {
		t.Fatal("isb-… must NOT be a swing position")
	}
	if isSwingPosition(&store.TraderPosition{}) {
		t.Fatal("empty CitedScenarioID must NOT be a swing position")
	}
}

// ── N15: EOD flat still runs with mentor mode ON and the day plan OFF. ────────
func TestB3EODFlatRunsWithMentorDayPlanOff(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	// mentorLoopback (not mentorB3Rig): MentorMode ON, DayPlan nil → day plan OFF.
	at, st, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	b3SessionRegistry(t, st, "TEST", "00:00", "10:00")
	now := b3Clock(11, 0)
	seedMentorArmedRow(t, at, ledger, "intraday-n15", now.UnixMilli()+60_000)

	if !at.mentorEnabled() {
		t.Fatal("fixture: mentor mode must be ON")
	}
	if at.dayPlanEnabled() {
		t.Fatal("fixture: day plan must be OFF")
	}
	if !at.enforceEODFlatAt(now) {
		t.Fatalf("EOD flat must RUN (and act) with mentor mode ON + day plan OFF")
	}
	row := readArmRow(t, ledger, "intraday-n15")
	if row.State != store.StateCancelled {
		t.Fatalf("intraday mentor arm must be cancelled by the EOD flat, got state=%q", row.State)
	}
}

// ── N6 (production): mentor placed at 14:40, refused at 15:50. ────────────────
func TestB3MentorPlacedBefore1545(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(14, 40)
	seedMentorArmedRow(t, at, ledger, "mentor-n6-early", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	row := readArmRow(t, ledger, "mentor-n6-early")
	if row.SignalID == "" || store.IsTerminalArmState(row.State) {
		t.Fatalf("mentor arm at 14:40 must be placed (exempt from the per-session last-entry), got state=%q signal=%q", row.State, row.SignalID)
	}
}

func TestB3MentorRefusedAfter1545(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	now := b3Clock(15, 50)
	seedMentorArmedRow(t, at, ledger, "mentor-n6-late", now.UnixMilli()+60_000)

	at.maybeManageArmedOrdersAtOpts(nil, now, armedPassOpts{})

	row := readArmRow(t, ledger, "mentor-n6-late")
	if row.SignalID != "" {
		t.Fatalf("mentor arm at 15:50 must be REFUSED by the 15:45 daily halt, got signal=%q state=%q", row.SignalID, row.State)
	}
}
