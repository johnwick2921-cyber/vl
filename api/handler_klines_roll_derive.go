package api

import (
	"math"
	"os"
	"sort"
	"strings"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// W-ROLL-DAY-CHART (2026-09-19) — the roll stitch trusted the old contract's
// STORED aggregates and shifted nothing: every timeframe of the roll day
// showed a hole at the seam (each tf rolled at a different hour) and a
// ~290-point basis cliff (the Sep/Dec basis). The prior side is now DERIVED
// from that contract's stored 1m rows with the planner's own bucket helper
// (kernel.AggregateToMinutes, canon 53), and the whole prior segment is
// shifted by the measured roll basis. Current contract: never touched.
//
// CHART_ROLL_STITCH=legacy serves today's stored-aggregate stitch BYTE-
// IDENTICALLY (pinned by the existing roll-hole tests). Default: derive+adjust.

// chartRollStitchDerive is the knob, resolved once at boot like chartAcrossRoll.
var chartRollStitchDerive = strings.ToLower(strings.TrimSpace(os.Getenv("CHART_ROLL_STITCH"))) != "legacy"

// ChartRollStitchResolved is READ onto the boot line (A11): derive+adjust vs
// legacy.
func ChartRollStitchResolved() string {
	if chartRollStitchDerive {
		return "derive+adjust"
	}
	return "legacy"
}

// rollStitchInfo rides the /klines envelope ONLY when a prior segment was
// stitched. Basis is present only when a shift was actually applied; Reason
// says why not when it was not.
type rollStitchInfo struct {
	Derived  bool     `json:"derived"`
	Adjusted bool     `json:"adjusted"`
	Basis    *float64 `json:"basis,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// basisPairWindowMs is the spec's "within 5 minutes of each other" bound for
// the basis measurement pair.
const basisPairWindowMs = 5 * 60_000

// klinesAcrossRollDerived prepends prior-contract bars DERIVED from that
// contract's stored 1m rows (bucket = the requested tf, via the planner's
// aggregateToMinutes), shifted by the measured roll basis. PURE over inputs.
// Returns (klines, stitchInfo) — stitchInfo nil when no prior segment was
// stitched (no boundary, no 1m rows, or nothing to fill).
func klinesAcrossRollDerived(base []market.Kline, bh *store.BarHistoryStore, current, symbol, tf string, limit int) ([]market.Kline, *rollStitchInfo) {
	if bh == nil || limit <= 0 {
		return base, nil
	}
	for i := range base {
		if base[i].Contract == "" {
			base[i].Contract = current
		}
	}
	boundary, ok, err := bh.FirstLiveOn(symbol, tf, current)
	if err != nil || !ok {
		return base, nil
	}
	// The seam is the current contract's first LIVE bar per timeframe — same
	// boundary as the legacy stitch; stray current rows before it still drop
	// (CLASS 143, dropCurrentRowsBefore stays).
	if dropped := dropCurrentRowsBefore(base, boundary); len(dropped) != len(base) {
		base = dropped
	}
	need := limit - len(base)
	if need <= 0 {
		return base, nil
	}
	tfMin := market.TFMinutes(tf)
	if tfMin <= 1 {
		// A 1m ask derives nothing (stored rows ARE 1m) — but the basis cliff
		// is the owner's "all tf": shift the prior 1m segment too.
		return legacyStitchWithAdjust(base, bh, current, symbol, tf, limit)
	}
	// The prior contract: the newest prior-contract 1m row before the
	// boundary names it (the legacy reader's own dedupe). No such row -> the
	// store holds no prior 1m rung: degrade to today's stored-aggregate
	// stitch, bare array.
	probe, err := bh.PriorContractBarsBefore(symbol, "1m", current, boundary, 1)
	if err != nil || len(probe) == 0 || probe[0].Contract == "" {
		return klinesAcrossRoll(base, bh, current, symbol, tf, limit), nil
	}
	prior := probe[0].Contract
	// Enough 1m rows for `need` derived bars + one bucket of slack; clamped.
	n := need*tfMin + tfMin
	if n > 60_000 {
		n = 60_000
	}
	rows, err := bh.LastNBarsOn(symbol, "1m", prior, n)
	if err != nil || len(rows) == 0 {
		// Degrade to the stored-aggregate stitch (bare array) — see above.
		return klinesAcrossRoll(base, bh, current, symbol, tf, limit), nil
	}
	// FULL prior 1m series (not boundary-filtered): the basis pair is measured
	// at the TRUE 1m seam (last old 1m at or before the first new 1m), which
	// for a coarser tf sits AFTER that tf's display boundary.
	bars1mFull := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		bars1mFull = append(bars1mFull, market.Kline{
			OpenTime: r.OpenTimeMs, CloseTime: r.OpenTimeMs + 60_000 - 1,
			Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V,
			Contract: r.Contract,
		})
	}
	if len(bars1mFull) == 0 {
		// No prior-contract 1m rows to derive from (e.g. a store that only
		// holds the requested tf): degrade to today's stored-aggregate stitch,
		// bare array — byte-identical to the legacy wire.
		return klinesAcrossRoll(base, bh, current, symbol, tf, limit), nil
	}
	// HOLE-FILL (2026-09-19, owner: "5m day 11" hole on every tf): where the
	// prior contract's 1m rows have INTERIOR gaps but the CURRENT contract's
	// 1m rows cover those minutes (Sept 11: NT8 was off ~00:29-07:45 CT on the
	// 09-26 rung while the imported 12-26 rows cover it), fill the gap with the
	// current rows CONVERTED into the prior contract's price space — the basis
	// measured at the nearest minute where BOTH contracts have a row (or, when
	// no shared minute exists at all, the 1m seam pair). One price space inside
	// the segment, so the per-tf seam shift still applies uniformly. Gaps the
	// current contract cannot cover (the 09:35-09:58 hole on the roll morning)
	// stay gaps — never fabricated.
	if filled, ok := fillPrior1mHoles(bh, symbol, prior, current, bars1mFull); ok {
		bars1mFull = filled
	}
	// Derivation subset: only 1m rows STRICTLY before THIS tf's display
	// boundary become derived bars of that tf (the boundary never moves).
	var subset []market.Kline
	for _, k := range bars1mFull {
		if k.OpenTime < boundary {
			subset = append(subset, k)
		}
	}
	derived := kernel.AggregateSessionAnchored(subset, tf)
	for i := range derived {
		derived[i].Derived = true
		derived[i].Contract = prior // the bucket helper carries no contract
	}
	// BASIS at THIS timeframe's own seam (the 2026-09-19 owner fix): the prior
	// segment ends at this tf's display boundary (the new contract's first live
	// bar in this tf), so the shift must be measured THERE — the new contract's
	// first tf-bar open vs the last prior-contract 1m close before it. The
	// shipped version measured at the TRUE 1m switch (10:33/10:34), hours after
	// the 15m/30m/1h seam: the Sep/Dec basis decays from ~290 in the morning to
	// ~15 at the 1m switch, so one 15.25 shift left a ~274–280-point cliff at
	// the 15m/30m/1h seam (the owner: "chart on 14 no good on all tf" STILL
	// after the first boot). The pair IS the visual join (prior close shifted →
	// new open), so a measured pair makes the seam continuous by construction.
	// The last prior 1m row sits <1 minute before the boundary, inside the
	// 5-minute window the spec bounds every honest pair with.
	info := &rollStitchInfo{Derived: true, Adjusted: false}
	adjust := false
	if len(base) > 0 {
		firstNewOpen := base[0].Open
		var lastPrior market.Kline
		for _, k := range bars1mFull {
			if k.OpenTime < boundary {
				lastPrior = k
			} else {
				break
			}
		}
		if lastPrior.OpenTime > 0 && boundary-lastPrior.OpenTime <= basisPairWindowMs {
			basis := firstNewOpen - lastPrior.Close
			if !math.IsNaN(basis) {
				shiftKlines(derived, basis)
				for i := range derived {
					derived[i].Adjusted = true
				}
				info.Adjusted = true
				info.Basis = &basis
				adjust = true
			}
		}
	}
	if !adjust {
		info.Reason = "basis pair unmeasurable within 5 minutes at this timeframe's seam"
	}

	out := append(derived, base...)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, info
}

// legacyStitchWithAdjust serves the 1m ask: the stored 1m rows (the legacy
// stitch) with the prior segment shifted by the measured basis. Envelope
// derived:false (nothing was aggregated).
func legacyStitchWithAdjust(base []market.Kline, bh *store.BarHistoryStore, current, symbol, tf string, limit int) ([]market.Kline, *rollStitchInfo) {
	out := klinesAcrossRoll(base, bh, current, symbol, tf, limit)
	info := &rollStitchInfo{Derived: false, Adjusted: false}
	adjust := false
	if firstNew, okNew, errNew := bh.FirstLiveOn(symbol, "1m", current); errNew == nil && okNew {
		priorRows, errP := bh.PriorContractBarsBefore(symbol, "1m", current, firstNew, 1)
		if errP == nil && len(priorRows) == 1 && firstNew-priorRows[0].OpenTimeMs <= basisPairWindowMs {
			basis := firstCloseOn(bh, symbol, current, firstNew) - priorRows[0].C
			if !math.IsNaN(basis) {
				for i := range out {
					if out[i].Contract == "" || out[i].Contract == current {
						continue
					}
					out[i].Open += basis
					out[i].High += basis
					out[i].Low += basis
					out[i].Close += basis
					out[i].Adjusted = true
				}
				info.Adjusted = true
				info.Basis = &basis
				adjust = true
			}
		}
	}
	if !adjust {
		info.Reason = "basis pair unmeasurable within 5 minutes"
		// No shift was applied and nothing was derived: serve the bare array
		// exactly as today, not an envelope that claims a stitch.
		return out, nil
	}
	return out, info
}

// firstCloseOn reads the close of the current contract's first live 1m bar —
// the SAME row FirstLiveOn named (source=live).
func firstCloseOn(bh *store.BarHistoryStore, symbol, current string, openMs int64) float64 {
	rows, err := bh.BarsBetweenOn(symbol, "1m", current, openMs, openMs+60_000)
	if err != nil || len(rows) == 0 {
		return math.NaN()
	}
	return rows[0].C
}

// fillPrior1mHoles fills interior gaps of the prior contract's 1m series with
// the current contract's 1m rows converted into the prior contract's price
// space. The conversion basis is measured at the nearest minute where BOTH
// contracts have a row (per gap: the last shared minute at or before the gap's
// left edge); with NO shared minute anywhere, the seam pair (last prior row vs
// first current row within the 5-minute window) is the fallback. Returns
// (rows, false) unchanged when the current contract has no rows over the span
// or no pair exists.
func fillPrior1mHoles(bh *store.BarHistoryStore, symbol, prior, current string, priorRows []market.Kline) ([]market.Kline, bool) {
	if bh == nil || len(priorRows) < 2 {
		return priorRows, false
	}
	first, last := priorRows[0].OpenTime, priorRows[len(priorRows)-1].OpenTime
	curRows, err := bh.BarsBetweenOn(symbol, "1m", current, first, last+60_000)
	if err != nil || len(curRows) == 0 {
		return priorRows, false
	}
	curByMin := make(map[int64]market.Kline, len(curRows))
	for _, r := range curRows {
		curByMin[r.OpenTimeMs] = market.Kline{
			OpenTime: r.OpenTimeMs, CloseTime: r.OpenTimeMs + 60_000 - 1,
			Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V,
			Contract: r.Contract,
		}
	}
	priorByMin := make(map[int64]market.Kline, len(priorRows))
	var shared []int64
	for _, k := range priorRows {
		priorByMin[k.OpenTime] = k
		if _, ok := curByMin[k.OpenTime]; ok {
			shared = append(shared, k.OpenTime)
		}
	}
	// basisAt returns the pair basis for a gap starting at `minute`: the last
	// shared minute at or before it, else the first shared minute after it,
	// else the seam pair (last prior vs first current within 5 minutes).
	seamBasis := func() (float64, bool) {
		pl := priorRows[len(priorRows)-1]
		cf, ok := curByMin[curRows[0].OpenTimeMs]
		if !ok || curRows[0].OpenTimeMs-pl.OpenTime > basisPairWindowMs {
			return 0, false
		}
		return cf.Close - pl.Close, true
	}
	basisAt := func(minute int64) (float64, bool) {
		best := int64(0)
		for _, s := range shared {
			if s <= minute {
				best = s
			} else {
				break
			}
		}
		if best == 0 && len(shared) > 0 {
			best = shared[0]
		}
		if best != 0 {
			if c, ok := curByMin[best]; ok {
				if p, ok := priorByMin[best]; ok {
					return c.Close - p.Close, true
				}
			}
		}
		return seamBasis()
	}
	out := make([]market.Kline, 0, len(priorRows)+len(curRows))
	for i, k := range priorRows {
		out = append(out, k)
		if i == len(priorRows)-1 {
			break
		}
		next := priorRows[i+1]
		if next.OpenTime-k.OpenTime <= 60_000 {
			continue
		}
		basis, ok := basisAt(k.OpenTime)
		if !ok {
			continue
		}
		for minute := k.OpenTime + 60_000; minute < next.OpenTime; minute += 60_000 {
			c, ok := curByMin[minute]
			if !ok {
				continue
			}
			c.Open -= basis
			c.High -= basis
			c.Low -= basis
			c.Close -= basis
			c.Contract = prior
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime < out[j].OpenTime })
	return out, true
}

// shiftKlines adds basis to O/H/L/C of every bar (volume untouched).
func shiftKlines(ks []market.Kline, basis float64) {
	for i := range ks {
		ks[i].Open += basis
		ks[i].High += basis
		ks[i].Low += basis
		ks[i].Close += basis
	}
}
