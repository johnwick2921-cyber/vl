package trader

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
	"vl/telemetry"
)

// STALLED-SETUP ALARM (release #22) — call-site tests.
//
// The 10-13 shape: the evaluator emitted level orders (PlaceStopEntry, ArmID
// "lvl-", Setup "PHL") and cancelled them in the SAME tick, so the emit counter
// climbed while the ledger never saw a row — silent for 5 days. The alarm fires
// once per kind per session day when a kind crosses "emitted >= 3, placed = 0"
// while mentor mode is on and the market is open.
//
// The emit counter lives in mentorDispatchIntent (the production dispatch call
// site): dropping it makes TestMentorStallAlarmFiresOnceForLevel RED — the
// alert never fires because "emitted" reads 0.

func stallAlarmTrader(t *testing.T, id string) (*AutoTrader, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	yes := true
	at := mkTrader("ninjatrader", &yes, "5m")
	at.store = st
	at.id = id
	return at, st
}

// levelIntent is a resting level order (the 10-13 shape): a PHL at a level with
// a "lvl-" ArmID. Valid geometry so the rule gate passes and the intent is only
// HELD (MENTOR_PLACE off in tests), never authored — placed stays 0.
func levelIntent(n int) mentor.Intent {
	return mentor.Intent{
		Action: mentor.PlaceStopEntry,
		Setup:  "PHL",
		Side:   mentor.SideLong,
		ArmID:  fmt.Sprintf("lvl-%d", n),
		Price:  100,
		Stop:   98,
		Target: 104,
	}
}

func mentorStallOpenTime() time.Time {
	return time.Date(2026, 10, 13, 10, 0, 0, 0, kernel.CTLocation()) // Tuesday, CME open
}

func TestMentorSetupKindMapping(t *testing.T) {
	cases := []struct {
		in   mentor.Intent
		want string
	}{
		{mentor.Intent{Action: mentor.PlaceStopEntry, Setup: "PHL", ArmID: "lvl-1"}, "lvl"},
		{mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "ISB", ArmID: "isb-1"}, "isb"},
		{mentor.Intent{Action: mentor.PlaceStopEntry, Setup: "ISB"}, "isb-reverse"},
		{mentor.Intent{Action: mentor.PlaceStopEntry, Setup: "PHL"}, "phl/plh"},
		{mentor.Intent{Action: mentor.PlaceStopEntry, Setup: "BOX"}, "box"},
		{mentor.Intent{Action: mentor.PlaceStopEntry, Setup: "SWING4H"}, "swing"},
		{mentor.Intent{Action: mentor.CancelArm, ArmID: "lvl-1"}, ""}, // not a placement intent
	}
	for _, c := range cases {
		if got := mentorSetupKind(c.in); got != c.want {
			t.Errorf("mentorSetupKind(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMentorStallAlarmFiresOnceForLevel(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	at, st := stallAlarmTrader(t, "t1")
	now := mentorStallOpenTime()

	// The 10-13 shape: three ticks each emit a level order that never reaches
	// the ledger (held, not authored — the same-tick cancel keeps placed at 0).
	for i := 1; i <= 3; i++ {
		at.mentorDispatchIntent(levelIntent(i), mentorTierInputs{}, 0, 0)
	}
	at.mentorStallAlarmCheck(now)

	rows, err := st.Alert().List("t1", 10)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 stalled-setup alert, got %d", len(rows))
	}
	if rows[0].Level != "P1" || rows[0].Kind != "mentor-setup-stalled" {
		t.Fatalf("wrong alert level/kind: %+v", rows[0])
	}
	// The body names the kind, the counts and the window.
	if !strings.Contains(rows[0].Body, "lvl") ||
		!strings.Contains(rows[0].Body, "3 emitted") ||
		!strings.Contains(rows[0].Body, "0 reached the ledger") ||
		!strings.Contains(rows[0].Body, "17:00 CT") {
		t.Fatalf("alert body must name kind/counts/window, got %q", rows[0].Body)
	}

	// Once per kind per day: a second check must not add a second row.
	at.mentorStallAlarmCheck(now)
	rows, _ = st.Alert().List("t1", 10)
	if len(rows) != 1 {
		t.Fatalf("the alert must fire once per kind per day, got %d", len(rows))
	}
}

func TestMentorStallNoAlertWhenPlaced(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	at, st := stallAlarmTrader(t, "t1")

	for i := 1; i <= 3; i++ {
		in := levelIntent(i)
		at.mentorDispatchIntent(in, mentorTierInputs{}, 0, 0)
		at.mentorCountSetupPlaced(in) // reached the ledger
	}
	at.mentorStallAlarmCheck(mentorStallOpenTime())

	rows, _ := st.Alert().List("t1", 10)
	if len(rows) != 0 {
		t.Fatalf("a kind that reached the ledger must not alert, got %d rows", len(rows))
	}
}

func TestMentorStallNoAlertWhenIdle(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	at, st := stallAlarmTrader(t, "t1")

	at.mentorStallAlarmCheck(mentorStallOpenTime())
	rows, _ := st.Alert().List("t1", 10)
	if len(rows) != 0 {
		t.Fatalf("no emissions must not alert, got %d rows", len(rows))
	}
}

func TestMentorStallNoAlertWhenMarketClosed(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	at, st := stallAlarmTrader(t, "t1")

	for i := 1; i <= 3; i++ {
		at.mentorDispatchIntent(levelIntent(i), mentorTierInputs{}, 0, 0)
	}
	// Saturday 12:00 CT — the market is closed; the alarm is silent.
	at.mentorStallAlarmCheck(time.Date(2026, 10, 10, 12, 0, 0, 0, kernel.CTLocation()))
	rows, _ := st.Alert().List("t1", 10)
	if len(rows) != 0 {
		t.Fatalf("market closed must not alert, got %d rows", len(rows))
	}
}

func TestMentorStallCountsNeverMixAcrossTraders(t *testing.T) {
	telemetry.ResetSetupStallsForTest()
	atA, _ := stallAlarmTrader(t, "tA")
	atB, _ := stallAlarmTrader(t, "tB")

	for i := 1; i <= 3; i++ {
		atA.mentorDispatchIntent(levelIntent(i), mentorTierInputs{}, 0, 0)
	}
	atB.mentorDispatchIntent(levelIntent(1), mentorTierInputs{}, 0, 0)

	atA.mentorStallAlarmCheck(mentorStallOpenTime())
	if due := telemetry.SetupStallAlertDue("tB"); len(due) != 0 {
		t.Fatalf("tB has only 1 emit and must not alert, got %+v", due)
	}

	_, table := telemetry.SetupStallSnapshot()
	if table["tA"]["lvl"].Emitted != 3 {
		t.Fatalf("tA lvl emitted want 3, got %+v", table)
	}
	if table["tB"]["lvl"].Emitted != 1 {
		t.Fatalf("tB lvl emitted want 1, got %+v", table)
	}
}
