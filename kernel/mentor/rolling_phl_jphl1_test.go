package mentor

import (
	"math"
	"testing"
)

// J-PHL-1 (full re-read 11-16, P0): the rolling PHL/PLH ladder took its SIDE
// from the 4h trigger line (rollingTrendSide returned h.FourH.Dir whenever the
// 4h/1h gate was off — the October default since #70), and with no 4h line it
// had no side at all, so the ladder never ran. October has no 4h/1h direction
// line: "Là khung 5 phút only" (the 5m frame only) [Oct D4.3 @07:26-07:35],
// the 1h line on the slide is skipped ("2 cái dòng này bỏ qua" [Oct D4.3
// @01:38-01:47]), "khung 5 phút nó kêu mình làm cái gì mình làm cái đó" (do
// what the 5m says) [Oct D4.3 @10:36-11:33]. Live 11-16 19:14: 4h short, 5m
// long — every rolling rung of the 19:12-19:20 rally was on the wrong side.
//
// Default (mentor_htf_gate_enabled=false): the side is the 5m trigger line's.
// Revert (mentor_htf_gate_enabled=true): the old HTF side (gate verdict, the
// 4h alone when the gate is off at that minute).
//
// These pins drive the REAL Evaluator.Tick on rollingFixture() (the production
// call site of rollingPHLTick). The fixture's newest closed candle (09:05, O 117
// C 113) is a RED pullback candle of an up leg from the old high 130.

// jphl1Fixture is rollingFixture with the HTF knob, the 4h line and the 5m line
// set; both lines are pinned (LastBucket max) so the fixture's own bars never
// move them.
func jphl1Fixture(htfGate bool, fourH, fiveM Side) (*Evaluator, int64, func() int) {
	e, bars, now := rollingFixture()
	e.Cfg.HTFGateEnabled = htfGate
	e.Cfg.LocTriggerFilter = true // the default: the rung must also sit on the 5m line's side (6b)
	e.State.HTF = HTF{}
	if fourH != "" {
		px := 140.0 // a short 4h line above price
		if fourH == SideLong {
			px = 90
		}
		e.State.HTF.FourH = TriggerLine{Dir: fourH, Price: px, LastBucket: math.MaxInt64}
	}
	e.State.HTF.OneH = TriggerLine{LastBucket: math.MaxInt64}
	e.State.Trigger = TriggerLine{LastBucket: math.MaxInt64}
	if fiveM != "" {
		px := 140.0
		if fiveM == SideLong {
			px = 90
		}
		e.State.Trigger = TriggerLine{Dir: fiveM, Price: px, LastBucket: math.MaxInt64}
	}
	return e, now, func() int { return len(rollingPlaceEntries(e.Tick(bars, now))) }
}

// The 11-16 19:14 state: 4h short, 5m long. Default: the red pullback candle of
// the 5m up leg starts the LONG ladder and its rung arms (a buy stop over its
// high). Before J-PHL-1 the side was the 4h's (short), a red candle is not a
// short pullback, and nothing armed.
func TestJPHL1_4hShort5mLong_RollingLongArms(t *testing.T) {
	e, _, tick := jphl1Fixture(false, SideShort, SideLong)
	if n := tick(); n != 1 {
		t.Fatalf("4h short + 5m long, default: want the long rolling rung armed (side = the 5m [Oct D4.3 @07:26-07:35, @10:36-11:33]), got %d; ladder=%+v refusals=%v", n, e.State.RollingPHL, e.State.Refusals)
	}
	if e.State.RollingPHL.Side != SideLong {
		t.Fatalf("the ladder side is %q, want long (the 5m trigger's side)", e.State.RollingPHL.Side)
	}
}

// No 4h line at all: the ladder still runs on the 5m side. Before J-PHL-1, no
// 4h line = no side = the ladder was cancelled on every tick.
func TestJPHL1_No4hLine_LadderStillRuns(t *testing.T) {
	e, _, tick := jphl1Fixture(false, "", SideLong)
	if n := tick(); n != 1 {
		t.Fatalf("no 4h line + 5m long, default: want the rolling long rung armed, got %d; ladder=%+v refusals=%v", n, e.State.RollingPHL, e.State.Refusals)
	}
}

// The 5m line is the side even when the 4h AGREES with the pullback's opposite:
// 4h long, 5m short — the red candle of this tape is a with-trend candle of a
// 5m down leg, not a pullback, so no long rung arms (the 4h no longer lends its
// side).
func TestJPHL1_4hLong5mShort_NoLongRung(t *testing.T) {
	e, _, tick := jphl1Fixture(false, SideLong, SideShort)
	if n := tick(); n != 0 {
		t.Fatalf("4h long + 5m short, default: no long rung may arm (the 5m is short), got %d; ladder=%+v", n, e.State.RollingPHL)
	}
	if e.State.RollingPHL.Side == SideLong {
		t.Fatalf("a long ladder runs under a 5m SELL line")
	}
}

// No 5m line yet: no side, no ladder (fail-closed — the 5m is the only big
// frame, and before its first break it says nothing).
func TestJPHL1_No5mLine_NoLadder(t *testing.T) {
	e, _, tick := jphl1Fixture(false, SideLong, "")
	if n := tick(); n != 0 {
		t.Fatalf("no 5m line, default: the rolling ladder must not run on the 4h alone, got %d armed; ladder=%+v", n, e.State.RollingPHL)
	}
}

// Revert (mentor_htf_gate_enabled=true): the old HTF side comes back exactly —
// 4h short, 5m long: the side is short, a red candle is no short pullback,
// nothing arms; no 4h line: no side, nothing arms.
func TestJPHL1_Revert_HTFGateEnabled_4hSide(t *testing.T) {
	e, _, tick := jphl1Fixture(true, SideShort, SideLong)
	if n := tick(); n != 0 {
		t.Fatalf("revert, 4h short + 5m long: the old 4h side (short) must hold, got %d armed; ladder=%+v", n, e.State.RollingPHL)
	}
	if e.State.RollingPHL.Side == SideLong {
		t.Fatalf("revert: a long ladder runs although the 4h is short")
	}
	e2, _, tick2 := jphl1Fixture(true, "", SideLong)
	if n := tick2(); n != 0 {
		t.Fatalf("revert, no 4h line: the old code had no side and no ladder, got %d armed", n)
	}
	// and the revert still trades WITH the 4h when it agrees (the pre-J-PHL-1 control)
	e3, _, tick3 := jphl1Fixture(true, SideLong, SideLong)
	if n := tick3(); n != 1 {
		t.Fatalf("revert control, 4h long + 5m long: want the long rung armed, got %d; refusals=%v", n, e3.State.Refusals)
	}
	_ = e2
}
