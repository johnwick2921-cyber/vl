package trader

import (
	"strings"
	"sync"
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── MENTOR EXIT DRIVE (DS-107) — pins + mutants ─────────────────────────────
//
// The pure rules (mentorRR11Stop / mentorLegStopB) are pinned directly; the
// loop (mentorExitDrivePos / mentorExitDrive) is pinned at the per-leg signal
// move seam so "which signal moved to which stop" is asserted, not inferred.
// dev's entry is ONE row → ONE position, so the live position is a SINGLE leg
// (Legs[0] = the whole position, Final = the runner).

type driveMove struct {
	signalID string
	side     string
	newStop  float64
}

// newDriveAT builds a minimal AutoTrader (mentor mode ON, a zero-value
// TCPTrader as the armed trader — the seam never touches it) and captures every
// signal-keyed move through mentorMoveStopForSignalWire.
func newDriveAT(t *testing.T) (*AutoTrader, func() []driveMove) {
	t.Helper()
	at := &AutoTrader{
		config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}},
		trader: &nttrader.TCPTrader{},
	}
	var mu sync.Mutex
	var moves []driveMove
	old := mentorMoveStopForSignalWire
	driveLegsMu.Lock()
	driveLegs = nil
	driveLegsMu.Unlock()
	mentorMoveStopForSignalWire = func(nt *nttrader.TCPTrader, signalID, side string, newStop float64, leg int) error {
		mu.Lock()
		moves = append(moves, driveMove{signalID, side, newStop})
		mu.Unlock()
		driveLegsMu.Lock()
		driveLegs = append(driveLegs, leg)
		driveLegsMu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		mentorMoveStopForSignalWire = old
		ResetMentorCountersForTest()
	})
	return at, func() []driveMove { mu.Lock(); defer mu.Unlock(); return append([]driveMove(nil), moves...) }
}

// bPos builds a SINGLE-leg live position: Legs[0] = the whole position (the
// runner, Final=true), Legs[1] empty — dev's one-row entry.
func bPos(mode, side string, entry, stop, target float64, qty int) *mentorLivePos {
	return &mentorLivePos{
		Pos: mentorPosition{
			Symbol:  "MNQ",
			Side:    side,
			Origin:  "PHL",
			Entry:   entry,
			Stop:    stop,
			Target:  target,
			R:       2,
			Mode:    mode,
			Leg1:    qty,
			Leg2:    0,
			Leg1TP:  target,
			ArmedBE: mode == "A-resonance",
		},
		Legs: [2]mentorLeg{
			{SignalID: "entry", Qty: qty, TP: target, Stop: stop, Final: true},
			{},
		},
	}
}

func assertMoves(t *testing.T, got []driveMove, want ...driveMove) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("moves = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("move[%d] = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
}

// pin (a): 10/20 long — close 15 → 10 (BE), 18 → 16, 16 after → stays 16;
// short mirror. [D1.2 p1 @11:36–16:07]
func TestMentorRR11Stop(t *testing.T) {
	if got := mentorRR11Stop("long", 10, 15, 20); got != 10 {
		t.Fatalf("long close 15 → %v, want 10 (BE)", got)
	}
	if got := mentorRR11Stop("long", 10, 18, 20); got != 16 {
		t.Fatalf("long close 18 → %v, want 16", got)
	}
	if got := mentorRR11Stop("long", 16, 16, 20); got != 16 {
		t.Fatalf("long close 16 after 16 → %v, want 16 (never widen)", got)
	}
	if got := mentorRR11Stop("short", 20, 15, 10); got != 20 {
		t.Fatalf("short close 15 → %v, want 20 (BE)", got)
	}
	if got := mentorRR11Stop("short", 20, 12, 10); got != 14 {
		t.Fatalf("short close 12 → %v, want 14", got)
	}
	if got := mentorRR11Stop("short", 14, 16, 10); got != 14 {
		t.Fatalf("short close 16 after 14 → %v, want 14 (never widen)", got)
	}
}

// The runner takes the TIGHTER of the 1:1 rule and the candle trail.
func TestMentorLegStopB_RunnerTrailTighter(t *testing.T) {
	if got := mentorLegStopB("long", 10, 18, 20, 15, true); got != 16 {
		t.Fatalf("long 1:1+trail → %v, want 16", got)
	}
	if got := mentorLegStopB("long", 10, 18, 20, 17.5, true); got != 17.5 {
		t.Fatalf("long trail tighter → %v, want 17.5", got)
	}
	if got := mentorLegStopB("short", 20, 12, 10, 15, true); got != 14 {
		t.Fatalf("short 1:1+trail → %v, want 14", got)
	}
	if got := mentorLegStopB("long", 10, 18, 20, 17.5, false); got != 16 {
		t.Fatalf("long no-trail → %v, want 16", got)
	}
}

// B BE on the single leg: the stop moves to entry once price covers half the
// distance to the trade target.
func TestMentorExitDrivePosB_ArmsBE(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 5)            // half = (13−10)/2 = 1.5 → BE at 11.5
	at.mentorExitDrivePos(nil, p, 11.0, 11.8, 10.8) // high 11.8 arms it
	assertMoves(t, moves(), driveMove{"entry", "long", 10})
	if !p.Pos.ArmedBE {
		t.Fatalf("ArmedBE = false, want true")
	}
}

// The single leg trails only AFTER the 1:1 point (entry + R) printed on a
// PRIOR candle (CTO gate, release #4: the course trails the runner after the
// 1:1 point), then takes the TIGHTER of the live 1:1 and the candle trail, the
// trail sitting 1 TICK beyond the candle's low (item 7 ruling [X8 @15:40–17:13]).
// Mutants: trail straight after BE (candle 1 moves to 11.75) → RED; trail at
// the low exactly (13.5) → RED.
func TestMentorExitDrivePosB_SingleLegTrails(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 10, 16, 5) // R = 2 → the 1:1 point is 12
	p.Pos.ArmedBE = true
	// Candle 1 prints the 1:1 point (high 13 ≥ 12): no trail yet; the live
	// 1:1 (2·12.5−16 = 9) is below the BE stop → no move.
	at.mentorExitDrivePos(nil, p, 12.5, 13, 12)
	assertMoves(t, moves())
	if !p.Pos.Scaled {
		t.Fatal("the 1:1 point printed — Scaled must be set")
	}
	// Candle 2: 1:1 = 2·14−16 = 12; trail = low 13.5 − 1 tick = 13.25 → 13.25.
	at.mentorExitDrivePos(nil, p, 14, 14.5, 13.5)
	assertMoves(t, moves(), driveMove{"entry", "long", 13.25})
}

// e2e pin (CTO 2026-10-04): ONE filled row registers the single-leg live
// position, and the next closed candle at half the distance to the trade target
// arms BE. The wiring is the point: fill callback → registerMentorLivePos →
// mentorLivePositions → exit-drive loop.
func TestMentorExitDriveE2E_SingleRowFillRegistersAndDrives(t *testing.T) {
	at, moves := newDriveAT(t)
	at.mentorExitModes = map[string]string{"long": "B"}
	row := store.ArmedOrderDB{ID: 1, SignalID: "sig-1", Side: "long", StopPx: 9, TargetPx: 13, FillQuantity: 5, Condition: "PHL"}
	u := ntwire.OrderUpdatePayload{State: "filled", SignalID: "sig-1", Account: "Sim101", FillPrice: 10, Quantity: 5}
	at.registerMentorLivePos(row, u)

	lp, ok := at.mentorLivePos["sig-1"]
	if !ok || lp == nil {
		t.Fatalf("live position not registered under the entry signal id")
	}
	if lp.Legs[0].Qty != 5 || !lp.Legs[0].Final || lp.Legs[1].SignalID != "" {
		t.Fatalf("single leg = whole position (Final runner, Legs[1] empty): %+v", lp.Legs)
	}
	at.mentorExitDrivePos(nil, lp, 11.0, 11.8, 10.8)
	assertMoves(t, moves(), driveMove{"sig-1", "long", 10})
	if !lp.Pos.ArmedBE {
		t.Fatalf("ArmedBE = false, want true")
	}
}

// Mode C (confluence): the stop NEVER moves [D4.2 p1 @14:57].
func TestMentorExitDrivePosC_NeverMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("C", "long", 10, 9, 16, 5)
	at.mentorExitDrivePos(nil, p, 15, 16, 14)
	assertMoves(t, moves())
}

// Mode A (resonance): no trail, no 1:1 tightening [D2.4 p1].
func TestMentorExitDrivePosA_NoMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("A-resonance", "long", 10, 10, 16, 5)
	p.Pos.ArmedBE = true
	at.mentorExitDrivePos(nil, p, 15, 16, 14)
	assertMoves(t, moves())
}

// SWING: no moves — the swing rules own it.
func TestMentorExitDrivePosSwing_NoMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("swing", "long", 10, 9, 0, 5)
	at.mentorExitDrivePos(nil, p, 15, 16, 14)
	assertMoves(t, moves())
}

// The loop never acts on a FORMING bar.
func TestMentorExitDriveRefusesFormingBar(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 5)
	p.FillBarOpen = 0
	at.mentorRegisterLivePos("sig-1", p)
	bars := []market.Kline{
		{OpenTime: 1, Close: 10, High: 10.5, Low: 9.5, Final: true},
		{OpenTime: 2, Close: 12, High: 12.5, Low: 11, Final: false}, // forming: would arm BE if driven
	}
	at.mentorExitDrive(bars)
	assertMoves(t, moves())
}

// The never-widen guard refuses a widening leg move before the wire.
func TestMentorMoveLegStop_NeverWidens(t *testing.T) {
	at, moves := newDriveAT(t)
	err := at.mentorMoveLegStop(nil, "long", &mentorLeg{SignalID: "sig-1", Stop: 12}, 10)
	if err == nil || !strings.Contains(err.Error(), "widen") {
		t.Fatalf("widen move not refused: %v", err)
	}
	assertMoves(t, moves())
}

// A same-side ISB within 3 candles of a PHL/PLH fill arms mode A: the single
// leg to BE now, and the leg-1 TP modify (out to the runner target) is LOGGED,
// unwired.
func TestMentorArmResonanceOnISB(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 16, 5)
	p.Pos.Origin = "PHL"
	p.BarsSinceFill = 1
	at.mentorRegisterLivePos("sig-1", p)
	at.mentorArmResonanceOnISB("long")
	if p.Pos.Mode != "A-resonance" || !p.Pos.ArmedBE {
		t.Fatalf("resonance not armed: mode=%s armed=%v", p.Pos.Mode, p.Pos.ArmedBE)
	}
	assertMoves(t, moves(), driveMove{"entry", "long", 10})
	snap := MentorCountSnapshot()
	if snap["resonance_armed"] != 1 || snap["modify_bracket_resonance_logged"] != 1 {
		t.Fatalf("resonance counters: armed=%d modify_logged=%d, want 1/1", snap["resonance_armed"], snap["modify_bracket_resonance_logged"])
	}
}

// The resonance trigger is an ISB entry intent (not a cancel/close action).
func TestIsISBEntryIntent(t *testing.T) {
	if !isISBEntryIntent(mentor.Intent{Setup: "ISB", Action: mentor.PlaceStopEntry}) {
		t.Fatal("ISB stop-entry is a resonance trigger")
	}
	if !isISBEntryIntent(mentor.Intent{Setup: "ISB", Action: mentor.PlaceStopLimitEntry}) {
		t.Fatal("ISB stop-limit-entry is a resonance trigger")
	}
	if isISBEntryIntent(mentor.Intent{Setup: "PHL", Action: mentor.PlaceStopEntry}) {
		t.Fatal("a PHL entry is not the ISB trigger")
	}
	if isISBEntryIntent(mentor.Intent{Setup: "ISB", Action: mentor.CancelArm}) {
		t.Fatal("a cancel is not the ISB trigger")
	}
}

// CTO gate (release #4): the PRODUCTION loop drops a position once the account
// reads flat on its side for two consecutive closed candles after the fill —
// nothing else ever unregistered it. A read error keeps it; an open side keeps
// it. Mutant: never unregister → RED.
func TestMentorExitDriveDropsAFlatPosition(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 1)
	at.mentorRegisterLivePos("entry", p)
	sides := map[string]bool{"long": true}
	readOK := true
	old := mentorDriveOpenSides
	mentorDriveOpenSides = func(*AutoTrader) (map[string]bool, bool) { return sides, readOK }
	t.Cleanup(func() { mentorDriveOpenSides = old })
	bar := func(i int) []market.Kline {
		return []market.Kline{{OpenTime: int64(i) * 60_000, Close: 10.2, High: 10.3, Low: 10.1, Final: true}}
	}
	at.mentorExitDrive(bar(1)) // open → kept
	if len(at.mentorLivePosList()) != 1 {
		t.Fatal("an open position must stay in the loop")
	}
	sides = map[string]bool{}
	readOK = false
	at.mentorExitDrive(bar(2)) // read error proves nothing → kept
	at.mentorExitDrive(bar(3))
	if len(at.mentorLivePosList()) != 1 {
		t.Fatal("a failed position read must never drop the position")
	}
	readOK = true
	at.mentorExitDrive(bar(4)) // flat read 1
	if len(at.mentorLivePosList()) != 1 {
		t.Fatal("one flat read is not enough (the fill may not be in the snapshot yet)")
	}
	at.mentorExitDrive(bar(5)) // flat read 2 → dropped
	if n := len(at.mentorLivePosList()); n != 0 {
		t.Fatalf("a position flat for two candles must leave the loop; %d left", n)
	}
	_ = moves
}

// CTO gate (release #4, canon 53): a FULL fill of a mentor-origin row, through
// the PRODUCTION order_update handler onArmedOrderUpdate, registers the live
// position the exit drive manages. Every other exit-drive pin registers by
// hand; without this one, dropping the fill-callback registration survived.
func TestMentorFullFillRegistersTheLivePositionAtTheFillHandler(t *testing.T) {
	at, _, ledger, _ := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	row := store.ArmedOrderDB{TraderID: at.id, PlanID: "mentor", Version: 1, Session: "MENTOR", Scenario: "lvl-3-0",
		Side: "long", EntryPx: 29600, StopPx: 29590, TargetPx: 29630, Kind: "stop_entry", Condition: "PHL",
		State: store.StateWorking, SignalID: "sig-fill-1", Origin: store.ArmOriginMentor, Contracts: store.IntPtr(2)}
	if err := ledger.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: "sig-fill-1", State: "filled", Quantity: 2, FillPrice: 29600, Account: "Sim101"}, ledger)
	got := at.mentorLivePosList()
	if len(got) != 1 {
		t.Fatalf("a full mentor fill must register ONE live position, got %d", len(got))
	}
	if got[0].Legs[0].SignalID != "sig-fill-1" || got[0].Legs[0].Qty != 2 || got[0].Pos.Entry != 29600 {
		t.Fatalf("registered leg = %+v entry %.2f, want sig-fill-1 ×2 @29600", got[0].Legs[0], got[0].Pos.Entry)
	}
}
