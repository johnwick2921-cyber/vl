package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// TestMentorSplitPlacesBothLegs pins the split AT PLACEMENT end-to-end (CTO
// ruling 2026-10-04): a 5-contract mentor intent authors TWO rows (leg1=3
// TP=entry+R, leg2=2 TP=trade target, shared EntryGroup) and BOTH reach the
// wire on a flat account — the one-entry guards treat the two legs as ONE.
func TestMentorSplitPlacesBothLegs(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	resetMentorCounters()

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-split", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29615, StopPts: 5, TargetPts: 15, ExpiryMs: rthInstant().UnixMilli() + 60_000}
	at.mentorDispatchIntent(in, mentorTierInputs{}, 1000, 1100)

	arm, ok := mentorLiveArmFor("isb-split")
	if !ok || len(arm.RowIDs) != 2 {
		t.Fatalf("a 5-contract split must author 2 leg rows, got %+v ok=%v", arm, ok)
	}
	var leg1, leg2 store.ArmedOrderDB
	if err := ledger.DB().First(&leg1, arm.RowIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := ledger.DB().First(&leg2, arm.RowIDs[1]).Error; err != nil {
		t.Fatal(err)
	}
	if leg1.Contracts == nil || *leg1.Contracts != 3 || leg2.Contracts == nil || *leg2.Contracts != 2 {
		t.Fatalf("contracts = (%v, %v), want (3, 2)", leg1.Contracts, leg2.Contracts)
	}
	if leg1.LegIndex != 0 || leg2.LegIndex != 1 {
		t.Fatalf("leg indexes = (%d, %d), want (0, 1)", leg1.LegIndex, leg2.LegIndex)
	}
	if leg1.EntryGroup == "" || leg1.EntryGroup != leg2.EntryGroup {
		t.Fatalf("both legs must share a non-empty EntryGroup, got %q / %q", leg1.EntryGroup, leg2.EntryGroup)
	}
	if leg1.TargetPx != 29605 || leg2.TargetPx != 29615 {
		t.Fatalf("leg TPs = (%.2f, %.2f), want (29605 = entry+R, 29615 = trade target)", leg1.TargetPx, leg2.TargetPx)
	}

	// Place on a FLAT account: BOTH legs reach the wire (place_pending + signal).
	now := rthInstant()
	s := at.armedTrader().GetServer()
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)

	var l1, l2 store.ArmedOrderDB
	if err := ledger.DB().First(&l1, arm.RowIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := ledger.DB().First(&l2, arm.RowIDs[1]).Error; err != nil {
		t.Fatal(err)
	}
	if l1.State != store.StatePlacePending || l1.SignalID == "" ||
		l2.State != store.StatePlacePending || l2.SignalID == "" {
		t.Fatalf("BOTH legs must place (place_pending + signal id), got leg1=%q/%q leg2=%q/%q",
			l1.State, l1.SignalID, l2.State, l2.SignalID)
	}
}

// TestMentorSplitSingleLeg pins n = 1 → a single leg row (LegCount 0) whose TP
// is the trade target (no partial — scale-out requires size ≥ 2).
func TestMentorSplitSingleLeg(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	resetMentorCounters()

	in := mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-one", Setup: "ISB", Side: mentor.SideShort,
		Price: 29600, Stop: 29605, Target: 29585, StopPts: 5, TargetPts: 15, ExpiryMs: rthInstant().UnixMilli() + 60_000}
	// A short with a 5-pt stop is in the base tier (5) — force n=1 via a
	// 1-contract choice.
	at.mentorArmIntent(in, mentorSizeChoice{Contracts: 1, Tier: "base"}, 1000, 1100, "B", 0)

	arm, ok := mentorLiveArmFor("isb-one")
	if !ok || len(arm.RowIDs) != 1 {
		t.Fatalf("n=1 must author a single leg row, got %+v ok=%v", arm, ok)
	}
	var row store.ArmedOrderDB
	if err := ledger.DB().First(&row, arm.RowIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if row.Contracts == nil || *row.Contracts != 1 {
		t.Fatalf("single leg contracts = %v, want 1", row.Contracts)
	}
	if row.LegCount != 0 {
		t.Fatalf("single leg LegCount = %d, want 0", row.LegCount)
	}
	if row.TargetPx != 29585 {
		t.Fatalf("single leg TP = %.2f, want 29585 (the trade target, not the +1R partial)", row.TargetPx)
	}
}
