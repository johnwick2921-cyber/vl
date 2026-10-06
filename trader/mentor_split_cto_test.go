package trader

import (
	"sync"
	"testing"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// driveLegs records the AddOn leg each captured move named (newDriveAT resets it).
var (
	driveLegsMu sync.Mutex
	driveLegs   []int
)

func capturedDriveLegs() []int {
	driveLegsMu.Lock()
	defer driveLegsMu.Unlock()
	return append([]int(nil), driveLegs...)
}

// ── REVIEW-SPLIT-2 Go folds (CTO 2026-10-04) ──────────────────────────────

// Spent day D: at most 2 contracts run after leg 1. The AddOn sizes the
// runner as fill − leg1_qty, so the frame's leg 1 must absorb what the cap
// takes off the runner. Mutant: return ceil(n/2) → n=7 spent sends a 3-lot runner.
func TestMentorLeg1ForFrameSpentDayCapsTheWireRunner(t *testing.T) {
	spent := mentor.Intent{Side: mentor.SideLong, Price: 100, Stop: 95, Target: 110, StopPts: 5, TargetPts: 10, SpentDay: true}
	normal := spent
	normal.SpentDay = false
	for _, c := range []struct {
		in      mentor.Intent
		n, want int
	}{
		{spent, 7, 5}, {spent, 6, 4}, {spent, 5, 3}, {spent, 4, 2}, {spent, 2, 1},
		{normal, 7, 4}, {normal, 6, 3}, {normal, 5, 3},
	} {
		got, _ := mentorLeg1ForFrame(c.in, c.n, "B", 0)
		if got != c.want {
			t.Errorf("spent=%v n=%d: leg1 = %d, want %d (runner %d)", c.in.SpentDay, c.n, got, c.want, c.n-c.want)
		}
	}
}

// The executor's leg 1 for the quantity actually SENT keeps the row's runner
// as a ceiling: the clamp to the trader max shrinks the legs, never grows the
// runner past the spent-day cap. Mutant: drop the rowRunner ceiling → RED.
func TestMentorWireLeg1KeepsTheRowRunnerCeiling(t *testing.T) {
	row := func(contracts, leg1 int) store.ArmedOrderDB {
		return store.ArmedOrderDB{Contracts: store.IntPtr(contracts), Leg1Qty: store.IntPtr(leg1)}
	}
	for _, c := range []struct {
		name string
		r    store.ArmedOrderDB
		sent int
		want int
	}{
		{"spent 7 sent 7", row(7, 5), 7, 5},
		{"spent 7 clamped to 6", row(7, 5), 6, 4},
		{"normal 5 sent 5", row(5, 3), 5, 3},
		{"normal 5 clamped to 3", row(5, 3), 3, 2},
		{"normal 5 clamped to 1 → single", row(5, 3), 1, 0},
		{"no split on the row", store.ArmedOrderDB{Contracts: store.IntPtr(5)}, 5, 0},
	} {
		if got := mentorWireLeg1(c.r, c.sent); got != c.want {
			t.Errorf("%s: mentorWireLeg1 = %d, want %d", c.name, got, c.want)
		}
	}
}

// The split the frame CARRIED registers as two legs under the one signal id,
// each move naming its AddOn leg: BE moves leg 1 (-sl) and leg 2 (-sl2)
// separately; after the 1:1 point the trail moves ONLY the runner (leg 2).
// Mutant: register single-leg regardless → no leg 2, legs [0] → RED.
func TestMentorSplitRegistersTwoLegsAndMovesEachByLeg(t *testing.T) {
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
	if lp == nil {
		t.Fatal("not registered")
	}
	if lp.Legs[0].Qty != 3 || lp.Legs[0].Wire != 1 || lp.Legs[0].Final || lp.Legs[0].TP != 12 {
		t.Fatalf("leg 1 = %+v, want 3 lots, wire leg 1, not the runner, TP 12", lp.Legs[0])
	}
	if lp.Legs[1].Qty != 2 || lp.Legs[1].Wire != 2 || !lp.Legs[1].Final || lp.Legs[1].TP != 16 || lp.Legs[1].SignalID != "sig-split" {
		t.Fatalf("runner = %+v, want 2 lots, wire leg 2, Final, TP 16, same signal", lp.Legs[1])
	}

	// R = 2, target 16 → half the distance = 3 → BE arms when high ≥ 13.
	at.mentorExitDrivePos(nil, lp, 12.8, 13.2, 12)
	assertMoves(t, moves(), driveMove{"sig-split", "long", 10}, driveMove{"sig-split", "long", 10})
	if got := capturedDriveLegs(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("BE legs = %v, want [1 2] (each OCO pair addressed by its own leg)", got)
	}
	// Next candle: leg 1's TP (12) already crossed on the BE candle → Scaled;
	// the 1:1 + trail runs on the RUNNER only.
	at.mentorExitDrivePos(nil, lp, 14, 14.5, 13.5)
	legs := capturedDriveLegs()
	for _, l := range legs[2:] {
		if l != 2 {
			t.Fatalf("after leg 1's TP only the runner (leg 2) may move, got legs %v", legs)
		}
	}
	if len(legs) < 3 {
		t.Fatalf("the runner never moved after the 1:1 point: legs %v", legs)
	}
}

// No split on the wire (old AddOn / leg1_tp refused / restart) → the single-leg
// view, and its move names no leg (0 = every live leg).
func TestMentorNoSentSplitRegistersSingleLegLegZero(t *testing.T) {
	at, moves := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	old := mentorSentSplit
	mentorSentSplit = func(*AutoTrader, string) (nttrader.SentSplit, bool) { return nttrader.SentSplit{}, false }
	t.Cleanup(func() { mentorSentSplit = old })
	row := store.ArmedOrderDB{ID: 8, SignalID: "sig-one", Side: "long", StopPx: 9, TargetPx: 13, FillQuantity: 5, Condition: "PHL", Leg1Qty: store.IntPtr(3), Leg1TP: 11}
	at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-one", FillPrice: 10, Quantity: 5})
	lp := at.mentorLivePos["sig-one"]
	if lp == nil || lp.Legs[1].Qty != 0 || lp.Legs[0].Wire != 0 || lp.Legs[0].Qty != 5 {
		t.Fatalf("no sent split must register the single leg (wire 0): %+v", lp)
	}
	at.mentorExitDrivePos(nil, lp, 11.0, 11.8, 10.8)
	assertMoves(t, moves(), driveMove{"sig-one", "long", 10})
	if got := capturedDriveLegs(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("single-leg move legs = %v, want [0]", got)
	}
}
