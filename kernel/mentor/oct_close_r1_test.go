package mentor

import "testing"

// oct-exact close r1: the reverse ISB takes the ISB first target (+1R, the fill-candle
// close partial) when no level gives it room [Oct D5.3 @16:43 OctD5.3-R39 "it is also
// our setup, we only read another name"; Oct D2.2 @28:37-28:56]. Scorecard rows it moves:
// D5.3-10/11/12/13 (reverse ISB at the EMA 9, short/long) and D2.3 WE4/WE5/WE6.
func TestOctCloseR1ReverseISBFillClose(t *testing.T) {
	e := &Evaluator{Cfg: DefaultConfig()}
	if !e.Cfg.ISBRevFillClose {
		t.Fatal("default ISBRevFillClose must be true (October)")
	}
	short := Intent{Side: SideShort, Price: 28334.50, Stop: 28350.00}
	// No level beyond at all: first target = entry - 1R.
	if got, fc := e.isbRevTarget(nil, short); got != 28319.00 || !fc {
		t.Fatalf("short, no level: target %.2f fillClose %v, want 28319.00 true", got, fc)
	}
	long := Intent{Side: SideLong, Price: 29644.75, Stop: 29632.25}
	if got, fc := e.isbRevTarget(nil, long); got != 29657.25 || !fc {
		t.Fatalf("long, no level: target %.2f fillClose %v, want 29657.25 true", got, fc)
	}
	// A level inside 1R (floor fails) takes the same first target.
	near := []Level{{Key: "old_extreme:28330.00", Kind: KindOldExtreme, Price: 28330.00, Role: OldLow}}
	if got, fc := e.isbRevTarget(near, short); got != 28319.00 || !fc {
		t.Fatalf("short, level inside 1R: target %.2f fillClose %v, want 28319.00 true", got, fc)
	}
	// A level with room stays the target.
	far := []Level{{Key: "old_extreme:28300.00", Kind: KindOldExtreme, Price: 28300.00, Role: OldLow}}
	if got, fc := e.isbRevTarget(far, short); got != 28300.00 || fc {
		t.Fatalf("short, level with room: target %.2f fillClose %v, want 28300.00 false", got, fc)
	}
	// Revert (mentor_isbrev_fill_close=false): the September level-or-refuse read.
	e.Cfg.ISBRevFillClose = false
	if got, fc := e.isbRevTarget(nil, short); got != 0 || fc {
		t.Fatalf("revert, no level: target %.2f fillClose %v, want 0 false (isbrev_missing_target)", got, fc)
	}
}
