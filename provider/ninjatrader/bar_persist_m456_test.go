package ninjatrader

import (
	"testing"
	"time"
)

// m456 (flake m427) — the baseline of a persistFlushed-delta test must not be
// poisoned by what the PREVIOUS test left in the singleton worker.
//
// persistFlushed is a process-wide counter bumped by one never-stopping worker
// that holds an in-memory batch (flushed on a 300ms tick) and resolves the
// persister at FLUSH time. A neighbour that detaches its persister while a
// closed bar sits in that batch (or whose persister has returned but whose
// credit has not yet landed) therefore gets that bar credited through the NEXT
// test's persister, after the next test read its baseline: "moved by 2, want
// exactly 1" in the partner repo's -race CI run.
//
// This test reproduces the neighbour's leftover deterministically and then runs
// the REAL m427 test body. It was RED before syncPersistWorker existed.
func TestM456M427BaselineSurvivesANeighbourLeftoverInTheBatch(t *testing.T) {
	startPersistWorker()
	old := time.Now().Add(-time.Hour).UnixMilli()
	SetBarPersister(func(bool, string, string, []Bar) {})
	fanOutBarPersist(func(string, ...interface{}) {}, false, "M456STALE", "1m", []Bar{{T: old, C: 1, Final: true}})
	SetBarPersister(nil) // exactly what a neighbour's t.Cleanup does
	TestM427WorkerDoesNotCreditAPanickedPersisterAsFlushed(t)
}

// syncPersistWorker returns only once the singleton worker has finished EVERYTHING
// enqueued or batched before the call, including the persistFlushed credit for
// it, so a test can read a persistFlushed baseline that nothing earlier can still
// move. idlePersistQueue is not enough: it waits for len(barPersistCh)==0 and
// cannot see the worker's private batch or an in-flight credit.
//
// Mechanism, no sleep-and-hope: the worker is one goroutine and runs the
// persister, then credits, message by message in FIFO order. A marker message
// (no bars, so its own credit is +0) is queued behind everything; when the
// persister is called for the marker, every earlier message has been persisted
// AND credited. Leftovers flush through THIS probe (a healthy no-op), so they are
// credited before the baseline is read instead of leaking into the next test.
// The probe is detached on return; callers install their own persister after.
func syncPersistWorker(t *testing.T) {
	t.Helper()
	startPersistWorker()
	reached := make(chan struct{})
	SetBarPersister(func(_ bool, sym, _ string, _ []Bar) {
		if sym == "M456SYNC" {
			close(reached)
		}
	})
	select {
	case barPersistCh <- persistMsg{symbol: "M456SYNC", tf: "1m"}:
	case <-time.After(10 * time.Second):
		t.Fatal("persist queue full: could not enqueue the sync marker")
	}
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("persist worker never reached the sync marker")
	}
	SetBarPersister(nil)
}

// The helper's contract: after it returns, a credit that was still in flight when
// it was called has landed. The persister below returns only after the call has
// started waiting, so without the FIFO marker the credit lands AFTER the read.
func TestM456SyncPersistWorkerWaitsForAnInFlightCredit(t *testing.T) {
	startPersistWorker()
	SetBarPersister(nil)
	flushed0 := persistFlushed.Load()
	entered := make(chan struct{})
	release := make(chan struct{})
	SetBarPersister(func(_ bool, sym, _ string, _ []Bar) {
		if sym == "M456INFLIGHT" {
			close(entered)
			<-release
		}
	})
	old := time.Now().Add(-time.Hour).UnixMilli()
	fanOutBarPersist(func(string, ...interface{}) {}, false, "M456INFLIGHT", "1m", []Bar{{T: old, C: 1, Final: true}})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight persister never entered")
	}
	// the persister is running and the credit has not landed
	go func() { time.Sleep(50 * time.Millisecond); close(release) }()
	syncPersistWorker(t)
	if got := persistFlushed.Load() - flushed0; got < 1 {
		t.Fatalf("persistFlushed moved by %d after syncPersistWorker, want >= 1: the in-flight credit had not landed", got)
	}
}
