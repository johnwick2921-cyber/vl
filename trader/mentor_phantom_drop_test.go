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
