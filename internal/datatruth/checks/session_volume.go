package checks

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// ── CHECK 3b: SESSION-ANCHORED VOLUME LEVELS ─────────────────────────────────
//
// What the kernel reads (derived 2026-10-29 from kernel/levels_volume.go, not
// assumed), every one of them a SUM over a whole session's closed 1m bars:
//
//	SessionVWAPLevels  VWAP and the +-1/+-2 sigma bands, anchored 17:00 CT
//	EVWAPLevels        VWAP anchored at the last 15:00 CT cash close
//	PDVWAPLevels       the prior session's VWAP
//	PriorDayProfile    the prior session's POC / VAH / VAL (1.25-pt rows,
//	                   70% value area) - and NakedPOC, which is that same
//	                   profile per prior session
//	MIDOLevels         (overnight high + low) / 2
//
// Unlike an EMA, which forgets, a hole or a volume-truncated bar skews these for
// the WHOLE session: the 10-26 1m hole (17:02-22:37 CT) moved the session VWAP
// -71 pt at 6 h into the session and still -63 pt at 12 h (the 5m, which has
// the span, gives the true value). A decay hold cannot model that, so these are
// judged by RECOMPUTING: the 1m the kernel reads against the same span with the
// missing volume restored from the independent 5m frame, and blocked when the
// level moves by more than a tolerance.
//
// Per 5m slot the missing volume is max(0, V5 - sum of the 1m volumes in it),
// priced at the 5m bar's typical price (H+L+C)/3 and, for the profile, spread
// over the 5m bar's [L, H]. A slot the 5m also lacks is priced from the 1m bars
// it does have, or - with none at all - from the session's median slot volume
// at the last known price. A slot that cannot be priced at all blocks.

// SessionVolParams are the tolerances (points).
type SessionVolParams struct {
	VWAPTolPts    float64 // |VWAP(1m) - VWAP(restored)|
	SigmaTolPts   float64 // |sigma(1m) - sigma(restored)|; the +-2 sigma band moves twice this
	ProfileTolPts float64 // |POC|, |VAH|, |VAL| shifts (profile rows are 1.25 pt)
	// MinMissShare is the share of the session volume below which the profile
	// is not recomputed at all: a profile is discontinuous (a near-tie POC can
	// flip rows), so a rounding-sized restoration must not be able to flip it.
	MinMissShare float64
}

// DefaultSessionVol: 2 pt matches the review's tolerance and sits ~6x above the
// worst clean-session shift measured on MNQ 12-26 10-12..10-27 (0.68 pt).
func DefaultSessionVol() SessionVolParams {
	return SessionVolParams{VWAPTolPts: 2.0, SigmaTolPts: 2.0, ProfileTolPts: 2.5, MinMissShare: 0.005}
}

// SessionVolVerdict is the measured result of one window.
type SessionVolVerdict struct {
	VWAP, VWAPRestored   float64
	Sigma, SigmaRestored float64
	POC, VAH, VAL        float64 // 1m profile (zero when not computed)
	POCRestored          float64
	VAHRestored          float64
	VALRestored          float64
	MissShare            float64 // restored volume / total volume
	Slots, Unpriced      int
	Blocked              bool
	Why                  string
}

// svp mirrors kernel/svp.go (SVPRowHeight 1.25 pt, SVPValueAreaPercent 0.70,
// svpAddBar, svpValueArea). A pin in provider/ninjatrader compares this against
// kernel.PriorDayProfileLevels on a real session so the two cannot drift.
const (
	svpRowHeight    = 0.25 * 5
	svpValueAreaPct = 0.70
)

func svpBin(p float64) int { return int(math.Floor(p / svpRowHeight)) }

func svpAdd(hist map[int]float64, low, high, vol float64) {
	if high < low {
		low, high = high, low
	}
	if high == low {
		hist[svpBin(low)] += vol
		return
	}
	span := high - low
	for b := svpBin(low); b <= svpBin(high); b++ {
		bl := float64(b) * svpRowHeight
		ov := math.Min(high, bl+svpRowHeight) - math.Max(low, bl)
		if ov <= 0 {
			continue
		}
		hist[b] += vol * (ov / span)
	}
}

// SVPProfile returns POC (row midpoint), VAH (top edge of the top VA row) and
// VAL (bottom edge of the bottom VA row) exactly as kernel.profileLevels does.
func SVPProfile(hist map[int]float64) (poc, vah, val float64, ok bool) {
	if len(hist) == 0 {
		return 0, 0, 0, false
	}
	minB, maxB := math.MaxInt, math.MinInt
	total := 0.0
	for b, v := range hist {
		total += v
		if b < minB {
			minB = b
		}
		if b > maxB {
			maxB = b
		}
	}
	if total <= 0 {
		return 0, 0, 0, false
	}
	maxVol := math.Inf(-1)
	for _, v := range hist {
		if v > maxVol {
			maxVol = v
		}
	}
	mid := float64(minB+maxB) / 2.0
	pocB, bestDist := minB, math.Inf(1)
	for b := minB; b <= maxB; b++ {
		if hist[b] != maxVol {
			continue
		}
		if d := math.Abs(float64(b) - mid); d < bestDist {
			bestDist, pocB = d, b
		}
	}
	lo, hi := pocB, pocB
	vol := hist[pocB]
	target := total * svpValueAreaPct
	for vol < target {
		aboveAvail, belowAvail := hi+1 <= maxB, lo-1 >= minB
		if !aboveAvail && !belowAvail {
			break
		}
		above, below := hist[hi+1]+hist[hi+2], hist[lo-1]+hist[lo-2]
		var up bool
		switch {
		case !belowAvail:
			up = true
		case !aboveAvail:
			up = false
		case above > below:
			up = true
		case below > above:
			up = false
		default:
			up = true
		}
		if up {
			hi++
			vol += hist[hi]
			if hi+1 <= maxB {
				hi++
				vol += hist[hi]
			}
		} else {
			lo--
			vol += hist[lo]
			if lo-1 >= minB {
				lo--
				vol += hist[lo]
			}
		}
	}
	return (float64(pocB) + 0.5) * svpRowHeight, float64(hi+1) * svpRowHeight, float64(lo) * svpRowHeight, true
}

type volAcc struct{ v, pv, pv2 float64 }

func (a *volAcc) add(vol, tp float64) {
	a.v += vol
	a.pv += vol * tp
	a.pv2 += vol * tp * tp
}

func (a volAcc) stats() (vwap, sigma float64) {
	if a.v <= 0 {
		return 0, 0
	}
	vwap = a.pv / a.v
	v := a.pv2/a.v - vwap*vwap
	if v < 0 {
		v = 0
	}
	return vwap, math.Sqrt(v)
}

func typical(b Bar) float64 { return (b.H + b.L + b.C) / 3 }

// JudgeSessionVolume judges one window [fromMs, toMs) of closed slots (a slot is
// judged only when it ended at or before now). useSigma judges the sigma bands
// (the current session's VWAP bands and eVWAP read them; the prior-session
// pdVWAP does not); profile judges POC/VAH/VAL.
func JudgeSessionVolume(name string, m1, m5 []Bar, fromMs, toMs int64, now time.Time, open OpenFn, p SessionVolParams, useSigma, profile bool) SessionVolVerdict {
	const slot = 5 * minuteMs
	var v SessionVolVerdict
	nowMs := now.UnixMilli()
	end := toMs
	// a slot is judged one minute after it ends: the 1m and 5m of the same
	// close land within seconds of each other, and a lag of that size must not
	// read as a missing minute at the thin start of a window.
	if lim := (nowMs - minuteMs) / slot * slot; lim < end {
		end = lim
	}
	start := (fromMs + slot - 1) / slot * slot
	if end <= start {
		return v
	}
	m1w := windowOf(m1, start, end)
	m5w := windowOf(m5, start, end)
	// median 5m slot volume: prices a slot neither frame holds
	var vols []float64
	for _, b := range m5w {
		if b.V > 0 {
			vols = append(vols, b.V)
		}
	}
	medSlot := median(vols)

	var base, full volAcc
	var histBase, histFull map[int]float64
	if profile {
		histBase, histFull = map[int]float64{}, map[int]float64{}
	}
	var restored float64
	lastTP := 0.0
	i1 := 0
	var firstUnpriced int64
	for t := start; t < end; t += slot {
		if !open.open(t) {
			continue
		}
		for i1 < len(m1w) && m1w[i1].T < t {
			i1++
		}
		var n1 int
		var v1, tpSum float64
		for j := i1; j < len(m1w) && m1w[j].T < t+slot; j++ {
			b := m1w[j]
			tp := typical(b)
			base.add(b.V, tp)
			full.add(b.V, tp)
			if profile {
				svpAdd(histBase, b.L, b.H, b.V)
				svpAdd(histFull, b.L, b.H, b.V)
			}
			n1++
			v1 += b.V
			tpSum += tp
			lastTP = tp
		}
		nopen := 0
		for k := int64(0); k < 5; k++ {
			if open.open(t + k*minuteMs) {
				nopen++
			}
		}
		v.Slots++
		var miss, tp, lo, hi float64
		if b5, ok := findT(m5w, t); ok {
			miss = b5.V - v1
			tp, lo, hi = typical(b5), b5.L, b5.H
		} else if n1 >= nopen {
			continue // 1m complete, the 5m just is not stored: nothing to restore
		} else if n1 > 0 {
			miss = v1 / float64(n1) * float64(nopen-n1)
			tp = tpSum / float64(n1)
			lo, hi = tp, tp
		} else if medSlot > 0 && lastTP > 0 {
			miss = medSlot
			tp = lastTP
			lo, hi = tp, tp
		} else {
			v.Unpriced++
			if firstUnpriced == 0 {
				firstUnpriced = t
			}
			continue
		}
		if miss <= 0.5 {
			continue
		}
		restored += miss
		full.add(miss, tp)
		if profile {
			svpAdd(histFull, lo, hi, miss)
		}
	}
	v.VWAP, v.Sigma = base.stats()
	v.VWAPRestored, v.SigmaRestored = full.stats()
	if full.v > 0 {
		v.MissShare = restored / full.v
	}
	if v.Unpriced > 0 {
		v.Blocked = true
		v.Why = fmt.Sprintf("%s: %d open 5m slot(s) (first %s) exist in neither the 1m nor the 5m frame and cannot be priced - the volume levels cannot be verified",
			name, v.Unpriced, time.UnixMilli(firstUnpriced).UTC().Format("01-02 15:04Z"))
		return v
	}
	if v.Slots > 0 && base.v <= 0 && full.v <= 0 {
		v.Blocked = true
		v.Why = fmt.Sprintf("%s: %d open 5m slot(s) and no volume in either frame - the volume levels cannot be verified", name, v.Slots)
		return v
	}
	var why []string
	if d := abs(v.VWAP - v.VWAPRestored); d > p.VWAPTolPts {
		why = append(why, fmt.Sprintf("VWAP %.2f (1m) vs %.2f restored from the 5m (%.1f pt > %.1f pt)", v.VWAP, v.VWAPRestored, d, p.VWAPTolPts))
	}
	if useSigma {
		if d := abs(v.Sigma - v.SigmaRestored); d > p.SigmaTolPts {
			why = append(why, fmt.Sprintf("sigma %.2f vs %.2f (%.1f pt > %.1f pt)", v.Sigma, v.SigmaRestored, d, p.SigmaTolPts))
		}
	}
	if profile && v.MissShare >= p.MinMissShare {
		poc, vah, val, ok1 := SVPProfile(histBase)
		pocR, vahR, valR, ok2 := SVPProfile(histFull)
		if ok1 && ok2 {
			v.POC, v.VAH, v.VAL = poc, vah, val
			v.POCRestored, v.VAHRestored, v.VALRestored = pocR, vahR, valR
			for _, c := range []struct {
				n    string
				a, b float64
			}{{"POC", poc, pocR}, {"VAH", vah, vahR}, {"VAL", val, valR}} {
				if d := abs(c.a - c.b); d > p.ProfileTolPts {
					why = append(why, fmt.Sprintf("%s %.2f (1m) vs %.2f restored (%.1f pt > %.1f pt)", c.n, c.a, c.b, d, p.ProfileTolPts))
				}
			}
		}
	}
	if len(why) > 0 {
		v.Blocked = true
		v.Why = fmt.Sprintf("%s: %.1f%% of the window's volume is missing from the 1m: %s", name, 100*v.MissShare, joinWhy(why))
	}
	return v
}

func joinWhy(w []string) string {
	s := ""
	for i, x := range w {
		if i > 0 {
			s += "; "
		}
		s += x
	}
	return s
}

func windowOf(bars []Bar, from, to int64) []Bar {
	lo := sort.Search(len(bars), func(i int) bool { return bars[i].T >= from })
	hi := sort.Search(len(bars), func(i int) bool { return bars[i].T >= to })
	return bars[lo:hi]
}

// silentSlots is how many of the newest closed 5m slots must hold at least one
// stored bar (either frame) while the exchange is open.
const silentSlots = 2

// storeSilent reports an OPEN market with no stored 1m or 5m bar in the last
// silentSlots closed 5m slots (the same one-minute grace as JudgeSessionVolume).
// It judges nothing unless every minute of that window was open, so the CME
// break, the weekend and the first ten minutes after a reopen are never read as
// silence: a check pauses entries only on a state it can positively call
// missing, never on the calendar.
func storeSilent(m1, m5 []Bar, now time.Time, open OpenFn) (bool, string) {
	const slot = 5 * minuteMs
	nowMs := now.UnixMilli()
	lim := (nowMs - minuteMs) / slot * slot
	w0 := lim - silentSlots*slot
	for t := w0; t < lim; t += minuteMs {
		if !open.open(t) {
			return false, ""
		}
	}
	if len(windowOf(m1, w0, nowMs+1)) > 0 || len(windowOf(m5, w0, nowMs+1)) > 0 {
		return false, ""
	}
	return true, fmt.Sprintf("cannot judge the volume levels: no stored 1m or 5m bar between %s and %s UTC while the market is open (store empty or stale)",
		time.UnixMilli(w0).UTC().Format("01-02 15:04"), now.UTC().Format("15:04"))
}

// JudgeSessionLevels judges every session-anchored volume level at `now`:
// the current session (VWAP + bands), eVWAP (since the last 15:00 CT cash
// close), the prior open session (pdVWAP + profile, hence nPOC) and the
// overnight range (MID-O). Windows with no bar in either frame are skipped
// exactly as the levels the kernel emits are (nothing to draw, nothing to
// verify); a window with bars in only one frame is judged against what the
// other frame lacks. m1/m5 must reach back at least to the eVWAP anchor and the
// prior session; a shorter history that hides the prior session reads as "no
// prior session" - the same thing the kernel then sees.
func JudgeSessionLevels(m1, m5 []Bar, now time.Time, loc *time.Location, open OpenFn, p SessionVolParams, midoTol float64) (blocked bool, why string) {
	// FAIL CLOSED when there is nothing to judge (review f7a round 3, D-C): every
	// window below is skipped when it holds no bar, so an empty or stale store
	// read with err == nil (the December roll, where the store is keyed to a
	// contract with no rows yet) would otherwise read as CLEAN.
	if silent, w := storeSilent(m1, m5, now, open); silent {
		return true, w
	}
	t := now.In(loc)
	cur := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, loc)
	if t.Before(cur) {
		cur = cur.AddDate(0, 0, -1)
	}
	var out []string
	add := func(v SessionVolVerdict) {
		if v.Blocked {
			out = append(out, v.Why)
		}
	}
	// 1. the current session: VWAP and the sigma bands
	if hasBars(m1, cur.UnixMilli(), now.UnixMilli()) || hasBars(m5, cur.UnixMilli(), now.UnixMilli()) {
		add(JudgeSessionVolume("session VWAP", m1, m5, cur.UnixMilli(), cur.AddDate(0, 0, 1).UnixMilli(), now, open, p, true, false))
	}
	// 2. eVWAP: since the most recent 15:00 CT cash close that traded
	anchor := time.Date(t.Year(), t.Month(), t.Day(), 15, 0, 0, 0, loc)
	if t.Hour() < 15 {
		anchor = anchor.AddDate(0, 0, -1)
	}
	for step := 0; step < 7; step++ {
		cand := anchor.AddDate(0, 0, -step)
		if hasBars(m1, cand.UnixMilli(), cand.Add(time.Hour).UnixMilli()) || hasBars(m5, cand.UnixMilli(), cand.Add(time.Hour).UnixMilli()) {
			anchor = cand
			break
		}
	}
	if hasBars(m1, anchor.UnixMilli(), now.UnixMilli()) || hasBars(m5, anchor.UnixMilli(), now.UnixMilli()) {
		add(JudgeSessionVolume("eVWAP", m1, m5, anchor.UnixMilli(), now.UnixMilli()+1, now, open, p, false, false))
	}
	// 3. the prior open session: pdVWAP and the profile (POC/VAH/VAL, nPOC)
	to := cur
	for i := 0; i < 5; i++ {
		from := to.AddDate(0, 0, -1)
		if hasBars(m1, from.UnixMilli(), to.UnixMilli()) || hasBars(m5, from.UnixMilli(), to.UnixMilli()) {
			add(JudgeSessionVolume("pdVWAP/profile of session "+from.Format("01-02"), m1, m5, from.UnixMilli(), to.UnixMilli(), now, open, p, false, true))
			break
		}
		to = from
	}
	// 4. MID-O: the overnight range of the current session (17:00 -> 08:30 CT)
	cut := time.Date(cur.Year(), cur.Month(), cur.Day()+1, 8, 30, 0, 0, loc)
	hiEnd := cut.UnixMilli()
	// one slot of grace: the 5m of the newest closed slot may not be stored yet
	if e := (now.UnixMilli()-minuteMs)/(5*minuteMs)*(5*minuteMs) - 5*minuteMs; e < hiEnd {
		hiEnd = e
	}
	if hiEnd > cur.UnixMilli() && (hasBars(m1, cur.UnixMilli(), hiEnd) || hasBars(m5, cur.UnixMilli(), hiEnd)) {
		if b, w := SessionExtremes(m1, m5, cur.UnixMilli(), hiEnd, midoTol); b {
			out = append(out, "MID-O overnight range: "+w)
		}
	}
	if len(out) == 0 {
		return false, ""
	}
	return true, joinWhy(out)
}

// ProfileOf is the prior-day profile (POC, VAH, VAL) of a closed 1m series with
// the kernel's definition (kernel.PriorDayProfileLevels); exported so a pin in a
// package that may import the kernel can hold the mirror to it.
func ProfileOf(bars []Bar) (poc, vah, val float64, ok bool) {
	h := map[int]float64{}
	for _, b := range bars {
		svpAdd(h, b.L, b.H, b.V)
	}
	return SVPProfile(h)
}
