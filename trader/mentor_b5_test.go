package trader

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── B5 (L8) — the exit mode is stored per ARM (signal id), never per side ────
//
// Before B5 the entry-time exit branch (A/B/C/swing) was registered under
// strings.ToLower(side), so a later same-side order overwrote an earlier arm's
// branch BEFORE it filled. Two resting longs at two levels would both fork from
// the SECOND arm's branch. The pin places two same-side arms — one forking B,
// one forking C — and asserts the first arm's branch survives the second's
// placement.

func TestB5ExitModeIsPerArmNotPerSide(t *testing.T) {
	ResetMentorCountersForTest()
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t)
	// The confluence flag rides the intent so the two arms fork differently.
	mentorConfluenceForIntent = func(in mentor.Intent) bool { return in.Confluence }
	t.Cleanup(func() { mentorConfluenceForIntent = nil })

	ct := kernel.CTLocation()
	mentorNowSource = func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) }
	t.Cleanup(func() { mentorNowSource = nil })

	var placed int
	mentorPlaceRecorderForTest = func(i mentor.Intent, n int) { placed++ }
	t.Cleanup(func() { mentorPlaceRecorderForTest = nil })
	mentorLatestPriceSource = func() (float64, bool) { return 20999.50, true }
	t.Cleanup(func() { mentorLatestPriceSource = nil })

	base := mentor.Intent{Action: mentor.PlaceStopEntry, Side: mentor.SideLong,
		Price: 21000, Stop: 20988, Target: 21024, Setup: "ISB", StopPts: 12}
	choice := mentorSizeChoice{Contracts: 5, Tier: "base", Why: "test"}

	first := base
	first.ArmID = "lvl-a"
	first.Confluence = false // forks B
	at.mentorPlaceIntent(first, choice, 1000, 1100)

	second := base
	second.ArmID = "lvl-b"
	second.Confluence = true // forks C
	at.mentorPlaceIntent(second, choice, 1000, 1100)

	if placed != 2 {
		t.Fatalf("both same-side arms must reach the placement path, placed=%d", placed)
	}
	if got := at.mentorExitMode(mentorScenarioFor("lvl-a")); got != "B" {
		t.Fatalf("the FIRST arm's branch must survive the second same-side arm (want B), got %q", got)
	}
	if got := at.mentorExitMode(mentorScenarioFor("lvl-b")); got != "C" {
		t.Fatalf("the second arm must register its own branch C, got %q", got)
	}
}

// TestB5ExitModePrunedAtFill — I13: the staged per-arm exit branch is deleted
// once the fill consumes it (copied onto the live-position struct), so the map
// does not grow for the life of the process.
func TestB5ExitModePrunedAtFill(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	at.setMentorExitMode("lvl-9-123", "B")

	r := store.ArmedOrderDB{SignalID: "sig-9", Scenario: "lvl-9-123", Side: "long",
		EntryPx: 29600, StopPx: 29590, TargetPx: 29620}
	u := ntwire.OrderUpdatePayload{FillPrice: 29600, Quantity: 1}
	at.registerMentorLivePos(r, u)

	if got := at.mentorExitMode("lvl-9-123"); got != "" {
		t.Fatalf("the staged branch must be pruned at fill, got %q", got)
	}
	if lp, ok := at.mentorLivePos["sig-9"]; !ok || lp.Pos.Mode != "B" {
		t.Fatalf("the fill must copy the branch onto the live position, got %+v (present=%v)", lp, ok)
	}
}
