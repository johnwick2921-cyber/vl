package pep

import (
	"math"
	"sync"
	"time"

	// Embed the zoneinfo database: the box start and the 15:00 flat are CT
	// wall-clock times, and a missing tzdata must NEVER fall back to a fixed
	// UTC-6 (wrong for half the year). With this import America/Chicago always
	// loads; if it somehow does not, Evaluate refuses (CodeTZUnavailable).
	_ "time/tzdata"
)

var (
	chicagoOnce sync.Once
	chicagoLoc  *time.Location
	chicagoErr  error
)

// chicago returns the cached America/Chicago location. The cache is immutable
// after first load, so it carries no state that can change a result.
func chicago() (*time.Location, error) {
	chicagoOnce.Do(func() { chicagoLoc, chicagoErr = time.LoadLocation("America/Chicago") })
	return chicagoLoc, chicagoErr
}

// CTLocation exposes the America/Chicago location to callers (the backtest, the
// store tests) that need the same zone pep reads.
func CTLocation() (*time.Location, error) { return chicago() }

// tickEps absorbs float noise when a quotient is a whole number of ticks.
const tickEps = 1e-9

// CeilTick rounds a POSITIVE distance UP to a whole number of ticks (OUTWARD
// for a stop distance).
func CeilTick(x, tick float64) float64 {
	if !(tick > 0) {
		return x
	}
	return math.Ceil(x/tick-tickEps) * tick
}

// RoundTick rounds a value to the nearest tick (half away from zero).
func RoundTick(x, tick float64) float64 {
	if !(tick > 0) {
		return x
	}
	return math.Round(x/tick) * tick
}

// snap removes accumulated float noise from a price on the tick grid.
func snap(x, tick float64) float64 { return RoundTick(x, tick) }

// candle is one complete closed aggregate candle.
type candle struct {
	Start, End int64 // [Start, End) ms; End is the close INSTANT
	High, Low  float64
	Close      float64
	EMA        float64
}

// fillEMA computes the recurrence ema += k*(close-ema), seeded with the first
// close, over the candles in order (the EMA is carried across sessions: the
// caller never resets it at a day boundary).
func fillEMA(cs []candle, period int) {
	if len(cs) == 0 {
		return
	}
	k := 2.0 / float64(period+1)
	cs[0].EMA = cs[0].Close
	for i := 1; i < len(cs); i++ {
		cs[i].EMA = cs[i-1].EMA + k*(cs[i].Close-cs[i-1].EMA)
	}
}
