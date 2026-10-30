package ninjatrader

import "time"

// ── THE MENTOR'S SEEDED SERIES (m445 round 2, reviewer A, D1) ───────────────
//
// The mentor evaluator is SEEDED once per process (and again after every
// history refill) from the contract-stitched 1m series, and the 1H RTH key
// levels, the open-HTF lines, the day latch and the trendlines it builds from
// that series live for the life of the process. A hole in an RTH candle of
// that series is therefore a hole in the mentor's key-level LOCATION input,
// however old it is, and the per-pass window (IntegrityIntradayOpenMin) cannot
// see it.
//
// The tracker cannot judge that series: it follows ONE contract's stored rows
// (the stitch cuts the older contract at its last stored bar and takes the
// head after it, so a hole in the head's pre-cut stretch is not in the series,
// and a hole in the older contract's stretch IS), and a whole-history scan of
// the head contract reads its sparse pre-roll import snapshots as holes
// (verified on the live store: 12-26 has one import row a day from 09-07 to
// 09-10 and 425 on 09-11). So the trader judges the series the seed WALKED —
// SeriesRTHHoles — at the seed and at every re-seed, and the verdict reads that
// measurement. A hole that a history reload fills bumps the refill generation,
// the evaluator re-seeds from the fuller series, and the next measurement is
// clear: resume is a measurement, as everywhere else.

// IntegritySeedReachDays — how far back a hole in the seeded series blocks. The
// 1m retention (store.BarRetentionDays default 90): older rows are not part of
// the live store, and a two-year-old hole in a backfilled contract can never be
// refilled by anything the owner does, so it must not pause trading forever.
// Pinned equal to the retention default by the trader package.
const IntegritySeedReachDays = 90

// SeriesRTHHoles judges an ASCENDING series of 1m open times (ms) — the series
// the mentor seed walked — together with the minutes the stitch CUT away
// although a contract stored them (`covered`, ascending: mentor.RollStitcher
// .SeamCovered), and returns every stretch of at least
// BARS_HOLE_MIN_GAP_MIN consecutive OPEN minutes with no bar that takes
// regular-trading-hours minutes (08:30–15:00 CT) and ends inside the last
// IntegritySeedReachDays before `now`. Overnight stretches are not returned:
// they feed no 1H RTH candle (the per-pass window and the tracker judge them
// where a decision reads them). Every field is READ from the series.
//
// A MINUTE IS MISSING ONLY IF NO CONTRACT IN THE STITCH STORES IT (m445 round
// 4, D1). The stitch cuts the older contract at its last stored bar and takes
// the head strictly after it, so a minute the older never recorded but the head
// did (2026-09-14 09:36–09:57 CT: MNQ 09-26 has a 22-minute hole there, MNQ
// 12-26 stores those minutes, the cut at 09-26's last bar 10:33 CT drops them)
// is absent from the series though the market was recorded. That is an
// artifact of how the series is stitched, not lost data: it must not pause
// trading, and no owner action could ever clear it.
func SeriesRTHHoles(symbol, label string, timesMs, covered []int64, now time.Time) []IntegrityHole {
	minGap, _ := barHoleMinGap()
	horizon := now.Add(-IntegritySeedReachDays * 24 * time.Hour).UnixMilli()
	var out []IntegrityHole
	for _, sp := range findBarHoles(unionSortedMs(timesMs, covered), minGap, barHoleExpected) {
		if sp.ToMs <= horizon {
			continue
		}
		rth := 0
		for m := sp.FromMs; m < sp.ToMs; m += 60_000 {
			if isRTHMinute(time.UnixMilli(m)) && barHoleExpected(time.UnixMilli(m)) {
				rth++
			}
		}
		if rth == 0 {
			continue
		}
		out = append(out, IntegrityHole{
			Symbol: symbol, Contract: label, FromMs: sp.FromMs, ToMs: sp.ToMs,
			Missing: sp.OpenMissing, State: "seeded",
			Window:     openWindowCTWith(barHoleExpected, sp.FromMs, sp.ToMs),
			Unfillable: true,
		})
	}
	return out
}

// unionSortedMs merges two ascending lists of open times into one ascending,
// duplicate-free list (the first is returned as is when the second is empty).
func unionSortedMs(a, b []int64) []int64 {
	if len(b) == 0 {
		return a
	}
	out := make([]int64, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		var v int64
		switch {
		case j >= len(b) || (i < len(a) && a[i] <= b[j]):
			v = a[i]
			i++
		default:
			v = b[j]
			j++
		}
		if n := len(out); n == 0 || out[n-1] != v {
			out = append(out, v)
		}
	}
	return out
}
