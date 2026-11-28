package mentor

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"vl/market"
)

// M413 / R7: the bot builds its 2m/5m/15m/30m candles from 1m bars with an
// EPOCH-aligned floor (bucketOpen). NinjaTrader's session-based bars for CME
// MNQ start at 17:00 CT, a whole UTC hour in both CST (23:00Z) and CDT
// (22:00Z), so every TF that divides 60 minutes is on the same grid. These
// tests PIN that equivalence: a 2m bucket opens at 08:30, 08:32, ...; no bucket
// straddles the 16:00-17:00 CT break; DST weeks stay aligned; and the 5m built
// from stored 1m equals NT8's own stored 5m on a real fixture (MNQ 12-26,
// 2026-10-21 08:30-09:30 CT, copied from data.db bars, source live).
// Data comparison over 2026-10-16..23: 5m 1361/1361, 15m 453/453, 30m 226/226
// bucket OPEN times identical to NT8's.

func m413ct(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func m413Load(t *testing.T, p string) []market.Kline {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var out []market.Kline
	for _, r := range rs {
		ms, _ := strconv.ParseInt(r[0], 10, 64)
		pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
		out = append(out, market.Kline{OpenTime: ms, Open: pf(1), High: pf(2), Low: pf(3), Close: pf(4), Volume: pf(5), CloseTime: ms + 59_999})
	}
	return out
}

func m413Mins(loc *time.Location, y int, mo time.Month, d, h, mi int) int64 {
	return time.Date(y, mo, d, h, mi, 0, 0, loc).UnixMilli()
}

func TestM413_2mBucketsOpenOnSessionGrid(t *testing.T) {
	loc := m413ct(t)
	// 17:00 CT session open and 08:30 CT RTH open sit on a 2m boundary.
	for _, tc := range []struct {
		h, m int
		want bool
	}{{8, 30, true}, {8, 31, false}, {8, 32, true}, {9, 0, true}, {17, 0, true}, {17, 1, false}} {
		ms := m413Mins(loc, 2026, 10, 21, tc.h, tc.m)
		got := bucketOpen(ms, 2) == ms
		if got != tc.want {
			t.Fatalf("2m bucketOpen(%02d:%02d CT) on-boundary=%v want %v", tc.h, tc.m, got, tc.want)
		}
	}
	// 1m bars 08:30..08:35 -> 2m buckets open 08:30, 08:32, 08:34.
	var bars []market.Kline
	for i := 0; i < 6; i++ {
		ms := m413Mins(loc, 2026, 10, 21, 8, 30+i)
		bars = append(bars, market.Kline{OpenTime: ms, Open: 1, High: 2, Low: 0.5, Close: 1.5, CloseTime: ms + 59_999})
	}
	agg := barsTF(bars, 2)
	if len(agg) != 3 {
		t.Fatalf("want 3 2m buckets, got %d", len(agg))
	}
	for i, a := range agg {
		want := m413Mins(loc, 2026, 10, 21, 8, 30+2*i)
		if a.OpenTime != want {
			t.Fatalf("2m bucket %d opens %s want %s", i, time.UnixMilli(a.OpenTime).In(loc), time.UnixMilli(want).In(loc))
		}
	}
}

func TestM413_NoBucketStraddlesCMEBreakOrDST(t *testing.T) {
	loc := m413ct(t)
	// Days: ordinary CDT, the CDT->CST day (2026-11-01), CST, the CST->CDT day
	// (2026-03-08) and the day after it.
	days := [][3]int{{2026, 10, 21}, {2026, 11, 1}, {2026, 11, 2}, {2026, 3, 8}, {2026, 3, 9}}
	for _, dd := range days {
		for _, tf := range []int{2, 3, 5, 15, 30, 60} {
			// every minute 15:00-17:59 CT of the day
			for mi := 15 * 60; mi < 18*60; mi++ {
				ms := time.Date(dd[0], time.Month(dd[1]), dd[2], 0, mi, 0, 0, loc).UnixMilli()
				open := bucketOpen(ms, tf)
				cl := open + int64(tf)*60_000
				oc := time.UnixMilli(open).In(loc)
				brk := time.Date(dd[0], time.Month(dd[1]), dd[2], 16, 0, 0, 0, loc).UnixMilli()
				if open < brk && brk < cl {
					t.Fatalf("%v tf=%d bucket %s-%s straddles the 16:00 CT break", dd, tf, oc.Format("15:04"), time.UnixMilli(cl).In(loc).Format("15:04"))
				}
				// session grid: open minus 17:00 CT of the session is a whole number of TFs
				sess := time.Date(dd[0], time.Month(dd[1]), dd[2], 17, 0, 0, 0, loc)
				if oc.Hour() < 17 {
					sess = sess.AddDate(0, 0, -1)
				}
				if (open-sess.UnixMilli())%(int64(tf)*60_000) != 0 {
					t.Fatalf("%v tf=%d open %s is off the 17:00 CT session grid", dd, tf, oc)
				}
			}
		}
	}
}

func TestM413_5mBucketsEqualNT8Fixture(t *testing.T) {
	loc := m413ct(t)
	one := m413Load(t, "testdata/m413_mnq_1m_20261021_0830ct.csv")
	nt := m413Load(t, "testdata/m413_nt8_5m_20261021_0830ct.csv")
	if len(one) != 60 || len(nt) != 12 {
		t.Fatalf("fixture sizes 1m=%d 5m=%d", len(one), len(nt))
	}
	if got := time.UnixMilli(nt[0].OpenTime).In(loc).Format("15:04"); got != "08:30" {
		t.Fatalf("fixture first NT8 5m opens %s want 08:30", got)
	}
	agg := barsTF(one, 5)
	if len(agg) != len(nt) {
		t.Fatalf("bot 5m buckets %d, NT8 %d", len(agg), len(nt))
	}
	for i := range nt {
		a, n := agg[i], nt[i]
		if a.OpenTime != n.OpenTime {
			t.Fatalf("5m[%d] open bot %s nt8 %s", i, time.UnixMilli(a.OpenTime).In(loc), time.UnixMilli(n.OpenTime).In(loc))
		}
		if a.Open != n.Open || a.High != n.High || a.Low != n.Low || a.Close != n.Close {
			t.Fatalf("5m[%d] %s OHLC bot %v/%v/%v/%v nt8 %v/%v/%v/%v", i, time.UnixMilli(a.OpenTime).In(loc).Format("15:04"), a.Open, a.High, a.Low, a.Close, n.Open, n.High, n.Low, n.Close)
		}
	}
	// 2m on the same fixture opens 08:30, 08:32, ... and the 2m at 09:00 is on the grid.
	a2 := barsTF(one, 2)
	if len(a2) != 30 {
		t.Fatalf("2m buckets %d want 30", len(a2))
	}
	if time.UnixMilli(a2[0].OpenTime).In(loc).Format("15:04") != "08:30" || time.UnixMilli(a2[15].OpenTime).In(loc).Format("15:04") != "09:00" {
		t.Fatalf("2m grid: first %s, [15] %s", time.UnixMilli(a2[0].OpenTime).In(loc).Format("15:04"), time.UnixMilli(a2[15].OpenTime).In(loc).Format("15:04"))
	}
}
