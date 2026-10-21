package trader

import (
	"testing"

	nttrader "vl/trader/ninjatrader"
)

// ── m198 #43-B pins: a wrong-side stop move is a FULL FLATTEN — exactly ONE
// close per loop, never one per open leg. Each loop that drives
// mentorMoveLegStop must STOP on errMentorStopThroughMarket (the same class as
// the m176 resonance fix, now in the OTHER loops). The mutant — `continue` past
// the through-market close instead of returning — sends N closes on an N-leg
// position and turns the 3-leg case RED.

// m198CloseCounter stubs the fresh-price seam (through the market) and the close
// wire (counting). Returns the counter, read after the drive (single goroutine).
func m198CloseCounter(t *testing.T, seamPrice float64) *int {
	t.Helper()
	oldPrice := mentorLatestPriceSeam
	mentorLatestPriceSeam = func(*AutoTrader) (float64, bool) { return seamPrice, true }
	t.Cleanup(func() { mentorLatestPriceSeam = oldPrice })
	closes := 0
	oldClose := mentorCloseLegAtMarketWire
	mentorCloseLegAtMarketWire = func(*AutoTrader, *nttrader.TCPTrader, string, int) error {
		closes++
		return nil
	}
	t.Cleanup(func() { mentorCloseLegAtMarketWire = oldClose })
	return &closes
}

// m198SplitLegs returns 2-leg and 3-leg split shapes for the BE-half loop and
// mentorBEBeforePartialStep (both iterate ALL legs).
func m198SplitLegs() []struct {
	name string
	legs [3]mentorLeg
} {
	return []struct {
		name string
		legs [3]mentorLeg
	}{
		{"2-leg", [3]mentorLeg{
			{SignalID: "s-1", Qty: 2, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 2, Stop: 9, Wire: 2, Final: true},
		}},
		{"3-leg", [3]mentorLeg{
			{SignalID: "s-1", Qty: 2, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 1, Stop: 9, Wire: 2},
			{SignalID: "s-3", Qty: 1, Stop: 9, Wire: 3, Final: true},
		}},
	}
}

// TestM198BEHalfLoopStopsOnThroughMarket — the mode-B BE-half loop
// (mentor_exit_drive.go, trail mode "off") moves every open leg to the entry.
// A through-market BE must close the WHOLE symbol once, not per leg.
func TestM198BEHalfLoopStopsOnThroughMarket(t *testing.T) {
	for _, tc := range m198SplitLegs() {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := newDriveAT(t)             // mentor_trail_mode=off → the BE-half loop
			closes := m198CloseCounter(t, 9.5) // below entry 10 → long BE stop 10 is through the market
			p := &mentorLivePos{
				Pos:  mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "B"},
				Legs: tc.legs,
			}
			at.mentorRegisterLivePos("s-1", p)
			// high 13.5 ≥ entry+half (13) → armed; the fresh seam 9.5 is below the BE stop 10.
			at.mentorExitDrivePos(nil, p, 11, 13.5, 9.8, 0)
			if *closes != 1 {
				t.Fatalf("BE-half loop must send exactly 1 close for a %s, got %d", tc.name, *closes)
			}
		})
	}
}

// TestM198BEBeforePartialStopsOnThroughMarket — mentorBEBeforePartialStep
// (stage 0) iterates ALL legs. Drive it directly (its production caller is
// mentorExitDriveCandle): the entry-frame close is past BE (c=11) while the
// fresh 1m seam is through the market (9.5).
func TestM198BEBeforePartialStopsOnThroughMarket(t *testing.T) {
	for _, tc := range m198SplitLegs() {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := newDriveAT(t)
			closes := m198CloseCounter(t, 9.5)
			p := &mentorLivePos{
				Pos:  mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "B"},
				Legs: tc.legs,
			}
			at.mentorRegisterLivePos("s-1", p)
			at.mentorBEBeforePartialStep(nil, p, 11)
			if *closes != 1 {
				t.Fatalf("BE-before-partial must send exactly 1 close for a %s, got %d", tc.name, *closes)
			}
		})
	}
}

// m198RunnerLegs returns the split shapes for the BE-after-partial and
// candle-trail loops — those iterate the RUNNER legs only, so the 2-leg shape
// has a single runner (trivially one close) and the 3-leg shape has two runners
// (the mutant-catcher: two closes without the fix).
func m198RunnerLegs() []struct {
	name string
	legs [3]mentorLeg
} {
	return []struct {
		name string
		legs [3]mentorLeg
	}{
		{"2-leg", [3]mentorLeg{
			{SignalID: "s-1", Qty: 3, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 2, Stop: 10, Wire: 2, Final: true},
		}},
		{"3-leg", [3]mentorLeg{
			{SignalID: "s-1", Qty: 3, Stop: 9, Wire: 1},
			{SignalID: "s-2", Qty: 1, Stop: 10, Wire: 2},
			{SignalID: "s-3", Qty: 1, Stop: 10, Wire: 3, Final: true},
		}},
	}
}

// TestM198BEAfterPartialStopsOnThroughMarket — the BE-after-partial loop
// (mentorExitDriveCandle, ArmedBE=false) moves each RUNNER to the entry. A
// through-market BE must close once, not per runner.
func TestM198BEAfterPartialStopsOnThroughMarket(t *testing.T) {
	for _, tc := range m198RunnerLegs() {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := newDriveAT(t)
			closes := m198CloseCounter(t, 9.5)
			p := &mentorLivePos{
				Pos:  mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "B", Scaled: true, ArmedBE: false},
				Legs: tc.legs,
			}
			at.mentorRegisterLivePos("s-1", p)
			// scaledNow=true, ArmedBE=false → BE-after-partial; c=11 past BE 10.
			at.mentorExitDriveCandle(nil, p, 11, 12, 10, true, false, true, true, [3]bool{})
			if *closes != 1 {
				t.Fatalf("BE-after-partial must send exactly 1 close for a %s, got %d", tc.name, *closes)
			}
		})
	}
}

// TestM198CandleTrailStopsOnThroughMarket — the candle-trail loop
// (mentorExitDriveCandle, ArmedBE=true) moves each RUNNER to 1 tick beyond the
// closed candle. A through-market trail (the fresh 1m seam is below the new
// candle-trail stop) must close once, not per runner.
func TestM198CandleTrailStopsOnThroughMarket(t *testing.T) {
	for _, tc := range m198RunnerLegs() {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := newDriveAT(t)
			closes := m198CloseCounter(t, 9.5)
			p := &mentorLivePos{
				Pos:  mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 10, Stop: 9, Target: 16, R: 2, Mode: "B", Scaled: true, ArmedBE: true},
				Legs: tc.legs,
			}
			at.mentorRegisterLivePos("s-1", p)
			// scaledNow=true, ArmedBE=true, scaledBefore=true, trail=true →
			// candle-trail. cand = l−tick = 10.5−0.25 = 10.25 > leg stop 10
			// (tighter); the fresh seam 9.5 is below cand → through the market.
			at.mentorExitDriveCandle(nil, p, 11, 11.5, 10.5, true, true, true, true, [3]bool{})
			if *closes != 1 {
				t.Fatalf("candle-trail must send exactly 1 close for a %s, got %d", tc.name, *closes)
			}
		})
	}
}
