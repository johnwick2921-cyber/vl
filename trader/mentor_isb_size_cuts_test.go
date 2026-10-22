package trader

import (
	"testing"
	"time"

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
// or inside a range, is sized at the reduced tier 3 (the course's tier; m186
// reverts the 2026-10-16 "reduced no longer cuts below 4" flatten). The
// evaluator stamps Intent.Flag; the sizing call site (mentorSizeFor, reached
// through mentorExtraFor exactly as mentorTick does) must read it. Mutants:
// (1) drop the two flag lines in mentorSizeFor → the cut tiers are never chosen
// → RED; (2) F-9 — revert either `clamp(reduced)` back to a literal in
// mentorContractsFor → the old-extreme / in-range cases size the base 5 → RED
// (the reduced knob must govern ALL three "reduce size" tiers).
func TestMentorISBSizeCutsAtSizingCallSite(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: true})
	cases := []struct {
		name     string
		in       mentor.Intent
		wantN    int
		wantTier string
	}{
		{"no flag → base", isbIntent(""), 5, "base"},
		{"at an old extreme → 3", isbIntent(mentor.FlagISBAtOldExtreme), 3, "isb_old_extreme"},
		{"in a range → 3", isbIntent(mentor.FlagISBInRange), 3, "isb_in_range"},
		{"both (joined as the evaluator emits) → 3, old-extreme tier", isbIntent(mentor.FlagISBAtOldExtreme + "|" + mentor.FlagISBInRange), 3, "isb_old_extreme"},
		{"an unrelated flag changes nothing", isbIntent("something_else"), 5, "base"},
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
	// (the old-extreme flag still cuts to the reduced 3); the spent-day cut
	// rides the 15-pt target cap and the runner cap instead.
	spent := isbIntent(mentor.FlagISBAtOldExtreme)
	spent.SpentDay = true
	if got, _ := at.mentorSizeFor(spent, mentorExtraFor(spent, false)); got.Tier != "isb_old_extreme" || got.Contracts != 3 {
		t.Fatalf("R09: spent day must NOT win over the old-extreme cut: %+v", got)
	}
	conf := isbIntent(mentor.FlagISBInRange)
	extra := mentorExtraFor(conf, false)
	extra.Confluence = true
	if got, _ := at.mentorSizeFor(conf, extra); got.Tier != "isb_in_range" || got.Contracts != 3 {
		t.Fatalf("an in-range ISB with confluence must still be cut to the reduced 3: %+v", got)
	}
}

// TestMentorISBCutTiersSizeThreeSplitTwoOne — the F-9 fix through the FULL
// production dispatch (mentorDispatchIntent → sizing → mentorArmIntent): an ISB
// at an old high/low and an ISB inside a range each size 3 (the reduced knob)
// and the authored row carries the 2+1 split (Leg1Qty=2). Anchored mutant
// (F-9): revert `clamp(reduced)` to `clamp(base)` in either cut branch →
// Contracts=5 on the row → RED.
func TestMentorISBCutTiersSizeThreeSplitTwoOne(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	ledger := st.ArmedOrders()
	wireMentorPlacementSeams(t, at)
	ct := time.FixedZone("CT", -5*3600)
	setMentorNowSource(func() time.Time { return time.Date(2026, 10, 2, 9, 0, 0, 0, ct) })
	t.Cleanup(func() { setMentorNowSource(nil) })

	for _, c := range []struct {
		name string
		flag string
	}{
		{"ISB at an old high/low", mentor.FlagISBAtOldExtreme},
		{"ISB inside a range", mentor.FlagISBInRange},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := isbIntent(c.flag)
			in.ArmID = "isb-cut"
			in.ExpiryMs = time.Now().UnixMilli() + 60_000
			at.mentorDispatchIntent(in, mentorTierInputs{}, 0, 0)
			var row store.ArmedOrderDB
			if err := ledger.DB().Where("scenario LIKE ?", "isb-cut-%").First(&row).Error; err != nil {
				t.Fatalf("the ISB cut tier must author a row: %v", err)
			}
			if row.Contracts == nil || *row.Contracts != 3 {
				t.Fatalf("%s: Contracts = %v, want 3 (the reduced knob)", c.name, intPtrText(row.Contracts))
			}
			if row.Leg1Qty == nil || *row.Leg1Qty != 2 {
				t.Fatalf("%s: Leg1Qty = %v, want 2 (3 = 2+1)", c.name, row.Leg1Qty)
			}
		})
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
