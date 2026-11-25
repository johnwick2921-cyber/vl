package trader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// exited-candle-pin (release #32, DS-105): the `exited [3]bool` leg mask must
// stop the exit drive from trailing / moving / re-selling a leg whose own TP
// receipt already closed it. The fixture registers a 3-wire-leg position (leg 1
// scaled at +1R, leg 2 exited at the runner-partial line, leg 3 the trailer);
// the mask is the ONLY thing that keeps the closed wire-2 leg out of the
// runner set, so a missing mask would double-exit it on the broker.

func hasWire(legs []int, w int) bool {
	for _, l := range legs {
		if l == w {
			return true
		}
	}
	return false
}

// exitPinAT builds the drive AutoTrader in the given trail mode and a fixed
// clock (HOME isolated per COMMON.md).
func exitPinAT(t *testing.T, mode string) (*AutoTrader, int64) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	at, _ := newDriveAT(t)
	at.config.StrategyConfig.RiskControl.MentorTrailMode = mode
	at.mentorExitModes = map[string]string{"long": "B"}
	T := int64(1_000_000)
	oldNow := getMentorNowSource()
	setMentorNowSource(func() time.Time { return time.UnixMilli(T) })
	t.Cleanup(func() { setMentorNowSource(oldNow) })
	return at, T
}

// exitPinRegister registers the 3/1/1 position and marks leg 1 scaled + wire 2
// exited through the PRODUCTION receipt marker (mentorMarkLeg1Scaled), the
// exact path the close-sync goroutine uses.
func exitPinRegister(t *testing.T, at *AutoTrader, sid string) *mentorLivePos {
	t.Helper()
	lp := g563Register(t, at, sid) // entry 10, stop 8 (R=2), leg1 TP 12, leg2 TP 14, target 16
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{SignalID: sid, PositionSide: "long", ExitReason: "tp", Leg: 1})
	if !lp.Pos.Scaled {
		t.Fatal("leg 1 TP receipt must mark Scaled")
	}
	at.mentorMarkLeg1Scaled(ntwire.PositionClosePayload{SignalID: sid, PositionSide: "long", ExitReason: "tp", Leg: 2})
	if !lp.Legs[1].Exited {
		t.Fatal("leg 2 TP receipt must mark the wire-2 leg Exited")
	}
	return lp
}

// candle mode (the live default): mentorRunnerLegs must drop the exited wire-2
// leg, so only the trailer (wire 3) goes to break-even.
func TestExitedCandleWire2ExitedOnlyRunnerMoves(t *testing.T) {
	at, T := exitPinAT(t, "candle")
	lp := exitPinRegister(t, at, "sig-exitpin-c1")
	// a closed candle past break-even (entry 10): close 13
	at.mentorExitDrivePos(nil, lp, 13, 13.5, 12.5, T+60_000)
	legs := capturedDriveLegs()
	if !hasWire(legs, 3) {
		t.Fatalf("the trailer (wire 3) must still go to BE; legs %v", legs)
	}
	if hasWire(legs, 2) {
		t.Fatalf("DEFECT: the exited wire-2 leg was moved; legs %v", legs)
	}
}

// off mode: the inline BE/trail loops read `|| exited[i]` directly, so the
// exited wire-2 leg must stay out of both passes.
func TestExitedOffModeWire2ExitedOnlyRunnerTrails(t *testing.T) {
	at, T := exitPinAT(t, "off")
	lp := exitPinRegister(t, at, "sig-exitpin-o1")
	// BE pass (covers half the distance to the target), then a trail pass.
	at.mentorExitDrivePos(nil, lp, 12.8, 13.2, 12.0, T-2)
	at.mentorExitDrivePos(nil, lp, 14, 14.5, 13.5, T+1)
	at.mentorExitDrivePos(nil, lp, 15, 15.5, 14.6, T+60_001)
	legs := capturedDriveLegs()
	if !hasWire(legs, 3) {
		t.Fatalf("the trailer (wire 3) must trail; legs %v", legs)
	}
	if hasWire(legs, 2) {
		t.Fatalf("DEFECT: the exited wire-2 leg was moved/trailed; legs %v", legs)
	}
}

// all runner legs exited → no order at all (mentorRunnerLegs returns empty).
func TestExitedCandleAllLegsExitedNoOrder(t *testing.T) {
	at, T := exitPinAT(t, "candle")
	lp := exitPinRegister(t, at, "sig-exitpin-a1")
	// The trailer's close is the whole-position close (no dedicated receipt
	// marker exists); set the flag the same field mentorExitedLegs reads.
	lp.Legs[2].Exited = true
	at.mentorExitDrivePos(nil, lp, 13, 13.5, 12.5, T+60_000)
	legs := capturedDriveLegs()
	if len(legs) != 0 {
		t.Fatalf("all legs exited must produce no order; legs %v", legs)
	}
}
