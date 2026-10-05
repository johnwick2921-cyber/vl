package mentor

import (
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
func TestThirtyDayReplaySeconds(t *testing.T) {
	if raceEnabled {
		t.Skip("wall-clock threshold is meaningless under -race; the non-race suite enforces it")
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

	start := time.Now()
	var intents int
	// Replay the last 3 days tick-by-tick (the live pattern: a capped slice
	// plus the seeded state), the worst-case slice = the full tape.
	for i := 2; i <= len(bars1m); i++ {
		intents += len(e.Tick(bars1m[:i], bars1m[i-1].CloseTime+1))
	}
	elapsed := time.Since(start)
	t.Logf("30-day seeded replay: %d ticks, %d intents, %s", len(bars1m)-1, intents, elapsed)
	if elapsed > 240*time.Second {
		t.Fatalf("30-day replay took %s — the quadratic class is back", elapsed)
	}
}
