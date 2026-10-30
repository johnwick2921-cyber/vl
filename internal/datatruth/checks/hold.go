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
// tick-skew holds only until the bar stops being the newest of its frame. A MENTOR
// trader additionally reads raw 1m highs and lows (mentor_range_1m, Extreme):
// a wrong high/low holds it for MentorRangeReadMin, not for the close-error's
// decay. KNOWN LIMIT (also in the guide): the AI path's own raw high/low ranges
// (PDH/PDL, 1h ranges) are judged separately by the swing and levels checks, the
// rest is not covered.

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
	if s.Extreme {
		// a raw max / min: the error enters at full weight for the whole window
		if errPts <= tol {
			return 0
		}
		return s.Period
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
// the bar is part of (tfs; nil = every spec). Capped at HoldCapMin. It charges
// ONE number to every spec (the pre-#55 model, kept for the pins that price a
// single error); the production holds go through HoldMinutesFor.
func HoldMinutes(specs []SeriesSpec, errPts float64, tfs ...int) int {
	best := 0
	for _, s := range specs {
		if s.PresenceOnly {
			continue
		}
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

// BarError is what a wrong bar did, split by what an indicator reads (release
// #55 review F1: the first model charged one number - the whole high/low
// excursion - to EVERY indicator, so a 163 pt wick that closes back at its open
// was priced as a 163 pt close error against EMA/RSI/MACD and held for 4 hours).
//
//	CloseErr  how far the bar's CLOSE is wrong: close-reading indicators (EMA,
//	          RSI, MACD) see this and nothing else. For a 1m bad print it is
//	          |close - next bar's open| (a real tape opens where it closed; the
//	          newest bar has no next bar yet, so it is charged the full range).
//	RangeErr  how far the bar's HIGH/LOW is wrong: range indicators (ATR) see it.
//	T         open time (ms) of the bad 1m bar, 0 = unknown. A 1m close changes a
//	          3m/5m/15m close only when it is the LAST minute of that bucket.
//	Frame     true = the error is on a bar of the spec's OWN frame (a cross-frame
//	          mismatch): T is irrelevant, the close error always reaches it.
type BarError struct {
	T                  int64
	CloseErr, RangeErr float64
	Frame              bool
}

// errFor is what spec s sees of e.
func (e BarError) errFor(s SeriesSpec) float64 {
	if s.Extreme {
		return e.RangeErr // only the bar's high/low enters a max / min
	}
	if s.Range {
		if e.CloseErr > e.RangeErr {
			return e.CloseErr // a wrong close also moves the next bar's true range
		}
		return e.RangeErr
	}
	if e.Frame || s.TFMin <= 1 || e.T <= 0 {
		return e.CloseErr
	}
	if (e.T+minuteMs)%(int64(s.TFMin)*minuteMs) == 0 {
		return e.CloseErr // the last minute of the bucket: its close IS the bucket's close
	}
	return 0
}

// readBy reports whether reader's decisions read the indicator s ("" = all).
func (s SeriesSpec) readBy(reader string) bool {
	return reader == "" || s.Reader == "" || s.Reader == reader
}

// HoldMinutesFor is how long a bar wrong by e must keep entries paused FOR ONE
// READER: the longest decay over the specs that reader's decisions read, each
// charged only the part of the error it reads (BarError). reader "" = every
// spec. tfs, when given, restricts to those frames. Capped at HoldCapMin.
func HoldMinutesFor(specs []SeriesSpec, reader string, e BarError, tfs ...int) int {
	best := 0
	for _, s := range specs {
		if !s.readBy(reader) || s.PresenceOnly {
			continue
		}
		if len(tfs) > 0 {
			in := false
			for _, tf := range tfs {
				in = in || tf == s.TFMin
			}
			if !in {
				continue
			}
		}
		if n := decayBars(s, e.errFor(s)); n > 0 {
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

// WithHold returns p with the production hold: a suspect bar keeps each reader's
// entries paused for as long as it moves any indicator THAT READER reads by more
// than its tolerance.
func (p BadPrintParams) WithHold(specs []SeriesSpec) BadPrintParams {
	if p.Hold == nil {
		p.Hold = func(reader string, e BarError) int { return HoldMinutesFor(specs, reader, e) }
	}
	return p
}

// WithHold returns p with the production hold: a mismatching HTF bar keeps each
// reader's entries paused for as long as it moves the indicators (specs) that
// reader reads on that frame. A frame no spec reads (the 15m trigger bars) holds
// only for its own length.
func (p CrossParams) WithHold(specs []SeriesSpec) CrossParams {
	if p.HoldFor == nil {
		p.HoldFor = func(reader string, tfMin int, e BarError) int { return HoldMinutesFor(specs, reader, e, tfMin) }
	}
	return p
}
