package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// ── FIX-MENTOR-PHANTOM-ARM — call-site pins ─────────────────────────────────
// When the trader refuses a placement as a DEAD setup, mentorPlaceIntent must
// report it back via Evaluator.DropArm so the evaluator forgets the setup at
// once instead of keeping a phantom arm that suppresses the next same-side
// setup and emits follow-up ExtendArm/CancelArm/MoveStopBE for an order that
// was never authored.

// TestMentorPlaceIntentDropArmOnNeverAdd — the named RED: the never-add refusal
// (open long + long intent) drops the evaluator's ISB arm, so the next same-side
// ISB is NOT refused isb_arm_active. Revert the DropArm call → the arm survives
// and the test fails.
func TestMentorPlaceIntentDropArmOnNeverAdd(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-test"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	wireMentorPlacementSeams(t)
	// never-add: an open LONG position + a LONG intent → the add gate refuses.
	mentorOpenSideSource = func() string { return "long" }
	t.Cleanup(func() { mentorOpenSideSource = nil })
	now := time.Date(2026, 9, 23, 9, 0, 0, 0, kernel.CTLocation())
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-test", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12, ExpiryMs: now.UnixMilli() + 60_000}
	at.mentorPlaceIntent(in, mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-test"]; ok {
		t.Fatalf("the never-add refusal must drop the evaluator's ISB arm")
	}
}

// TestMentorPlaceIntentDropArmOnDoneAfterWinSwing — the done-after-win refusal
// on a SWING drops the evaluator's swing Pending, so a later 5m close through
// the line emits NO CancelArm for that id (the phantom follow-up the CTO saw:
// "+1R → stop to break-even" on a swing arm that was never placed). Revert the
// DropArm call in the done-after-win branch → the Pending survives and the
// second SwingTick emits the CancelArm → this test goes RED.
func TestMentorPlaceIntentDropArmOnDoneAfterWinSwing(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	wireMentorPlacementSeams(t)
	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	mentorDayNetSource = func() (float64, bool) { return 120, true }
	mentorClosedProfitSource = func() (bool, bool) { return true, true }
	t.Cleanup(func() {
		mentorNowSource = nil
		mentorDayNetSource = nil
		mentorClosedProfitSource = nil
	})

	// Drive a real SwingTick to produce a resting SHORT swing (the evaluator
	// state that carries the pending arm).
	mk := func(day, hour, minute int, o, h, l, c float64) market.Kline {
		ot := time.Date(2026, 9, day, hour, minute, 0, 0, ct)
		return market.Kline{OpenTime: ot.UnixMilli(), Open: o, High: h, Low: l, Close: c, CloseTime: ot.UnixMilli() + 4*60_000}
	}
	tape := []market.Kline{
		mk(14, 17, 5, 9999, 10000, 9998, 10000),   // closed bucket 17:00
		mk(14, 21, 5, 10999, 11000, 10998, 11000), // closed bucket 21:00
		mk(15, 1, 5, 11999, 12000, 11998, 12000),  // closed bucket 01:00
		mk(15, 5, 0, 9950, 9960, 9945, 9955),      // prev: below the line
		mk(15, 5, 5, 10160, 10175, 10155, 10160),  // touches, closes back below → SHORT reject
	}
	sw := &mentor.SwingState{}
	out := mentor.SwingTick(sw, tape, mentor.DefaultSwingCfg(), tape[4].OpenTime+60_000)
	if len(out) != 1 || out[0].Action != mentor.PlaceStopEntry {
		t.Fatalf("swing tape must emit exactly one stop entry: got %+v", out)
	}
	swing := out[0]
	at.mentorEval.State.Swing = *sw

	// The done-after-win gate refuses the swing placement and drops the arm.
	at.mentorPlaceIntent(swing, mentorSizeChoice{Contracts: 1, Tier: "swing4h"}, 1000, 1100)
	if at.mentorEval.State.Swing.Pending != nil {
		t.Fatalf("the done-after-win refusal must drop the swing Pending for %q", swing.ArmID)
	}
	if at.mentorEval.State.Swing.Pos != nil {
		t.Fatalf("a refused (never-placed) swing must never open a filled position")
	}

	// Later: a 5m close through the line must NOT emit a CancelArm for that id.
	through := mk(15, 5, 10, 10170, 10210, 10160, 10200)
	later := mentor.SwingTick(&at.mentorEval.State.Swing, append(tape, through), mentor.DefaultSwingCfg(), through.OpenTime+60_000)
	for _, in := range later {
		if in.ArmID == swing.ArmID {
			t.Fatalf("after the done-after-win refusal dropped the arm, no intent for %q may follow; got %+v", swing.ArmID, later)
		}
	}
}

// TestMentorPlaceIntentDropArmOnStaleData — the #449 stale-data refusal AT
// AUTHORING drops the arm (a setup computed on stale prices is not a setup),
// while the armed-pass keep-the-row behaviour is unchanged (its tests stay green).
func TestMentorPlaceIntentDropArmOnStaleData(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-stale"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	now := time.Date(2026, 9, 23, 9, 0, 0, 0, kernel.CTLocation())
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	// stale 1m feed: the newest bar is 3 min old.
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		return []market.Kline{{OpenTime: now.UnixMilli() - 3*60_000, CloseTime: now.UnixMilli() - 2*60_000 - 1}}
	}
	t.Cleanup(func() { market.FuturesBarsProvider = nil })

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-stale", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", ExpiryMs: now.UnixMilli() + 60_000}
	at.mentorPlaceIntent(in, mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-stale"]; ok {
		t.Fatalf("the stale-data authoring refusal must drop the evaluator's ISB arm")
	}
}
