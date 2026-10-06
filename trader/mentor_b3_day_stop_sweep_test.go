package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// ── B3 (L4) — a day-stop gate must cancel RESTING unfilled mentor arms ────────
//
// Before this fix the day-stop gates (done-after-win, the news hold, the window
// end) ran ONLY inside the placement path — i.e. only while an entry was being
// WRITTEN. A resting arm placed earlier could still fill after the day had been
// shut. The pins below drive the production call site (mentorEvalOnce, which
// runs mentorDayStopSweep every tick) and assert the resting intraday arm is
// cancelled while a SWING4H arm survives.

// authorMentorArmForDayStop authors a mentor arm through the production path
// (mentorArmIntent → ledger row + live-arm registry) and returns its row id.
func authorMentorArmForDayStop(t *testing.T, at *AutoTrader, armID, setup string, now time.Time) int64 {
	t.Helper()
	at.mentorArmIntent(
		mentor.Intent{
			Action: mentor.PlaceStopLimitEntry, ArmID: armID,
			Side: mentor.SideLong, Price: 29600, Stop: 29590, Target: 29620,
			Setup: setup, ExpiryMs: now.UnixMilli() + 60_000,
		},
		mentorSizeChoice{Contracts: 1, Tier: "base"},
		now.UnixMilli(), now.UnixMilli(), "B", 0,
	)
	live, ok := mentorLiveArmFor(armID)
	if !ok {
		t.Fatalf("arm %q not in the live registry", armID)
	}
	return live.RowID
}

func readMentorArmRowByID(t *testing.T, ledger *store.ArmedOrderStore, rowID int64) store.ArmedOrderDB {
	t.Helper()
	var r store.ArmedOrderDB
	if err := ledger.DB().First(&r, rowID).Error; err != nil {
		t.Fatalf("row %d unreadable: %v", rowID, err)
	}
	return r
}

// TestB3DayStopDoneAfterWinCancelsRestingArm — the RED→GREEN pin: done-after-win
// trips on a tick where NO entry is being written, and the resting intraday arm
// is cancelled while the SWING4H arm is untouched.
func TestB3DayStopDoneAfterWinCancelsRestingArm(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)

	now := b3Clock(10, 30) // in-window (the loopback rig window is all-day), no news
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	// done-after-win DEFINITELY trips: a closed profit and a net-positive day.
	mentorDayNetSource = func() (float64, bool) { return 100, true }
	mentorClosedProfitSource = func() (bool, bool) { return true, true }
	t.Cleanup(func() {
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
	})

	intradayRow := authorMentorArmForDayStop(t, at, "isb-b3-daystop", "ISB", now)
	swingRow := authorMentorArmForDayStop(t, at, "swing-b3-daystop", "SWING4H", now)

	// The tick: a new 1m close with NO entry intent — the old code did nothing
	// here (the gate only ran while an entry was being written).
	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1), bar(2)})

	if got := readMentorArmRowByID(t, ledger, intradayRow); got.State != store.StateCancelled {
		t.Fatalf("done-after-win trip must cancel the resting intraday arm, got state=%q", got.State)
	}
	if got := readMentorArmRowByID(t, ledger, swingRow); store.IsTerminalArmState(got.State) {
		t.Fatalf("the SWING4H arm must survive the day-stop sweep, got state=%q", got.State)
	}
	if mentorCounters["day_stop_cancel_done_after_win"] < 1 {
		t.Fatalf("day_stop_cancel_done_after_win counter must record the cancel, got %v", mentorCounters)
	}
}

// TestB3DayStopWindowEndCancelsRestingArm — the window-END half: once the
// trading window has ended, a resting intraday arm must be cancelled.
func TestB3DayStopWindowEndCancelsRestingArm(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	// done-after-win must NOT trip here (no closed profit): only the window end.
	// mentorWireSeams already wires net=0 / closed=false, which is a clean
	// "not done" read.

	// A 08:30–09:30 CT window: at 09:31 it has ended.
	at.config.StrategyConfig.RiskControl.MentorWindowStart = "08:30"
	at.config.StrategyConfig.RiskControl.MentorWindowMinutes = 60
	now := b3Clock(9, 31)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	intradayRow := authorMentorArmForDayStop(t, at, "isb-b3-window", "ISB", now)
	swingRow := authorMentorArmForDayStop(t, at, "swing-b3-window", "SWING4H", now)

	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1), bar(2)})

	if got := readMentorArmRowByID(t, ledger, intradayRow); got.State != store.StateCancelled {
		t.Fatalf("window end must cancel the resting intraday arm, got state=%q", got.State)
	}
	if got := readMentorArmRowByID(t, ledger, swingRow); store.IsTerminalArmState(got.State) {
		t.Fatalf("the SWING4H arm must survive the window-end sweep, got state=%q", got.State)
	}
	if mentorCounters["day_stop_cancel_window_end"] < 1 {
		t.Fatalf("day_stop_cancel_window_end counter must record the cancel, got %v", mentorCounters)
	}
}

// TestB3MentorDoneAfterWinTrippedUnknownIsNotTrip — an UNRESOLVED day read is
// "unknown", not a trip: the sweep must NOT force-cancel a resting arm on a data
// gap (the placement gate still fails closed for new entries).
func TestB3MentorDoneAfterWinTrippedUnknownIsNotTrip(t *testing.T) {
	at := &AutoTrader{}
	// Unwired sources: mentorDoneAfterWinTripped must report false.
	if trip, _ := at.mentorDoneAfterWinTripped(); trip {
		t.Fatal("unwired day sources must NOT trip the done-after-win sweep")
	}
	// Wired but unresolved: still not a trip.
	mentorDayNetSource = func() (float64, bool) { return 0, false }
	mentorClosedProfitSource = func() (bool, bool) { return false, true }
	t.Cleanup(func() {
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
	})
	if trip, _ := at.mentorDoneAfterWinTripped(); trip {
		t.Fatal("an unresolved day read must NOT trip the done-after-win sweep")
	}
	// Definite: net positive + a closed profit → trip.
	mentorDayNetSource = func() (float64, bool) { return 50, true }
	mentorClosedProfitSource = func() (bool, bool) { return true, true }
	if trip, _ := at.mentorDoneAfterWinTripped(); !trip {
		t.Fatal("a net-positive day with a closed profit MUST trip the done-after-win sweep")
	}
}

// TestB3DayStopNeverAddCancelsRestingArms — the CTO's L4 never-add pin: while a
// mentor position is OPEN, a resting level arm (and the swing, fail-closed)
// must be cancelled on BOTH sides so a second fill cannot ADD or REDUCE/FLIP
// the position. Drives the production call site (mentorEvalOnce →
// mentorDayStopSweep) on a tick with NO entry being written.
func TestB3DayStopNeverAddCancelsRestingArms(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)

	now := b3Clock(10, 30)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	// A mentor position is OPEN (the P1 driver wires this from the account).
	mentorOpenSideSource = func() string { return "long" }
	t.Cleanup(func() { mentorOpenSideSource = nil })

	levelRow := authorMentorArmForDayStop(t, at, "lvl-neveradd", "PHL", now)
	swingRow := authorMentorArmForDayStop(t, at, "swing-neveradd", "SWING4H", now)

	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1), bar(2)})

	if got := readMentorArmRowByID(t, ledger, levelRow); got.State != store.StateCancelled {
		t.Fatalf("a resting level arm must be cancelled while a mentor position is open, got state=%q", got.State)
	}
	if got := readMentorArmRowByID(t, ledger, swingRow); got.State != store.StateCancelled {
		t.Fatalf("the SWING4H arm must be cancelled too (the course is silent — fail-closed: one position at a time), got state=%q", got.State)
	}
	if mentorCounters["day_stop_cancel_never_add"] < 1 {
		t.Fatalf("day_stop_cancel_never_add counter must record the cancel, got %v", mentorCounters)
	}
}

// TestB3MentorWindowEnded — the window END reads correctly: a pre-open 07:00
// reads ENDED (yesterday's window; no intraday arm can be authored outside the
// window, so a pre-open arm can only come from an ended window), inside is NOT
// ended, after the end IS ended.
func TestB3MentorWindowEnded(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowStart: "08:30", MentorWindowMinutes: 60})
	if ended, _ := at.mentorWindowEnded(b3Clock(7, 0)); !ended {
		t.Fatal("07:00 with an 08:30/60 window must read ended (yesterday's window)")
	}
	if ended, _ := at.mentorWindowEnded(b3Clock(9, 0)); ended {
		t.Fatal("inside the window, mentorWindowEnded must be false")
	}
	if ended, _ := at.mentorWindowEnded(b3Clock(9, 31)); !ended {
		t.Fatal("after the window ends, mentorWindowEnded must be true")
	}
}

// TestB3SweepCountsOnlyActualCancelsAndPrunes — I5: the sweep counts and logs
// only a cancel ACTUALLY requested (an already-terminal row is pruned, not
// counted), and it acts only on THIS trader's arms.
func TestB3SweepCountsOnlyActualCancelsAndPrunes(t *testing.T) {
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	ResetMentorCountersForTest()

	// A live arm for THIS trader, already terminal (the old code re-counted it
	// as a "cancel" every tick).
	now := b3Clock(10, 30)
	termRow := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-terminal", Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: "ISB", ExpiryMs: now.UnixMilli() + 60_000,
		State: store.StateCancelled, Origin: store.ArmOriginMentor,
	}
	if err := ledger.DB().Create(&termRow).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("isb-terminal", at.id, termRow.ID, "long", 29600)

	// Another trader's arm — must NOT be touched.
	mentorRegisterLiveArm("isb-other", "other-trader", 999999, "long", 29600)

	if at.cancelLiveMentorArms("test sweep", false) {
		t.Fatal("a sweep with only terminal/other-trader arms must report no actual cancel")
	}
	if _, ok := mentorLiveArmFor("isb-terminal"); ok {
		t.Fatal("the terminal arm must be pruned from the registry")
	}
	if _, ok := mentorLiveArmFor("isb-other"); !ok {
		t.Fatal("another trader's arm must NOT be pruned by this trader's sweep")
	}
	if MentorCountSnapshot()["cancel_requested"] != 0 {
		t.Fatal("no cancel was actually requested — the counter must stay 0")
	}
}

// TestB3DayStopStopAfterLossCancelsRestingArm — the stop-after-loss half of the
// day-stop sweep (DS-105 I1): with the knob ON and a resolved closed loss, a
// resting intraday arm is cancelled while the SWING4H arm survives.
func TestB3DayStopStopAfterLossCancelsRestingArm(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	ResetMentorCountersForTest()

	on := true
	at.config.StrategyConfig.RiskControl.MentorStopAfterLoss = &on
	mentorClosedLossSource = func() (bool, bool) { return true, true } // a resolved closed loss
	mentorStopAfterLossTripped = at.mentorStopAfterLossTrip
	t.Cleanup(func() {
		mentorClosedLossSource = nil
		mentorStopAfterLossTripped = nil
	})

	now := b3Clock(10, 30)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	intradayRow := authorMentorArmForDayStop(t, at, "isb-b3-salloss", "ISB", now)
	swingRow := authorMentorArmForDayStop(t, at, "swing-b3-salloss", "SWING4H", now)

	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1), bar(2)})

	if got := readMentorArmRowByID(t, ledger, intradayRow); got.State != store.StateCancelled {
		t.Fatalf("stop-after-loss trip must cancel the resting intraday arm, got state=%q", got.State)
	}
	if got := readMentorArmRowByID(t, ledger, swingRow); store.IsTerminalArmState(got.State) {
		t.Fatalf("the SWING4H arm must survive the stop-after-loss sweep, got state=%q", got.State)
	}
	if mentorCounters["day_stop_cancel_stop_after_loss"] < 1 {
		t.Fatalf("day_stop_cancel_stop_after_loss counter must record the cancel, got %v", mentorCounters)
	}
}

// TestB3DayStopStopAfterLossOffCancelsNothing — the knob OFF (nil) is not a trip:
// the sweep must not cancel a resting arm.
func TestB3DayStopStopAfterLossOffCancelsNothing(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)
	ResetMentorCountersForTest()

	// knob OFF (nil), but a resolved closed loss — the hook is wired and returns
	// false, so nothing cancels.
	mentorClosedLossSource = func() (bool, bool) { return true, true }
	mentorStopAfterLossTripped = at.mentorStopAfterLossTrip
	t.Cleanup(func() {
		mentorClosedLossSource = nil
		mentorStopAfterLossTripped = nil
	})

	now := b3Clock(10, 30)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	intradayRow := authorMentorArmForDayStop(t, at, "isb-b3-saloff", "ISB", now)

	bar := func(m int) market.Kline {
		ot := now.UnixMilli() + int64(m)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100, Final: true}
	}
	at.mentorEvalOnce([]market.Kline{bar(0), bar(1), bar(2)})

	if got := readMentorArmRowByID(t, ledger, intradayRow); store.IsTerminalArmState(got.State) {
		t.Fatalf("the knob OFF must cancel nothing, got state=%q", got.State)
	}
	if mentorCounters["day_stop_cancel_stop_after_loss"] != 0 {
		t.Fatalf("day_stop_cancel_stop_after_loss must stay 0 with the knob OFF, got %v", mentorCounters)
	}
}
