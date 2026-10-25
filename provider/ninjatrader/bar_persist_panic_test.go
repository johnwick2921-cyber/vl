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
