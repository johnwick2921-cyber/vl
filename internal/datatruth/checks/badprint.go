package checks

import (
	"fmt"
	"time"
)

// ── CHECK 1: BAD-PRICE FILTER ────────────────────────────────────────────────
//
// INVESTIGATION (2026-10-29, data/data.db MNQ 12-26 1m, sqlite3 -readonly):
// the 27 MNQ 1m bars with range > 60 pt in the last 7 days (row ids 2035326,
// 2035332, 2035333, 2035373, 2036589, 2039946, 2040052, 2043121, 2043238,
// 2043289, 2043319, 2052712, 2047675, 2054164, 2054390, 2054496, 2054522,
// 2054531, 2054544, 2054555, 2054566, 2054597, 2054782, 2054789, 2054791,
// 2054798, 2054803) are ALL REAL: each sits on a data-release / FOMC / open
// minute (08:30, 09:00, 13:00-15:05 CT), carries 4-18x the prior-30-minute
// median volume, ES prints the same direction in the same minute (scaled
// excursion 0.27-1.0 of the MNQ excursion) and the next bar opens at this bar's
// close. NONE is a bad print. The two real bad prints in the store are OUTSIDE
// that 7-day window (10-19 CT) and were found by the same scan:
//
//	row 2020662  MNQ 10-19 08:58  o 29702.25 h 29781.0 l 29698.75 c 29774.5 v 2907
//	             the next bar opens 29705.25 (a -69 pt gap from this close); ES flat
//	             (3.25 pt range = 12.5 MNQ pt-equivalents, 0.15 of the 82 pt range)
//	row 2020732  MNQ 10-19 09:19  o 29850.75 h 29855.0 l 29687.75 c 29833.0 v 16002
//	             a 163 pt wick that closes back at the open; ES range 3.5 (13.4
//	             pt-equivalents, 0.08 of 167); volume only 1.7x normal
//
// Both are stored `live`, so no replay can overwrite them (the upsert rule
// protects live rows) and they sit in every ATR / swing / level read of that day.
//
// THE RULE (tuned on 485 extreme bars of MNQ 12-26 + 09-26 that ES covers; it
// flags exactly those two and nothing else):
//
//	EXTREME  X = max(range, |h-prevClose|, |l-prevClose|) >= max(MinPts,
//	         ATRMult x median range of the prior 30 minutes)
//	CONFIRMED (any one clears the bar):
//	  E  ES moved >= ESRatio (0.20) of X in the same minute (ES excursion scaled
//	     by the price ratio) - measured real minimum on confirmed news bars 0.27
//	  V  ES moved >= ESRatioVol (0.10) of X AND volume >= VolMult (3.5) x the
//	     prior-30 median - a genuine fast tape shows both
//	  C  the displacement continued: |c-prevClose| >= 0.6 X and the next bar
//	     opens within max(8, 0.15 |c-prevClose|) of this close
//	D  (overrides E and V) the displacement is >= 0.6 X but the next bar opens
//	   more than 0.5 of it away from this close: a real tape opens where it
//	   closed. Needed because a RESTART replays a stale series into BOTH feeds
//	   (10-14 20:00: ES carried the same stale level, so E alone "confirmed" it).
//	   Across 515 extreme bars only 10-19 08:58 trips D.
//	SUSPECT = EXTREME and not CONFIRMED. A missing ES bar cannot confirm by E or
//	V (fail closed); only C can.
//
// A real market move never pauses: every one of the 27 above and every
// FOMC/CPI/NFP bar in the store is CONFIRMED (E, V or C).

// BadPrintParams are the tunables; DefaultBadPrint is the measured set.
type BadPrintParams struct {
	WindowMin    int     // bars younger than this are "inside the decision window"
	MinPts       float64 // floor of the EXTREME bound
	ATRMult      float64 // x median 1m range of the prior ATRLookback minutes
	ATRLookback  int
	ESRatio      float64 // E
	ESRatioVol   float64 // V (ES leg)
	VolMult      float64 // V (volume leg)
	ContFrac     float64 // C: displacement share of X
	ContGapPts   float64 // C: max next-open gap floor
	ContGapFrac  float64 // C: max next-open gap as share of displacement
	DiscontFrac  float64 // D: a next-open gap above this share of the displacement is a discontinuity
	MinPriorBars int     // fewer prior bars than this: ATR leg is not used, MinPts rules
}

// DefaultBadPrint returns the parameters tuned on the real store (see above).
func DefaultBadPrint() BadPrintParams {
	return BadPrintParams{
		WindowMin: 30, MinPts: 40, ATRMult: 2, ATRLookback: 30,
		ESRatio: 0.20, ESRatioVol: 0.10, VolMult: 3.5,
		ContFrac: 0.6, ContGapPts: 8, ContGapFrac: 0.15, DiscontFrac: 0.5, MinPriorBars: 10,
	}
}

// Suspect is one unconfirmed extreme bar.
type Suspect struct {
	T       int64
	X       float64
	ESRatio float64 // -1 when ES had no bar for that minute
	VolRat  float64
	// Discontinuous marks rule D: the next bar opens far from this close.
	Discontinuous bool
	Detail        string
}

// SuspectBars returns the unconfirmed extreme bars of `bars` (1m, ascending)
// whose open time is inside the decision window ending at now. partner is the
// correlated symbol's 1m series (ES for MNQ/NQ); nil/empty means no partner at
// all, so only the continuation leg can confirm.
func SuspectBars(bars, partner []Bar, now time.Time, p BadPrintParams) []Suspect {
	if p.WindowMin <= 0 {
		p = DefaultBadPrint()
	}
	nowMs := now.UnixMilli()
	from := nowMs - int64(p.WindowMin)*minuteMs
	pm := byT(partner)
	var out []Suspect
	for i := range bars {
		b := bars[i]
		if b.T < from || b.T > nowMs {
			continue
		}
		// previous close: the immediately preceding minute, else the last bar
		// within 3 minutes; with none, the jump leg is unavailable (range only).
		pc, havePC := 0.0, false
		if i > 0 && b.T-bars[i-1].T <= 3*minuteMs {
			pc, havePC = bars[i-1].C, true
		}
		var rngs, vols []float64
		for j := i - 1; j >= 0 && len(rngs) < p.ATRLookback && b.T-bars[j].T <= 60*minuteMs; j-- {
			rngs = append(rngs, bars[j].H-bars[j].L)
			vols = append(vols, bars[j].V)
		}
		thr := p.MinPts
		if len(rngs) >= p.MinPriorBars {
			if m := p.ATRMult * median(rngs); m > thr {
				thr = m
			}
		}
		x := b.H - b.L
		if havePC {
			if v := abs(b.H - pc); v > x {
				x = v
			}
			if v := abs(b.L - pc); v > x {
				x = v
			}
		}
		if x < thr {
			continue
		}
		// C / D: the next bar either continues the displacement or undoes it.
		if havePC && i+1 < len(bars) && bars[i+1].T-b.T == minuteMs {
			disp := abs(b.C - pc)
			gap := abs(bars[i+1].O - b.C)
			lim := p.ContGapPts
			if v := p.ContGapFrac * disp; v > lim {
				lim = v
			}
			if disp >= p.ContFrac*x {
				if gap <= lim { // C
					continue
				}
				if gap > p.DiscontFrac*disp { // D: not even a partner can vouch for it
					out = append(out, Suspect{T: b.T, X: x, ESRatio: -1, Discontinuous: true,
						Detail: fmt.Sprintf("1m bar %s: moved %.1f pt to a close the next bar opens %.1f pt away from (a real tape opens where it closed) - bad print, whatever the partner shows",
							time.UnixMilli(b.T).UTC().Format("01-02 15:04Z"), disp, gap)})
					continue
				}
			}
		}
		esr := -1.0
		if e, ok := pm[b.T]; ok && e.C > 0 {
			scale := b.C / e.C
			ex := e.H - e.L
			if pe, ok := pm[b.T-minuteMs]; ok {
				if v := abs(e.H - pe.C); v > ex {
					ex = v
				}
				if v := abs(e.L - pe.C); v > ex {
					ex = v
				}
			}
			esr = ex * scale / x
		}
		vr := 0.0
		if mv := median(vols); mv > 0 {
			vr = b.V / mv
		}
		if esr >= p.ESRatio { // E
			continue
		}
		if esr >= p.ESRatioVol && vr >= p.VolMult { // V
			continue
		}
		out = append(out, Suspect{T: b.T, X: x, ESRatio: esr, VolRat: vr,
			Detail: fmt.Sprintf("1m bar %s: move %.1f pt (bound %.1f), partner confirms %.2f of it (need %.2f), volume %.1fx normal, next bar does not continue it",
				time.UnixMilli(b.T).UTC().Format("01-02 15:04Z"), x, thr, esr, p.ESRatio, vr)})
	}
	return out
}
