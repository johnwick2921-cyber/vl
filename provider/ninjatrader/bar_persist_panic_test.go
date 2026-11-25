package ninjatrader

import (
	"testing"
	"time"
)

// m424 P3 — a panicking persister is logged, counted, and NOT credited as
// flushed; the worker survives it. Driven through the real worker queue.
func TestM424PersisterPanicIsCountedAndNotCreditedAsFlushed(t *testing.T) {
	// the credit decision, deterministic: a panicking persister returns false
	before := persistFlushPanics.Load()
	if runPersister(func(bool, string, string, []Bar) { panic("boom m424") }, persistMsg{symbol: "M424", tf: "1m", bars: []Bar{{T: 1}}}) {
		t.Fatal("runPersister reported success for a panicking persister — persistFlushed would be credited")
	}
	if persistFlushPanics.Load()-before != 1 || PersistFlushPanics() != persistFlushPanics.Load() {
		t.Fatalf("panic not counted: delta %d", persistFlushPanics.Load()-before)
	}
	if !runPersister(func(bool, string, string, []Bar) {}, persistMsg{}) {
		t.Fatal("a healthy persister must report success")
	}

	// through the real worker: it logs, counts and SURVIVES a panic
	prevPanics := persistFlushPanics.Load()
	SetBarPersister(func(_ bool, sym, _ string, _ []Bar) {
		if sym == "M424PANIC" {
			panic("boom m424 worker")
		}
	})
	t.Cleanup(func() { SetBarPersister(nil) })
	old := time.Now().Add(-time.Hour).UnixMilli()
	fanOutBarPersist(func(string, ...interface{}) {}, false, "M424PANIC", "1m", []Bar{{T: old, C: 1, Final: true}})
	deadline := time.Now().Add(3 * time.Second)
	for persistFlushPanics.Load() == prevPanics && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if persistFlushPanics.Load() == prevPanics {
		t.Fatal("worker did not count the panic")
	}
	ok := make(chan struct{}, 1)
	SetBarPersister(func(_ bool, sym, _ string, _ []Bar) {
		if sym == "M424OK" {
			ok <- struct{}{}
		}
	})
	fanOutBarPersist(func(string, ...interface{}) {}, false, "M424OK", "1m", []Bar{{T: old, C: 1, Final: true}})
	select {
	case <-ok:
	case <-time.After(3 * time.Second):
		t.Fatal("persist worker died after a panic")
	}
}

func TestM424BreakFlushTailSkipsReplayBars(t *testing.T) {
	bars := []Bar{{T: 0, Source: BarSourceLive}, {T: 60_000, Source: BarSourceHistorical}, {T: 120_000, Source: BarSourceLive}}
	got := BreakFlushTail(func(string, string) []Bar { return bars }, "MNQ", "1m", 10*60_000, 2)
	if len(got) != 2 || got[0].T != 0 || got[1].T != 120_000 {
		t.Fatalf("tail = %+v", got)
	}
}

// m427/6 — the CREDIT decision through the real worker. A panicking persister
// is handed 3 bars via the production fan-out; a healthy one is handed 1 bar
// next. persistFlushed must move by exactly the healthy 1 — the 3 panicked bars
// are never credited. (TestM424Persister… above calls runPersister directly; the
// `if runPersister(...) { persistFlushed.Add }` line in startPersistWorker was
// unpinned: replacing it with an unconditional Add survived.)
// Mutant: `runPersister(fn, m); if true { persistFlushed.Add(...) }` -> RED.
//
// m456 (flake): persistFlushed is process-wide and the worker is a singleton with
// a private batch, so a neighbour's leftover bar (or an in-flight credit) used to
// land inside this test's window ("moved by 2" in the partner repo CI). The
// baseline is now read only after syncPersistWorker has drained the worker.
func TestM427WorkerDoesNotCreditAPanickedPersisterAsFlushed(t *testing.T) {
	syncPersistWorker(t)
	prevPanics := persistFlushPanics.Load()
	flushed0 := persistFlushed.Load()
	SetBarPersister(func(_ bool, sym, _ string, _ []Bar) {
		if sym == "M427PANIC" {
			panic("boom m427 worker")
		}
	})
	t.Cleanup(func() { SetBarPersister(nil) })
	old := time.Now().Add(-time.Hour).UnixMilli()
	fanOutBarPersist(func(string, ...interface{}) {}, false, "M427PANIC", "1m",
		[]Bar{{T: old, C: 1, Final: true}, {T: old + 60_000, C: 1, Final: true}, {T: old + 120_000, C: 1, Final: true}})
	deadline := time.Now().Add(3 * time.Second)
	for persistFlushPanics.Load() == prevPanics && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if persistFlushPanics.Load() == prevPanics {
		t.Fatal("worker did not count the panic")
	}
	// the healthy marker: 1 bar, credited
	fanOutBarPersist(func(string, ...interface{}) {}, false, "M427OK", "1m", []Bar{{T: old, C: 1, Final: true}})
	deadline = time.Now().Add(3 * time.Second)
	for persistFlushed.Load()-flushed0 < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond) // two more worker ticks: any wrongly credited batch has landed
	if got := persistFlushed.Load() - flushed0; got != 1 {
		t.Fatalf("persistFlushed moved by %d, want exactly 1 (the healthy bar; the 3 panicked bars must not be credited)", got)
	}
}
