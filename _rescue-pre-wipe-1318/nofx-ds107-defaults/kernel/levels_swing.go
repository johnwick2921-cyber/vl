package kernel

import (
	"time"

	"nofx/market"
)

// Level-truth wave (2026-08-27) — SWING-POINT DETECTOR (T3).
//
// The day-plan map is prior-day/overnight-anchored; the 2026-08-27 verification
// measured 43% of 5m fractal swing turns (the real intraday turning points) with
// NO seated level within ±4 points (worst session: 08-26 NY, 94%). This detector
// closes that gap: the structure engine's own fractal-swing algorithm (k =
// structureSwingK, min-move = structureMinSwingATR × ATR — the SAME knobs the
// G1/G4/G8 structure lines use) is re-run on the 5m and 15m series and the
// RECENT confirmed swings are emitted as line levels:
//
//	SWG-H·5m / SWG-L·5m / SWG-H·15m / SWG-L·15m
//
// Evidence is anchor-class (typeEvidence 0.85), freshness decays on the anchor
// ladder (freshMult 1.0/0.8/0.6/0.5), role = react_zone. Pure + deterministic:
// input is the same closed-bar slice the rest of the assembly already uses.

// SwingPointLookbackBars bounds how far back a swing may sit to still be "recent"
// (5m: 144 bars = 12h; 15m: 96 bars = 24h).
const (
	Swing5mLookbackBars  = 144
	Swing15mLookbackBars = 96
	// SwingPointsPerTFSide caps the emitted count per TF per side (the newest
	// swings win; the seats are scarce and old swings are re-anchored daily).
	SwingPointsPerTFSide = 3
)

// SwingPointLevels runs the structure swing detector on the 5m and 15m series
// aggregated from the passed bars (typically the 1m slice) and emits the recent
// swing highs/lows as DetectedLevel lines. Empty input → nothing.
func SwingPointLevels(bars []market.Kline, now time.Time) []DetectedLevel {
	if len(bars) == 0 {
		return nil
	}
	var out []DetectedLevel
	for _, tfMin := range []int{5, 15} {
		agg := aggregateBars(bars, tfMin)
		if len(agg) < 2*structureSwingK()+1 {
			continue
		}
		out = append(out, swingPointsFor(agg, tfMin, now)...)
	}
	return out
}

// swingPointsFor is the per-TF swing emission: fractal extremes with the
// structure engine's significance filter, newest-first within the lookback.
func swingPointsFor(agg []market.Kline, tfMin int, now time.Time) []DetectedLevel {
	iv := int64(tfMin) * 60_000
	closed := make([]market.Kline, 0, len(agg))
	for _, b := range agg {
		if b.CloseTime >= now.UnixMilli() {
			continue
		}
		closed = append(closed, b)
	}
	n := len(closed)
	if n < 2*structureSwingK()+1 {
		return nil
	}
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i, b := range closed {
		highs[i] = b.High
		lows[i] = b.Low
		closes[i] = b.Close
	}
	atr := simpleATR14(highs, lows, closes)
	if atr <= 0 {
		return nil
	}
	k := structureSwingK()
	type pt struct {
		high        bool
		price       float64
		timeMs      int64
		confirmedMs int64 // recording only; pivot selection continues to use timeMs
	}
	swings := make([]pt, 0, 16)
	for i := k; i < n-k; i++ {
		isHigh, isLow := true, true
		for j := i - k; j <= i+k; j++ {
			if j == i {
				continue
			}
			if highs[j] >= highs[i] {
				isHigh = false
			}
			if lows[j] <= lows[i] {
				isLow = false
			}
		}
		if !isHigh && !isLow {
			continue
		}
		price, hi := highs[i], true
		if isLow {
			price, hi = lows[i], false
		}
		t := closed[i].OpenTime + iv // bar CLOSE instant (repo convention)
		if len(swings) > 0 && swings[len(swings)-1].high == hi {
			if (hi && price <= swings[len(swings)-1].price) || (!hi && price >= swings[len(swings)-1].price) {
				continue
			}
			swings[len(swings)-1] = pt{price: price, timeMs: t, high: hi, confirmedMs: closed[i+k].CloseTime}
			continue
		}
		if len(swings) > 0 {
			move := price - swings[len(swings)-1].price
			if move < 0 {
				move = -move
			}
			if move < structureMinSwingATR()*atr {
				continue
			}
		}
		swings = append(swings, pt{price: price, timeMs: t, high: hi, confirmedMs: closed[i+k].CloseTime})
	}
	if len(swings) == 0 {
		return nil
	}
	// Recent only: within the lookback, newest first.
	cutoff := now.UnixMilli() - int64(lookbackFor(tfMin))*iv
	day := CMESessionDayKey(now)
	var out []DetectedLevel
	addedH, addedL := 0, 0
	for i := len(swings) - 1; i >= 0 && (addedH < SwingPointsPerTFSide || addedL < SwingPointsPerTFSide); i-- {
		s := swings[i]
		if s.timeMs < cutoff {
			continue
		}
		kind := KindSWGH
		label := "SWG-H·"
		if !s.high {
			kind = KindSWGL
			label = "SWG-L·"
		}
		if s.high {
			if addedH >= SwingPointsPerTFSide {
				continue
			}
			addedH++
		} else {
			if addedL >= SwingPointsPerTFSide {
				continue
			}
			addedL++
		}
		out = append(out, DetectedLevel{
			Kind:       kind,
			Price:      s.price,
			Lo:         s.price,
			Hi:         s.price,
			Label:      label + tfName(tfMin),
			OriginDate: day,
			TF:         tfName(tfMin),
		})
		out[len(out)-1] = WithFormationClose(out[len(out)-1], s.confirmedMs, len(closed), "pivot_confirmation_close", now)
		// Presentation evidence from the exact selected pivot, after selection.
		for _, bar := range closed {
			if bar.OpenTime == s.timeMs {
				wick := ZonePivotWick(bar, s.high)
				out[len(out)-1].ZoneDefiningWick = &wick
				break
			}
		}
	}
	return out
}

func lookbackFor(tfMin int) int {
	if tfMin >= 15 {
		return Swing15mLookbackBars
	}
	return Swing5mLookbackBars
}

func tfName(tfMin int) string {
	if tfMin >= 15 {
		return "15m"
	}
	return "5m"
}

// aggregateBars rolls the passed series (assumed 1m, ascending) into tfMin bars
// keyed by OpenTime = start of the tfMin bucket. The closed-bar convention is
// preserved downstream (callers filter by CloseTime).
func aggregateBars(bars []market.Kline, tfMin int) []market.Kline {
	bucket := int64(tfMin) * 60_000
	type agg struct{ o, h, l, c, v float64 }
	m := map[int64]*agg{}
	var order []int64
	for _, b := range bars {
		key := b.OpenTime - b.OpenTime%bucket
		a, ok := m[key]
		if !ok {
			a = &agg{o: b.Open, h: b.High, l: b.Low, c: b.Close}
			m[key] = a
			order = append(order, key)
			continue
		}
		if b.High > a.h {
			a.h = b.High
		}
		if b.Low < a.l {
			a.l = b.Low
		}
		a.c = b.Close
		a.v += b.Volume
	}
	out := make([]market.Kline, 0, len(order))
	for _, key := range order {
		a := m[key]
		out = append(out, market.Kline{
			OpenTime: key,
			Open:     a.o, High: a.h, Low: a.l, Close: a.c, Volume: a.v,
			// CloseTime = OpenTime + interval (repo convention — the last
			// instant INSIDE the bar).
			CloseTime: key + bucket,
		})
	}
	return out
}
