package trader

import (
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// TestMentorNewsCancelFlattenAtEvalOnceCallSite — the PRODUCTION call-site pin
// for item 18 part 2: driving mentorEvalOnce on a T1 07:30 print day at 07:20
// CT must cancel a live INTRADAY mentor arm (authored through mentorArmIntent)
// by that tick, while a SWING4H arm is untouched. The mutant that deletes the
// `at.mentorNewsCancelFlattenAt(mentorClockNow())` call from mentorEvalOnce
// fails this test.
func TestMentorNewsCancelFlattenAtEvalOnceCallSite(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)

	now := b3Clock(7, 20) // inside the 07:20–07:35 print window
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	mentorDayEventsForTest = func() ([]calendar.Event, bool) {
		return []calendar.Event{{
			Time:   time.Date(2026, 9, 23, 7, 30, 0, 0, kernel.CTLocation()),
			Title:  "CPI m/m",
			Impact: calendar.T1,
		}}, true
	}
	t.Cleanup(func() { mentorDayEventsForTest = nil })

	// Author through the production path: mentorArmIntent writes the ledger row
	// AND registers the live arm (the registry mentorCancelArm resolves by).
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
	author("isb-news", "ISB")       // intraday — must be cancelled before the print
	author("swing-news", "SWING4H") // swing — exempt

	readByArmID := func(armID string) store.ArmedOrderDB {
		t.Helper()
		live, ok := mentorLiveArmFor(armID)
		if !ok {
			t.Fatalf("arm %q not in the live registry", armID)
		}
		var r store.ArmedOrderDB
		if err := ledger.DB().First(&r, live.RowID).Error; err != nil {
			t.Fatalf("arm %q row %d unreadable: %v", armID, live.RowID, err)
		}
		return r
	}
	// I5: the sweep now PRUNES a cancelled arm from the registry, so capture
	// the row ids BEFORE the sweep, then read rows by id.
	isbRow := readByArmID("isb-news")
	swingRow := readByArmID("swing-news")
	readRowByID := func(rowID int64) store.ArmedOrderDB {
		t.Helper()
		var r store.ArmedOrderDB
		if err := ledger.DB().First(&r, rowID).Error; err != nil {
			t.Fatalf("row %d unreadable: %v", rowID, err)
		}
		return r
	}

	// The authoring event: a NEW 1m close at 07:20.
	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1), bar(2)})

	if got := readRowByID(isbRow.ID); got.State != store.StateCancelled {
		t.Fatalf("the 07:20 tick must cancel the intraday mentor arm, got state=%q", got.State)
	}
	if got := readRowByID(swingRow.ID); store.IsTerminalArmState(got.State) {
		t.Fatalf("the SWING4H arm must survive the news sweep, got state=%q", got.State)
	}
	if _, ok := mentorLiveArmFor("isb-news"); ok {
		t.Fatal("the cancelled intraday arm must be pruned from the registry (I5)")
	}
	if _, ok := mentorLiveArmFor("swing-news"); !ok {
		t.Fatal("the surviving swing arm must stay in the registry")
	}
}
