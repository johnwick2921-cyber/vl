package kernel

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// mk1mCT returns 1m bars from hhmmFrom to hhmmTo (inclusive) on the given CT
// date, each with a distinct close (the minute index), so the aggregate bucket
// boundaries are observable from the OpenTimes alone.
func mk1mCT(t *testing.T, year int, month time.Month, day, fromHH, fromMM, toHH, toMM int) []market.Kline {
	t.Helper()
	loc := CTLocation()
	from := time.Date(year, month, day, fromHH, fromMM, 0, 0, loc)
	to := time.Date(year, month, day, toHH, toMM, 0, 0, loc)
	var out []market.Kline
	for i, cur := 0, from; !cur.After(to); i, cur = i+1, cur.Add(time.Minute) {
		c := float64(1000 + i)
		out = append(out, market.Kline{
			OpenTime:  cur.UnixMilli(),
			CloseTime: cur.Add(time.Minute - 1).UnixMilli(),
			Open:      c, High: c + 1, Low: c - 1, Close: c, Volume: 1,
		})
	}
	return out
}

// ctHHMM returns the CT HH:MM of a ms timestamp (bucket start) as "HH:MM".
func ctHHMM(ms int64) string {
	return time.UnixMilli(ms).In(CTLocation()).Format("15:04")
}

// TestFourHBucketStart_17CTAnchor pins the ONE 4h anchor: a bar before 17:00 CT
// belongs to the bucket that STARTED at 13:00 CT; a bar at/after 17:00 CT starts
// the 17:00 CT bucket. CDT and CST both (the UTC offset differs, the CT anchor
// must not).
func TestFourHBucketStart_17CTAnchor(t *testing.T) {
	cases := []struct {
		name string
		year int
		mon  time.Month
		day  int
	}{
		{"CDT", 2026, time.August, 17},
		{"CST", 2026, time.December, 14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loc := CTLocation()
			at := func(hh, mm int) int64 { return time.Date(c.year, c.mon, c.day, hh, mm, 0, 0, loc).UnixMilli() }
			if got := ctHHMM(FourHBucketStart(at(16, 30), loc)); got != "13:00" {
				t.Fatalf("16:30 CT → bucket %s, want 13:00", got)
			}
			if got := ctHHMM(FourHBucketStart(at(17, 0), loc)); got != "17:00" {
				t.Fatalf("17:00 CT → bucket %s, want 17:00", got)
			}
			if got := ctHHMM(FourHBucketStart(at(21, 0), loc)); got != "21:00" {
				t.Fatalf("21:00 CT → bucket %s, want 21:00", got)
			}
		})
	}
}

// TestAggregate4HCT_17CTBoundaries pins the planner's 4h candle boundaries: a
// fixture spanning 16:00–23:00 CT must produce 4h buckets starting at
// 13:00 / 17:00 / 21:00 CT — NOT the UTC-midnight grid AggregateBars would give.
// Mutant (revert to AggregateBars(bars, 240*60_000)): the boundary set moves and
// this test goes RED.
func TestAggregate4HCT_17CTBoundaries(t *testing.T) {
	for _, c := range []struct {
		name string
		year int
		mon  time.Month
		day  int
	}{
		{"CDT", 2026, time.August, 17},
		{"CST", 2026, time.December, 14},
	} {
		t.Run(c.name, func(t *testing.T) {
			bars := mk1mCT(t, c.year, c.mon, c.day, 16, 0, 22, 59)
			got := Aggregate4HCT(bars)
			var starts []string
			for _, r := range got {
				starts = append(starts, ctHHMM(r.OpenTime))
			}
			if len(starts) != 3 || starts[0] != "13:00" || starts[1] != "17:00" || starts[2] != "21:00" {
				t.Fatalf("Aggregate4HCT bucket starts = %v, want [13:00 17:00 21:00]", starts)
			}
			// The OLD epoch-floor 4h must NOT agree — this is the named RED guard.
			epoch := AggregateBars(bars, 240*60_000)
			if len(epoch) == len(got) {
				same := true
				for i := range epoch {
					if epoch[i].OpenTime != got[i].OpenTime {
						same = false
						break
					}
				}
				if same {
					t.Fatalf("epoch-floor AggregateBars(4h) must differ from the 17:00 CT anchor; both start at %v", starts)
				}
			}
		})
	}
}

// TestBuildPlannerCandleTablesAt_4hAnchorDisclosure pins the P3-1 header note:
// the candle table says the 4h is 17:00 CT session-anchored, so the offset is
// on the record in the prompt.
func TestBuildPlannerCandleTablesAt_4hAnchorDisclosure(t *testing.T) {
	bars := mk1mCT(t, 2026, time.August, 17, 9, 0, 23, 59)
	out := BuildPlannerCandleTablesAt(bars, len(bars), time.Date(2026, time.August, 18, 0, 0, 0, 0, CTLocation()))
	if !strings.Contains(out, "4h = 17:00 CT session-anchored") {
		t.Fatalf("the candle-table header must state the 4h anchor:\n%s", out[:400])
	}
}

// TestBuildPlannerCandleTablesAt_4hRowsOn17CTBoundaries is the NAMED RED pin at
// the call site: the rendered "### 4h" section must show bucket starts at 17:00
// and 21:00 CT. Reverting the 4h table to AggregateBars (UTC-midnight buckets)
// moves the starts to 15:00/19:00 CT (CDT) and this test goes RED.
func TestBuildPlannerCandleTablesAt_4hRowsOn17CTBoundaries(t *testing.T) {
	for _, c := range []struct {
		name string
		year int
		mon  time.Month
		day  int
	}{
		{"CDT", 2026, time.August, 17},
		{"CST", 2026, time.December, 14},
	} {
		t.Run(c.name, func(t *testing.T) {
			bars := mk1mCT(t, c.year, c.mon, c.day, 16, 0, 23, 59)
			out := BuildPlannerCandleTablesAt(bars, len(bars), time.Date(c.year, c.mon, c.day+1, 0, 0, 0, 0, CTLocation()))
			i4h := strings.Index(out, "### 4h")
			iDaily := strings.Index(out, "### daily")
			if i4h < 0 || iDaily <= i4h {
				t.Fatalf("expected the 4h section before the daily section:\n%s", out[:600])
			}
			section := out[i4h:iDaily]
			if !strings.Contains(section, "17:00") || !strings.Contains(section, "21:00") {
				t.Fatalf("the 4h section must start buckets at 17:00 and 21:00 CT:\n%s", section)
			}
		})
	}
}
