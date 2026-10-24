package mentor

import (
	"math"
	"time"

	"vl/market"
)

// m407 — frame-switch carry. The intraday trendline build is keyed by the FRAME
// open times of its anchors, so at the 1m→2m switch (09:00 CT) and the 2m→1m
// switch (08:30 CT) the two builds return DISJOINT key sets and a VALID line —
// a live location — would silently leave the Level set. The latch below carries
// every intraday line that is VALID (ValidAt > 0) and not Dead across the switch
// until a closed 5m candle CLOSES through it (brokenBy5m, read by time) or the
// trading day ends. FORMING lines are not carried: the new frame redraws them.
// Same idea as e.tlHTF — persisted state on the Evaluator, never serialized.

// tlCarryTick is one MNQ tick: the anchor-price tolerance for "the same line".
const tlCarryTick = 0.25

type carriedTL struct {
	line  Trendline
	frame int    // build frame (1 or 2) the line was last seen VALID on
	day   string // trading day key
}

// carryIntradayLines latches valid intraday lines from `built` (the current
// frame's build) and returns built plus any carried lines the frame switch
// dropped. `frame` is the current build frame; knob OFF never switches frame, so
// the caller passes carry=false and the latch is cleared (byte-identical to the
// 1m-all-day build).
func (e *Evaluator) carryIntradayLines(built []Trendline, bars []market.Kline, now time.Time, frame int, carry bool) []Trendline {
	if !carry {
		e.tlCarry = nil
		return built
	}
	if e.tlCarry == nil {
		e.tlCarry = map[string]carriedTL{}
	}
	day := tradingDayKey(now.In(ctime()))
	for k, c := range e.tlCarry {
		if c.day != day {
			delete(e.tlCarry, k)
		}
	}
	present := map[string]int{}
	for i, tl := range built {
		present[tl.key()] = i
	}
	// 1. latch / release from the current build.
	for _, tl := range built {
		k := tl.key()
		switch {
		case tl.Dead:
			delete(e.tlCarry, k)
		case tl.ValidAt > 0:
			e.tlCarry[k] = carriedTL{line: tl, frame: frame, day: day}
		}
	}
	// 2. carry what the switch dropped.
	var b5 []market.Kline
	for k, c := range e.tlCarry {
		if _, ok := present[k]; ok {
			continue
		}
		if c.frame == frame {
			// same frame, key absent: the window slid past it, or the build no
			// longer draws it — not a switch; release (steady state unchanged).
			delete(e.tlCarry, k)
			continue
		}
		if b5 == nil {
			b5 = barsTF(bars, 5)
		}
		if intradayBrokenBy5m(c.line, b5, now) {
			delete(e.tlCarry, k)
			continue
		}
		tl := c.line
		tl.carried = true // already passed its first test: m311 must not re-apply
		// ONE line: a build line with the same side and anchors is replaced by
		// the carried valid one.
		dup := -1
		for i, b := range built {
			if sameCarriedAnchors(tl, b, frame) {
				dup = i
				break
			}
		}
		if dup >= 0 {
			built = append(append([]Trendline(nil), built[:dup]...), built[dup+1:]...)
		}
		built = append(built, tl)
	}
	return built
}

// sameCarriedAnchors: same side, anchor prices within one tick, anchor times in
// the same bucket of the current frame.
func sameCarriedAnchors(a, b Trendline, frame int) bool {
	if a.Side != b.Side {
		return false
	}
	if math.Abs(a.P0Px-b.P0Px) > tlCarryTick || math.Abs(a.P1Px-b.P1Px) > tlCarryTick {
		return false
	}
	return bucketOpen(a.P0T, frame) == bucketOpen(b.P0T, frame) && bucketOpen(a.P1T, frame) == bucketOpen(b.P1T, frame)
}

// intradayBrokenBy5m is trendlineScanTF's break rule read by TIME: a CLOSED 5m
// candle that opened after P1 and CLOSED through the line.
func intradayBrokenBy5m(tl Trendline, b5 []market.Kline, now time.Time) bool {
	nowMs := now.UnixMilli()
	for _, b := range b5 {
		if b.CloseTime >= nowMs || b.OpenTime <= tl.P1T {
			continue
		}
		if tl.brokenBy5m(b) {
			return true
		}
	}
	return false
}
