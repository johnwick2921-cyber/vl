package trader

import (
	"testing"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// #47 P1 — the AddOn derives exit_reason from the ORDER NAME suffix
// (VLTraderTCPClient.cs:1845-1850), so a MOVED stop (break-even or trail) keeps
// "-sl". The fill PRICE decides whether a "sl" exit is a real stop-out loss:
// only an ORIGINAL-stop fill (ledger stop_px ± tick) on the losing side of
// entry books a loss. A break-even fill (≈ entry) or a trailed stop in profit
// drops the trade without a loss. MUTANT: restore the old "sl → wasStop=true"
// reading in mentorMarkLeg1Scaled (drop the fill-price guard) → the BE fill
// enqueues wasStop=true and the receipt pin goes RED.

// TestMentorStopFillIsLossBE is the pure pin: a break-even "sl" fill (exit ≈
// entry, NOT at the original stop) is NOT a loss.
func TestMentorStopFillIsLossBE(t *testing.T) {
	if mentorStopFillIsLoss("long", 100.0, 95.0, 100.0, 0.25) {
		t.Fatal("a break-even fill (exit ≈ entry) must not be a loss")
	}
	if mentorStopFillIsLoss("short", 100.0, 105.0, 100.0, 0.25) {
		t.Fatal("a break-even fill (exit ≈ entry) must not be a loss (short)")
	}
}

// TestMentorStopFillIsLossOriginalStop is the pure pin: an ORIGINAL-stop fill on
// the losing side of entry IS a loss.
func TestMentorStopFillIsLossOriginalStop(t *testing.T) {
	if !mentorStopFillIsLoss("long", 95.0, 95.0, 100.0, 0.25) {
		t.Fatal("a long fill at the original stop below entry must be a loss")
	}
	if !mentorStopFillIsLoss("short", 105.0, 105.0, 100.0, 0.25) {
		t.Fatal("a short fill at the original stop above entry must be a loss")
	}
}

// TestMentorStopFillIsLossTrailInProfit is the trail pin: a trailed stop fill on
// the WINNING side of entry is not a loss.
func TestMentorStopFillIsLossTrailInProfit(t *testing.T) {
	if mentorStopFillIsLoss("long", 110.0, 95.0, 100.0, 0.25) {
		t.Fatal("a long trail in profit must not be a loss")
	}
	if mentorStopFillIsLoss("short", 90.0, 105.0, 100.0, 0.25) {
		t.Fatal("a short trail in profit must not be a loss")
	}
}

// TestMentorMarkLeg1ScaledBEStopNoLoss is the call-site pin: a whole-position
// "sl" receipt whose fill is at break-even (not the original stop) enqueues
// wasStop=false — no loss booked.
func TestMentorMarkLeg1ScaledBEStopNoLoss(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.mentorExitCh = make(chan mentorExitReceipt, 8)
	ledger := st.ArmedOrders()
	if err := ledger.UpsertArm(&store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "phl-be", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110,
		Kind: "stop_entry", Condition: "PHL",
		State: store.StateWorking, SignalID: "sig-be", FillPrice: 100, FillQuantity: 1,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1),
	}); err != nil {
		t.Fatal(err)
	}

	// The broker exits at break-even: fill == entry, far from the original stop.
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{
		SignalID: "sig-be", PositionSide: "long", ExitReason: "sl", ExitPrice: 100, Leg: 0,
	})

	r, ok := mentorExitReceiptForTest(t, at)
	if !ok {
		t.Fatal("a flat BE stop exit must still enqueue (to drop the open trade)")
	}
	if r.signalID != "sig-be" || r.wasStop {
		t.Fatalf("a BE stop exit must enqueue wasStop=false, got %+v", r)
	}
}

// TestMentorMarkLeg1ScaledOriginalStopLoss is the call-site pin: a whole-position
// "sl" receipt whose fill is at the ORIGINAL stop enqueues wasStop=true — a loss
// IS booked.
func TestMentorMarkLeg1ScaledOriginalStopLoss(t *testing.T) {
	at, st := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.mentorExitCh = make(chan mentorExitReceipt, 8)
	ledger := st.ArmedOrders()
	if err := ledger.UpsertArm(&store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "phl-sl", Side: "long", EntryPx: 100, StopPx: 95, TargetPx: 110,
		Kind: "stop_entry", Condition: "PHL",
		State: store.StateWorking, SignalID: "sig-sl", FillPrice: 100, FillQuantity: 1,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(1),
	}); err != nil {
		t.Fatal(err)
	}

	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{
		SignalID: "sig-sl", PositionSide: "long", ExitReason: "sl", ExitPrice: 95, Leg: 0,
	})

	r, ok := mentorExitReceiptForTest(t, at)
	if !ok {
		t.Fatal("an original-stop exit must enqueue a receipt")
	}
	if r.signalID != "sig-sl" || !r.wasStop {
		t.Fatalf("an original-stop exit must enqueue wasStop=true, got %+v", r)
	}
}

// TestMentorLeg1StopWithRunnerOpenDefers is the P3 pin: a leg-1 "sl" receipt
// while the runner (leg 2/3) is still live must NOT enqueue anything — the
// trade is undecided and the runner's own receipt decides loss vs drop.
func TestMentorLeg1StopWithRunnerOpenDefers(t *testing.T) {
	at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}})
	at.mentorExitCh = make(chan mentorExitReceipt, 8)
	at.mentorLivePos = map[string]*mentorLivePos{
		"sig-split": {
			Legs: [3]mentorLeg{
				{SignalID: "sig-split", Qty: 3, Wire: 1, Final: false},
				{SignalID: "sig-split", Qty: 2, Wire: 2, Final: true},
			},
		},
	}

	// Leg 1's own stop fills at the original stop, but the runner is still open.
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{
		SignalID: "sig-split", PositionSide: "long", ExitReason: "sl", ExitPrice: 95, Leg: 1,
	})

	if r, ok := mentorExitReceiptForTest(t, at); ok {
		t.Fatalf("a leg-1 stop with the runner open must defer (no enqueue), got %+v", r)
	}
}

// mentorExitReceiptForTest drains one receipt non-blockingly (the tests never
// run the real evaluator loop, so the channel is read directly).
func mentorExitReceiptForTest(t *testing.T, at *AutoTrader) (mentorExitReceipt, bool) {
	t.Helper()
	select {
	case r := <-at.mentorExitCh:
		return r, true
	default:
		return mentorExitReceipt{}, false
	}
}
