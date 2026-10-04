package trader

import (
	"strings"
	"sync"
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── MENTOR EXIT DRIVE (DS-107) — pins + mutants ─────────────────────────────
//
// The pure rules (mentorRR11Stop / mentorLegStopB) are pinned directly; the
// loop (mentorExitDrivePos / mentorExitDrive) is pinned at the per-leg signal
// move seam so "which signal moved to which stop" is asserted, not inferred.

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
	mentorMoveStopForSignalWire = func(nt *nttrader.TCPTrader, signalID, side string, newStop float64) error {
		mu.Lock()
		moves = append(moves, driveMove{signalID, side, newStop})
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		mentorMoveStopForSignalWire = old
		ResetMentorCountersForTest()
	})
	return at, func() []driveMove { mu.Lock(); defer mu.Unlock(); return append([]driveMove(nil), moves...) }
}

func bPos(mode, side string, entry, stop, target, leg1TP float64, leg1Qty, runnerQty int) *mentorLivePos {
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
			Leg1:    leg1Qty,
			Leg2:    runnerQty,
			Leg1TP:  leg1TP,
			ArmedBE: mode == "A-resonance",
		},
		Legs: [2]mentorLeg{
			{SignalID: "leg1", Qty: leg1Qty, TP: leg1TP, Stop: stop},
			{SignalID: "runner", Qty: runnerQty, TP: 0, Stop: stop},
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
	// long: 1:1 = 2·18−20 = 16; trail = low 15 → tighter is 16.
	if got := mentorLegStopB("long", 10, 18, 20, 15, true); got != 16 {
		t.Fatalf("long 1:1+trail → %v, want 16", got)
	}
	// long: trail low 17.5 beats 1:1 16 → 17.5 (tighter).
	if got := mentorLegStopB("long", 10, 18, 20, 17.5, true); got != 17.5 {
		t.Fatalf("long trail tighter → %v, want 17.5", got)
	}
	// short: 1:1 = 2·12−10 = 14; trail = high 15 → tighter is 14.
	if got := mentorLegStopB("short", 20, 12, 10, 15, true); got != 14 {
		t.Fatalf("short 1:1+trail → %v, want 14", got)
	}
	// no trail: the runner's trail is never applied when applyTrail is false.
	if got := mentorLegStopB("long", 10, 18, 20, 17.5, false); got != 16 {
		t.Fatalf("long no-trail → %v, want 16", got)
	}
}

// B BE: both legs' stops move to entry once price covers half the distance to
// the trade target.
func TestMentorExitDrivePosB_ArmsBEOnBothLegs(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 12, 2, 3) // half = (13−10)/2 = 1.5 → BE at 11.5
	// close 11.0 keeps the 1:1 rule at/below entry (2·11−12 = 10, 2·11−13 = 9),
	// so the candle arms BE only — the 1:1 tightening is pinned on later candles.
	at.mentorExitDrivePos(nil, p, 11.0, 11.8, 10.8)
	assertMoves(t, moves(), driveMove{"leg1", "long", 10}, driveMove{"runner", "long", 10})
	if !p.Pos.ArmedBE || p.Pos.Scaled {
		t.Fatalf("ArmedBE=%v Scaled=%v, want true/false", p.Pos.ArmedBE, p.Pos.Scaled)
	}
}

// The runner trails each closed candle AFTER leg 1's TP — on the next candle.
func TestMentorExitDrivePosB_RunnerTrailsAfterLeg1TP(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 10, 16, 12, 2, 3)
	p.Pos.ArmedBE = true
	p.Pos.Scaled = true
	p.Legs[0].Final = true // leg 1 already exited at its native TP
	// 1:1 = 2·14−16 = 12; trail = low 13.5 → 13.5.
	at.mentorExitDrivePos(nil, p, 14, 14.5, 13.5)
	assertMoves(t, moves(), driveMove{"runner", "long", 13.5})
}

// Mode C (confluence): the stop NEVER moves [D4.2 p1 @14:57].
func TestMentorExitDrivePosC_NeverMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("C", "long", 10, 9, 16, 12, 2, 3)
	at.mentorExitDrivePos(nil, p, 15, 16, 14)
	assertMoves(t, moves())
}

// Mode A (resonance): no trail, no 1:1 tightening [D2.4 p1].
func TestMentorExitDrivePosA_NoMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("A-resonance", "long", 10, 10, 16, 12, 2, 3)
	p.Pos.ArmedBE = true
	at.mentorExitDrivePos(nil, p, 15, 16, 14)
	assertMoves(t, moves())
}

// SWING: no moves — the swing rules own it.
func TestMentorExitDrivePosSwing_NoMoves(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("swing", "long", 10, 9, 0, 0, 2, 3)
	at.mentorExitDrivePos(nil, p, 15, 16, 14)
	assertMoves(t, moves())
}

// Mode D (spent day): the runner ≤ 2 is ASSERTED (counted), never reduced.
func TestMentorExitDrivePosSpentDay_AssertsRunnerOverCap(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 12, 2, 4) // runner 4 > cap 2
	p.SpentDay = true
	at.mentorExitDrivePos(nil, p, 11.0, 11.8, 10.8)
	if c := MentorCountSnapshot()["spent_day_runner_over_cap"]; c != 1 {
		t.Fatalf("spent_day_runner_over_cap = %d, want 1", c)
	}
	if p.Legs[1].Qty != 4 {
		t.Fatalf("runner qty changed to %d — the loop must never reduce", p.Legs[1].Qty)
	}
	// The BE still arms (stops still move; size is never touched).
	assertMoves(t, moves(), driveMove{"leg1", "long", 10}, driveMove{"runner", "long", 10})
}

// The loop never acts on a FORMING bar.
func TestMentorExitDriveRefusesFormingBar(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 13, 12, 2, 3)
	p.FillBarOpen = 0
	at.mentorRegisterLivePos("leg1", p)
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
	err := at.mentorMoveLegStop(nil, "long", &mentorLeg{SignalID: "leg1", Stop: 12}, 10)
	if err == nil || !strings.Contains(err.Error(), "widen") {
		t.Fatalf("widen move not refused: %v", err)
	}
	assertMoves(t, moves())
}

// A same-side ISB within 3 candles of a PHL/PLH fill arms mode A: both legs to
// BE now, and the leg-1 TP modify (out to the runner target) is LOGGED, unwired.
func TestMentorArmResonanceOnISB(t *testing.T) {
	at, moves := newDriveAT(t)
	p := bPos("B", "long", 10, 9, 16, 12, 2, 3)
	p.Pos.Origin = "PHL"
	p.BarsSinceFill = 1
	at.mentorRegisterLivePos("leg1", p)
	at.mentorArmResonanceOnISB("long")
	if p.Pos.Mode != "A-resonance" || !p.Pos.ArmedBE {
		t.Fatalf("resonance not armed: mode=%s armed=%v", p.Pos.Mode, p.Pos.ArmedBE)
	}
	assertMoves(t, moves(), driveMove{"leg1", "long", 10}, driveMove{"runner", "long", 10})
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
