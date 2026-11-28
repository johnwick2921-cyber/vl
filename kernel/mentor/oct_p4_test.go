package mentor

import "testing"

// P4-missesB D2.3-WE8 (Thu 29 Oct 2026 20:50-21:03). The reverse ISB is retagged PLH (D5.4 @03:52), and the
// mentor refuses a PLH whose old low stands inside 1R: "no room and no target, so there is no setup, you do
// not trade it" [Oct D2.3-R53 @54:00]; the early entry 28,839.75 with 9.25 pts to the near-old-low target is
// refused and the late one preferred [R55 @57:00]. The bot's own numbers at 20:56: entry 28,839.75, stop
// 28,854.00 (14.25), old low 28,825.00 (14.75 away, 9.75 to the shy target 28,830.00).
func TestOctP4WE8ReverseISBOldLowInsideOneR(t *testing.T) {
	e := &Evaluator{Cfg: DefaultConfig()}
	if !e.Cfg.ISBRevOldExtremeRoom {
		t.Fatal("default ISBRevOldExtremeRoom must be true (October)")
	}
	short := Intent{Side: SideShort, Price: 28839.75, Stop: 28854.00}
	oldLow := []Level{{Key: "old_extreme:28825.00", Kind: KindOldExtreme, Price: 28825.00, Role: OldLow}}
	if got, fc := e.isbRevTarget(oldLow, short); got != 0 || fc {
		t.Fatalf("WE8: old low inside 1R of the shy target: target %.2f fillClose %v, want 0 false (refused)", got, fc)
	}
	// The same geometry beside a box edge keeps the r1 fill-close (D5.3-10..13 trades).
	edge := []Level{{Key: "ftgl_edge:28832.00", Kind: KindFTGLEdge, Price: 28832.00}}
	if got, fc := e.isbRevTarget(edge, short); got != 28825.50 || !fc {
		t.Fatalf("box edge inside 1R: target %.2f fillClose %v, want 28825.50 true (fill-close)", got, fc)
	}
	// An old low with real room stays the target.
	far := []Level{{Key: "old_extreme:28800.00", Kind: KindOldExtreme, Price: 28800.00, Role: OldLow}}
	if got, fc := e.isbRevTarget(far, short); got != 28800.00 || fc {
		t.Fatalf("old low with room: target %.2f fillClose %v, want 28800.00 false", got, fc)
	}
	// Revert.
	e.Cfg.ISBRevOldExtremeRoom = false
	if got, fc := e.isbRevTarget(oldLow, short); got != 28825.00 || fc {
		t.Fatalf("revert: target %.2f fillClose %v, want the r1 target 28825.00 (armed, no shy measure)", got, fc)
	}
}
