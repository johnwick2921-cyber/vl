package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// TestEODFlatRunsOnTheMonitorBeat (W26 P3-11) — the EOD 14:45 flatten must run
// on the 60 s wall-clock monitor beat, not only on the ~2 min runCycle. With NO
// bar arriving (a feed outage / NT8 lag across the close), a single monitorTick
// past the flat must still cancel the intraday mentor arm; the SWING4H arm
// survives (it is exempt). MUTANT: drop at.enforceEODFlat() from monitorTick →
// this test goes RED.
func TestEODFlatRunsOnTheMonitorBeat(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorLoopback(t, "") // mentor ON, DEFAULT sessions (no all-day TEST override)
	mentorWireSeams(t, at, ledger)

	// 15:00 CT — between sessions: past every session's flat by definition.
	now := time.Date(2026, 9, 23, 15, 0, 0, 0, kernel.CTLocation())
	testNow = func() time.Time { return now }
	t.Cleanup(func() { testNow = nil })
	setMentorNowSource(func() time.Time { return now })
	t.Cleanup(func() { setMentorNowSource(nil) })

	author := func(armID, setup string) {
		at.mentorArmIntent(
			mentor.Intent{
				Action: mentor.PlaceStopLimitEntry, ArmID: armID,
				Side: mentor.SideLong, Price: 29600, Stop: 29590, Target: 29620,
				Setup: setup, ExpiryMs: now.UnixMilli() + 60_000,
			},
			mentorSizeChoice{Contracts: 1, Tier: "base"},
			now.UnixMilli(), now.UnixMilli(), "B", 0,
		)
	}
	author("isb-eod", "ISB")       // intraday — must be cancelled at the flat
	author("swing-eod", "SWING4H") // swing — exempt

	isb, ok := at.mentorLiveArmFor("isb-eod")
	swing, ok2 := at.mentorLiveArmFor("swing-eod")
	if !ok || !ok2 {
		t.Fatal("precondition: both arms registered")
	}

	at.monitorTick(now) // the wall-clock beat — no mentorEvalOnce, no bar

	var r store.ArmedOrderDB
	if err := ledger.DB().First(&r, isb.RowID).Error; err != nil {
		t.Fatal(err)
	}
	if !store.IsTerminalArmState(r.State) {
		t.Fatalf("the monitor beat past the flat must cancel the intraday mentor arm, got state=%q", r.State)
	}
	var s store.ArmedOrderDB
	if err := ledger.DB().First(&s, swing.RowID).Error; err != nil {
		t.Fatal(err)
	}
	if store.IsTerminalArmState(s.State) {
		t.Fatalf("the SWING4H arm must survive the EOD flat, got state=%q", s.State)
	}
}
