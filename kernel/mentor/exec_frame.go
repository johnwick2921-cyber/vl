package mentor

import "vl/market"

// ClosedCandleTF returns the CLOSED candle of the tfMin frame that the newest
// 1m bar in bars completed — the candle a position entered on that frame
// trails behind [X8 @18:02–18:14: "nếu như mình trade khung nào thì mình gồng
// theo khung đó, theo cây nến của khung đó"].
//
//   - tfMin <= 1 → the newest 1m bar itself (ok = true): the 1m trail, today's
//     behaviour, byte-identical.
//   - tfMin >= 2 → the tfMin bucket built by closedBucketsTF — the SAME
//     aggregation the X5-10 exec-2m ISB read uses (eval.go execPrev/execCur),
//     so the trail reads the very candles the entry was read on. ok = false
//     while the newest 1m bar only part-fills its bucket (the tfMin candle is
//     still forming): nothing trails on a half-built candle.
//
// bars must be CLOSED 1m bars, oldest first (the exit drive's feed).
func ClosedCandleTF(bars []market.Kline, tfMin int) (market.Kline, bool) {
	n := len(bars)
	if n == 0 {
		return market.Kline{}, false
	}
	last := bars[n-1]
	if tfMin <= 1 {
		return last, true
	}
	agg := closedBucketsTF(bars, tfMin, BarCloseInstant(last))
	if len(agg) == 0 {
		return market.Kline{}, false
	}
	b := agg[len(agg)-1]
	if b.OpenTime != bucketOpen(last.OpenTime, tfMin) {
		// the newest closed bucket is an OLDER one: the bucket the newest 1m
		// bar belongs to has not closed yet.
		return market.Kline{}, false
	}
	return b, true
}
