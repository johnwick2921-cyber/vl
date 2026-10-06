package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── BUILD-ALL review fixes I6 / I7 / I10 / I4 (call-site pins) ──────────────

// I6: a leg-1 TP receipt that lands MID-candle must NOT start the runner's trail
// on the crossing candle — the trail begins only on candles that OPEN after the
// confirmation instant (next-candle rule). MUTANT: read pos.Scaled unlocked
// (scaledBefore = Scaled) → the trail runs on candle 1 → RED.
func TestMentorI6ReceiptMidCandleTrailsNextCandle(t *testing.T) {
	at, moves := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	old := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, sid string) (nttrader.SentSplit, bool) {
		if sid == "sig-split" {
			return nttrader.SentSplit{Leg1Qty: 3, Leg1TP: 12}, true
		}
		return nttrader.SentSplit{}, false
	}
	t.Cleanup(func() { mentorSentSplit = old })

	row := store.ArmedOrderDB{ID: 7, SignalID: "sig-split", Side: "long", StopPx: 8, TargetPx: 16, FillQuantity: 5, Condition: "PHL"}
	at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-split", Account: "Sim101", FillPrice: 10, Quantity: 5})
	lp := at.mentorLivePos["sig-split"]
	if lp == nil || lp.Legs[1].Qty != 2 {
		t.Fatalf("split not registered: %+v", lp)
	}
	// Arm BE first (high 13.2 ≥ half 11.5), so the trail leg can run next candle.
	at.mentorExitDrivePos(nil, lp, 12.8, 13.2, 12, 0)
	if !lp.Pos.ArmedBE {
		t.Fatal("fixture: BE must be armed")
	}
	// The receipt lands MID-candle 1 (candle open 1000, receipt 1500).
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{
		SignalID: "sig-split", PositionSide: "long", ExitReason: "tp", Leg: 1,
		ExitTime: time.UnixMilli(1500).UTC().Format(time.RFC3339Nano),
	})
	if !lp.Pos.Scaled || lp.Pos.Leg1ExitedAtMs != 1500 {
		t.Fatalf("the receipt must latch Scaled at 1500: %+v", lp.Pos)
	}
	// Candle 1 closes (open 1000): the confirmation is MID-candle, so NO trail —
	// the runner only gets the 1:1 (2·14 − 16 = 12). (The first two moves are the
	// BE candle arming both legs.)
	at.mentorExitDrivePos(nil, lp, 14, 14.5, 13.5, 1000)
	assertMoves(t, moves(),
		driveMove{"sig-split", "long", 10}, driveMove{"sig-split", "long", 10},
		driveMove{"sig-split", "long", 12})
	// Candle 2 closes (open 2000 > receipt): the trail applies (low 13.5 − 1 tick = 13.25).
	at.mentorExitDrivePos(nil, lp, 14, 14.5, 13.5, 2000)
	assertMoves(t, moves(),
		driveMove{"sig-split", "long", 10}, driveMove{"sig-split", "long", 10},
		driveMove{"sig-split", "long", 12}, driveMove{"sig-split", "long", 13.25})
}

// I7: when the broker snapshot shows the side reduced to the runner's qty (leg 1
// gone) and the TP receipt never arrived, the drive latches Scaled with the same
// next-candle rule. Never latches while leg 1 is still present. MUTANT: drop the
// snapshot latch → Scaled never set → RED.
func TestMentorI7BrokerSnapshotLatchesScaledWhenLeg1Gone(t *testing.T) {
	at, _ := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	old := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, sid string) (nttrader.SentSplit, bool) {
		if sid == "sig-split" {
			return nttrader.SentSplit{Leg1Qty: 3, Leg1TP: 12}, true
		}
		return nttrader.SentSplit{}, false
	}
	t.Cleanup(func() { mentorSentSplit = old })
	row := store.ArmedOrderDB{ID: 7, SignalID: "sig-split", Side: "long", StopPx: 8, TargetPx: 16, FillQuantity: 5, Condition: "PHL"}
	at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-split", Account: "Sim101", FillPrice: 10, Quantity: 5})
	lp := at.mentorLivePos["sig-split"]
	if lp == nil || lp.Legs[1].Qty != 2 {
		t.Fatalf("split not registered: %+v", lp)
	}

	T := int64(2_000)
	oldNow := mentorNowSource
	mentorNowSource = func() time.Time { return time.UnixMilli(T) }
	t.Cleanup(func() { mentorNowSource = oldNow })

	sides := map[string]bool{"long": true}
	qty := map[string]float64{"long": 5} // leg 1 still present
	oldSides := mentorDriveOpenSides
	mentorDriveOpenSides = func(*AutoTrader) (map[string]bool, bool) { return sides, true }
	t.Cleanup(func() { mentorDriveOpenSides = oldSides })
	oldQty := mentorDriveOpenQty
	mentorDriveOpenQty = func(*AutoTrader) (map[string]float64, bool) { return qty, true }
	t.Cleanup(func() { mentorDriveOpenQty = oldQty })

	bars := []market.Kline{{OpenTime: T + 100, Close: 14, High: 14.5, Low: 13.5, Final: true}}
	at.mentorExitDrive(bars)
	if lp.Pos.Scaled {
		t.Fatal("I7: qty 5 (leg 1 present) must not latch Scaled")
	}

	// The broker now shows only the runner's 2 contracts — leg 1 is gone.
	qty["long"] = 2
	at.mentorExitDrive(bars)
	if !lp.Pos.Scaled || lp.Pos.Leg1ExitedAtMs != T {
		t.Fatalf("I7: qty 2 (leg 1 gone) must latch Scaled at now: %+v", lp.Pos)
	}
}

// I10: a partial fill ≤ leg 1's quantity registers leg 1 ONLY, with leg 1's own
// 1:1 target (not the trade target). MUTANT: keep the single-leg branch → TP =
// the far target → RED.
func TestMentorI10PartialFillAtMostLeg1RegistersLeg1Only(t *testing.T) {
	at, _ := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	old := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, sid string) (nttrader.SentSplit, bool) {
		if sid == "sig-part" {
			return nttrader.SentSplit{Leg1Qty: 3, Leg1TP: 12}, true
		}
		return nttrader.SentSplit{}, false
	}
	t.Cleanup(func() { mentorSentSplit = old })
	row := store.ArmedOrderDB{ID: 9, SignalID: "sig-part", Side: "long", StopPx: 8, TargetPx: 16, FillQuantity: 2, Condition: "PHL"}
	at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-part", Account: "Sim101", FillPrice: 10, Quantity: 2})
	lp := at.mentorLivePos["sig-part"]
	if lp == nil {
		t.Fatal("not registered")
	}
	if lp.Legs[0].Qty != 2 || lp.Legs[0].Wire != 1 || lp.Legs[0].Final || lp.Legs[0].TP != 12 {
		t.Fatalf("I10: leg 1 = %+v, want 2 lots, wire leg 1, not the runner, TP 12 (the 1:1 target, not the trade target)", lp.Legs[0])
	}
	if lp.Legs[1].SignalID != "" || lp.Legs[1].Qty != 0 {
		t.Fatalf("I10: the runner must be empty for a partial ≤ leg 1: %+v", lp.Legs[1])
	}
	if lp.Pos.Leg1 != 2 || lp.Pos.Leg2 != 0 || lp.Pos.Leg1TP != 12 {
		t.Fatalf("I10: Pos leg fields = leg1 %d leg2 %d leg1tp %.2f, want 2/0/12", lp.Pos.Leg1, lp.Pos.Leg2, lp.Pos.Leg1TP)
	}
}

// I4: the B3 day-stop / never-add sweep cancels a PARTIALLY filled arm's
// remainder through mentorCancelArm, which must register the FILLED part in the
// exit drive (the B1 register-if-absent path). MUTANT: no registration in
// mentorCancelArm → no live position → RED.
func TestMentorI4CancelArmRegistersPartialFill(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "isb-i4",
		Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "ISB",
		State: store.StateWorking, SignalID: "i4-can-sig", FillPrice: 29600, FillQuantity: 2,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5)}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	mentorRegisterLiveArm("i4-arm", row.ID, "long", 29600)
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.CancelArm, ArmID: "i4-arm", Reason: "never-add sweep"}, mentorTierInputs{}, 1000, 1100)
	lp := at.mentorLivePos["i4-can-sig"]
	if lp == nil {
		t.Fatal("I4: the partial fill must register in the exit drive on cancel")
	}
	if lp.Pos.Contracts != 2 || lp.Pos.Leg1 != 2 {
		t.Fatalf("I4: contracts = %d / leg1 %d, want 2/2 (the FILLED part)", lp.Pos.Contracts, lp.Pos.Leg1)
	}
}

// I9 (U3): mentorUnregisterLivePos forgets the split record ONLY when the armed
// row is TERMINAL. While the row is still working its REMAINDER may still be at
// the broker, so the forget is skipped. MUTANT: forget unconditionally → the
// working row's split record is deleted → RED.
func TestMentorI9ForgetSignalMapsSkipsWhileWorking(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildSplitLegs)
	nt := at.armedTrader()
	sid, err := nt.PlaceStopEntry("MNQ", "long", 5, 100, 98, 106, 3, 102.13)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if _, ok := nt.SplitSentFor(sid); !ok {
		t.Fatal("fixture: the split record must be recorded")
	}
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "i9",
		Side: "long", EntryPx: 100, StopPx: 98, TargetPx: 106, Kind: "stop_entry", Condition: "ISB",
		State: store.StateWorking, SignalID: sid, Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5)}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	// Register + unregister while the row is WORKING → the split record survives.
	at.mentorRegisterLivePos(sid, &mentorLivePos{Pos: mentorPosition{Symbol: "MNQ", Side: "long"}})
	at.mentorUnregisterLivePos(sid)
	if _, ok := nt.SplitSentFor(sid); !ok {
		t.Fatal("I9: the split record must survive while the armed row is still working")
	}
	// The row goes TERMINAL → a later unregister forgets.
	if err := ledger.DB().Model(&store.ArmedOrderDB{}).Where("signal_id = ?", sid).Update("state", store.StateCancelled).Error; err != nil {
		t.Fatal(err)
	}
	at.mentorRegisterLivePos(sid, &mentorLivePos{Pos: mentorPosition{Symbol: "MNQ", Side: "long"}})
	at.mentorUnregisterLivePos(sid)
	if _, ok := nt.SplitSentFor(sid); ok {
		t.Fatal("I9: the split record must be forgotten once the row is terminal")
	}
}
