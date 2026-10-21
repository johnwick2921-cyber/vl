package ninjatrader

import "testing"

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

func TestAggregate1mToTF(t *testing.T) {
	oneMin := []Bar{
		{T: 0, O: 10, H: 12, L: 9, C: 11, V: 1},
		{T: 60_000, O: 11, H: 14, L: 10, C: 13, V: 1},
		{T: 120_000, O: 13, H: 15, L: 12, C: 14, V: 1},
		{T: 180_000, O: 14, H: 16, L: 13, C: 15, V: 1},
		{T: 240_000, O: 15, H: 17, L: 14, C: 16, V: 1},
		{T: 300_000, O: 16, H: 18, L: 15, C: 17, V: 1},
		{T: 360_000, O: 17, H: 19, L: 16, C: 18, V: 1},
		{T: 420_000, O: 18, H: 20, L: 17, C: 19, V: 1},
		{T: 480_000, O: 19, H: 21, L: 18, C: 20, V: 1},
		{T: 540_000, O: 20, H: 22, L: 19, C: 21, V: 1},
	}
	got := Aggregate1mToTF(oneMin, 5, 0)
	if len(got) != 2 {
		t.Fatalf("want 2 complete buckets, got %d", len(got))
	}
	b := got[0]
	if b.T != 0 || b.O != 10 || b.H != 17 || b.L != 9 || b.C != 16 || b.V != 5 {
		t.Fatalf("wrong aggregate bucket 0: %+v", b)
	}
	if got[1].T != 300_000 || got[1].C != 21 || got[1].H != 22 || got[1].L != 15 {
		t.Fatalf("wrong aggregate bucket 5: %+v", got[1])
	}
}

// TestAggregate1mToTFDropsHoleyBucket is the m148 pin (a): the 00:31→00:39
// 8-minute hole must leave the touched 5m buckets ABSENT after aggregation, with
// the complete neighbours still present. MUTANT: drop the coverage check (always
// insert) → bucket 00:30 is inserted with a fake close → RED.
func TestAggregate1mToTFDropsHoleyBucket(t *testing.T) {
	var oneMin []Bar
	for m := int64(0); m <= 59; m++ {
		if m >= 31 && m <= 39 {
			continue // the 8-minute hole
		}
		c := 100 + 0.1*float64(m)
		oneMin = append(oneMin, Bar{T: m * 60_000, O: c, H: c + 0.2, L: c - 0.2, C: c, V: 1})
	}
	got := Aggregate1mToTF(oneMin, 5, 0)
	have := map[int64]bool{}
	for _, b := range got {
		have[b.T/60_000] = true
	}
	if !have[25] {
		t.Fatalf("complete neighbour bucket 00:25 must be present, got %v", have)
	}
	if have[30] {
		t.Fatalf("holey bucket 00:30 (4 missing minutes) must be absent, got %v", have)
	}
	if have[35] {
		t.Fatalf("bucket 00:35 (all missing) must be absent, got %v", have)
	}
	if !have[40] {
		t.Fatalf("complete neighbour bucket 00:40 must be present, got %v", have)
	}
}

// TestAggregate1mToTFAllowsOneMissingMinute is the m148 pin (b): a single quiet
// no-trade minute must NOT drop a bucket.
func TestAggregate1mToTFAllowsOneMissingMinute(t *testing.T) {
	var oneMin []Bar
	for m := int64(0); m <= 4; m++ {
		if m == 2 {
			continue // one quiet minute
		}
		c := 100 + 0.1*float64(m)
		oneMin = append(oneMin, Bar{T: m * 60_000, O: c, H: c + 0.2, L: c - 0.2, C: c, V: 1})
	}
	got := Aggregate1mToTF(oneMin, 5, 0)
	if len(got) != 1 {
		t.Fatalf("a bucket with one missing minute must still be inserted, got %d buckets", len(got))
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

func ema50(bars []Bar) float64 {
	k := 2.0 / 51.0
	ema := bars[0].C
	for _, b := range bars[1:] {
		ema = ema + k*(b.C-ema)
	}
	return ema
}
