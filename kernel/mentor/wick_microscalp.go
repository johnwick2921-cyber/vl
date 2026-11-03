package mentor

import (
	"fmt"
	"sort"

	"vl/market"
)

// D4.3 wick microscalp — the TARGET half (m465 P5, D4.3-R17/R27; knob
// wick_microscalp, default OFF). The zone half (detector, box, lifecycle,
// entries) is wick_zone.go.
//
// "Target ở đâu? Thì target vẫn là khung 5 phút. Target thì là những cái râu
// của khung 5 phút, đúng không?" [D4.3 @09:39–09:46]; "scalp thì scalp đi đâu
// anh chị? Có phải là scalp về đây không?… Chứ không phải là scalp mà bắn một
// phát lên 100-200 điểm. Không có." [@07:30–07:49]. On screen he draws the
// rays at the TOPS of nearby 5m wicks: Ex.1 28,919.75 / 28,933.50, Ex.2
// 29,267.75 / 29,275.75 [screen @07:40–07:44, @09:42–09:53]; no target was drawn
// for the shorts (Ex.3/Ex.4) — the mirror (the BOTTOMS of nearby 5m lower
// wicks) is D4.3-R27, derived from the examples, not spoken.
//
// Pinned (no number in the course): "nearby" = the last wickTargetLookback5m
// closed 5m candles; a candle offers a target only if it HAS the wick (high
// above its body for a long, low below its body for a short); the ladder is
// capped wickTargetMaxPts beyond the zone's near edge ("not 100–200 points").
//
// The pre-m465 wickMicroscalpLevel (the run's own far wick as a single
// target, fed by the 5m TRIGGER direction, never wired to a knob) is replaced
// by this ladder: the course target is the nearby 5m wicks, and the trend is
// the 5m structure (wick_zone.go wickTrend).

const (
	// wickTargetLookback5m — the last N closed 5m candles read for target
	// wicks (one hour; ours — the course says "nearby").
	wickTargetLookback5m = 12
	// wickTargetMaxPts — no target more than this beyond the zone's near
	// edge: "không phải là scalp mà bắn một phát lên 100-200 điểm" [@07:30–07:49].
	wickTargetMaxPts = 100.0
)

// wickTargetLadder returns the zone's 5m-wick targets, nearest first: for a
// LONG zone the highs of the recent closed 5m candles that carry an UPPER
// wick, above the zone top and at most wickTargetMaxPts over it; for a SHORT
// zone the mirror (lows with a LOWER wick, under the zone bottom). Distinct
// prices only. Empty when nothing qualifies.
func wickTargetLadder(closed5m []market.Kline, z WickZone) []float64 {
	from := len(closed5m) - wickTargetLookback5m
	if from < 0 {
		from = 0
	}
	seen := map[float64]bool{}
	var out []float64
	for _, c := range closed5m[from:] {
		lower, upper := wickParts(c)
		var p float64
		switch z.Side {
		case SideLong:
			if upper <= 0 || c.High <= z.Top || c.High-z.Top > wickTargetMaxPts {
				continue
			}
			p = c.High
		case SideShort:
			if lower <= 0 || c.Low >= z.Bottom || z.Bottom-c.Low > wickTargetMaxPts {
				continue
			}
			p = c.Low
		default:
			return nil
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if z.Side == SideLong {
		sort.Float64s(out)
	} else {
		sort.Sort(sort.Reverse(sort.Float64Slice(out)))
	}
	return out
}

// wickTargetLevels turns the ladder into TARGET-ONLY levels
// (KindWickMicroscalp): nextLevelBeyondRoom / phlTarget pick the nearest one
// beyond the entry; the touch loop skips the kind and levelIsLocation is false
// for it (it is never a place to enter).
func wickTargetLevels(closed5m []market.Kline, z WickZone) []Level {
	ladder := wickTargetLadder(closed5m, z)
	out := make([]Level, 0, len(ladder))
	for _, p := range ladder {
		out = append(out, Level{Key: fmt.Sprintf("%s:%s:%.2f", KindWickMicroscalp, z.Side, p), Kind: KindWickMicroscalp, Price: p})
	}
	return out
}

// wickParts returns a candle's lower and upper wick lengths (each ≥ 0).
func wickParts(c market.Kline) (lower, upper float64) {
	bodyLow, bodyHigh := c.Open, c.Close
	if bodyLow > bodyHigh {
		bodyLow, bodyHigh = bodyHigh, bodyLow
	}
	lower, upper = bodyLow-c.Low, c.High-bodyHigh
	if lower < 0 {
		lower = 0
	}
	if upper < 0 {
		upper = 0
	}
	return lower, upper
}
