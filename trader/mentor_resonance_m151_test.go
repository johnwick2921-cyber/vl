package trader

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"vl/kernel/mentor"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── m151 RESONANCE PINS — all at the production path (mentorPlaceIntent /
// mentorExitDrivePos). The rule: a same-side ISB that passes every placement
// gate EXCEPT the never-add gate arms resonance (mode A, nothing placed).

// m151ShortPos registers an open PLH short position one candle after the fill.
func m151ShortPos(at *AutoTrader, signalID string) {
	at.mentorRegisterLivePos(signalID, &mentorLivePos{
		Pos:           mentorPosition{Symbol: "MNQ", Side: "short", Origin: "PLH", Entry: 29600, Stop: 29605, Target: 29590, R: 5, Mode: "B"},
		Legs:          [3]mentorLeg{{SignalID: signalID, Qty: 1, Stop: 29605, Final: true}},
		BarsSinceFill: 1,
	})
}

func m151ISB(side mentor.Side) mentor.Intent {
	return mentor.Intent{Action: mentor.PlaceStopLimitEntry, Setup: "ISB", Side: side,
		Price: 29600, Stop: func() float64 {
			if side == mentor.SideShort {
				return 29605
			}
			return 29595
		}(), Target: func() float64 {
			if side == mentor.SideShort {
				return 29590
			}
			return 29610
		}(), ExpiryMs: 1_800_000_000_000}
}

// captureMoves installs a stop-move capture seam and returns a count accessor.
func captureMoves(t *testing.T) func() []driveMove {
	t.Helper()
	var mu sync.Mutex
	var moves []driveMove
	old := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *nttrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
		mu.Lock()
		moves = append(moves, driveMove{signalID, side, newStop})
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorMoveStopForSignalWire = old })
	return func() []driveMove { mu.Lock(); defer mu.Unlock(); return append([]driveMove(nil), moves...) }
}

// ── (a) c6cdfdc3 replay: PLH short open, same-side ISB, STALE gate refusing → B.
func TestM151ResonanceStaleGateDoesNotArm(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	evening := eveningInstant()
	pinMentorClock(t, evening)
	pinBars(t, evening.UnixMilli()-10*60_000) // 10-minute-old bar → B4 stale
	at.mentorOpenSideSource = func() string { return "short" }
	t.Cleanup(func() { at.mentorOpenSideSource = nil })
	m151ShortPos(at, "sig-phl")
	moves := captureMoves(t)

	at.mentorPlaceIntent(m151ISB(mentor.SideShort), mentorSizeChoice{Contracts: 1, Tier: "base"}, evening.UnixMilli(), 1100)

	if lp := at.mentorLivePos["sig-phl"]; lp == nil || lp.Pos.Mode != "B" {
		t.Fatalf("a stale-refused ISB must NOT arm resonance: %+v", lp)
	}
	if n := len(moves()); n != 0 {
		t.Fatalf("a stale-refused ISB must not move a stop: %d moves", n)
	}
}

// ── (b) same-side ISB, every other gate passes, add gate refuses → mode A, nothing placed.
func TestM151ResonanceArmsAtAddGate(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	evening := eveningInstant()
	pinMentorClock(t, evening)
	pinBars(t, evening.UnixMilli())                                         // fresh
	mentorLatestPriceSource = func() (float64, bool) { return 29601, true } // above the short trigger: no-chase passes
	at.mentorOpenSideSource = func() string { return "short" }
	t.Cleanup(func() { at.mentorOpenSideSource = nil })
	m151ShortPos(at, "sig-phl")
	moves := captureMoves(t)

	at.mentorPlaceIntent(m151ISB(mentor.SideShort), mentorSizeChoice{Contracts: 1, Tier: "base"}, evening.UnixMilli(), 1100)

	if lp := at.mentorLivePos["sig-phl"]; lp == nil || lp.Pos.Mode != "A-resonance" {
		t.Fatalf("a same-side ISB past every gate but add must arm resonance: %+v", lp)
	}
	if n := len(moves()); n != 0 {
		t.Fatalf("arming must not move a stop: %d moves", n)
	}
	var n int64
	if err := ledger.DB().Model(&store.ArmedOrderDB{}).Where("origin = ? AND condition = 'ISB'", store.ArmOriginMentor).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the resonance ISB must NOT be placed: %d armed rows", n)
	}
}

// ── (c) opposite-side ISB → never resonance.
func TestM151ResonanceOppositeSideNeverArms(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	evening := eveningInstant()
	pinMentorClock(t, evening)
	pinBars(t, evening.UnixMilli()) // fresh
	at.mentorOpenSideSource = func() string { return "short" }
	t.Cleanup(func() { at.mentorOpenSideSource = nil })
	m151ShortPos(at, "sig-phl")
	moves := captureMoves(t)

	at.mentorPlaceIntent(m151ISB(mentor.SideLong), mentorSizeChoice{Contracts: 1, Tier: "base"}, evening.UnixMilli(), 1100)

	if lp := at.mentorLivePos["sig-phl"]; lp == nil || lp.Pos.Mode != "B" {
		t.Fatalf("an opposite-side ISB must never arm resonance: %+v", lp)
	}
	if n := len(moves()); n != 0 {
		t.Fatalf("an opposite-side ISB must not move a stop: %d moves", n)
	}
}

// ── (d) wire 2 ok + wire 3 refused → ArmedBE false; wire 3 retried next candle.
func TestM151ResonanceMultiLegRetriesUnapplied(t *testing.T) {
	at, _ := newDriveAT(t)
	p := &mentorLivePos{
		Pos: mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "A-resonance", Scaled: true},
		Legs: [3]mentorLeg{
			{SignalID: "s-1", Qty: 1, Stop: 9, Wire: 1, Exited: true}, // leg 1 already closed on its +1R TP
			{SignalID: "s-2", Qty: 1, Stop: 9, Wire: 2},
			{SignalID: "s-3", Qty: 1, Stop: 9, Wire: 3, Final: true},
		},
	}
	at.mentorRegisterLivePos("s-1", p)
	var mu sync.Mutex
	sent := map[int]int{}
	old := mentorMoveStopForSignalWire
	mentorMoveStopForSignalWire = func(_ *nttrader.TCPTrader, _ string, _ string, _ float64, leg int) error {
		mu.Lock()
		sent[leg]++
		mu.Unlock()
		if leg == 3 {
			return fmt.Errorf("broker refused")
		}
		return nil
	}
	t.Cleanup(func() { mentorMoveStopForSignalWire = old })

	at.mentorExitDrivePos(nil, p, 11, 12, 10, 0) // Scaled → no min distance
	if p.Pos.ArmedBE {
		t.Fatal("ArmedBE must stay false while wire 3 is unapplied")
	}
	if p.Legs[1].Stop != 10 {
		t.Fatalf("wire 2 must be applied: %.2f", p.Legs[1].Stop)
	}
	if p.Legs[2].Stop != 9 {
		t.Fatalf("wire 3 must keep the old stop: %.2f", p.Legs[2].Stop)
	}
	at.mentorExitDrivePos(nil, p, 11, 12, 10, 0) // next candle retries wire 3
	mu.Lock()
	n3 := sent[3]
	mu.Unlock()
	if n3 != 2 {
		t.Fatalf("wire 3 must be retried on the next candle: %d sends", n3)
	}
}

// ── (e) leg 1 (wire 1) moves to BE too.
func TestM151ResonanceMovesLeg1(t *testing.T) {
	at, moves := newDriveAT(t)
	p := &mentorLivePos{
		Pos: mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "A-resonance"},
		Legs: [3]mentorLeg{
			{SignalID: "s-1", Qty: 1, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 1, Stop: 9, Wire: 2, Final: true},
		},
	}
	at.mentorRegisterLivePos("s-1", p)
	at.mentorExitDrivePos(nil, p, 10.5, 11, 9.5, 0) // no partial; 10.5 >= entry + 0.25*2
	got := moves()
	if len(got) != 2 {
		t.Fatalf("both open legs must move to BE: %+v", got)
	}
	if got[0].newStop != 10 || got[1].newStop != 10 {
		t.Fatalf("both legs must move to entry 10: %+v", got)
	}
}

// ── (f) no partial: entry+1 tick → no move; entry+0.25R → move.
func TestM151ResonanceMinDistance(t *testing.T) {
	at, moves := newDriveAT(t)
	p := &mentorLivePos{
		Pos: mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 100, Stop: 90, Target: 130, R: 10, Mode: "A-resonance"},
		Legs: [3]mentorLeg{
			{SignalID: "s-1", Qty: 1, Stop: 90, Final: true},
		},
	}
	at.mentorRegisterLivePos("s-1", p)
	at.mentorExitDrivePos(nil, p, 100.25, 101, 99, 0) // entry + 1 tick (0.25)
	if n := len(moves()); n != 0 {
		t.Fatalf("entry+1 tick must not move BE: %d moves", n)
	}
	at.mentorExitDrivePos(nil, p, 102.5, 103, 101, 0) // entry + 0.25R = 102.5
	if n := len(moves()); n != 1 || moves()[0].newStop != 100 {
		t.Fatalf("entry+0.25R must move BE to 100: %+v", moves())
	}
	if !p.Pos.ArmedBE {
		t.Fatal("ArmedBE must be true after the 0.25R BE move")
	}

	// short side: entry 100, stop 110 → R 10, 0.25R below entry = 97.5.
	at2, moves2 := newDriveAT(t)
	ps := &mentorLivePos{
		Pos: mentorPosition{Symbol: "MNQ", Side: "short", Origin: "PLH", Entry: 100, Stop: 110, Target: 80, R: 10, Mode: "A-resonance"},
		Legs: [3]mentorLeg{
			{SignalID: "s-2", Qty: 1, Stop: 110, Final: true},
		},
	}
	at2.mentorRegisterLivePos("s-2", ps)
	at2.mentorExitDrivePos(nil, ps, 99.75, 101, 99, 0) // entry − 1 tick (0.25) = 99.75 → still above 97.5
	if n := len(moves2()); n != 0 {
		t.Fatalf("short entry−1 tick must not move BE: %d moves", n)
	}
	at2.mentorExitDrivePos(nil, ps, 97.5, 99, 96, 0) // entry − 0.25R = 97.5
	if n := len(moves2()); n != 1 || moves2()[0].newStop != 100 {
		t.Fatalf("short entry−0.25R must move BE to 100: %+v", moves2())
	}
}

// ── (g) restart after arm → mode A persisted on the row.
func TestM151ResonancePersistsModeA(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	row := store.ArmedOrderDB{
		TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR",
		Scenario: "phl-1", Side: "short", State: store.StateFilled, SignalID: "sig-phl",
		EntryPx: 29600, StopPx: 29605, TargetPx: 29590, Kind: "stop_entry", Condition: "PLH",
		Origin: store.ArmOriginMentor, ExitMode: mentorExitModePtr("B"), Contracts: store.IntPtr(1),
	}
	if err := ledger.UpsertArm(&row); err != nil {
		t.Fatal(err)
	}
	m151ShortPos(at, "sig-phl")
	at.mentorArmResonanceOnISB("short")
	var got store.ArmedOrderDB
	if err := ledger.DB().Where("signal_id = ?", "sig-phl").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ExitMode == nil || *got.ExitMode != "A-resonance" {
		t.Fatalf("mode A must be persisted for the restart rebuild: %v", got.ExitMode)
	}
}

// m151 (g) restart half: a fresh trader rebuilds mode A from the DB row (the
// persisted exit_mode), not from any in-memory staged branch.
func TestM151ResonanceRebuildRestoresAFromRow(t *testing.T) {
	at, st := f2Trader(t)
	sig := "aa07e583-0000-0000-0000-0000000000a1"
	arm := f2Arm(sig, "PHL", 2, 1, 29600, 29590, 29610, mentorExitModePtr("A-resonance"))
	if err := st.ArmedOrders().UpsertArm(arm); err != nil {
		t.Fatal(err)
	}
	if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 29600)); err != nil {
		t.Fatal(err)
	}
	f2PinBook(t, []ntwire.NT8Order{
		f2StopOrder(sig+"-sl", 29590, 1),
		f2StopOrder(sig+"-sl2", 29590, 1),
		f2TPOrder(sig+"-tp", 29610),
	}, true, 0)

	rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now())
	if !ran || rebuilt != 1 || refused != 0 {
		t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
	}
	lp := at.mentorLivePos[sig]
	if lp == nil || lp.Pos.Mode != "A-resonance" {
		t.Fatalf("a restart must rebuild mode A from the DB row: %+v", lp)
	}
}

// ── (h) a wrong-side market close fires once, not every candle.
func TestM151ResonanceWrongSideCloseOnce(t *testing.T) {
	at, _ := newDriveAT(t)
	oldPrice := mentorLatestPriceSeam
	mentorLatestPriceSeam = func(*AutoTrader) (float64, bool) { return 9.5, true } // price below BE 10 → sell stop through the market
	t.Cleanup(func() { mentorLatestPriceSeam = oldPrice })
	var mu sync.Mutex
	closes := 0
	oldClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error {
		mu.Lock()
		closes++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = oldClose })
	p := &mentorLivePos{
		Pos: mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "A-resonance", Scaled: true},
		Legs: [3]mentorLeg{
			{SignalID: "s-1", Qty: 1, Stop: 9, Final: true},
		},
	}
	at.mentorRegisterLivePos("s-1", p)
	at.mentorExitDrivePos(nil, p, 11, 12, 10, 0) // Scaled → BE move, stop 10 is through the market (price 9.5)
	at.mentorExitDrivePos(nil, p, 11, 12, 10, 0) // next candle must NOT close again
	mu.Lock()
	n := closes
	mu.Unlock()
	if n != 1 {
		t.Fatalf("the wrong-side close must fire once, not %d times", n)
	}
}

// m176 (Sonnet probe 8): a wrong-side BE close is a FULL FLATTEN — exactly ONE
// close per candle, never one per open leg. The mutant (continue past the
// through-market close instead of returning) sends N closes on an N-leg
// position and turns this RED.
func TestM176ResonanceWrongSideCloseOncePerPosition(t *testing.T) {
	for _, tc := range []struct {
		name string
		legs [3]mentorLeg
	}{
		{"2-leg", [3]mentorLeg{
			{SignalID: "s-1", Qty: 1, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 1, Stop: 9, Wire: 2, Final: true},
		}},
		{"3-leg", [3]mentorLeg{
			{SignalID: "s-1", Qty: 1, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 1, Stop: 9, Wire: 2},
			{SignalID: "s-3", Qty: 1, Stop: 9, Wire: 3, Final: true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := newDriveAT(t)
			oldPrice := mentorLatestPriceSeam
			mentorLatestPriceSeam = func(*AutoTrader) (float64, bool) { return 9.5, true } // below BE 10 → sell stop through the market
			t.Cleanup(func() { mentorLatestPriceSeam = oldPrice })
			var mu sync.Mutex
			closes := 0
			oldClose := mentorCloseLegAtMarketWire
			mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error {
				mu.Lock()
				closes++
				mu.Unlock()
				return nil
			}
			t.Cleanup(func() { mentorCloseLegAtMarketWire = oldClose })
			p := &mentorLivePos{
				Pos:  mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "A-resonance", Scaled: true},
				Legs: tc.legs,
			}
			at.mentorRegisterLivePos("s-1", p)
			at.mentorExitDrivePos(nil, p, 11, 12, 10, 0)
			mu.Lock()
			n := closes
			mu.Unlock()
			if n != 1 {
				t.Fatalf("a wrong-side BE must send exactly 1 close for a %s, got %d", tc.name, n)
			}
		})
	}
}
