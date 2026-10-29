package checks

import (
	"testing"
	"time"

	"vl/kernel"
)

// dstPerfectBars is a gap-free 1m series over [from, to): a bar at EVERY minute
// the real CME calendar (kernel.IsCMEOpen) says is open, nothing else. This is
// what a perfect feed looks like across a DST change.
func dstPerfectBars(from, to time.Time, step int) []Bar {
	var out []Bar
	c := 30000.0
	for tm := from; tm.Before(to); tm = tm.Add(time.Duration(step) * time.Minute) {
		if !kernel.IsCMEOpen(tm) {
			continue
		}
		c += 0.25
		out = append(out, Bar{T: tm.UnixMilli(), O: c, H: c + 1, L: c - 1, C: c, V: 10, Final: true})
	}
	return out
}

// TestBuckets4hDSTRealCalendar is the P1 of the release #55 review (DST
// fall-back, Sun 2026-11-01 17:00 CT): with the production calendar and a
// PERFECT feed, every 4h bucket Buckets4h returns must hold data, and the swing
// judge must not block. Both transitions are pinned: fall-back (25 h day) and
// spring-forward (23 h day).
func TestBuckets4hDSTRealCalendar(t *testing.T) {
	loc := kernel.CTLocation()
	cases := []struct {
		name     string
		from, to string
		now      string
	}{
		{"fall-back 2026-11-01", "2026-09-28 17:00", "2026-11-04 12:00", "2026-11-04 12:00"},
		{"fall-back evening check", "2026-09-28 17:00", "2026-11-02 08:00", "2026-11-01 17:30"},
		{"spring-forward 2027-03-14", "2027-02-08 17:00", "2027-03-17 12:00", "2027-03-17 12:00"},
		{"spring-forward evening check", "2027-02-08 17:00", "2027-03-15 08:00", "2027-03-14 17:30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parse := func(s string) time.Time {
				v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			now := parse(tc.now)
			m1 := dstPerfectBars(parse(tc.from), now, 1)
			m5 := dstPerfectBars(parse(tc.from), now, 5)
			bs := Buckets4h(m1, m5, func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) }, now, loc)
			if len(bs) < 100 {
				t.Fatalf("only %d buckets", len(bs))
			}
			for _, b := range bs {
				if !b.Has || !b.HasAlt {
					t.Errorf("PHANTOM empty 4h bucket on perfect data: start %s (%s) has1m=%v has5m=%v",
						time.UnixMilli(b.Start).In(loc).Format("Mon 01-02 15:04 MST"), time.UnixMilli(b.Start).UTC().Format("15:04Z"), b.Has, b.HasAlt)
				}
			}
			for i := 1; i < len(bs); i++ {
				if bs[i].Start <= bs[i-1].Start {
					t.Fatalf("buckets not strictly increasing at %d", i)
				}
			}
			v := JudgeSwing4h(bs, 34, 0, 1.0)
			if v.Blocked {
				t.Errorf("swing4h BLOCKS perfect data across %s: %s", tc.name, v.Why)
			}
		})
	}
}
