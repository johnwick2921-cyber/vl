// BARS HORIZON (2026-09-09) — bar-horizon detections, per arm.
//
// COUNTERS RECORD, NEVER INFER (class 35). The warn is deduped to one line per
// key per 15 minutes; without a counter beside it, 699 of 700 detections would
// be invisible and nobody could tell one stale window from a feed that is
// permanently short. Every DETECTION is counted here BEFORE the dedupe decides
// whether to emit, so the rate always has its numerator.
//
// WARN-ONLY (A10): nothing here refuses, blocks or resizes anything.

package telemetry

import "sync/atomic"

var (
	barHorizonShort      atomic.Int64
	barHorizonHoled      atomic.Int64
	barHorizonEmpty      atomic.Int64
	barHorizonGraced     atomic.Int64
	barHorizonBeyondRing atomic.Int64
	barHorizonSuppressed atomic.Int64
)

// IncBarHorizon counts one detection. kind is "short" | "holed" | "empty" |
// "graced" | "beyond_ring" (m452: a depth request larger than the ring) | "suppressed"; an unrecognised kind is dropped rather than
// attributed to the wrong arm.
func IncBarHorizon(kind string) {
	switch kind {
	case "short":
		barHorizonShort.Add(1)
	case "holed":
		barHorizonHoled.Add(1)
	case "empty":
		barHorizonEmpty.Add(1)
	case "graced":
		barHorizonGraced.Add(1)
	case "beyond_ring":
		barHorizonBeyondRing.Add(1)
	case "suppressed":
		barHorizonSuppressed.Add(1)
	}
}

// BarHorizonCounts returns every arm. Reading never resets (class 35).
func BarHorizonCounts() map[string]int64 {
	return map[string]int64{
		"short":       barHorizonShort.Load(),
		"holed":       barHorizonHoled.Load(),
		"empty":       barHorizonEmpty.Load(),
		"graced":      barHorizonGraced.Load(),
		"beyond_ring": barHorizonBeyondRing.Load(),
		"suppressed":  barHorizonSuppressed.Load(),
	}
}
