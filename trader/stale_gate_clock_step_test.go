package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// ── release #22 OPS-SAFETY — the stale gate at its PRODUCTION call sites ────
//
// Verified at base 9accae11f (throwaway test, quoted in the PR): the same
// 200 s-dead feed was REFUSED on the true clock and PLACED after a −125 s wall
// step. These pins drive mentorPlaceIntent (arm authoring) and
// protectionFreshPrice with an injected stepped clock.

func staleStepPlace(t *testing.T, at *AutoTrader) []mentor.Intent {
	t.Helper()
	var placed []mentor.Intent
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed = append(placed, i) }
	defer func() { mentorPlaceRecorderForTest = nil }()
	at.mentorPlaceIntent(mentorStaleBlockIntent(), mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100)
	return placed
}

func staleStepReceipt(t *testing.T, ago time.Duration) {
	t.Helper()
	recv := time.Now().Add(-ago) // carries the monotonic reading, like the bar cache's stamp
	kernel.FeedLastLiveReceipt = func(string, string) (time.Time, bool) { return recv, true }
	t.Cleanup(func() { kernel.FeedLastLiveReceipt = nil })
}

// TestStaleGateRefusesDeadFeedUnderBackwardWallStep — THE named RED for the
// mutant "wall-clock age again": the feed has been dead 200 s of real time,
// the wall clock reads 125 s behind. The wall rule alone passes it; the
// monotonic receipt leg must refuse it.
func TestStaleGateRefusesDeadFeedUnderBackwardWallStep(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	real := mentorStaleBlockClock(9, 0)
	t.Cleanup(kernel.ClockSampleForTest(kernel.NewSteppedClock(real).Sample))
	wall := real.Add(-125 * time.Second)
	mentorNowSource = func() time.Time { return wall }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, real, 200*time.Second)
	staleStepReceipt(t, 200*time.Second)

	if placed := staleStepPlace(t, at); len(placed) != 0 {
		t.Fatalf("a 200 s-dead feed under a −125 s wall step must NOT author an arm (fail closed); %d placement(s) reached the recorder", len(placed))
	}
	if got := at.mentorCountSnapshot()["stale_data"]; got != 1 {
		t.Fatalf("the refusal must be counted once as stale_data, got %d", got)
	}
	if got := at.mentorCountSnapshot()["placement_refused_stale_data"]; got != 1 {
		t.Fatalf("the per-order refusal must be counted, got %d", got)
	}
}

// TestStaleGateHoldsEntriesAfterAWallStepWithAFreshFeed — any wall step at or
// above the freeze threshold holds NEW entries even when the feed is live, and
// the hold clears on MONOTONIC time only.
func TestStaleGateHoldsEntriesAfterAWallStepWithAFreshFeed(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	now := mentorStaleBlockClock(9, 0)
	clk := kernel.NewSteppedClock(now)
	t.Cleanup(kernel.ClockSampleForTest(clk.Sample))
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })
	wireMentorPlacementSeams(t, at)
	mentorStaleBlockFeed(t, now, 30*time.Second) // fresh on the wall rule
	staleStepReceipt(t, time.Second)             // fresh on the receipt leg

	kernel.ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-125 * time.Second)
	if placed := staleStepPlace(t, at); len(placed) != 0 {
		t.Fatalf("a −125 s wall step must hold NEW entries; %d placement(s) went through", len(placed))
	}
	if got := at.mentorCountSnapshot()["placement_refused_stale_data"]; got != 1 {
		t.Fatalf("the clock-step refusal must be counted, got %d", got)
	}
	clk.Advance(time.Duration(kernel.LoadClockStepConfig().HoldS+1) * time.Second)
	if placed := staleStepPlace(t, at); len(placed) != 1 {
		t.Fatalf("after the hold window the live feed must place again, placed=%d", len(placed))
	}
}

// TestProtectionFreshPriceRefusesDeadFeedUnderBackwardStep — protection's
// price is judged on the FEED legs: a dead feed's last close is not fresh just
// because the wall clock stepped back. (The clock-step HOLD is not consulted:
// protection is never frozen by a step.)
func TestProtectionFreshPriceRefusesDeadFeedUnderBackwardStep(t *testing.T) {
	real := mentorStaleBlockClock(9, 0)
	clk := kernel.NewSteppedClock(real)
	t.Cleanup(kernel.ClockSampleForTest(clk.Sample))
	mentorStaleBlockFeed(t, real, 200*time.Second)
	staleStepReceipt(t, 200*time.Second)
	if _, ok := protectionFreshPrice("MNQ", real.Add(-125*time.Second)); ok {
		t.Fatalf("a 200 s-dead feed's close must not read fresh under a −125 s wall step")
	}
	// Live feed + a clock step: protection still gets its price.
	staleStepReceipt(t, time.Second)
	mentorStaleBlockFeed(t, real, 30*time.Second)
	kernel.ObserveClock()
	clk.Advance(time.Second)
	clk.Step(-125 * time.Second)
	if _, ok := protectionFreshPrice("MNQ", real); !ok {
		t.Fatalf("a clock-step hold must never freeze protection's price on a live feed")
	}
}
