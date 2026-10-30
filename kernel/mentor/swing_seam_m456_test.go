package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// m456 defect 2 — holedFourHBuckets counts a minute present when ANY contract
// of the stitch stores it (RollStitcher.SeamCovered). Unit pins of the walker;
// the production call sites (seed + re-seed) are pinned in trader/.

func seamMinutes(from, to time.Time) []int64 {
	var out []int64
	for m := from; m.Before(to); m = m.Add(time.Minute) {
		out = append(out, m.UnixMilli())
	}
	return out
}

// emptyBucketBars is a 1h tape that skips the 09-10 01:00-05:00 CT bucket.
func emptyBucketBars() []market.Kline {
	loc := ctime()
	var out []market.Kline
	for h := time.Date(2026, 9, 9, 17, 0, 0, 0, loc); h.Before(time.Date(2026, 9, 10, 16, 0, 0, 0, loc)); h = h.Add(time.Hour) {
		if !h.Before(time.Date(2026, 9, 10, 1, 0, 0, 0, loc)) && h.Before(time.Date(2026, 9, 10, 5, 0, 0, 0, loc)) {
			continue
		}
		out = append(out, market.Kline{OpenTime: h.UnixMilli(), CloseTime: h.UnixMilli() + 3_599_999, Close: 100})
	}
	return out
}

func TestHoledFourHBuckets_SeamMinutesCountAsPresent(t *testing.T) {
	loc := ctime()
	bars := emptyBucketBars()
	from := time.Date(2026, 9, 9, 17, 0, 0, 0, loc).UnixMilli()
	to := time.Date(2026, 9, 10, 9, 0, 0, 0, loc).UnixMilli()
	s0, s1 := time.Date(2026, 9, 10, 1, 0, 0, 0, loc), time.Date(2026, 9, 10, 5, 0, 0, 0, loc)

	if holes, _ := holedFourHBuckets(bars, 3_600_000, from, to, nil); len(holes) != 1 || holes[0].Start != s0.UnixMilli() {
		t.Fatalf("control: the empty open bucket is a hole, got %v", holes)
	}
	if holes, _ := holedFourHBuckets(bars, 3_600_000, from, to, seamMinutes(s0, s1)); len(holes) != 0 {
		t.Fatalf("a bucket whose every minute some contract stores is not a hole, got %v", holes)
	}
	// all but 61 minutes stored: 61 open minutes still uncovered -> a hole;
	// all but 59: below the floor -> not a hole.
	if holes, _ := holedFourHBuckets(bars, 3_600_000, from, to, seamMinutes(s0, s1.Add(-61*time.Minute))); len(holes) != 1 {
		t.Fatalf("61 uncovered open minutes are still a hole, got %v", holes)
	}
	if holes, _ := holedFourHBuckets(bars, 3_600_000, from, to, seamMinutes(s0, s1.Add(-59*time.Minute))); len(holes) != 0 {
		t.Fatalf("59 uncovered open minutes are under the floor, got %v", holes)
	}
	// seam minutes of ANOTHER bucket do not excuse this one.
	if holes, _ := holedFourHBuckets(bars, 3_600_000, from, to, seamMinutes(s0.Add(-3*time.Hour), s0)); len(holes) != 1 {
		t.Fatalf("seam minutes outside the bucket must not cover it, got %v", holes)
	}
}
