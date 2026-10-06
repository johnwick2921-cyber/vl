package trader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── BUILD-ALL B1 + B2 (owner "build all", 2026-10-05) ───────────────────────
//
// B1 (L2): a PART fill whose remainder is cancelled at expiry must still be
// handed to the mentor exit drive — the filled quantity is the FINAL position.
// B2 (L9): for a SPLIT, leg 1's Scaled is marked ONLY on the broker's
// position_close receipt of leg 1's TP, never on a candle-price cross (a single
// leg keeps the candle-priced 1:1 — see mentor_exit_drive_test.go).

// TestMentorPartFillExpiryHandsFilledQtyToExitDrive is the B1 call-site pin at
// the REAL expiry-sweep path (runArmedPlacementAtFiltered): a mentor arm that
// part-filled 2 of 5 and whose remainder is cancelled at expiry must register
// the FILLED 2 in the exit drive (a registration that used to happen only on
// the FULL fill, so a partial-then-expiry position got NO BE / 1:1 / trail).
// MUTANT: drop the B1 block in the expiry-cancel path → no live position → RED.
func TestMentorPartFillExpiryHandsFilledQtyToExitDrive(t *testing.T) {
	t.Setenv("STOP_ENTRY_SEAM", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := time.Now()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-b1", Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630,
		Kind: "stop_entry", Condition: "ISB",
		State: store.StateWorking, SignalID: "sig-b1", FillPrice: 29600, FillQuantity: 2,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
		ExpiryMs: now.UnixMilli() - 1, // already expired: the N12 sweep fires
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}

	at.runArmedPlacementAtFiltered(nil, 0, now, nil, isMentorArmOrigin)

	lp := at.mentorLivePos["sig-b1"]
	if lp == nil {
		t.Fatal("B1: the filled part must register in the exit drive at expiry")
	}
	if lp.Pos.Contracts != 2 || lp.Pos.Leg1 != 2 {
		t.Fatalf("B1: exit-drive contracts = %d / leg1 %d, want 2/2 (the FILLED part, not the signed 5)", lp.Pos.Contracts, lp.Pos.Leg1)
	}
}

// TestMentorSplitScaledOnlyOnBrokerTPReceipt is the B2 call-site pin: a SPLIT
// position whose candle crosses leg 1's TP does NOT mark Scaled (the runner's
// trail must not begin before the broker actually confirms leg 1 exited) — only
// the position_close receipt (Leg=1, ExitReason="tp") marks it. MUTANT: restore
// the candle-price Scaled in section (3) → Scaled true on the cross → RED.
func TestMentorSplitScaledOnlyOnBrokerTPReceipt(t *testing.T) {
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
	if lp == nil || lp.Legs[1].Qty != 2 || lp.Legs[1].SignalID == "" {
		t.Fatalf("split not registered: %+v", lp)
	}

	// BE candle: high 13.2 ≥ half (13) AND ≥ leg 1's TP (12). The candle arms BE
	// but must NOT mark Scaled for a split (the old candle-price guess is gone).
	at.mentorExitDrivePos(nil, lp, 12.8, 13.2, 12)
	if lp.Pos.Scaled {
		t.Fatal("B2: a candle-price cross of leg 1's TP must not mark Scaled for a split")
	}
	if !lp.Pos.ArmedBE {
		t.Fatal("fixture: the BE candle must arm BE")
	}

	// The broker's receipt of leg 1's TP is what marks Scaled.
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{SignalID: "sig-split", PositionSide: "long", ExitReason: "tp", Leg: 1})
	if !lp.Pos.Scaled {
		t.Fatal("B2: the broker's leg-1 TP receipt must mark Scaled")
	}

	// A non-leg-1 / non-TP receipt must NOT mark a position that is not yet scaled.
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{SignalID: "sig-split", PositionSide: "long", ExitReason: "sl", Leg: 1})
	// (already scaled — the guard returns; the pin is that a stop receipt never
	// un-scales or double-counts, so we only assert it stays scaled.)
	if !lp.Pos.Scaled {
		t.Fatal("B2: Scaled must stay marked after the TP receipt")
	}
}

// TestMentorPartFillExpiryDoesNotResetRegisteredPos is the B1 defensive-fold pin
// (CTO 2026-10-05): when the signal is ALREADY registered (any present or future
// path), the expiry partial-fill path must NOT overwrite it — the in-flight
// position's BE-armed / Scaled / trail state survives. MUTANT: make the B1 call
// register unconditionally → the pre-registered state is reset → RED.
func TestMentorPartFillExpiryDoesNotResetRegisteredPos(t *testing.T) {
	t.Setenv("STOP_ENTRY_SEAM", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	now := time.Now()
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "isb-b1r", Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630,
		Kind: "stop_entry", Condition: "ISB",
		State: store.StateWorking, SignalID: "sig-b1r", FillPrice: 29600, FillQuantity: 2,
		Origin: store.ArmOriginMentor, Contracts: store.IntPtr(5),
		ExpiryMs: now.UnixMilli() - 1,
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	// The signal is ALREADY live — BE armed, leg 1 scaled, 99 contracts — from
	// any earlier path. The expiry sweep must leave this state untouched.
	at.mentorRegisterLivePos("sig-b1r", &mentorLivePos{
		Pos: mentorPosition{
			Symbol: "MNQ", Side: "long", Entry: 29600, Stop: 29590, Target: 29630,
			R: 10, Contracts: 99, Leg1: 99, Leg1TP: 29630, ArmedBE: true, Scaled: true,
		},
		Legs: [2]mentorLeg{{SignalID: "sig-b1r", Qty: 99, TP: 29630, Stop: 29590, Final: true}, {}},
	})

	at.runArmedPlacementAtFiltered(nil, 0, now, nil, isMentorArmOrigin)

	lp := at.mentorLivePos["sig-b1r"]
	if lp == nil {
		t.Fatal("the pre-registered position must survive the expiry sweep")
	}
	if lp.Pos.Contracts != 99 || lp.Pos.Leg1 != 99 {
		t.Fatalf("B1 fold: contracts = %d / leg1 %d, want 99/99 (the expiry partial path must not overwrite the registered position)", lp.Pos.Contracts, lp.Pos.Leg1)
	}
	if !lp.Pos.ArmedBE || !lp.Pos.Scaled {
		t.Fatalf("B1 fold: ArmedBE=%v Scaled=%v, want both true (in-flight state must be preserved)", lp.Pos.ArmedBE, lp.Pos.Scaled)
	}
}
