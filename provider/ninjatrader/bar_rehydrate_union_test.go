package ninjatrader

import (
	"testing"
	"time"

	"vl/kernel"
)

// ── m134 PINS (2026-10-21) — RehydrateUnion fills an INTERIOR restart hole ──
//
// A short NT8 replay tail leaves a gap between the replay tail and the first
// live bar. RehydrateOlder only reaches strictly-OLDER bars, so the interior
// hole survives and the 5m EMA50 is computed over a broken series (wrong by up
// to 125 pts on 10-20). RehydrateUnion merges by time-UNION: the store fills
// both the older end AND the interior, and a bar the ring already holds wins
// on overlap (a live bar is never replaced).

func open5m(minutes int64) int64 { return minutes * 60_000 }

func TestRehydrateUnionFillsInteriorHole(t *testing.T) {
	c := NewBarCache(2500)
	// The ring after a restart with a short replay: replay tail 08:05/08:10,
	// live head 11:20/11:25 — an interior hole between 08:10 and 11:20.
	// SeedHistorical stamps CLOSE→OPEN (T -= 5m), so hand it close-stamped
	// times and the ring lands on the open-stamped grid.
	ringClose := []Bar{
		{T: open5m(8*60 + 10), O: 100, H: 102, L: 98, C: 101},   // opens 08:05
		{T: open5m(8*60 + 15), O: 101, H: 103, L: 99, C: 102},   // opens 08:10
		{T: open5m(11*60 + 25), O: 130, H: 132, L: 128, C: 131}, // opens 11:20
		{T: open5m(11*60 + 30), O: 131, H: 133, L: 129, C: 132}, // opens 11:25
	}
	c.SeedHistorical("MNQ", "5m", ringClose)
	if got := len(c.Get("MNQ", "5m")); got != 4 {
		t.Fatalf("ring holds %d bars, want 4", got)
	}
	// Store-derived complete 5m series (aggregated from complete 1m),
	// open-stamped: 08:05..11:25 every 5 minutes, a DIFFERENT close so a merge
	// that let the store win on the live tail is visible.
	var store []Bar
	for m := int64(8*60 + 5); m <= 11*60+25; m += 5 {
		store = append(store, Bar{T: open5m(m), O: 100, H: 103, L: 97, C: 101, Source: BarSourceHistorical})
	}
	added := c.RehydrateUnion("MNQ", "5m", store)
	after := c.Get("MNQ", "5m")
	// (11:25-08:05)/5 + 1 = 41 buckets, every one present — no hole.
	if len(after) != 41 {
		t.Fatalf("ring holds %d bars after union, want 41 (hole filled); added=%d", len(after), added)
	}
	for i := 1; i < len(after); i++ {
		if d := after[i].T - after[i-1].T; d != 5*60_000 {
			t.Fatalf("hole remains at %d: gap %dms", i, d)
		}
	}
	// The live tail (11:20, 11:25) must NOT have been replaced by the store
	// (store C=101; live C=131/132).
	tail := after[len(after)-2:]
	if tail[0].C != 131 || tail[1].C != 132 {
		t.Fatalf("live tail was replaced: C=%v,%v want 131,132", tail[0].C, tail[1].C)
	}
}

// TestRehydrateOlderLeavesInteriorHole pins the MUTANT: the pre-m134 path
// (older-only) cannot fill the interior hole — which is exactly the restart
// chart hole the fix exists for.
func TestRehydrateOlderLeavesInteriorHole(t *testing.T) {
	c := NewBarCache(2500)
	ringClose := []Bar{
		{T: open5m(8*60 + 10), O: 100, H: 102, L: 98, C: 101},
		{T: open5m(8*60 + 15), O: 101, H: 103, L: 99, C: 102},
		{T: open5m(11*60 + 25), O: 130, H: 132, L: 128, C: 131},
		{T: open5m(11*60 + 30), O: 131, H: 133, L: 129, C: 132},
	}
	c.SeedHistorical("MNQ", "5m", ringClose)
	oldest := c.Get("MNQ", "5m")[0].T
	var store []Bar
	for m := int64(8*60 + 5); m <= 11*60+25; m += 5 {
		if open5m(m) < oldest {
			store = append(store, Bar{T: open5m(m), O: 100, H: 103, L: 97, C: 101, Source: BarSourceHistorical})
		}
	}
	c.RehydrateOlder("MNQ", "5m", store)
	after := c.Get("MNQ", "5m")
	for i := 1; i < len(after); i++ {
		if d := after[i].T - after[i-1].T; d > 5*60_000 {
			return // the interior hole survives — as documented
		}
	}
	t.Fatalf("expected the interior hole to survive RehydrateOlder (older-only)")
}

// oneMinFromCT builds one 1m bar per minute for `n` minutes starting at `base`
// (already in CT), skipping the given minute offsets (indexed from 0).
func oneMinFromCT(base time.Time, n int, skip ...int) []Bar {
	skipSet := make(map[int]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}
	bars := make([]Bar, 0, n)
	for i := 0; i < n; i++ {
		if skipSet[i] {
			continue
		}
		c := float64(1000 + i) // integer prices — exact in float64, so == is safe
		bars = append(bars, Bar{T: base.Add(time.Duration(i) * time.Minute).UnixMilli(), O: c, H: c + 2, L: c - 2, C: c, V: 1})
	}
	return bars
}

// bucketKeys indexes aggregated buckets by their minute offset from `base`.
func bucketKeys(bars []Bar, base time.Time) map[int]bool {
	m := make(map[int]bool, len(bars))
	for _, b := range bars {
		m[int((b.T-base.UnixMilli())/60_000)] = true
	}
	return m
}

func TestAggregate1mToTF(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, kernel.CTLocation()) // a Tuesday, RTH open
	oneMin := oneMinFromCT(base, 10)
	got := Aggregate1mToTF(oneMin, 5, 0)
	if len(got) != 2 {
		t.Fatalf("want 2 complete buckets, got %d", len(got))
	}
	b := got[0]
	if b.T != base.UnixMilli() || b.O != 1000 || b.H != 1006 || b.L != 998 || b.C != 1004 || b.V != 5 {
		t.Fatalf("wrong aggregate bucket 0: %+v", b)
	}
	if got[1].T != base.Add(5*time.Minute).UnixMilli() || got[1].C != 1009 || got[1].H != 1011 || got[1].L != 1003 {
		t.Fatalf("wrong aggregate bucket 5: %+v", got[1])
	}
}

// TestAggregate1mToTFDropsHoleyBucket is the m148 pin (a): the minute 31→39
// 8-minute hole must leave the touched 5m buckets ABSENT after aggregation, with
// the complete neighbours still present. MUTANT: drop the coverage check (always
// insert) → bucket 10:30 is inserted with a fake close → RED.
func TestAggregate1mToTFDropsHoleyBucket(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, kernel.CTLocation()) // Tuesday RTH
	oneMin := oneMinFromCT(base, 60, 31, 32, 33, 34, 35, 36, 37, 38, 39)
	got := Aggregate1mToTF(oneMin, 5, 0)
	have := bucketKeys(got, base)
	if !have[25] {
		t.Fatalf("complete neighbour bucket 10:25 must be present, got %v", have)
	}
	if have[30] {
		t.Fatalf("holey bucket 10:30 (4 missing minutes) must be absent, got %v", have)
	}
	if have[35] {
		t.Fatalf("bucket 10:35 (all missing) must be absent, got %v", have)
	}
	if !have[40] {
		t.Fatalf("complete neighbour bucket 10:40 must be present, got %v", have)
	}
}

// TestAggregate1mToTFAllowsOneMissingMinute is the m148 pin (b): a single quiet
// no-trade minute must NOT drop a bucket.
func TestAggregate1mToTFAllowsOneMissingMinute(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, kernel.CTLocation())
	oneMin := oneMinFromCT(base, 5, 2) // one quiet minute
	got := Aggregate1mToTF(oneMin, 5, 0)
	if len(got) != 1 {
		t.Fatalf("a bucket with one missing minute must still be inserted, got %d buckets", len(got))
	}
}

// TestAggregate1mToTFKeepsEarlyCloseBucket is the m168 early-close pin: the
// 12:00 30m/1h bucket on a 12:15-close day has only 15 OPEN minutes (12:00–
// 12:14); the rest are the halted tail. Counting ALL minutes would read 15 (30m)
// / 45 (1h) missing and drop it; counting OPEN minutes keeps it. MUTANT: count
// every minute (the pre-m168 rule) → the 12:00 bucket disappears → RED.
func TestAggregate1mToTFKeepsEarlyCloseBucket(t *testing.T) {
	// 2026-11-27 is the Friday after Thanksgiving — shortened, close 12:15 CT.
	base := time.Date(2026, 11, 27, 8, 0, 0, 0, kernel.CTLocation())
	// Complete 1m bars 08:00..12:14 (255 minutes).
	oneMin := oneMinFromCT(base, 4*60+15)
	noon := time.Date(2026, 11, 27, 12, 0, 0, 0, kernel.CTLocation())
	for _, tc := range []struct {
		tfMin int
	}{
		{30},
		{60},
	} {
		got := Aggregate1mToTF(oneMin, tc.tfMin, 0)
		have := bucketKeys(got, base)
		noonOff := int((noon.UnixMilli() - base.UnixMilli()) / 60_000)
		if !have[noonOff] {
			t.Fatalf("%dm: the 12:00 bucket on an early-close day must be kept (open minutes complete), got %v", tc.tfMin, have)
		}
		// The 12:30 bucket is entirely inside the halted tail — never built.
		if have[noonOff+tc.tfMin] {
			t.Fatalf("%dm: the 12:30 bucket (all halted) must be absent", tc.tfMin)
		}
	}
}

// TestAggregate1mToTFCoverageTolerance is the m168 tolerance + run pin: missing
// OPEN minutes are counted against max(1, tfMin/20) and a run of ≥3 consecutive
// missing open minutes drops the bucket whatever the total.
func TestAggregate1mToTFCoverageTolerance(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, kernel.CTLocation()) // Tuesday RTH
	cases := []struct {
		name  string
		tfMin int
		n     int
		skip  []int
		kept  bool
	}{
		{"5m one missing kept", 5, 5, []int{0}, true},
		{"5m two missing dropped", 5, 5, []int{0, 2}, false},
		{"1h two missing kept", 60, 60, []int{5, 25}, true},
		{"1h three spread missing kept", 60, 60, []int{5, 25, 45}, true},
		{"1h three consecutive missing dropped", 60, 60, []int{5, 6, 7}, false},
		{"1h four missing dropped", 60, 60, []int{5, 25, 45, 50}, false},
	}
	for _, tc := range cases {
		got := Aggregate1mToTF(oneMinFromCT(base, tc.n, tc.skip...), tc.tfMin, 0)
		have := bucketKeys(got, base)
		if have[0] != tc.kept {
			t.Fatalf("%s: kept=%v, want %v (buckets %v)", tc.name, have[0], tc.kept, have)
		}
	}
}

// TestAggregate1mToTFKeepsSundayOpenBucket is the m168 Sunday-open pin: the
// first 5m bucket after the Sunday 17:00 CT open with a quiet first minute must
// still be inserted (the minute is OPEN — the Sunday open is a whole-hour
// boundary, so the 17:00 bucket is all open minutes).
func TestAggregate1mToTFKeepsSundayOpenBucket(t *testing.T) {
	// 2026-10-25 is a Sunday; Globex opens 17:00 CT.
	base := time.Date(2026, 10, 25, 17, 0, 0, 0, kernel.CTLocation())
	oneMin := oneMinFromCT(base, 5, 0) // 17:00 quiet, 17:01..17:04 present
	got := Aggregate1mToTF(oneMin, 5, 0)
	if len(got) != 1 {
		t.Fatalf("the Sunday-open 5m bucket with a quiet first minute must be kept, got %d buckets", len(got))
	}
}

// TestRehydrateUnionColdKeyIsNoOp is the m168 COLD-KEY pin: a store must NEVER
// substitute for a live feed, so RehydrateUnion on a key the ring has never held
// adds nothing. MUTANT: drop/weaken the cold-key guard (treat `existing` as nil
// and proceed) → the store bars fill the empty ring → RED.
func TestRehydrateUnionColdKeyIsNoOp(t *testing.T) {
	c := NewBarCache(2500)
	store := []Bar{{T: open5m(10*60 + 5), O: 100, H: 102, L: 98, C: 101, Source: BarSourceHistorical}}
	if added := c.RehydrateUnion("MNQ", "5m", store); added != 0 {
		t.Fatalf("cold key must add nothing, got %d", added)
	}
	if got := c.Get("MNQ", "5m"); len(got) != 0 {
		t.Fatalf("cold key must stay empty — the store never substitutes for the live feed: %+v", got)
	}
}

// TestRehydrateUnionBootShapeEMA50MatchesRecompute is the m134 10-20 boot-shape
// pin: replay tail through 08:10, live from 11:20, the 1m store complete → the
// 5m EMA50 after the union is within 0.1 of the complete-series recompute.
// MUTANT: RehydrateOlder in place of RehydrateUnion leaves the interior hole
// and the EMA50 is off by far more than 0.1 → RED.
func TestRehydrateUnionBootShapeEMA50MatchesRecompute(t *testing.T) {
	n1m := 305 // minutes 0..304 → 61 complete 5m buckets
	oneMin := make([]Bar, 0, n1m)
	for i := 0; i < n1m; i++ {
		cc := 100 + 0.1*float64(i)
		oneMin = append(oneMin, Bar{T: int64(i) * 60_000, O: cc - 0.05, H: cc + 0.2, L: cc - 0.2, C: cc, V: 1})
	}
	complete5m := Aggregate1mToTF(oneMin, 5, 0) // open-stamped 5m buckets, now=0 → no forming filter

	c := NewBarCache(2500)
	// Boot-shape ring: replay tail buckets T<=70 and live buckets T>=200, with
	// the interior hole 75..195. SeedHistorical subtracts 5m, so hand it
	// close-stamped times (open+5m) to land on the open grid.
	var ring []Bar
	for _, b := range complete5m {
		if b.T <= 70*60_000 || b.T >= 200*60_000 {
			rb := b
			rb.T = b.T + 5*60_000 // close-stamped for SeedHistorical
			ring = append(ring, rb)
		}
	}
	c.SeedHistorical("MNQ", "5m", ring)

	store5m := make([]Bar, len(complete5m))
	copy(store5m, complete5m)
	for i := range store5m {
		store5m[i].Source = BarSourceHistorical // the store's rows enter historical
	}
	c.RehydrateUnion("MNQ", "5m", store5m)

	got := c.Get("MNQ", "5m")
	want := ema50(complete5m)
	have := ema50(got)
	if d := have - want; d < -0.1 || d > 0.1 {
		t.Fatalf("5m EMA50 after union = %.4f, recompute = %.4f (Δ %.4f) — the hole is not filled", have, want, d)
	}
	if len(got) != len(complete5m) {
		t.Fatalf("ring holds %d bars, want the complete %d", len(got), len(complete5m))
	}
}

// TestRehydrateUnionDropsStoreBarsNewerThanTail is the m149 P2 pin: store bars
// NEWER than the live tail are dropped, so a later live update to the forming
// bar REPLACES instead of being refused as out-of-order. MUTANT: re-append
// incoming[j:] in mergeRehydrateUnion → the tail advances to the store bucket,
// the live update drops (outOfOrderDrops=1) and the forming bar freezes → RED.
func TestRehydrateUnionDropsStoreBarsNewerThanTail(t *testing.T) {
	c := NewBarCache(2500)
	// Live ring tail at open 11:20 (seed close-stamped 11:25).
	c.SeedHistorical("MNQ", "5m", []Bar{{T: open5m(11*60 + 25), O: 130, H: 132, L: 128, C: 131}})
	// Store tape: 11:20 (overlap — existing wins) + 11:25 (NEWER than tail —
	// must be dropped).
	store := []Bar{
		{T: open5m(11*60 + 20), O: 100, H: 103, L: 97, C: 101, Source: BarSourceHistorical},
		{T: open5m(11*60 + 25), O: 101, H: 104, L: 98, C: 102, Source: BarSourceHistorical},
	}
	c.RehydrateUnion("MNQ", "5m", store)
	after := c.Get("MNQ", "5m")
	if len(after) != 1 || after[0].T != open5m(11*60+20) || after[0].C != 131 {
		t.Fatalf("store bars newer than the live tail must be dropped: %+v", after)
	}
	// The live feed updates the forming 11:20 bar — it must REPLACE, never drop
	// as out-of-order.
	c.Upsert("MNQ", "5m", []Bar{{T: open5m(11*60 + 25), O: 131, H: 134, L: 129, C: 133}})
	if c.OutOfOrderDrops() != 0 {
		t.Fatalf("live update to the forming bar was dropped as out-of-order: %d", c.OutOfOrderDrops())
	}
	after = c.Get("MNQ", "5m")
	if after[0].C != 133 {
		t.Fatalf("forming bar did not update after the rehydrate: %+v", after)
	}
}

func ema50(bars []Bar) float64 {
	k := 2.0 / 51.0
	ema := bars[0].C
	for _, b := range bars[1:] {
		ema = ema + k*(b.C-ema)
	}
	return ema
}
