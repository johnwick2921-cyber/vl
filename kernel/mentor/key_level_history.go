package mentor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vl/market"
)

// KEYLEVEL-FULL-HISTORY (release #10): the 1H RTH key-level source currently
// has no lookback cap (F7) but the LIVE input cuts it to the current
// contract's last 50000 1m bars (~3.5 weeks). The store holds 1m back through
// 19 contracts (2022-04-11). The levels the mentor draws on TradingView come
// from the ACTUAL contract's own history (D3.4 frames, D5.1 p1 @18:10 + p2
// @11:20), so older contracts are stitched onto today's contract price scale
// through a MEASURED roll gap — never a guessed one.

// Contract1M is one contract's 1m history (ascending OpenTime), on its OWN
// price scale, plus its native 1h history (used ONLY for the roll-gap
// measurement: the native 1h store has longer retention than 1m, so adjacent
// contracts overlap on more 1h bars).
type Contract1M struct {
	Contract string
	Bars     []market.Kline // 1m, ascending
	Bars1H   []market.Kline // native 1h, ascending (whole-hour aligned)
}

// rollGap returns the median of (newer.Close − older.Close) over bars of both
// contracts at the SAME OpenTime — the roll overlap. ok=false when the two
// contracts share no timestamp (no overlap: the gap is UNKNOWN and must not be
// guessed).
func rollGap(newer, older []market.Kline) (gap float64, n int, ok bool) {
	m := make(map[int64]float64, len(newer))
	for _, b := range newer {
		m[b.OpenTime] = b.Close
	}
	var diffs []float64
	for _, b := range older {
		if nc, has := m[b.OpenTime]; has {
			diffs = append(diffs, nc-b.Close)
		}
	}
	if len(diffs) == 0 {
		return 0, 0, false
	}
	sort.Float64s(diffs)
	return diffs[len(diffs)/2], len(diffs), true
}

// backAdjust shifts every OHLC of bars by gap onto the newest contract's
// price scale.
func backAdjust(bars []market.Kline, gap float64) []market.Kline {
	out := make([]market.Kline, len(bars))
	for i, b := range bars {
		b.Open += gap
		b.High += gap
		b.Low += gap
		b.Close += gap
		out[i] = b
	}
	return out
}

// barsBefore returns the prefix of ascending bars with OpenTime < cutoff — the
// pre-overlap history of an older contract. cutoff < 0 (no shared minute) means
// NO overlap: every bar is pre-overlap, so the whole series is returned.
func barsBefore(older []market.Kline, cutoff int64) []market.Kline {
	if cutoff < 0 {
		return older
	}
	n := sort.Search(len(older), func(i int) bool { return older[i].OpenTime >= cutoff })
	return older[:n]
}

// StitchKeyLevelHistory builds the full 08:30-anchored 1H RTH candle series
// (the key-level walk input) from per-contract 1m histories, OLDEST→NEWEST.
// The newest contract stays on its own scale. Each older contract's roll gap
// is measured on the NATIVE 1H bars (both contracts at the same hour — the 1h
// store outlives 1m retention, so adjacent contracts overlap on far more 1h
// bars) against the ALREADY-STITCHED series' 1h (which lives on the newest
// scale), so one back-adjustment places the older 1m directly on the newest
// scale. The older 1m bars EARLIER than the stitched series' first bar are
// prepended; the overlap minutes stay the newer series' (duplicating them
// would merge into garbage candles). The stitch STOPS at the first roll
// (walking backward) whose 1h gap cannot be measured — no overlap, never a
// guess. The full series then goes through keyLevel1HBars (08:30 anchor, RTH
// only).
//
// It returns the stitched 1H RTH series, the per-contract gaps actually
// applied (oldest→newest, cumulative onto the newest scale), and the roll
// (contract pair) where it stopped — "" means every contract stitched (full).
func StitchKeyLevelHistory(contracts []Contract1M) (bars []market.Kline, gaps []float64, stoppedAt string) {
	if len(contracts) == 0 {
		return nil, nil, ""
	}
	all := append([]market.Kline(nil), contracts[len(contracts)-1].Bars...) // newest 1m, own scale
	all1h := append([]market.Kline(nil), contracts[len(contracts)-1].Bars1H...)
	if len(all) == 0 {
		return nil, nil, ""
	}
	gaps = make([]float64, 0, len(contracts)-1)
	for i := len(contracts) - 2; i >= 0; i-- {
		older := contracts[i]
		gap, n, ok := rollGap(all1h, older.Bars1H) // all1h is already on the newest scale
		if !ok || n == 0 {
			return keyLevel1HBars(all), gaps, older.Contract + "→" + contracts[i+1].Contract
		}
		// Prepend the older 1m bars EARLIER than the stitched series' first
		// bar (the overlap minutes — when they exist — stay the newer series').
		// The 1m stores often do NOT share exact minutes across a roll (sparse
		// import snapshots, maintenance gaps), so "first shared minute" is the
		// wrong boundary: it can be -1 and empty the whole older history.
		pre := backAdjust(barsBefore(older.Bars, all[0].OpenTime), gap)
		gaps = append([]float64{gap}, gaps...) // oldest→newest order
		all = append(pre, all...)
		// extend the 1h reference with the older contract's 1h, back-adjusted,
		// pre-overlap — keeps the next gap measurement on the same newest scale.
		pre1h := backAdjust(barsBefore(older.Bars1H, all1h[0].OpenTime), gap)
		all1h = append(pre1h, all1h...)
	}
	return keyLevel1HBars(all), gaps, ""
}

// KeyLevelHistoryLine renders the release-#10 boot line: the full-history 1H
// RTH key-level seed's depth, contract span, roll gaps, and whether the
// stitch reached every contract or stopped at a no-overlap roll.
func KeyLevelHistoryLine(contracts []Contract1M, stitched []market.Kline, gaps []float64, stoppedAt string) string {
	from := "n/a"
	if len(stitched) > 0 {
		from = time.UnixMilli(stitched[0].OpenTime).In(ctime()).Format("2006-01-02")
	}
	names := make([]string, len(contracts))
	for i, c := range contracts {
		names[i] = c.Contract
	}
	gapStrs := make([]string, len(gaps))
	for i, g := range gaps {
		gapStrs[i] = fmt.Sprintf("%.1f", g)
	}
	stop := "full"
	if stoppedAt != "" {
		stop = fmt.Sprintf("stopped at %s: no overlap", stoppedAt)
	}
	return fmt.Sprintf("🧑‍🏫 key-level history: from %s · %d 1H RTH candles · contracts %s · gaps %s · %s",
		from, len(stitched), strings.Join(names, ","), strings.Join(gapStrs, ","), stop)
}
