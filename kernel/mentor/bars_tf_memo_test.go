package mentor

import (
	"reflect"
	"testing"

	"vl/market"
)

// ── FU-2 barsTF memo pins (DS-105) ────────────────────────────────────────
// barsTFMemo builds each (tf, slice identity) at most once per Tick and
// returns buckets byte-identical to a fresh barsTF. The identity guard is
// len + first/last OpenTime, so a NEW bar (longer slice, later last bar)
// always rebuilds — a stale bucket can never be read.
// ───────────────────────────────────────────────────────────────────────────

// tape builds n consecutive 1m RTH bars (09:00 CT + i minutes) so every TF
// from 2m up to 240m spans at least one full bucket boundary.
func tape(n int) []market.Kline {
	bars := make([]market.Kline, 0, n)
	for i := 0; i < n; i++ {
		o := 100.0 + 0.25*float64(i%4)
		c := 100.0 + 0.25*float64((i+1)%4)
		bars = append(bars, rthBars(i, o, c+1, c-1, c))
	}
	return bars
}

// PIN 1 — a memo hit returns the SAME buckets as a fresh barsTF for every TF
// the evaluator aggregates (the build and the hit are both byte-identical to
// barsTF).
func TestBarsTFMemoHitMatchesFresh(t *testing.T) {
	e := New(DefaultConfig())
	bars := tape(600) // 10h of 1m — covers 2/5/15/30/60/240 bucket boundaries
	for _, tf := range []int{2, 5, 15, 30, 60, 240} {
		first := e.barsTFMemo(bars, tf)
		if !reflect.DeepEqual(first, barsTF(bars, tf)) {
			t.Fatalf("tf=%d: first memo build != fresh barsTF", tf)
		}
		second := e.barsTFMemo(bars, tf) // hit
		if !reflect.DeepEqual(second, barsTF(bars, tf)) {
			t.Fatalf("tf=%d: memo hit != fresh barsTF", tf)
		}
	}
}

// PIN 2 — a NEW bar invalidates: appending one 1m bar (same tf) must rebuild,
// never return the pre-append buckets.
func TestBarsTFMemoInvalidatesOnNewBar(t *testing.T) {
	e := New(DefaultConfig())
	bars := tape(300)
	const tf = 5
	first := e.barsTFMemo(bars, tf)
	if !reflect.DeepEqual(first, barsTF(bars, tf)) {
		t.Fatalf("precondition: memo build != fresh barsTF")
	}
	// one more bar, in a NEW 5m bucket (so a stale read would differ by a
	// whole bucket, and by close/high/low inside it).
	next := rthBars(300, 101, 101.5, 100, 101)
	bars2 := append(append([]market.Kline(nil), bars...), next)
	got := e.barsTFMemo(bars2, tf)
	if !reflect.DeepEqual(got, barsTF(bars2, tf)) {
		t.Fatalf("tf=%d: memo returned STALE buckets after a new bar (len %d -> %d)", tf, len(bars), len(bars2))
	}
	// the cached slot must now match the longer slice, not the old one.
	if en := e.tfMemo[tf]; en.n != len(bars2) || en.last != next.OpenTime {
		t.Fatalf("memo slot not refreshed: n=%d last=%d, want n=%d last=%d", en.n, en.last, len(bars2), next.OpenTime)
	}
}

// PIN 3 — Tick clears the memo, so the NEXT Tick's buckets are rebuilt from the
// new tape (the per-Tick scope, not a session-long cache).
func TestBarsTFMemoClearedPerTick(t *testing.T) {
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	bars := tape(60)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if len(e.tfMemo) == 0 {
		t.Fatalf("precondition: a Tick must populate the memo")
	}
	// A new Tick with a DIFFERENT tape must not read the prior Tick's buckets.
	bars2 := tape(61)
	e.Tick(bars2, bars2[len(bars2)-1].CloseTime+1)
	for _, en := range e.tfMemo {
		if en.n == len(bars) && en.last == bars[len(bars)-1].OpenTime {
			t.Fatalf("memo slot survived into the next Tick (stale len=%d)", en.n)
		}
	}
}

// PIN 4 — no consumer may mutate a memo'd bucket: after a Tick, every memo slot
// must still equal a fresh barsTF of the SAME input. A consumer that writes the
// shared backing array (append, b[i]=, b[i].Field=, sort) turns this pin red.
func TestBarsTFMemoUncorruptedAfterTick(t *testing.T) {
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	bars := tape(300)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if len(e.tfMemo) == 0 {
		t.Fatalf("precondition: Tick must populate the memo")
	}
	for tf, en := range e.tfMemo {
		// Empty print windows make the HTF feed byte-identical to the raw tape,
		// so every memo slot derives from `bars`.
		if en.n != len(bars) || en.first != bars[0].OpenTime || en.last != bars[len(bars)-1].OpenTime {
			t.Fatalf("tf=%d: memo slot is not the raw tape (n=%d first=%d last=%d)", tf, en.n, en.first, en.last)
		}
		if !reflect.DeepEqual(en.bucket, barsTF(bars, tf)) {
			t.Fatalf("tf=%d: memo bucket was mutated during Tick", tf)
		}
	}
}
