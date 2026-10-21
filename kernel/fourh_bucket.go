package kernel

import (
	"time"

	"vl/market"
)

// FourHBucketStart returns the start (ms) of the 17:00 CT-anchored 4h bucket a
// bar belongs to — the ONE 4h anchor definition. The mentor's HTF gate and
// SWING4H read this exact series (kernel/mentor swing4h.go fourHBucketStart
// delegates here); the planner's 4h candle table uses Aggregate4HCT below, so
// the AI and the gate see the same 4h candles [D5.2 p3 @12:30, "the 4h candles
// chain from the 17:00 CT anchor"].
//
// DST-aware via America/Chicago. loc nil → CTLocation().
func FourHBucketStart(openMs int64, loc *time.Location) int64 {
	if loc == nil {
		loc = CTLocation()
	}
	t := time.UnixMilli(openMs).In(loc)
	anchor := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, loc)
	if t.Before(anchor) {
		anchor = anchor.AddDate(0, 0, -1)
	}
	delta := t.Sub(anchor)
	const step = 4 * time.Hour
	return anchor.Add(delta - delta%step).UnixMilli()
}

// Aggregate4HCT buckets a 1m series into 17:00 CT-anchored 4h bars — the SAME
// bucket boundary FourHBucketStart defines (so it is byte-identical to the
// mentor's barsTF(bars, 240)). AggregateBars stays epoch-floored and is NOT
// used for the planner's 4h table.
func Aggregate4HCT(bars []market.Kline) []market.Kline {
	return aggregateBarsBy(bars, 4*time.Hour.Milliseconds(), func(openMs int64) int64 {
		return FourHBucketStart(openMs, CTLocation())
	})
}
