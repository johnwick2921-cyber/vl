package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// TestMentorRefusedIntentLeavesNoSimFill is the X-07 call-site pin: an entry
// intent the trader REFUSES must not leave a pending G1/G2 sim fill in the
// kernel's Limits state — on a later candle it must not phantom-fill and spend
// the leg budget / open a loss box. Mutant: removing the DropArm guard from
// mentorDispatchEntry leaves the pend behind and the refused sub-test goes RED
// (the control proves the seed+fill machinery would otherwise fill it).
func TestMentorRefusedIntentLeavesNoSimFill(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "") // off — every entry is SIZED then held (refused)
	now := time.Now().UnixMilli()
	cfg := mentor.DefaultConfig()
	levels := []mentor.Level{{Kind: mentor.KindOldExtreme, Price: 29650}}
	prev := market.Kline{Open: 29500, High: 29590, Low: 29450, Close: 29580}
	cur := market.Kline{Open: 29580, High: 29590, Low: 29500, Close: 29550}
	fillCur := market.Kline{Open: 29590, High: 29620, Low: 29540, Close: 29610}

	seed := mentor.Intent{
		Action: mentor.PlaceStopLimitEntry, ArmID: "x-07", Setup: "ISB",
		Side: mentor.SideLong, Price: 29600, Stop: 29595, Target: 29610,
		StopPts: 5, TargetPts: 10, ExpiryMs: now + 60_000,
	}

	// Control: the seed registers a pend that WOULD fill on the next candle
	// and create the G1 leg — proves the machinery the pin asserts against.
	t.Run("control_pend_fills_when_not_dropped", func(t *testing.T) {
		ev := mentor.New(cfg)
		ev.State.Limits.Apply([]mentor.Intent{seed}, prev, cur, now, levels, cfg)
		ev.State.Limits.Apply(nil, prev, fillCur, now+60_000, levels, cfg)
		if ev.State.Limits.Long == nil {
			t.Fatalf("control: the seeded pend must fill and create the G1 leg")
		}
	})

	// The pin: the same seed, dispatched through the trader's refusal path
	// (MENTOR_PLACE off) — the pend must be dropped, so no fill, no leg.
	t.Run("refused_intent_leaves_no_sim_fill", func(t *testing.T) {
		at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
		at.mentorEval = mentor.New(cfg)
		mentorLiveMu.Lock()
		mentorLiveArms = map[string]mentorLiveArm{}
		mentorLiveMu.Unlock()
		t.Cleanup(func() {
			mentorLiveMu.Lock()
			mentorLiveArms = map[string]mentorLiveArm{}
			mentorLiveMu.Unlock()
		})

		at.mentorEval.State.Limits.Apply([]mentor.Intent{seed}, prev, cur, now, levels, cfg)
		resetMentorCounters()
		at.mentorDispatchIntent(seed, mentorTierInputs{}, 1000, 1100)
		if got := MentorCountSnapshot()["placement_held"]; got != 1 {
			t.Fatalf("the intent must be refused via MENTOR_PLACE off (placement_held=1), got %d", got)
		}
		at.mentorEval.State.Limits.Apply(nil, prev, fillCur, now+60_000, levels, cfg)
		if at.mentorEval.State.Limits.Long != nil {
			t.Fatalf("a trader-refused intent left a phantom G1/G2 sim fill (leg created)")
		}
	})
}
