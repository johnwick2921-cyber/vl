package mentor

import (
	"runtime"
	"syscall"
	"testing"
	"time"

	"vl/market"
)

// TestThirtyDayReplaySeconds: the CTO's parity gate — 30 days of 1m bars
// replayed through the SEEDED evaluator must finish in seconds, not the
// 10-minute timeout DS-105 hit on the unseeded (quadratic) Tick. Duration is
// logged (measure, not a hard assert); a hard ceiling guards the regression
// class. Ceiling 240s (CTO 2026-10-04, release #5): the quadratic class it
// guards ran 600s+; on this box the test measures 36.9s at release #4 and
// 43.1s at release #5 (+17%, linear: the 15m/30m box ladder + closed-bucket
// reads), and the GitHub runner is ~3x slower — 127.7s tripped the old 120s
// cap on a linear cost. Follow-up: memoize barsTF per Tick (14% of the run).
//
// m468 (2026-11-07): the ceiling now reads the process's CPU time with the Go
// scheduler held to ONE thread for the replay, not wall time. Wall time is
// mostly a reading of how busy the CI runner is: "Backend Tests" runs every
// package at once on 4 vCPUs, and over 41 runs (10-29..11-06) this replay took
// anywhere from 77s to 257s of wall time, failing at 248.5s and 256.7s on
// 5e50da0ce and 82a36eafe and passing at 206.7s on the identical tree
// c27e3cacf. Measured on the owner box (Core Ultra 9 285K) at b1917cde8:
//   - alone, all cores:             wall 101s, CPU 173s (parallel GC work)
//   - alone, one thread (this test): wall 117s, CPU 117s
//   - one thread, pinned to one core shared with two busy loops:
//     wall 400s (the old 240s wall cap fails), CPU 134s (+15%)
//
// One thread runs ~16% longer than all cores here (117s vs 101s wall), so the
// worst CI run so far comes to about 300s of single-thread CPU at most. The
// ceiling is 480s of CPU: ~1.6x that, for slower runner hardware and normal
// feature growth (this replay grew 43s -> 117s locally since release #5).
// What it still catches is a jump in complexity (work per tick growing faster
// than the slice), which multiplies this run many times over. Measured at the
// same commit: per-tick cost now grows with the slice length (1.1s for the
// first 1,365 ticks, 20.2s for the last 1,365), and over the last 200 ticks of
// this tape a cold (unseeded) evaluator was no slower than the seeded one
// (2.29s vs 2.74s). So this ceiling no longer tells the seeded path from the
// cold one; live Ticks get a capped slice, which bounds the per-tick cost.
//
// m468 review: that is the quadratic class, already present — per-tick cost is
// linear in the slice (Tick clears its memo, eval.go `e.tfMemo = nil`, and
// barsTF re-reads the whole slice, mentor.go), so a full-tape replay is
// O(n²). The 480s ceiling alone would pass a further doubling. So the test now
// also pins the GROWTH RATE: per-tick CPU over the last tenth of the replay,
// divided by the first tenth, may be at most twice the ratio of the two
// windows' average slice lengths (≈19 on this tape, measured ≈18). Per-tick
// cost growing faster than the slice (n log n is ~1.3x on top, n^1.5 is ~4x)
// fails here on any machine speed. Making Tick incremental (barsTF per new bar,
// not per Tick) is the tracked follow-up; until then the message below says
// what is pinned, not that the quadratic cost is absent.
func TestThirtyDayReplaySeconds(t *testing.T) {
	if raceEnabled {
		t.Skip("the CPU-time ceiling is meaningless under -race (5-10x slower); the non-race suite enforces it")
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	var bars1m []market.Kline
	cl := 20000.0
	for d := 0; d < 35; d++ {
		// RTH 1m: 08:30–15:00 CT = 390 bars/day
		for m := 0; m < 390; m++ {
			ot := ctMs(t, d, 8, 30) + int64(m)*60_000
			cl += 0.25
			bars1m = append(bars1m, mkBar(cl-0.125, cl, ot, 1))
		}
	}
	// The 4h warm-up is derived from the 1m history (P1): 35 RTH days of 1m
	// aggregate into >=102 closed 4h buckets on the 17:00 CT anchor.
	now := ctMs(t, 34, 15, 0)

	e := New(cfg)
	if m := Seed(e, bars1m, now); len(m) != 0 {
		t.Fatalf("30-day store must be warm, got %v", m)
	}

	// One scheduler thread for the replay: the CPU time then counts the replay's
	// own work (GC included) the same way on a 4-core runner and a 24-core box.
	// Nothing else in this package runs in parallel with this test.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	cpu0 := processCPUTime(t)
	start := time.Now()
	var intents int
	// Two measuring windows of a tenth of the replay each: the first ticks
	// (short slices) and the last (the full tape).
	n := len(bars1m)
	w := n / 10
	firstEnd, lastStart := 2+w, n-w+1 // first window: i in [2, firstEnd); last: i in [lastStart, n]
	var cpuFirst, cpuLastStart time.Duration
	// Replay the last 3 days tick-by-tick (the live pattern: a capped slice
	// plus the seeded state), the worst-case slice = the full tape.
	for i := 2; i <= n; i++ {
		if i == firstEnd {
			cpuFirst = processCPUTime(t) - cpu0
		}
		if i == lastStart {
			cpuLastStart = processCPUTime(t)
		}
		intents += len(e.Tick(bars1m[:i], bars1m[i-1].CloseTime+1))
	}
	cpuEnd := processCPUTime(t)
	elapsed := time.Since(start)
	cpu := cpuEnd - cpu0
	cpuLast := cpuEnd - cpuLastStart
	// The average slice length in each window, and the growth the two windows'
	// per-tick CPU may show: at most twice what a linear per-tick cost gives.
	avgFirst := float64(2+firstEnd-1) / 2
	avgLast := float64(lastStart+n) / 2
	sliceRatio := avgLast / avgFirst
	growth := float64(cpuLast) / float64(cpuFirst)
	t.Logf("30-day seeded replay: %d ticks, %d intents, %s (cpu %s); per-tick cpu last/first tenth = %.1f (slice ratio %.1f, limit %.1f)",
		n-1, intents, elapsed, cpu, growth, sliceRatio, 2*sliceRatio)
	if cpu > 480*time.Second {
		t.Fatalf("30-day replay used %s of CPU (wall %s) — over the 480s ceiling: the replay's work grew a lot (per-tick cost is already linear in the slice; see the m468 note)", cpu, elapsed)
	}
	if cpuFirst <= 0 {
		t.Fatalf("the first window measured no CPU time (%s) — cannot judge the growth rate", cpuFirst)
	}
	if growth > 2*sliceRatio {
		t.Fatalf("per-tick CPU grew %.1fx from the first tenth of the replay to the last, while the slice grew %.1fx (limit %.1fx): "+
			"a Tick now costs MORE than linear in the slice — worse than the known O(n) per tick (first %s, last %s)",
			growth, sliceRatio, 2*sliceRatio, cpuFirst, cpuLast)
	}
}

// processCPUTime is the CPU time (user + system) this test process has used so
// far. The replay ceiling reads CPU time, not wall time — see the m468 note on
// TestThirtyDayReplaySeconds.
func processCPUTime(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
