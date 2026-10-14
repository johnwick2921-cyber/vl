package telemetry

import "sync/atomic"

// ── GAP-BACKFILL (2026-10-14) — a bar hole is COUNTED ──────────────────────
//
// 10-14 03:44–07:48 CT: a 244-minute 1m hole sat in the store until an
// unrelated restart re-sent bars_subscribe. These counters record every step
// of the self-refill the bar-hole tracker now runs, so "did the bot ask, and
// did NT8 answer" is a number on /api/health rather than a grep through the
// log. Counters record, never infer (canon 35): each is incremented at the
// one site that does the thing it names.
var (
	barsHoleDetected   atomic.Int64 // holes opened (one per window)
	barsHoleFilled     atomic.Int64 // holes re-measured filled from the store
	barsHoleUnfilled   atomic.Int64 // holes still open after the last scheduled attempt (ONE per hole)
	barsHoleRerequests atomic.Int64 // gap re-requests actually SENT (one per symbol per send)
	barsHoleDeferred   atomic.Int64 // attempts held back: feed down / link down / market closed
	barsHoleRefused    atomic.Int64 // attempts the wire refused (budget, spacing, send error)
	barsHoleOverflow   atomic.Int64 // holes not tracked because the open-hole cap was full
	// filledAt[n] — fills by the attempt that filled them (0 = filled before
	// any re-request; the last slot collects every attempt past it).
	barsHoleFilledAt [8]atomic.Int64
)

func IncBarsHoleDetected()  { barsHoleDetected.Add(1) }
func IncBarsHoleUnfilled()  { barsHoleUnfilled.Add(1) }
func IncBarsHoleRerequest() { barsHoleRerequests.Add(1) }
func IncBarsHoleDeferred()  { barsHoleDeferred.Add(1) }
func IncBarsHoleRefused()   { barsHoleRefused.Add(1) }
func IncBarsHoleOverflow()  { barsHoleOverflow.Add(1) }

// IncBarsHoleFilled records one fill and the attempt number that filled it.
func IncBarsHoleFilled(attempt int) {
	barsHoleFilled.Add(1)
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(barsHoleFilledAt) {
		attempt = len(barsHoleFilledAt) - 1
	}
	barsHoleFilledAt[attempt].Add(1)
}

// BarsHoleCounts is the READ view for /api/health (A11). Every field is a
// counter since boot; zero is a measured zero.
type BarsHoleCounts struct {
	Detected   int64   `json:"bars_hole_detected"`
	Filled     int64   `json:"bars_hole_filled"`
	Unfilled   int64   `json:"bars_hole_unfilled"`
	Rerequests int64   `json:"bars_hole_rerequests"`
	Deferred   int64   `json:"bars_hole_deferred"`
	Refused    int64   `json:"bars_hole_refused"`
	Overflow   int64   `json:"bars_hole_overflow"`
	FilledAt   []int64 `json:"bars_hole_filled_by_attempt"` // index = attempt number; last index = that attempt or later
}

// BarsHoleCountsSnapshot reads every counter once.
func BarsHoleCountsSnapshot() BarsHoleCounts {
	out := BarsHoleCounts{
		Detected:   barsHoleDetected.Load(),
		Filled:     barsHoleFilled.Load(),
		Unfilled:   barsHoleUnfilled.Load(),
		Rerequests: barsHoleRerequests.Load(),
		Deferred:   barsHoleDeferred.Load(),
		Refused:    barsHoleRefused.Load(),
		Overflow:   barsHoleOverflow.Load(),
		FilledAt:   make([]int64, len(barsHoleFilledAt)),
	}
	for i := range barsHoleFilledAt {
		out.FilledAt[i] = barsHoleFilledAt[i].Load()
	}
	return out
}
