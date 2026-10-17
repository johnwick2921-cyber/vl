package trader

import (
	"sync"
	"testing"
	"time"
)

// drainReReads waits for every async planner read a test triggered to finish
// before the test's seam resets run — W-EXEC-TRUTH W0b, CTO M4 (pre-existing
// flake, test-only).
//
// maybeRunSessionReadsAt / maybeRereadAfterDeath / maybeRereadAfterFlip launch
// their reads on goroutines. A test that returned without joining them let its
// deferred and t.Cleanup seam resets (market.FuturesBarsProvider = nil, the
// recorder swap) race a goroutine still inside the read — the race detector
// caught TestFlipRereadDeathConditionUnchanged's cleanup writing the provider
// while the death re-read read it through kernel.FeedClockDriftMs.
//
// The flip and death re-reads store their in-flight key BEFORE the `go`, and
// delete it as the goroutine's LAST deferred act, so an empty map means no
// goroutine remains. The planner read claims inside its goroutine; a test that
// already observed that read (its recorder) holds the claim until the read's
// deferred release, so draining after that observation is exact too.
//
// Register it with `defer drainReReads(t)` AFTER the trigger: defers run LIFO
// and before every t.Cleanup, so it runs first on any exit. t.Errorf, not
// Fatalf — it may run while the test is already exiting.
func drainReReads(t *testing.T) {
	t.Helper()
	empty := func(m *sync.Map) bool {
		n := 0
		m.Range(func(_, _ any) bool { n++; return false })
		return n == 0
	}
	deadline := time.Now().Add(5 * time.Second)
	for !(empty(&flipRereadInFlight) && empty(&deathRereadInFlight) && empty(&plannerReadInFlight)) {
		if time.Now().After(deadline) {
			t.Errorf("an async planner re-read never finished before the test's seam resets (the cleanup would race it)")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
