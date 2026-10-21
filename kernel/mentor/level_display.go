package mentor

// DisplayLevels builds the level set the chart and the mentor-truth panel DRAW.
//
// r22 (P1, "the chart and the trading code read ONE source"): the old highs/
// lows are drawn EXACTLY as the trading code reads them — the one nearest
// unbroken old high and the one nearest unbroken old low (swingOldExtremeLevels),
// never folded into a key level and never pruned. The old build folded/pruned
// the 12 raw pivots for the DRAW only, so on 10-13 at 08:30 the owner's chart
// showed 5 old extremes while the bot traded on 12 — validating the bot by eye
// meant validating a different set.
//
// It is a pure view: key levels and EMA lines are copied unchanged (MergedFrom
// cleared on the copies), and the trading slice is never mutated. Returns key
// levels + EMA/other lines + old extremes, in that order. prunePts is unused
// since r22 (kept so the panel call site stays one line).
func DisplayLevels(levels []Level, prunePts float64) []Level {
	var keys, others, old []Level
	for _, l := range levels {
		l.MergedFrom = nil
		switch l.Kind {
		case KindKeyLevel:
			keys = append(keys, l)
		case KindOldExtreme:
			old = append(old, l)
		default:
			others = append(others, l)
		}
	}
	out := make([]Level, 0, len(keys)+len(others)+len(old))
	out = append(out, keys...)
	out = append(out, others...)
	out = append(out, old...)
	return out
}
