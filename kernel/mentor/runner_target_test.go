package mentor

import "testing"

// CTO pin (release #4 gate on #360, item 5): the PHL emit site stamps the
// resonance runner's target BEYOND the old high — the nearest level past it
// [D2.4 p1 @01:56] — at the production call site Evaluator.Tick. Mutant: drop
// the eval.go RunnerTarget assignment → RED.
func TestPHLRunnerTargetIsTheNearestLevelBeyondTheOldHigh(t *testing.T) {
	oldHighs := []Level{
		{Key: "old-high:130", Kind: KindOldExtreme, Price: 130},
		{Key: "far:150", Kind: KindKeyLevel, Price: 150},
		{Key: "beyond:140", Kind: KindKeyLevel, Price: 140},
	}
	e, bars, now, _ := b14Fixture(oldHighs, 99.5)
	var got *Intent
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopEntry {
			in := in
			got = &in
		}
	}
	if got == nil {
		t.Fatalf("the higher-low PHL must emit; refusals %v", e.State.Refusals)
	}
	if got.Target != 130 {
		t.Fatalf("precondition: the trade target is the old high 130, got %.2f", got.Target)
	}
	if got.RunnerTarget != 140 {
		t.Fatalf("RunnerTarget = %.2f, want 140 (the nearest level beyond the old high 130)", got.RunnerTarget)
	}
}
