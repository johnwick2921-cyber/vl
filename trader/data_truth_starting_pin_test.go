package trader

import (
	"testing"
	"time"
)

// Round 4 re-review 5 P2: DataTruthStarting (the boot-wait read behind the
// /api/health "data_truth_starting" pause and the dashboard banner) had no pin
// of its own - the health pin swaps it for a closure. This drives the real
// function on both sides of every input it reads.
//
// MUTANTS: return false (D1) -> RED; drop the dataTruthWired guard (D1c) ->
// RED; ignore the grace -> RED; ignore the ready edge -> RED.
func TestDataTruthStartingFollowsWireReadyAndGrace(t *testing.T) {
	prevWired, prevAt, prevReady := dataTruthWired.Load(), dataTruthWiredAt.Load(), dataTruthReadyFn
	t.Cleanup(func() {
		dataTruthWired.Store(prevWired)
		dataTruthWiredAt.Store(prevAt)
		dataTruthReadyFn = prevReady
	})
	wiredAt := time.Date(2026, 10, 29, 15, 35, 56, 0, time.UTC)
	ready := false
	dataTruthReadyFn = func() bool { return ready }
	dataTruthWiredAt.Store(wiredAt.UnixMilli())

	dataTruthWired.Store(false)
	if DataTruthStarting(wiredAt.Add(time.Minute)) {
		t.Fatal("an unwired gate refuses nothing, so it is not 'starting'")
	}

	dataTruthWired.Store(true)
	if !DataTruthStarting(wiredAt.Add(time.Minute)) {
		t.Fatal("wired, not judge-ready, inside the grace: the boot wait holds and health must say paused")
	}
	if DataTruthStarting(wiredAt.Add(dataTruthBootGrace + time.Second)) {
		t.Fatal("past the grace the checks are judged (and name what is missing); the quiet wait is over")
	}
	ready = true
	if DataTruthStarting(wiredAt.Add(time.Minute)) {
		t.Fatal("judge-ready ends the boot wait even inside the grace")
	}
}
