package trader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── REL9 EXIT-DRIVE review fixes (2×P2 + 4×P3) call-site pins ───────────────

// P2-1: positionAmt is SIGNED (short < 0); mentorOpenQtyForDrive must Abs it or
// the I7 leg-1-gone latch returns at qty<=0 for shorts. MUTANT: drop the Abs →
// openQty["short"] < 0 → RED.
func TestMentorRel9OpenQtyAbsForShort(t *testing.T) {
	at, _, _, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	at.armedTrader().GetServer().SeedPositionsForTest("Sim101", []ntwire.OpenPosition{
		{Symbol: "MNQ", Side: "short", Quantity: 2, AvgPrice: 100},
	})
	qty, ok := at.mentorOpenQtyForDrive()
	if !ok {
		t.Fatal("open qty read failed")
	}
	if qty["short"] != 2 {
		t.Fatalf("P2-1: short qty = %v, want 2 (Abs of the signed positionAmt)", qty["short"])
	}
}

// P2-2: the I13 prune moved into the shared builder lost a staged C/swing mode
// when a partial fill (B1 expiry / I4 cancel) built the position and the
// remainder filled before the cancel settled. The prune must happen ONLY at the
// registration sites. MUTANT: prune in mentorBuildLivePos → the full-fill
// rebuild reads Mode "" → RED.
func TestMentorRel9ModeSurvivesPartialThenFullFill(t *testing.T) {
	at, _ := newDriveAT(t)
	scenario := "rel9-arm-123"
	at.setMentorExitMode(scenario, "C")
	row := store.ArmedOrderDB{ID: 1, SignalID: "rel9-sig", Side: "long", StopPx: 98, TargetPx: 106,
		FillQuantity: 2, Condition: "ISB", Scenario: scenario, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(3)}

	// Partial fill 2/3 → the I4/B1 register-if-absent path (mentorBuildLivePos).
	if !at.mentorRegisterPartialFillIfAbsent(row) {
		t.Fatal("the partial fill must register")
	}
	if at.mentorExitMode(scenario) != "C" {
		t.Fatalf("P2-2: the partial build pruned the staged mode — got %q, want C", at.mentorExitMode(scenario))
	}
	lp := at.mentorLivePos["rel9-sig"]
	if lp == nil || lp.Pos.Mode != "C" {
		t.Fatalf("P2-2: partial registered mode = %+v, want C", lp)
	}
	// The remainder fills (full 3) → registerMentorLivePos rebuilds and MUST read C.
	at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{SignalID: "rel9-sig", Quantity: 3, FillPrice: 100})
	lp2 := at.mentorLivePos["rel9-sig"]
	if lp2 == nil || lp2.Pos.Mode != "C" {
		t.Fatalf("P2-2: the full-fill rebuild lost the staged mode: got %+v, want C", lp2)
	}
	// And NOW the full fill consumed the staged branch.
	if at.mentorExitMode(scenario) != "" {
		t.Fatalf("P2-2: the full fill must prune the staged mode, got %q", at.mentorExitMode(scenario))
	}
}

// P3-3a: the shape counters fire ONLY when a registration actually happened —
// the I4 sweep re-runs every bar while the row is cancel_pending, and a
// register-if-absent miss must not bump the counter (canon 35). MUTANT: bump in
// the shared builder → the second call bumps again → RED.
func TestMentorRel9ShapeCounterOnlyOnRegistration(t *testing.T) {
	at, _ := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	old := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, sid string) (nttrader.SentSplit, bool) {
		if sid == "rel9-cnt" {
			return nttrader.SentSplit{Leg1Qty: 2, Leg1TP: 12}, true
		}
		return nttrader.SentSplit{}, false
	}
	t.Cleanup(func() { mentorSentSplit = old })
	row := store.ArmedOrderDB{ID: 1, SignalID: "rel9-cnt", Side: "long", StopPx: 98, TargetPx: 106,
		FillQuantity: 4, Condition: "PHL", Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5)}
	ResetMentorCountersForTest()
	if !at.mentorRegisterPartialFillIfAbsent(row) {
		t.Fatal("the first partial registration must succeed")
	}
	if c := MentorCountSnapshot()["exit_drive_split_registered"]; c != 1 {
		t.Fatalf("first registration split counter = %d, want 1", c)
	}
	// The sweep re-runs while the row is cancel_pending: already registered → no bump.
	if at.mentorRegisterPartialFillIfAbsent(row) {
		t.Fatal("a second sweep must not re-register")
	}
	if c := MentorCountSnapshot()["exit_drive_split_registered"]; c != 1 {
		t.Fatalf("a register-if-absent miss bumped the split counter to %d, want 1", c)
	}
}

// P3-3b: a partial-fill sweep must NOT re-register a position the drive already
// unregistered as flat (tombstone). MUTANT: drop the tombstone → re-registered
// → RED.
func TestMentorRel9FlatUnregisteredNotReregistered(t *testing.T) {
	at, _ := newDriveAT(t)
	at.mentorRegisterLivePos("rel9-flat", &mentorLivePos{Pos: mentorPosition{Symbol: "MNQ", Side: "long"}})
	at.mentorUnregisterLivePos("rel9-flat") // flat → tombstone
	row := store.ArmedOrderDB{ID: 1, SignalID: "rel9-flat", Side: "long", StopPx: 98, TargetPx: 106,
		FillQuantity: 2, Condition: "PHL", Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5)}
	if at.mentorRegisterPartialFillIfAbsent(row) {
		t.Fatal("P3-3b: a flat-unregistered signal must not be re-registered by a partial sweep")
	}
	if _, ok := at.mentorLivePos["rel9-flat"]; ok {
		t.Fatal("P3-3b: the flat position was resurrected")
	}
}

// P3-6: a row still cancel_pending when the side went flat was unregistered
// WITHOUT its split-record forget — forget it HERE when it later settles
// terminal. MUTANT: no forget in the settlement path → SplitSentFor still true
// → RED.
func TestMentorRel9ForgetSplitOnTerminalSettle(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildSplitLegs)
	nt := at.armedTrader()
	sid, err := nt.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102.13)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if _, ok := nt.SplitSentFor(sid); !ok {
		t.Fatal("fixture: the split record must be recorded")
	}
	now := time.Now()
	row := &store.ArmedOrderDB{TraderID: at.id, PlanID: "p1", Version: 1, Session: "MENTOR", Scenario: "rel9-settle",
		Side: "long", EntryPx: 100, StopPx: 98, TargetPx: 106, State: store.StateWorking, SignalID: sid}
	if err := ledger.UpsertArm(row); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RequestCancel(row.ID, "gate changed", now.Add(-2*time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// A FRESH book showing the order absent (a confirmable cancel). The account
	// must match the trader's bound account ("Sim101") — persistedBook reads by it.
	if err := at.store.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{
		Account: "Sim101", OrdersJSON: "[]", ReceivedMs: now.Add(-time.Second).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	settled, _, _ := at.confirmPendingCancels(ledger, nil, now)
	if settled != 1 {
		t.Fatalf("settled = %d, want 1", settled)
	}
	if _, ok := nt.SplitSentFor(sid); ok {
		t.Fatal("P3-6: the split record must be forgotten when the cancel settles terminal")
	}
}
