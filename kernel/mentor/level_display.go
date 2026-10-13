package mentor

import (
	"fmt"
	"sort"
)

// DisplayLevels builds the level set the chart and the mentor-truth panel DRAW
// (course 5.3 @15:59–16:50: lines closer than 20 pts read as one). It is a PURE
// view over the TRADING set — it never changes what the trading readers see.
//
//  1. exact-duplicate old-extreme prices collapse to one line;
//  2. an old extreme within prunePts of a KEY level is folded INTO that key
//     level (the key wins — it is the owner-visible line);
//  3. the remaining old extremes closer than prunePts keep only the MORE
//     RECENT (rightmost) — the SAME pruneLevelsByRecency tie-break the key
//     levels use.
//
// Every absorbed price lands in exactly one kept line's MergedFrom. Key levels
// are cloned before their MergedFrom is filled, so the caller's TRADING set is
// never mutated. Returns key levels (with MergedFrom) + EMA lines + the cleaned
// old extremes, in that order.
func DisplayLevels(levels []Level, prunePts float64) []Level {
	var keys, emas, old []Level
	for _, l := range levels {
		switch l.Kind {
		case KindKeyLevel:
			keys = append(keys, l)
		case KindOldExtreme:
			old = append(old, l)
		default:
			emas = append(emas, l)
		}
	}
	// Clone the key levels so MergedFrom is filled on the DISPLAY copy only.
	keyClones := make([]Level, len(keys))
	for i, k := range keys {
		keyClones[i] = k
		keyClones[i].MergedFrom = nil
	}
	cleaned := cleanOldExtremesForDisplay(old, keyClones, prunePts)
	out := make([]Level, 0, len(keyClones)+len(emas)+len(cleaned))
	out = append(out, keyClones...)
	out = append(out, emas...)
	out = append(out, cleaned...)
	// Deterministic, readable order for every merged_from list.
	for i := range out {
		if len(out[i].MergedFrom) > 1 {
			sort.Float64s(out[i].MergedFrom)
		}
	}
	return out
}

// cleanOldExtremesForDisplay applies the display cleanup to the RAW old
// extremes against the (cloned) key levels and fills MergedFrom on the
// survivors and the key levels that absorbed an old extreme.
func cleanOldExtremesForDisplay(old []Level, keys []Level, prunePts float64) []Level {
	if len(old) == 0 {
		return nil
	}
	// (1) exact-duplicate prices → one line; the duplicate's price is merged
	// into the first.
	byPrice := make(map[string]int) // price key -> index into dedup
	dedup := make([]Level, 0, len(old))
	for _, l := range old {
		k := fmtPriceKey(l.Price)
		if i, ok := byPrice[k]; ok {
			dedup[i].MergedFrom = append(dedup[i].MergedFrom, l.Price)
			continue
		}
		byPrice[k] = len(dedup)
		dedup = append(dedup, l)
	}
	// (2) within prunePts of a KEY level → fold into the key (key wins). The
	// absorbed line's own MergedFrom (its duplicates) rides along.
	far := make([]Level, 0, len(dedup))
	for _, l := range dedup {
		if foldOldExtremeIntoKey(l, keys, prunePts) {
			continue
		}
		far = append(far, l)
	}
	// (3) pairwise < prunePts → keep the more recent; the dropped prices merge
	// into the survivor.
	return pruneOldExtremesForDisplay(far, prunePts)
}

// foldOldExtremeIntoKey records an old extreme into the nearest key level within
// prunePts and reports whether it was folded. The old extreme's own MergedFrom
// (its absorbed duplicates) rides along, so every raw price is accounted once.
func foldOldExtremeIntoKey(l Level, keys []Level, prunePts float64) bool {
	best := -1
	bestDist := prunePts
	for i := range keys {
		d := abs(keys[i].Price - l.Price)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return false
	}
	keys[best].MergedFrom = append(keys[best].MergedFrom, l.Price)
	keys[best].MergedFrom = append(keys[best].MergedFrom, l.MergedFrom...)
	return true
}

// pruneOldExtremesForDisplay is pruneLevelsByRecency that also records every
// dropped price (and its transitively-absorbed duplicates) into the surviving
// line's MergedFrom.
func pruneOldExtremesForDisplay(levels []Level, prunePts float64) []Level {
	if len(levels) < 2 {
		return levels
	}
	sorted := make([]Level, len(levels))
	copy(sorted, levels)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].AtTime > sorted[j].AtTime })
	var kept []Level
	for _, l := range sorted {
		dropped := false
		for i := range kept {
			if abs(l.Price-kept[i].Price) < prunePts {
				kept[i].MergedFrom = append(kept[i].MergedFrom, l.Price)
				kept[i].MergedFrom = append(kept[i].MergedFrom, l.MergedFrom...)
				dropped = true
				break
			}
		}
		if !dropped {
			kept = append(kept, l)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].AtTime < kept[j].AtTime })
	return kept
}

// fmtPriceKey is the dedupe key for an exact price (2 decimals, like the level
// key's price suffix).
func fmtPriceKey(price float64) string {
	return fmt.Sprintf("%.2f", price)
}
