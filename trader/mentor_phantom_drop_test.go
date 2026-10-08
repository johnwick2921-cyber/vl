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
// The trader refuses a DEAD setup inside mentorDispatchEntry, whose deferred
// guard drops EVERY evaluator trace of the intent (the G1/G2 sim pend AND the
// ISB arm / swing Pending) unless the arm reached mentorRegisterLiveArm. The
// one guard lives in mentorDropEvalArm — no scattered per-gate calls — so no
// refusal path can leave a phantom that suppresses the next same-side setup or
// emits a follow-up ExtendArm/CancelArm/MoveStopBE for an order never authored.

// resetMentorLiveArms clears the live-arm registry for a dispatch test.
func resetMentorLiveArms(t *testing.T) {
	t.Helper()
	mentorLiveMu.Lock()
	mentorLiveArms = map[string]mentorLiveArm{}
	mentorLiveMu.Unlock()
	t.Cleanup(func() {
		mentorLiveMu.Lock()
		mentorLiveArms = map[string]mentorLiveArm{}
		mentorLiveMu.Unlock()
	})
}

// TestMentorDispatchDropArmOnNeverAdd — the named RED: the never-add refusal
// (open long + long intent) drops the evaluator's ISB arm, so the next
// same-side ISB is NOT refused isb_arm_active. Revert the drop guard (remove
// the mentorDropEvalArm call from the defer) → the arm survives → FAIL.
func TestMentorDispatchDropArmOnNeverAdd(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "1")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-test"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	resetMentorLiveArms(t)
	wireMentorPlacementSeams(t)
	// never-add: an open LONG position + a LONG intent → the add gate refuses.
	mentorOpenSideSource = func() string { return "long" }
	t.Cleanup(func() { mentorOpenSideSource = nil })
	now := time.Date(2026, 9, 23, 9, 0, 0, 0, kernel.CTLocation())
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-test", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12, ExpiryMs: now.UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-test"]; ok {
		t.Fatalf("the never-add refusal must drop the evaluator's ISB arm")
	}
}

// TestMentorDispatchDropArmOnDoneAfterWinSwing — the done-after-win refusal on
// a SWING drops the evaluator's swing Pending, so a later 5m close through the
// line emits NO CancelArm for that id (the phantom follow-up the CTO saw:
// "+1R → stop to break-even" on a swing arm never placed). Revert the drop
// guard → the Pending survives and the second SwingTick emits the CancelArm.
func TestMentorDispatchDropArmOnDoneAfterWinSwing(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "1")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	resetMentorLiveArms(t)
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

	// The done-after-win gate refuses the swing placement; the deferred guard
	// drops the pending arm on the way out.
	at.mentorDispatchIntent(swing, mentorTierInputs{}, 1000, 1100)
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

// TestMentorDispatchDropArmOnStaleData — the #449 stale-data refusal AT
// AUTHORING drops the arm (a setup computed on stale prices is not a setup),
// while the armed-pass keep-the-row behaviour is unchanged (its tests stay green).
func TestMentorDispatchDropArmOnStaleData(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "1")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-stale"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	resetMentorLiveArms(t)
	now := mentorStaleBlockClock(9, 0)
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t)
	mentorStaleBlockFeed(t, now, 3*time.Minute) // stale: ~3 min old, CME open

	at.mentorDispatchIntent(mentorStaleBlockIntent(), mentorTierInputs{}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-stale"]; ok {
		t.Fatalf("the stale-data authoring refusal must drop the evaluator's ISB arm")
	}
}

// TestMentorDispatchDropArmOnRuleGateRefusal — a mentorRuleGate refusal (the
// untagged-setup refusal) also drops the arm through the ONE deferred guard:
// it fires BEFORE mentorPlaceIntent, so a scattered per-gate call would have
// missed it entirely.
func TestMentorDispatchDropArmOnRuleGateRefusal(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-rule"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	resetMentorLiveArms(t)

	// Empty Setup → "untagged setup" refusal at the top of mentorDispatchEntry.
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-rule", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-rule"]; ok {
		t.Fatalf("the rule-gate refusal must drop the evaluator's ISB arm")
	}
}

// TestMentorDispatchDropArmOnStaleIntent — the N10 stale-intent refusal (a
// reference bar that is not the newest closed bar) drops the arm through the
// same deferred guard.
func TestMentorDispatchDropArmOnStaleIntent(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "1")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-n10"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	resetMentorLiveArms(t)
	now := time.Date(2026, 9, 23, 9, 0, 0, 0, kernel.CTLocation())
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t)

	// RefBarMs 2000 ≠ barCloseMs 1000 → the N10 stale-intent refusal.
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-n10", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12,
		RefBarMs: 2000, ExpiryMs: now.UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-n10"]; ok {
		t.Fatalf("the N10 stale-intent refusal must drop the evaluator's ISB arm")
	}
}

// TestMentorDispatchRegisteredLiveArmNotDropped — a REGISTERED live arm (the
// row was authored) is NOT dropped on a later refusal: the deferred guard's
// mentorLiveArmFor check exempts it. Named RED: removing the check makes the
// refusal drop the registered arm → FAIL.
func TestMentorDispatchRegisteredLiveArmNotDropped(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.mentorEval = mentor.New(mentor.Config{})
	at.mentorEval.State.ISBArms["isb-reg"] = mentor.ISBArm{Inside: 0, Side: mentor.SideLong}
	resetMentorLiveArms(t)
	mentorRegisterLiveArm("isb-reg", at.id, 1, "long", 21000)

	// A refusal (untagged setup) on an arm that IS registered must NOT drop it.
	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-reg", Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)

	if _, ok := at.mentorEval.State.ISBArms["isb-reg"]; !ok {
		t.Fatalf("a REGISTERED live arm must NOT be dropped on a refusal")
	}
}
