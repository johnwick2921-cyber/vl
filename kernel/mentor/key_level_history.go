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

// Roll-gap rule (CTO REL10-438-FIXES #1):
//   - The ROLL of a pair = the first session day (17:00 CT flip) on which the
//     NEWER contract has >= denseFront1mMin 1m bars (dense live/front).
//   - The gap = the median of (newer.close − older.close) over the overlapping
//     NATIVE 1h bars of the last FULL session day before the roll — the last
//     session day before the roll day with at least minRollOverlap overlapping
//     1h bars. The whole-overlap median is wrong: the spread ranges widely
//     (measured 258.25..352.25 over 1493 bars), so the gap must be read at the
//     roll boundary, not across three months.
//   - Fewer than minRollOverlap overlapping bars → no measured gap → the
//     stitch STOPS (never guess a gap).
const (
	denseFront1mMin = 1000
	minRollOverlap  = 10
)

// Contract1M is one contract's 1m history (ascending OpenTime), on its OWN
// price scale, plus its native 1h history (used ONLY for the roll-gap
// measurement: the native 1h store has longer retention than 1m, so adjacent
// contracts overlap on more 1h bars).
type Contract1M struct {
	Contract string
	Bars     []market.Kline // 1m, ascending
	Bars1H   []market.Kline // native 1h, ascending (whole-hour aligned)
}

// RollGap records one measured roll: the NATIVE pair gap (median of
// newer.close − older.close on the measurement day), the session day it was
// measured on, that day's spread, and the pair. The gap places the older
// contract onto the newer contract's scale; the stitch accumulates pair gaps
// onto the newest scale.
type RollGap struct {
	Gap  float64
	Day  string // session-day key (17:00 CT flip, "2006-01-02")
	N    int
	Min  float64
	Max  float64
	Pair string // "older→newer"
}

// rollDayKey returns the FIRST session-day key (17:00 CT flip) on which the
// contract has >= denseFront1mMin 1m bars — the day it became the dense
// live/front series. ok=false when no session day reaches the threshold.
func rollDayKey(bars []market.Kline) (string, bool) {
	counts := make(map[string]int, 64)
	for _, b := range bars {
		counts[sessionKeyCT(b.OpenTime)]++
	}
	best := ""
	for k, n := range counts {
		if n >= denseFront1mMin && (best == "" || k < best) {
			best = k
		}
	}
	return best, best != ""
}

// trimAfter keeps the bars whose OpenTime is STRICTLY after cutoff — the
// head's continuation past the older contract's last stored bar. bars is
// ascending, so this is a binary search (no allocation).
func trimAfter(bars []market.Kline, cutoff int64) []market.Kline {
	n := sort.Search(len(bars), func(i int) bool { return bars[i].OpenTime > cutoff })
	return bars[n:]
}

// trimPreRoll drops the newest contract's sparse pre-roll import snapshots —
// the bars BEFORE its dense roll day (the first session day with >=
// denseFront1mMin bars). Used ONLY when no older contract was stitched, so the
// Add-time cut (which normally drops them) never ran.
func trimPreRoll(bars []market.Kline) []market.Kline {
	rd, ok := rollDayKey(bars)
	if !ok {
		return bars
	}
	n := sort.Search(len(bars), func(i int) bool { return sessionKeyCT(bars[i].OpenTime) >= rd })
	return bars[n:]
}

// lastFullOverlapBefore returns the most recent session-day key strictly
// before rollDay with at least minRollOverlap overlapping native 1h bars
// (same OpenTime) between newer1h and older1h. "" when no such day exists —
// the roll gap is then unmeasurable and the stitch must stop.
func lastFullOverlapBefore(newer1h, older1h []market.Kline, rollDay string) string {
	m := make(map[int64]float64, len(newer1h))
	for _, b := range newer1h {
		if sessionKeyCT(b.OpenTime) < rollDay {
			m[b.OpenTime] = b.Close
		}
	}
	counts := make(map[string]int, 64)
	for _, b := range older1h {
		k := sessionKeyCT(b.OpenTime)
		if k >= rollDay {
			continue
		}
		if _, ok := m[b.OpenTime]; ok {
			counts[k]++
		}
	}
	best := ""
	for k, n := range counts {
		if n >= minRollOverlap && k > best {
			best = k
		}
	}
	return best
}

// measureRollGap returns the median of (newer.close − older.close) over the
// native 1h bars of both contracts at the SAME OpenTime on the given session
// day, plus that day's min/max spread. ok=false when fewer than
// minRollOverlap bars overlap that day.
func measureRollGap(newer1h, older1h []market.Kline, day string) (RollGap, bool) {
	m := make(map[int64]float64, len(newer1h))
	for _, b := range newer1h {
		if sessionKeyCT(b.OpenTime) == day {
			m[b.OpenTime] = b.Close
		}
	}
	var diffs []float64
	for _, b := range older1h {
		if sessionKeyCT(b.OpenTime) != day {
			continue
		}
		if nc, has := m[b.OpenTime]; has {
			diffs = append(diffs, nc-b.Close)
		}
	}
	if len(diffs) < minRollOverlap {
		return RollGap{}, false
	}
	sort.Float64s(diffs)
	return RollGap{
		Gap: diffs[len(diffs)/2],
		Day: day,
		N:   len(diffs),
		Min: diffs[0],
		Max: diffs[len(diffs)-1],
	}, true
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

// RollStitcher accumulates the full stitched history contract-by-contract,
// NEWEST→OLDEST, stopping at the first roll whose gap cannot be measured.
// CUT (CTO REL10-STITCH-CUT): the older contract supplies every bar up to and
// including ITS OWN LAST STORED BAR; the newer contract supplies every bar
// STRICTLY AFTER that instant. The newer's sparse pre-roll snapshots lie
// before the older's last bar and so are dropped, while the newer's same-day
// continuation is kept — no mid-session hole.
type RollStitcher struct {
	stitched1m   []market.Kline // stitched 1m, newest scale, ascending
	head1h       []market.Kline // head contract's NATIVE 1h (own scale)
	headRollDay  string         // roll day of the head contract
	headContract string
	cumGap       float64 // cumulative gap from the head contract to the newest
	gaps         []RollGap
	stoppedAt    string
}

// NewRollStitcher starts the stitch from the NEWEST contract (own scale). The
// newest's bars are kept FULL here — the cut to "strictly after the older's
// last bar" happens in Add, when the older contract is known.
func NewRollStitcher(newest Contract1M) *RollStitcher {
	s := &RollStitcher{
		stitched1m:   newest.Bars,
		head1h:       newest.Bars1H,
		headContract: newest.Contract,
	}
	rd, ok := rollDayKey(newest.Bars)
	if !ok {
		// A contract that never reaches the dense-front threshold has no
		// measurable roll; nothing can be stitched onto it.
		s.stoppedAt = newest.Contract
		return s
	}
	s.headRollDay = rd
	return s
}

// Add stitches one OLDER contract onto the accumulated series. It returns
// false when the roll gap cannot be measured (fewer than minRollOverlap
// overlapping native 1h bars on the last full session day before the head's
// roll) — the stitch STOPS and the caller reads no older contract (FIX 5).
func (s *RollStitcher) Add(older Contract1M) bool {
	if s.stoppedAt != "" || len(s.stitched1m) == 0 {
		return false
	}
	measDay := lastFullOverlapBefore(s.head1h, older.Bars1H, s.headRollDay)
	if measDay == "" {
		s.stoppedAt = older.Contract + "→" + s.headContract
		return false
	}
	pair, ok := measureRollGap(s.head1h, older.Bars1H, measDay)
	if !ok {
		s.stoppedAt = older.Contract + "→" + s.headContract
		return false
	}
	pair.Pair = older.Contract + "→" + s.headContract
	if len(older.Bars) == 0 {
		s.stoppedAt = older.Contract + "→" + s.headContract
		return false
	}
	// The older's own roll day is needed only for the NEXT pair's gap
	// measurement. A contract that never went dense (e.g. a mid-session tail
	// in a fixture) can still be stitched; its "" roll day then makes the next
	// Add()'s measurement day "" → the stitch stops there.
	olderRD, _ := rollDayKey(older.Bars)
	// CUT (CTO REL10-STITCH-CUT): older supplies every bar up to and including
	// its own last stored bar (where the store stopped recording it); the head
	// supplies every bar STRICTLY AFTER that instant. Back-adjust onto the
	// NEWEST scale by the cumulative gap.
	total := pair.Gap + s.cumGap
	cutTime := older.Bars[len(older.Bars)-1].OpenTime
	pre := backAdjust(older.Bars, total)     // every bar ≤ cutTime (the whole older series)
	head := trimAfter(s.stitched1m, cutTime) // strictly after the older's last bar
	s.stitched1m = append(pre, head...)
	s.gaps = append(s.gaps, pair)
	s.head1h = older.Bars1H
	s.headRollDay = olderRD
	s.headContract = older.Contract
	s.cumGap = total
	return true
}

// Result returns the stitched 1m series (newest scale), the 08:30-anchored 1H
// RTH key-level walk series, the per-pair gaps (measurement order: newest
// pair first), and the roll where the stitch stopped ("" = full).
func (s *RollStitcher) Result() (stitched1m, bars1hRTH []market.Kline, gaps []RollGap, stoppedAt string) {
	stitched1m = s.stitched1m
	// When NO older contract was stitched (the first pair's gap was
	// unmeasurable, or the store holds one contract), the Add-time cut never
	// ran, so the newest's sparse pre-roll import snapshots would still be in
	// the walk and could draw bogus single-bar 1H candles. Drop them here.
	if len(s.gaps) == 0 && len(stitched1m) > 0 {
		stitched1m = trimPreRoll(stitched1m)
	}
	return stitched1m, keyLevel1HBars(stitched1m), s.gaps, s.stoppedAt
}

// StitchKeyLevelHistory builds the full stitched history from a PRE-LOADED
// contract slice (oldest→newest). The trader uses RollStitcher directly so it
// can stop READING at the first failed roll (FIX 5); this wrapper serves
// tests and degenerate callers that already hold every contract.
func StitchKeyLevelHistory(contracts []Contract1M) (stitched1m, bars1hRTH []market.Kline, gaps []RollGap, stoppedAt string) {
	if len(contracts) == 0 {
		return nil, nil, nil, ""
	}
	s := NewRollStitcher(contracts[len(contracts)-1])
	for i := len(contracts) - 2; i >= 0; i-- {
		if !s.Add(contracts[i]) {
			break
		}
	}
	return s.Result()
}

// KeyLevelHistoryLine renders the release-#10 boot line: the full-history 1H
// RTH key-level seed's depth, contract span, roll gaps (with their day and
// min/max spread), and whether the stitch reached every contract or stopped
// at a no-measured-gap roll.
func KeyLevelHistoryLine(contracts []Contract1M, stitched []market.Kline, gaps []RollGap, stoppedAt string) string {
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
		gapStrs[i] = fmt.Sprintf("%.1f@%s[%.2f..%.2f]", g.Gap, g.Day, g.Min, g.Max)
	}
	stop := "full"
	if stoppedAt != "" {
		stop = fmt.Sprintf("stopped at %s: no measured gap", stoppedAt)
	}
	return fmt.Sprintf("🧑‍🏫 key-level history: from %s · %d 1H RTH candles · contracts %s · gaps %s · %s",
		from, len(stitched), strings.Join(names, ","), strings.Join(gapStrs, ","), stop)
}
