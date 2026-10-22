package trader

import (
	"testing"
	"time"
)

// TestM283SingleBracketTrailAtTheExtreme pins m283 P2: the single-bracket /
// off-mode runner trail in mentorExitDrivePos must read the SAME candle
// extreme as mentorCandleTrailStop (AT the low/high — never one tick beyond).
// Before the fix the inline path computed trailL − tick / trailH + tick while
// mentorCandleTrailStop (the split runner's path) used AT, so the two paths
// disagreed by one tick.
//
// Mutant: re-add the tick offset (trailL − tick / trailH + tick) → the move
// lands at 12.25 instead of 12.50 → RED.
func TestM283SingleBracketTrailAtTheExtreme(t *testing.T) {
	at, moves := newDriveAT(t) // mentor_trail_mode=off → reaches the inline trail path
	T := int64(1_000_000)
	oldNow := getMentorNowSource()
	setMentorNowSource(func() time.Time { return time.UnixMilli(T) })
	t.Cleanup(func() { setMentorNowSource(oldNow) })

	lp := &mentorLivePos{
		Pos: mentorPosition{
			Symbol: "MNQ", Side: "long", Origin: "PHL",
			Entry: 10, Stop: 8, Target: 16, R: 2,
			Mode: "B", Scaled: true, ArmedBE: true,
		},
		// single bracket: leg [0] is the whole position, Wire 0, Final = the runner.
		Legs: [3]mentorLeg{{SignalID: "sig-m283", Qty: 1, Stop: 8, Final: true, Wire: 0}},
	}
	at.mentorRegisterLivePos("sig-m283", lp)

	// A closed candle whose low (12.50) is tighter than the 1:1 rule (2·13−16=10)
	// → the runner trails to AT the low.
	at.mentorExitDrivePos(nil, lp, 13, 13.5, 12.5, T+60_000)

	got := moves()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 trail move, got %+v", got)
	}
	if got[0].newStop != 12.50 {
		t.Fatalf("the single-bracket trail must sit AT the candle low 12.50 (never one tick beyond); got %.2f", got[0].newStop)
	}

	// The split runner's path (mentorCandleTrailStop) must agree with the same
	// candle: both helpers return the same price.
	if cand := mentorCandleTrailStop("long", 13.5, 12.5, at.mentorInstrumentTick()); cand != 12.50 {
		t.Fatalf("mentorCandleTrailStop = %.2f, want 12.50 — the two paths must agree", cand)
	}
}
