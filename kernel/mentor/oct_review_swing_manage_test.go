package mentor

import (
	"testing"

	"vl/market"
)

// Review fix (livepath P2): CTO ruling A1 #3 - with the SWING 4H setup OFF (the October default,
// Oct D4.3-R10 @07:58) an ALREADY-OPEN swing leg or resting swing order must still be managed:
// exit / BE / cancel intents are emitted, only NEW PlaceStopEntry arms are dropped. These pins run
// the DEFAULT config through runSwing (the production call site) with State.Swing.Pending / .Pos set.
// oneMinute expands 5m test bars to the 1m feed the evaluator consumes (runSwing aggregates 1m -> 5m).
func oneMinute(five []market.Kline) []market.Kline {
	var out []market.Kline
	mx := func(a, b float64) float64 {
		if a > b {
			return a
		}
		return b
	}
	mn := func(a, b float64) float64 {
		if a < b {
			return a
		}
		return b
	}
	for _, b := range five {
		mk := func(i int, o, h, l, c float64) market.Kline {
			return market.Kline{OpenTime: b.OpenTime + int64(i)*60_000, CloseTime: b.OpenTime + int64(i)*60_000 + 59_999, Open: o, High: h, Low: l, Close: c}
		}
		out = append(out,
			mk(0, b.Open, b.Open, b.Open, b.Open),
			mk(1, b.Open, b.High, b.Open, b.High),
			mk(2, b.High, b.High, b.Low, b.Low),
			mk(3, b.Low, b.Low, b.Low, b.Low),
			mk(4, b.Low, mx(b.Low, b.Close), mn(b.Low, b.Close), b.Close))
	}
	return out
}

func TestOctReviewSwingOffStillCancelsRestingOrder(t *testing.T) {
	s, entryBars, armID, _ := swingR31Entry(t)
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	if e.Cfg.Swing4HEnabled {
		t.Fatal("swing 4h must be OFF by default")
	}
	e.State.Swing = *s
	// a 5m close THROUGH the line (R31): the resting order must be cancelled even with the setup OFF.
	through := append(append([]market.Kline{}, entryBars...), mk5m(t, 15, 5, 10, 10170, 10200, 10170, 10200))
	m1 := oneMinute(through)
	out := runSwing(e, m1, m1[len(m1)-1].CloseTime+1)
	cancelled := false
	for _, in := range out {
		if in.Action == CancelArm && in.ArmID == armID {
			cancelled = true
		}
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			t.Fatalf("a NEW swing entry leaked with the setup OFF: %+v", in)
		}
	}
	if !cancelled {
		t.Fatalf("OFF setup must still CancelArm(%s) on a close through the line; got %+v", armID, out)
	}
}

func TestOctReviewSwingOffStillManagesOpenPosition(t *testing.T) {
	s, entryBars, armID, _ := swingR31Entry(t)
	// fill: a bar trades down through the sell stop 10155 and closes at the low.
	fill := append(append([]market.Kline{}, entryBars...), mk5m(t, 15, 5, 10, 10156, 10156, 10100, 10150))
	SwingTick(s, fill, DefaultSwingCfg(), fill[len(fill)-1].OpenTime+60_000)
	if s.Pos == nil {
		t.Fatalf("setup: the swing must be filled; pending=%v", s.Pending)
	}
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	e.State.Swing = *s
	// +1R on a closed 5m: BE owed while the setup is OFF.
	risk := e.State.Swing.Pos.Risk
	px := e.State.Swing.Pos.Entry - risk - 1
	be := append(append([]market.Kline{}, fill...), mk5m(t, 15, 5, 15, 10150, 10150, px-1, px))
	m1 := oneMinute(be)
	out := runSwing(e, m1, m1[len(m1)-1].CloseTime+1)
	gotBE := false
	for _, in := range out {
		if in.Action == ActionMoveStopBE && in.ArmID == armID {
			gotBE = true
		}
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			t.Fatalf("a NEW swing entry leaked with the setup OFF: %+v", in)
		}
	}
	if !gotBE {
		t.Fatalf("OFF setup must still emit MoveStopBE for the open swing; got %+v (pos=%+v)", out, e.State.Swing.Pos)
	}
}

// A restart with the setup OFF: the rebuilt swing leg (RebuildSwingPos) is still managed under the
// DEFAULT config - the same BE-at-+1R as TestEvaluatorTickRebuiltSwingBreakEvenOnlyAtOneR, which runs
// under preOctCfg (Swing4HEnabled=true) and so could not see a regression that drops exits when OFF.
func TestOctReviewRebuiltSwingManagedWithSetupOff(t *testing.T) {
	tape, fill, oneR := swingRiskTape()
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	if e.Cfg.Swing4HEnabled {
		t.Fatal("swing 4h must be OFF by default")
	}
	refBar := tape[5]
	afterFill := append(append([]market.Kline{}, tape...), fill)
	e.State.Swing = swingWarmState(fourHBucketStart(refBar.OpenTime, ctime()), tape[6].OpenTime, nil)
	if !e.State.Swing.RebuildSwingPos(SwingRebuild{Side: SideShort, Entry: 930, EntryStop: 950, LiveStop: 950, Target: 890,
		EntryBucket: fourHBucketStart(refBar.OpenTime, ctime()), ArmID: "swing-rebuilt", MovedBE: false}) {
		t.Fatal("RebuildSwingPos refused a valid short swing")
	}
	atOneR := swingIntentsOf(e, append(afterFill, oneR))
	if n := countAction(atOneR, ActionMoveStopBE); n != 1 {
		t.Fatalf("setup OFF: the rebuilt swing must still break even at +1R: got %d (%+v)", n, atOneR)
	}
}
