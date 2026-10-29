package checks

import "math"

// ── HOW LONG A KNOWN-WRONG BAR KEEPS THE PAUSE (review round 1, D4) ──────────
//
// The first draft cleared a bad_print / cross_frame pause by AGE (30 / 12
// minutes after the bar) while the wrong bar was still inside the lookbacks the
// indicators read for hours. A pause that ends because time passed is not a pause
// that ends because the data is right. The honest clearing conditions are:
//
//  1. the bar is REPLACED or REPAIRED (the check is re-run on every Evaluate, so
//     a re-requested bar that now agrees clears at once), or
//  2. the bar's remaining INFLUENCE on every indicator that reads it has decayed
//     below that indicator's own tolerance - the same pricing JudgeSeries uses
//     for a hole: a bar `a` slots old carries weight k(1-k)^a in an EMA and 1/n
//     in an n-bar window, so an error of e points moves the indicator by
//     weight x e, and the pause holds until that is <= the tolerance.
//
// A 56 pt wrong 5m low (10-27 08:30, row 2045884) holds ~100 minutes; a 0.75 pt
// tick-skew holds only until the bar stops being the newest of its frame. KNOWN
// LIMIT (also in the guide): a bar older than that is still in the raw
// high/low ranges of longer windows (PDH/PDL, 1h ranges); the swing and levels
// checks judge those separately, the rest is not covered.

// HoldCapMin bounds any hold: no single bar pauses entries longer than 8 hours.
const HoldCapMin = 480

// ImpactTolPts is the tolerance used for indicators that carry none in points
// (ATR / RSI / MACD): a bar whose error moves them by more than 2 pt still
// matters. 2 pt is ~10% of a typical 3m/5m MNQ ATR, the same share of the
// indicator's mass JudgeSeries tolerates for a missing bar (MaxWeight 0.10).
const ImpactTolPts = 2.0

// decayBars is how many slots after the bad one the indicator still moves by
// more than its tolerance because of an error of errPts (0 = never).
func decayBars(s SeriesSpec, errPts float64) int {
	if errPts <= 0 || s.Period <= 0 {
		return 0
	}
	tol := ImpactTolPts
	if s.PriceUnits && s.TolPts > 0 {
		tol = s.TolPts
	}
	if s.EMA {
		k := 2.0 / float64(s.Period+1)
		w := k * errPts
		if w <= tol {
			return 0
		}
		return int(math.Ceil(math.Log(tol/w) / math.Log(1-k)))
	}
	if errPts/float64(s.Period) <= tol {
		return 0
	}
	return s.Period
}

// HoldMinutes is how long (minutes, from the bad bar's open) a bar wrong by
// errPts must keep entries paused: the longest decay over every spec whose frame
// the bar is part of (tfs; nil = every spec). Capped at HoldCapMin.
func HoldMinutes(specs []SeriesSpec, errPts float64, tfs ...int) int {
	best := 0
	for _, s := range specs {
		if len(tfs) > 0 {
			in := false
			for _, tf := range tfs {
				in = in || tf == s.TFMin
			}
			if !in {
				continue
			}
		}
		if n := decayBars(s, errPts); n > 0 {
			if m := (n + 1) * s.TFMin; m > best {
				best = m
			}
		}
	}
	if best > HoldCapMin {
		best = HoldCapMin
	}
	return best
}

// WithHold returns p with the production hold: a suspect bar keeps entries
// paused for as long as it moves any of specs by more than its tolerance.
func (p BadPrintParams) WithHold(specs []SeriesSpec) BadPrintParams {
	if p.Hold == nil {
		p.Hold = func(x float64) int { return HoldMinutes(specs, x) }
	}
	return p
}

// WithHold returns p with the production hold: a mismatching HTF bar keeps
// entries paused for as long as it moves the indicators (specs) that read that
// frame. A frame no spec reads (the 15m trigger bars) holds only for its own
// length.
func (p CrossParams) WithHold(specs []SeriesSpec) CrossParams {
	if p.Hold == nil {
		p.Hold = func(tfMin int, e float64) int { return HoldMinutes(specs, e, tfMin) }
	}
	return p
}
