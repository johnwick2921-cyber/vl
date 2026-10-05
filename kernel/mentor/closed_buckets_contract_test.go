package mentor

import (
	"testing"

	"vl/market"
)

// ── FU-3: the closedBuckets/closedBucketsTF `now` contract ──────────────────
// `now` must be STRICTLY AFTER the newest closed 1m bar's close. The pin below
// documents the +1 convention (BarCloseInstant = last.CloseTime + 1): a caller
// passing last.CloseTime verbatim over-drops the just-completed bucket, so a
// change of convention fails loudly.

// tfTape builds n consecutive 1m bars starting at openMs, all with the same
// OHLC (the bucket geometry only depends on the times).
func tfTape(openMs int64, n int) []market.Kline {
	bars := make([]market.Kline, 0, n)
	for i := 0; i < n; i++ {
		ot := openMs + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: 100, High: 101, Low: 99, Close: 100})
	}
	return bars
}

// TestClosedBucketsTFKeepsJustCompletedAtPlusOne — two full 2m buckets: at
// last.CloseTime+1 the second (just-completed) bucket is KEPT; at
// last.CloseTime it is DROPPED (the contract the comment names).
func TestClosedBucketsTFKeepsJustCompletedAtPlusOne(t *testing.T) {
	bars := tfTape(auditMs(2026, 9, 15, 9, 0, 0), 4) // two full 2m buckets
	last := bars[len(bars)-1]
	if got := len(closedBucketsTF(bars, 2, last.CloseTime+1)); got != 2 {
		t.Fatalf("now = last.CloseTime+1 must KEEP the just-completed bucket, got %d buckets", got)
	}
	if got := len(closedBucketsTF(bars, 2, last.CloseTime)); got != 1 {
		t.Fatalf("now = last.CloseTime verbatim must DROP the just-completed bucket, got %d buckets", got)
	}
}

// TestClosedBucketsKeepsJustCompletedAtPlusOne — the 5m variant holds the same
// contract.
func TestClosedBucketsKeepsJustCompletedAtPlusOne(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := tfTape(auditMs(2026, 9, 15, 9, 0, 0), 10) // two full 5m buckets
	last := bars[len(bars)-1]
	if got := len(closedBuckets(bars, last.CloseTime+1, cfg)); got != 2 {
		t.Fatalf("now = last.CloseTime+1 must KEEP the just-completed 5m bucket, got %d buckets", got)
	}
	if got := len(closedBuckets(bars, last.CloseTime, cfg)); got != 1 {
		t.Fatalf("now = last.CloseTime verbatim must DROP the just-completed 5m bucket, got %d buckets", got)
	}
}
