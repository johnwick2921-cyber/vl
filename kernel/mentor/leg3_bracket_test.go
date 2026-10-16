package mentor

import "testing"

// ── release #31 (leg-3 bracket) — runnerPartialLine ─────────────────────────

// TestRunnerPartialLineFirstLineBetweenLeg1AndTarget: the runner's first partial
// line is the nearest target line STRICTLY between the 1:1 leg-1 exit and the
// trade target. Entry 100 / stop 98 → leg 1 at 102; target 106; the line 104
// sits between them. Mutant: return the trade target → 106 → RED.
func TestRunnerPartialLineFirstLineBetweenLeg1AndTarget(t *testing.T) {
	levels := []Level{
		{Key: "k1", Kind: KindKeyLevel, Price: 104},
		{Key: "ema", Kind: KindEMA34, Price: 106},
	}
	if got := runnerPartialLine(levels, 100, 98, 106, SideLong); got != 104 {
		t.Fatalf("runnerPartialLine = %v, want 104 (the first line between leg 1 and the target)", got)
	}
}

// TestRunnerPartialLineShortSide: mirror for the short — entry 100 / stop 102 →
// leg 1 at 98; target 94; the line 96 sits between them.
func TestRunnerPartialLineShortSide(t *testing.T) {
	levels := []Level{
		{Key: "k1", Kind: KindKeyLevel, Price: 96},
		{Key: "ema", Kind: KindEMA34, Price: 94},
	}
	if got := runnerPartialLine(levels, 100, 102, 94, SideShort); got != 96 {
		t.Fatalf("runnerPartialLine = %v, want 96 (the first line between leg 1 and the target)", got)
	}
}

// TestRunnerPartialLineNoneWhenOnlyTheTargetIsBeyondLeg1: when the only line
// beyond the leg-1 exit IS the trade target, there is no partial line → 0 (the
// runner rides to the target as one pair). Mutant: return the target → RED.
func TestRunnerPartialLineNoneWhenOnlyTheTargetIsBeyondLeg1(t *testing.T) {
	levels := []Level{{Key: "k1", Kind: KindKeyLevel, Price: 106}}
	if got := runnerPartialLine(levels, 100, 98, 106, SideLong); got != 0 {
		t.Fatalf("runnerPartialLine = %v, want 0 (the target itself is not a partial line)", got)
	}
}

// TestRunnerPartialLineNoneWithNoLevels: an empty ladder has no partial line.
func TestRunnerPartialLineNoneWithNoLevels(t *testing.T) {
	if got := runnerPartialLine(nil, 100, 98, 106, SideLong); got != 0 {
		t.Fatalf("runnerPartialLine = %v, want 0", got)
	}
}
