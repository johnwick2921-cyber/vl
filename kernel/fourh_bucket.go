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
	return SessionBucketStart(openMs, 4*time.Hour, loc)
}

// SessionBucketStart is the generalised 17:00 CT-anchored bucket start for a
// coarse CME timeframe step (2h/4h/6h/8h/12h; 24h gives the session-day
// boundary). FourHBucketStart is the 4h specialisation. #42 (m220): the native
// coarse ring is session-anchored, NOT epoch-floored, so a chart backfill that
// floors to the Unix epoch misaligns and overlaps the first native bar.
//
// DST-aware via America/Chicago. loc nil → CTLocation().
func SessionBucketStart(openMs int64, step time.Duration, loc *time.Location) int64 {
	if loc == nil {
		loc = CTLocation()
	}
	t := time.UnixMilli(openMs).In(loc)
	anchor := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, loc)
	if t.Before(anchor) {
		anchor = anchor.AddDate(0, 0, -1)
	}
	delta := t.Sub(anchor)
	return anchor.Add(delta - delta%step).UnixMilli()
}

// cTMidnightFloor floors an open stamp to the CT midnight boundary — the grid
// the native 1d series sits on (open-stamped (D-1) 00:00 CT, see #15).
func cTMidnightFloor(openMs int64) int64 {
	t := time.UnixMilli(openMs).In(CTLocation())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, CTLocation()).UnixMilli()
}

// AggregateSessionAnchored buckets a finer series into the native coarse-TF
// grid (#42): 17:00 CT-anchored for 2h/4h/6h/8h/12h, CT midnight for 1d. Sub-
// 2h TFs divide 24h exactly, so epoch-floor and any hour anchor coincide and
// they fall back to the epoch floor (identical to aggregateToMinutes). Mirrors
// aggregateToMinutes' never-invent-a-bucket + CloseTime = end − 1ms semantics;
// the ONLY difference is the bucket key for the coarse session-anchored TFs.
func AggregateSessionAnchored(bars []market.Kline, tf string) []market.Kline {
	tfMin := market.TFMinutes(tf)
	if tfMin <= 1 || len(bars) == 0 {
		return bars
	}
	span := int64(tfMin) * 60_000
	bucketFn := func(openMs int64) int64 { return openMs / span * span } // epoch floor
	switch tf {
	case "2h":
		bucketFn = func(o int64) int64 { return SessionBucketStart(o, 2*time.Hour, nil) }
	case "4h":
		bucketFn = func(o int64) int64 { return SessionBucketStart(o, 4*time.Hour, nil) }
	case "6h":
		bucketFn = func(o int64) int64 { return SessionBucketStart(o, 6*time.Hour, nil) }
	case "8h":
		bucketFn = func(o int64) int64 { return SessionBucketStart(o, 8*time.Hour, nil) }
	case "12h":
		bucketFn = func(o int64) int64 { return SessionBucketStart(o, 12*time.Hour, nil) }
	case "1d":
		bucketFn = cTMidnightFloor
	}
	out := make([]market.Kline, 0, len(bars)/tfMin+1)
	var curBucket int64 = -1
	for i := range bars {
		b := bars[i]
		bucket := bucketFn(b.OpenTime)
		if bucket != curBucket {
			out = append(out, market.Kline{
				OpenTime:  bucket,
				CloseTime: bucket + span - 1,
				Open:      b.Open, High: b.High, Low: b.Low, Close: b.Close,
				Volume: b.Volume,
			})
			curBucket = bucket
			continue
		}
		agg := &out[len(out)-1]
		if b.High > agg.High {
			agg.High = b.High
		}
		if b.Low < agg.Low {
			agg.Low = b.Low
		}
		agg.Close = b.Close
		agg.Volume += b.Volume
	}
	return out
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
