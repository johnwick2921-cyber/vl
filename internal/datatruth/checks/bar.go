// Package checks holds the five data-truth judgments of release #54 (m446) as
// PURE functions over plain bar slices, so each one is pinned standalone on real
// data and the integrator wires them into internal/datatruth without this
// package importing trader/ or kernel/ (the CME calendar is injected as OpenFn).
//
// The five checks, each its own datatruth check name (see register.go):
//
//	bad_print        a 1m bar whose range / jump is far outside normal and is
//	                 confirmed by neither ES, volume nor the next bar
//	cross_frame      NT8's stored 3m/5m/15m must equal the same span built from 1m
//	indicator_lookback  every indicator the mentor evaluator / AI path reads is
//	                 computed from its full bar count with no (material) hole
//	ingest_loss      a FINAL or HISTORICAL bar the ingest queue dropped is data
//	                 loss until the affected minutes are verified present
//	contract_roll    two contracts in one decision window, or a live contract that
//	                 differs from the stored history contract
package checks

import "sort"

const minuteMs = 60_000

// Bar is one OHLCV bar, open-stamped (T = open time, ms UTC).
type Bar struct {
	T          int64
	O, H, L, C float64
	V          float64
	Final      bool
}

// OpenFn reports whether the exchange is open at ms (UTC). nil = always open.
type OpenFn func(ms int64) bool

func (f OpenFn) open(ms int64) bool {
	if f == nil {
		return true
	}
	return f(ms)
}

// tailFrom returns the suffix of the ascending slice with T >= from (a binary
// search, no copy): the checks only ever need the decision window and a short
// lookback, never the whole ring.
func tailFrom(bars []Bar, from int64) []Bar {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].T >= from })
	return bars[i:]
}

// findT looks one open time up in an ascending slice.
func findT(bars []Bar, t int64) (Bar, bool) {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].T >= t })
	if i < len(bars) && bars[i].T == t {
		return bars[i], true
	}
	return Bar{}, false
}

func byT(bars []Bar) map[int64]Bar {
	m := make(map[int64]Bar, len(bars))
	for _, b := range bars {
		m[b.T] = b
	}
	return m
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
