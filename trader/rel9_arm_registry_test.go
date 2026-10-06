package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── REL9 arm-registry fold (P1 + 3×P3) — call-site pins ──────────────────────
//
// P1: a FILLED arm is a live position — the registry entry must survive the
//     never-add sweep so the swing's MoveStopBE / ClosePosition still resolve.
// P3#2: a DB read error must NOT prune a resting arm.
// P3#4: midnight-crossing window end.
// P3#5: cancel_pending is not re-requested / re-counted.

// TestRel9FilledSwingSurvivesNeverAddSweep — P1: swing fills → position open →
// never-add sweep runs → MoveStopBE is ACCEPTED (the registry still has the
// arm). RED: re-allow the filled prune → the pin fails.
func TestRel9FilledSwingSurvivesNeverAddSweep(t *testing.T) {
	t.Setenv("MENTOR_STOP_LIMIT", "on")
	at, _, ledger, _ := mentorB3Rig(t)
	mentorWireSeams(t, at, ledger)

	oldMoveStopWire := moveStopWire
	moveStopWire = func(nt *ntTrader.TCPTrader, side string, newStop float64) error { return nil }
	t.Cleanup(func() { moveStopWire = oldMoveStopWire })

	now := b3Clock(10, 30)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	mentorOpenSideSource = func() string { return "long" } // a mentor position is OPEN
	t.Cleanup(func() { mentorOpenSideSource = nil })

	// Author the swing arm through the production path, then fill it.
	at.mentorArmIntent(
		mentor.Intent{
			Action: mentor.PlaceStopLimitEntry, ArmID: "swing-rel9",
			Side: mentor.SideLong, Price: 29600, Stop: 29590, Target: 29620,
			Setup: "SWING4H", ExpiryMs: now.UnixMilli() + 60_000,
		},
		mentorSizeChoice{Contracts: 1, Tier: "base"},
		now.UnixMilli(), now.UnixMilli(), "swing", 0,
	)
	live, ok := mentorLiveArmFor("swing-rel9")
	if !ok {
		t.Fatal("fixture: swing arm not registered")
	}
	if err := ledger.SetState(live.RowID, store.StateFilled, "test fill"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetFillQuantity(live.RowID, 1); err != nil {
		t.Fatal(err)
	}

	// The never-add sweep runs on this tick (position open). It must not touch
	// the FILLED swing.
	at.mentorDayStopSweep(now)

	if _, ok := mentorLiveArmFor("swing-rel9"); !ok {
		t.Fatal("a FILLED swing arm must NOT be pruned by the never-add sweep (P1)")
	}
	if got := MentorCountSnapshot()["cancel_requested"]; got != 0 {
		t.Fatalf("the never-add sweep must count/log nothing for a filled arm, got cancel_requested=%d", got)
	}

	// MoveStopBE must still be ACCEPTED — the registry entry survived.
	ResetMentorCountersForTest()
	at.mentorDispatchIntent(
		mentor.Intent{Action: mentor.ActionMoveStopBE, ArmID: "swing-rel9", Reason: "test +1R BE"},
		mentorTierInputs{}, now.UnixMilli(), now.UnixMilli(),
	)
	if got := MentorCountSnapshot()["move_be_sent"]; got != 1 {
		t.Fatalf("MoveStopBE must be accepted for the filled swing, move_be_sent=%d counters=%v", got, MentorCountSnapshot())
	}
}

// TestRel9CancelReadErrorDoesNotPrune — P3#2: a DB read error must NOT prune a
// resting arm (log + return false; the next tick retries).
func TestRel9CancelReadErrorDoesNotPrune(t *testing.T) {
	at, _, _, _ := mentorB3Rig(t)
	mentorRegisterLiveArm("lvl-readerr", at.id, 999999, "long", 29600)

	if at.mentorCancelArm(mentor.Intent{Action: mentor.CancelArm, ArmID: "lvl-readerr", Reason: "test"}) {
		t.Fatal("a read error must not request a cancel")
	}
	if _, ok := mentorLiveArmFor("lvl-readerr"); !ok {
		t.Fatal("a read error must NOT prune the registry entry")
	}
}

// TestRel9CancelPendingNotRecounted — P3#5: a cancel already in flight is not
// re-requested and not re-counted.
func TestRel9CancelPendingNotRecounted(t *testing.T) {
	at, _, ledger, _ := mentorB3Rig(t)
	now := b3Clock(10, 30)
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "lvl-cp", Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29620,
		Kind: "stop_entry", Condition: "PHL", ExpiryMs: now.UnixMilli() + 60_000,
		Origin: store.ArmOriginMentor,
	}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetState(row.ID, store.StateCancelPending, "test"); err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("lvl-cp", at.id, row.ID, "long", 29600)

	ResetMentorCountersForTest()
	if at.mentorCancelArm(mentor.Intent{Action: mentor.CancelArm, ArmID: "lvl-cp", Reason: "test"}) {
		t.Fatal("a cancel_pending row must not be re-requested")
	}
	if got := MentorCountSnapshot()["cancel_requested"]; got != 0 {
		t.Fatalf("a cancel_pending row must not be re-counted, got cancel_requested=%d", got)
	}
	if _, ok := mentorLiveArmFor("lvl-cp"); !ok {
		t.Fatal("a cancel_pending row is non-terminal and must stay registered")
	}
}

// TestRel9WindowEndedMidnightCrossing — P3#4: the window END reads correctly
// across midnight.
func TestRel9WindowEndedMidnightCrossing(t *testing.T) {
	loc := kernel.CTLocation()

	// 23:00 + 120 min.
	atLate := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowStart: "23:00", MentorWindowMinutes: 120})
	if ended, _ := atLate.mentorWindowEnded(time.Date(2026, 9, 24, 1, 30, 0, 0, loc)); !ended {
		t.Fatal("23:00/120 at 01:30 must read ended (the most recent window opened yesterday 23:00, ended 01:00)")
	}
	if ended, _ := atLate.mentorWindowEnded(time.Date(2026, 9, 24, 0, 30, 0, 0, loc)); ended {
		t.Fatal("23:00/120 at 00:30 must NOT read ended (still active)")
	}

	// 08:30 + 60 min at 07:00 — yesterday's window ended.
	atAM := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowStart: "08:30", MentorWindowMinutes: 60})
	if ended, _ := atAM.mentorWindowEnded(time.Date(2026, 9, 24, 7, 0, 0, 0, loc)); !ended {
		t.Fatal("08:30/60 at 07:00 must read ended (yesterday's window)")
	}

	// -1 → disabled → never ended.
	atOff := mentoredTrader(t, store.RiskControlConfig{MentorMode: true, MentorWindowStart: "08:30", MentorWindowMinutes: -1})
	if ended, _ := atOff.mentorWindowEnded(time.Date(2026, 9, 24, 7, 0, 0, 0, loc)); ended {
		t.Fatal("a disabled window must never read ended")
	}
}
