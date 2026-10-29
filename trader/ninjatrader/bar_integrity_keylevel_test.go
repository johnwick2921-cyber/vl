package ninjatrader

import (
	"strings"
	"testing"
	"time"
)

// ── KEY-LEVEL LEGS (m445 round 2, reviewer A, D1) ───────────────────────────
//
// The mentor evaluator seeds its 1H RTH key levels (and the open-HTF lines, the
// day latch, the trendlines) ONCE from the contract-stitched 1m history and
// keeps them for the life of the process; the 1500-minute intraday window does
// not cover that. Two legs close it: IntegrityVerdictForMentor (the tracker's
// RTH holes of any age, below) and SeriesRTHHoles (the series the seed walked,
// judged by the trader at the seed and every re-seed).
//
// MUTANTS (each shown RED in the round-2 report):
//   K1  `add` ignores keyLevels                      -> TestMentorVerdictBlocksAnRTHHoleTwoSessionsBack RED
//   K4  SeriesRTHHoles counts every open minute      -> TestSeriesRTHHolesJudgesTheSeededSeries RED
//   K5  keepLocked never sets retiredEvicted         -> TestRetiredListOverflowIsUnknownNotClear RED
//   K6  SeriesRTHHoles ignores the 90-day reach      -> TestSeriesRTHHolesJudgesTheSeededSeries RED

// The tracker's key-level leg: an RTH hole the tracker holds (detected live, or by
// the boot scan inside its reach) blocks the mentor verdict whatever its age.
// The reviewer's own probe: 1m continuous except an RTH stretch two sessions
// back. The per-pass window (and so IntegrityVerdictFor) reads it as clear; the
// mentor verdict must not.
func TestMentorVerdictBlocksAnRTHHoleTwoSessionsBack(t *testing.T) {
	h := newMemHarness(t)
	h.run(ct(2026, 10, 26, 17, 0), ct(2026, 10, 27, 9, 29))
	h.run(ct(2026, 10, 27, 10, 40), ct(2026, 10, 29, 10, 0)) // hole 10-27 09:30-10:39 CT = inside the 1H RTH candles
	now := ct(2026, 10, 29, 10, 1)

	if v := h.verdict(IntegrityIntradayOpenMin, now); v.Blocked {
		t.Fatalf("setup: the per-pass intraday window (%d open min) must not reach a hole two sessions back: %+v", IntegrityIntradayOpenMin, v)
	}
	for _, win := range []int{IntegrityIntradayOpenMin, IntegrityDeepOpenMin, IntegritySwingOpenMin} {
		v := IntegrityVerdictForMentor("MNQ", win, now)
		if win == IntegrityDeepOpenMin || win == IntegritySwingOpenMin {
			// these windows reach the hole by themselves; the key-level leg must agree
			if !v.Blocked || v.Class != IntegrityClassHole {
				t.Fatalf("window %d: %+v", win, v)
			}
			continue
		}
		if !v.Blocked || v.Class != IntegrityClassHole || len(v.Holes) != 1 || v.Holes[0].Window != "10-27 09:30–10:40 CT" {
			t.Fatalf("the mentor intraday verdict must block on the RTH hole in the seeded key-level history: %+v", v)
		}
	}

	// measured FILLED -> the mentor verdict clears (resumes only on a measurement).
	// The hole is in the state after its re-requests are spent, the way a hole
	// two sessions old stands; the owner reloads NT8 history and the minutes land.
	h.tr.mu.Lock()
	hole := h.tr.holes[0]
	hole.State, hole.Attempts, hole.slowCount = holeStateUnfilled, 12, barHoleSlowCapTotal
	hole.saidSlowCap = true
	hole.nextAttemptAt, hole.measureAt = time.Time{}, time.Time{}
	h.tr.mu.Unlock()
	h.tr.tick(now.Add(2 * time.Minute))
	if v := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, now.Add(2*time.Minute)); !v.Blocked {
		t.Fatalf("an unrefilled hole keeps blocking the mentor verdict: %+v", v)
	}
	for m := ct(2026, 10, 27, 9, 30); m.Before(ct(2026, 10, 27, 10, 40)); m = m.Add(time.Minute) {
		h.st.mu.Lock()
		h.st.times = insertSorted(h.st.times, m.UnixMilli())
		h.st.mu.Unlock()
	}
	h.tr.tick(now.Add(4 * time.Minute))
	if v := IntegrityVerdictForMentor("MNQ", IntegrityIntradayOpenMin, now.Add(4*time.Minute)); v.Blocked {
		t.Fatalf("a hole measured filled must release the mentor verdict: %+v\n%s", v, h.say.dump())
	}
}

func insertSorted(a []int64, v int64) []int64 {
	i := 0
	for i < len(a) && a[i] < v {
		i++
	}
	if i < len(a) && a[i] == v {
		return a
	}
	a = append(a, 0)
	copy(a[i+1:], a[i:])
	a[i] = v
	return a
}

// SeriesRTHHoles judges the series the mentor seed walked (the stitched 1m): an
// RTH hole of any age inside the 90-day reach, whatever contract it came from.
func TestSeriesRTHHolesJudgesTheSeededSeries(t *testing.T) {
	now := ct(2026, 10, 29, 9, 0)
	build := func(skip func(m time.Time) bool) []int64 {
		var ts []int64
		for m := ct(2026, 7, 1, 17, 0); m.Before(now); m = m.Add(time.Minute) {
			if barHoleExpected(m) && (skip == nil || !skip(m)) {
				ts = append(ts, m.UnixMilli())
			}
		}
		return ts
	}
	within := func(from, to time.Time) func(time.Time) bool {
		return func(m time.Time) bool { return !m.Before(from) && m.Before(to) }
	}
	hole := func(ts []int64) []IntegrityHole { return SeriesRTHHoles("MNQ", "seeded 1m series", ts, nil, now) }

	if hs := hole(build(nil)); len(hs) != 0 {
		t.Fatalf("a complete series (the break, weekends and the holiday calendar are not holes): %+v", hs)
	}
	// 28 days back, RTH: found, named in CT, with the open minutes it takes
	hs := hole(build(within(ct(2026, 10, 1, 10, 0), ct(2026, 10, 1, 10, 37))))
	if len(hs) != 1 || hs[0].Window != "10-01 10:00–10:37 CT" || hs[0].Missing != 37 || hs[0].State != "seeded" {
		t.Fatalf("an RTH hole 28 days back must be returned: %+v", hs)
	}
	// overnight: feeds no 1H RTH candle
	if hs := hole(build(within(ct(2026, 10, 2, 2, 0), ct(2026, 10, 2, 2, 40)))); len(hs) != 0 {
		t.Fatalf("an overnight stretch takes no RTH minute: %+v", hs)
	}
	// older than the reach: out
	if hs := hole(build(within(ct(2026, 7, 10, 10, 0), ct(2026, 7, 10, 10, 37)))); len(hs) != 0 {
		t.Fatalf("a hole older than %d days is outside the reach: %+v", IntegritySeedReachDays, hs)
	}
	// under the minimum gap: not a hole
	if hs := hole(build(within(ct(2026, 10, 1, 10, 0), ct(2026, 10, 1, 10, 2)))); len(hs) != 0 {
		t.Fatalf("two missing minutes are under BARS_HOLE_MIN_GAP_MIN: %+v", hs)
	}
	// a stretch that begins in the overnight and ends inside RTH counts (it takes RTH minutes)
	hs = hole(build(within(ct(2026, 10, 5, 7, 0), ct(2026, 10, 5, 9, 0))))
	if len(hs) != 1 || hs[0].Window != "10-05 07:00–09:00 CT" {
		t.Fatalf("a stretch that crosses 08:30 takes RTH minutes: %+v", hs)
	}
}

// A flood of holes past the retained-list cap must not let the oldest vanish and
// read as clear (review round 2, reviewer A [C]): the verdict turns UNKNOWN.
func TestRetiredListOverflowIsUnknownNotClear(t *testing.T) {
	h := newMemHarness(t)
	h.run(ct(2026, 10, 28, 8, 0), ct(2026, 10, 28, 8, 30))
	now := ct(2026, 10, 28, 8, 31)
	if v := h.verdict(IntegrityIntradayOpenMin, now); v.Blocked {
		t.Fatalf("setup: clean: %+v", v)
	}
	h.tr.mu.Lock()
	for i := 0; i <= integrityRetiredCap; i++ {
		from := ct(2026, 10, 20, 9, 0).Add(time.Duration(i) * 10 * time.Minute)
		h.tr.keepLocked(&barHole{Symbol: "MNQ", Contract: ghContract, FromMs: from.UnixMilli(), ToMs: from.Add(5 * time.Minute).UnixMilli(), Missing: 5, State: "retired"})
	}
	h.tr.mu.Unlock()
	v := h.verdict(IntegrityIntradayOpenMin, now)
	if !v.Blocked || v.Class != IntegrityClassUnknown || !strings.Contains(v.Unknown, "dropped") {
		t.Fatalf("an overflowed retained list must be UNKNOWN (blocked), never clear: %+v", v)
	}
}
