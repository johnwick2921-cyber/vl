package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestBarsTFClockAlignedOverlap — B1: two windows that start 1 minute apart
// produce IDENTICAL buckets over their overlap; buckets never re-anchor on a
// slide. (Mutant: anchor to the first bar → RED.)
func TestBarsTFClockAlignedOverlap(t *testing.T) {
	// window A starts 09:00:00 CT, window B starts 09:01:00 CT, both covering
	// 20 minutes of 1m bars.
	start := int64(9*60) * 60_000
	mk := func(s int64) []market.Kline {
		out := make([]market.Kline, 0, 20)
		for i := 0; i < 20; i++ {
			open := s + int64(i)*60_000
			out = append(out, market.Kline{OpenTime: open, CloseTime: open + 59_999, High: float64(i), Low: float64(i) - 1, Close: float64(i) - 0.5})
		}
		return out
	}
	a := barsTF(mk(start), 5)
	b := barsTF(mk(start+60_000), 5)
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("empty aggregation")
	}
	// every bucket in B beyond the first minute equals a bucket in A (or is a
	// strict extension of the last one); the shared buckets must align
	// exactly on open time.
	overlap := map[int64]market.Kline{}
	for _, x := range a {
		overlap[x.OpenTime] = x
	}
	matched := 0
	for _, y := range b {
		if x, ok := overlap[y.OpenTime]; ok {
			if x.OpenTime != y.OpenTime || x.CloseTime != y.CloseTime {
				t.Fatalf("overlap bucket shifted: A %+v vs B %+v", x, y)
			}
			matched++
		}
	}
	if matched == 0 {
		t.Fatal("no shared buckets between the two windows")
	}
	t.Logf("overlap buckets matched: %d/%d", matched, len(b))
}

// TestBarsTF4HAnchoredTo1700CT — B1: the 4h buckets anchor to the CME session

// open 17:00 CT: 17–21, 21–01, 01–05, 05–09, 09–13, 13–16. Real-UTC epochs
// through America/Chicago (EPOCH RULING 2026-10-03), on a CDT day and a CST
// day.
func TestBarsTF4HAnchoredTo1700CT(t *testing.T) {
	for _, c := range auditDates {
		for _, tc := range []struct {
			hh, mm int
			wantHh int
			wantD  int // day offset from c.d
		}{
			{17, 0, 17, 0}, // 17:00 → bucket 17:00
			{20, 0, 17, 0}, // 20:00 → 17:00 bucket
			{21, 0, 21, 0}, // 21:00 → new bucket
			{0, 0, 21, -1}, // 00:00 → previous day's 21:00 bucket
			{1, 0, 1, 0},   // 01:00 → 01:00 bucket
			{9, 0, 9, 0},   // 09:00 → 09:00
			{13, 0, 13, 0}, // 13:00 → 13:00
			{16, 0, 13, 0}, // 16:00 → 13:00 bucket (last, 13–16)
		} {
			got := bucketOpen(auditMs(c.y, c.mo, c.d, tc.hh, tc.mm, 0), 240)
			want := auditMs(c.y, c.mo, c.d+tc.wantD, tc.wantHh, 0, 0)
			if got != want {
				t.Fatalf("%s %02d:%02d: bucketOpen(4h) = %s, want %s",
					c.label, tc.hh, tc.mm,
					time.UnixMilli(got).In(ctime()).Format("01-02 15:04"),
					time.UnixMilli(want).In(ctime()).Format("01-02 15:04"))
			}

		}
	}
}

// TestClosedBucketsDropForming — B4: the still-forming 5m bucket is dropped so
// the 15m/5m conflict reads CLOSED candles only [D4.2 p1 @ 05:10].
func TestClosedBucketsDropForming(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	start := int64(9*60) * 60_000 // 09:00 CT
	bars := []market.Kline{
		{OpenTime: start, CloseTime: start + 59_999, High: 1, Low: 0, Close: 0.5},                       // 1m bar in the 09:00 bucket
		{OpenTime: start + 5*60_000, CloseTime: start + 5*60_000 + 59_999, High: 2, Low: 1, Close: 1.5}, // first bar of the 09:05 bucket
	}
	// now = inside the 09:05 bucket → the 09:00 bucket is the only closed one
	closed := closedBuckets(bars, start+6*60_000, cfg)
	if len(closed) != 1 || closed[0].OpenTime != start {
		t.Fatalf("closedBuckets = %+v, want only the 09:00 bucket", closed)
	}
	// now = after the 09:05 bucket closes → both are closed
	closed = closedBuckets(bars, start+10*60_000, cfg)
	if len(closed) != 2 {
		t.Fatalf("closedBuckets after the close = %d buckets, want 2", len(closed))
	}
}
