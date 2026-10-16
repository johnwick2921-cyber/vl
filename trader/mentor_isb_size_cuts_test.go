package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/store"
)

func isbIntent(flag string) mentor.Intent {
	return mentor.Intent{
		Action: mentor.PlaceStopEntry, Side: mentor.SideLong, Setup: "ISB",
		Price: 21000, Stop: 20992, Target: 21016, StopPts: 8, TargetPts: 16, Flag: flag,
	}
}

// ISB written rules 2 and 3 (D4.1 p1 @08:05/09:40): an ISB at an old high/low,
// or inside a range, is sized at the reduced tier (4 under owner order
// 2026-10-16). The evaluator stamps Intent.Flag; the sizing call site
// (mentorSizeFor, reached through mentorExtraFor exactly as mentorTick does)
// must read it. Mutant: drop the two flag lines in mentorSizeFor → the cut
// tiers are never chosen → RED.
func TestMentorISBSizeCutsAtSizingCallSite(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	cases := []struct {
		name     string
		in       mentor.Intent
		wantN    int
		wantTier string
	}{
		{"no flag → base", isbIntent(""), 4, "base"},
		{"at an old extreme → 4", isbIntent(mentor.FlagISBAtOldExtreme), 4, "isb_old_extreme"},
		{"in a range → 4", isbIntent(mentor.FlagISBInRange), 4, "isb_in_range"},
		{"both (joined as the evaluator emits) → 4, old-extreme tier", isbIntent(mentor.FlagISBAtOldExtreme + "|" + mentor.FlagISBInRange), 4, "isb_old_extreme"},
		{"an unrelated flag changes nothing", isbIntent("something_else"), 4, "base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := at.mentorSizeFor(tc.in, mentorExtraFor(tc.in, false))
			if err != nil {
				t.Fatal(err)
			}
			if got.Contracts != tc.wantN || got.Tier != tc.wantTier {
				t.Fatalf("sized %d @ %q, want %d @ %q", got.Contracts, got.Tier, tc.wantN, tc.wantTier)
			}
		})
	}

	// R09: a spent day no longer wins over the ISB cuts — it sizes normally
	// (the old-extreme flag still cuts to the reduced 4); the spent-day cut
	// rides the 15-pt target cap and the runner cap instead.
	spent := isbIntent(mentor.FlagISBAtOldExtreme)
	spent.SpentDay = true
	if got, _ := at.mentorSizeFor(spent, mentorExtraFor(spent, false)); got.Tier != "isb_old_extreme" || got.Contracts != 4 {
		t.Fatalf("R09: spent day must NOT win over the old-extreme cut: %+v", got)
	}
	conf := isbIntent(mentor.FlagISBInRange)
	extra := mentorExtraFor(conf, false)
	extra.Confluence = true
	if got, _ := at.mentorSizeFor(conf, extra); got.Tier != "isb_in_range" || got.Contracts != 4 {
		t.Fatalf("an in-range ISB with confluence must still be cut to the reduced 4: %+v", got)
	}
}

// The same cut through the injector's dispatch (the production entry path): the
// tier counter and the sized-not-placed line carry the cut tier.
func TestMentorISBSizeCutsThroughDispatch(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "")
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	wireMentorPlacementSeams(t, at)

	in := isbIntent(mentor.FlagISBAtOldExtreme)
	at.mentorDispatchIntent(in, mentorExtraFor(in, false), 1000, 1100)
	snap := at.mentorCountSnapshot()
	if snap["tier_isb_old_extreme"] != 1 {
		t.Fatalf("an ISB at an old extreme must be sized at the cut tier: %v", snap)
	}
	if snap["tier_base"] != 0 {
		t.Fatalf("it must not also be sized base: %v", snap)
	}
}
