package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// m456 defect 2 — unit pins of the walker and of the seam merge; the production
// call sites (seed + re-seed) are pinned in trader/.

// emptyBucketBars is a 1h tape that skips the 09-10 01:00-05:00 CT bucket.
func emptyBucketBars(skip bool) []market.Kline {
	loc := ctime()
	var out []market.Kline
	for h := time.Date(2026, 9, 9, 17, 0, 0, 0, loc); h.Before(time.Date(2026, 9, 10, 16, 0, 0, 0, loc)); h = h.Add(time.Hour) {
		if skip && !h.Before(time.Date(2026, 9, 10, 1, 0, 0, 0, loc)) && h.Before(time.Date(2026, 9, 10, 5, 0, 0, 0, loc)) {
			continue
		}
		out = append(out, market.Kline{OpenTime: h.UnixMilli(), CloseTime: h.UnixMilli() + 3_599_999, Close: 100})
	}
	return out
}

// The walker is a pure presence test on the series it is handed: the seam is
// no longer a parameter (the seed hands it the series WITH the seam bars, see
// swing_seam_value_m456_test.go), so a bucket with bars is not a hole and the
// same bucket without them is.
func TestHoledFourHBuckets_PresenceIsTheWholeTest(t *testing.T) {
	loc := ctime()
	from := time.Date(2026, 9, 9, 17, 0, 0, 0, loc).UnixMilli()
	to := time.Date(2026, 9, 10, 9, 0, 0, 0, loc).UnixMilli()
	s0 := time.Date(2026, 9, 10, 1, 0, 0, 0, loc)
	if holes, _ := holedFourHBuckets(emptyBucketBars(true), 3_600_000, from, to); len(holes) != 1 || holes[0].Start != s0.UnixMilli() {
		t.Fatalf("an open bucket with no bars is a hole, got %v", holes)
	}
	if holes, _ := holedFourHBuckets(emptyBucketBars(false), 3_600_000, from, to); len(holes) != 0 {
		t.Fatalf("a bucket with bars is not a hole, got %v", holes)
	}
}

// withSeamBars: ordered merge, closed-by-now only, no duplicate minute, and the
// identity when there is no seam.
func TestWithSeamBars(t *testing.T) {
	k := func(ms int64, c float64) market.Kline {
		return market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Close: c}
	}
	base := []market.Kline{k(0, 1), k(60_000, 2), k(240_000, 5)}
	seam := []market.Kline{k(120_000, 3), k(180_000, 4), k(240_000, 99), k(300_000, 6)}
	got := withSeamBars(base, seam, 1<<40)
	want := []int64{0, 60_000, 120_000, 180_000, 240_000, 300_000}
	if len(got) != len(want) {
		t.Fatalf("merged %d bars, want %d: %+v", len(got), len(want), got)
	}
	for i, b := range got {
		if b.OpenTime != want[i] {
			t.Fatalf("bar %d open %d, want %d", i, b.OpenTime, want[i])
		}
	}
	if got[4].Close != 5 {
		t.Fatalf("a minute the series already has keeps the series bar, got close %v", got[4].Close)
	}
	if g := withSeamBars(base, seam, 180_000+59_999); len(g) != 5 { // 240_000 dup skipped, 300_000 not closed
		t.Fatalf("seam bars that close after now must not be folded, got %d bars", len(g))
	}
	if g := withSeamBars(base, nil, 1<<40); &g[0] != &base[0] {
		t.Fatal("no seam bars: the series is returned as is")
	}
}
