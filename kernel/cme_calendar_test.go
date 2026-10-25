package kernel

import (
	"testing"
	"time"
)

func TestIsCMEOpen(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("failed to load America/Chicago timezone: %v", err)
	}
	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		// 2026-09-07 (session calendar): these expectations CHANGED with the wave
		// that made shortened sessions trading sessions. Every date below is still
		// pinned — none was deleted — but a date the calendar classes SHORTENED is
		// now OPEN before its stated close and CLOSED after it, and each such case
		// carries both halves. The dates classed CLOSED are unchanged.
		{"Mon 10am normal trading", time.Date(2026, 6, 15, 10, 0, 0, 0, chicago), true},
		{"Mon daily break 4:30pm CT", time.Date(2026, 6, 15, 16, 30, 0, 0, chicago), false},
		{"Saturday closed", time.Date(2026, 6, 20, 12, 0, 0, 0, chicago), false},
		{"New Year's Day", time.Date(2026, 1, 1, 10, 0, 0, 0, chicago), false},
		{"MLK Day 2026 — UNESTABLISHED, closed under C4", time.Date(2026, 1, 19, 10, 0, 0, 0, chicago), false},
		{"Presidents Day 2026 — UNESTABLISHED, closed under C4", time.Date(2026, 2, 16, 10, 0, 0, 0, chicago), false},
		{"Good Friday 2026 (Apr 3)", time.Date(2026, 4, 3, 10, 0, 0, 0, chicago), false},
		{"Memorial Day 2026 — UNESTABLISHED, closed under C4", time.Date(2026, 5, 25, 10, 0, 0, 0, chicago), false},
		{"Juneteenth 2026 — UNESTABLISHED, closed under C4", time.Date(2026, 6, 19, 10, 0, 0, 0, chicago), false},
		{"Independence Day 2026 (Jul 4) — falls SATURDAY, closed by the weekly rule", time.Date(2026, 7, 4, 10, 0, 0, 0, chicago), false},
		{"Independence Day observed 2026 (Jul 3) — UNESTABLISHED, closed under C4", time.Date(2026, 7, 3, 10, 0, 0, 0, chicago), false},
		{"Labor Day 2026 (Sep 7) — SHORTENED, before close (THE DAY THIS WAVE WAS BORN)", time.Date(2026, 9, 7, 10, 0, 0, 0, chicago), true},
		{"Labor Day 2026 — after its 12:00 CT close", time.Date(2026, 9, 7, 12, 30, 0, 0, chicago), false},
		{"Thanksgiving 2026 (Nov 26)", time.Date(2026, 11, 26, 10, 0, 0, 0, chicago), false},
		{"Day after Thanksgiving 2026 — SHORTENED, before its SOURCED 12:15 close", time.Date(2026, 11, 27, 12, 10, 0, 0, chicago), true},
		{"Day after Thanksgiving 2026 — after 12:15 CT (not 12:00: the sourced time won the fold)", time.Date(2026, 11, 27, 12, 20, 0, 0, chicago), false},
		{"Christmas Eve 2026 — SHORTENED, before its SOURCED 12:15 close", time.Date(2026, 12, 24, 12, 10, 0, 0, chicago), true},
		{"Christmas Eve 2026 — after 12:15 CT (not 12:00: the sourced time won the fold)", time.Date(2026, 12, 24, 12, 20, 0, 0, chicago), false},
		{"Christmas Day", time.Date(2026, 12, 25, 10, 0, 0, 0, chicago), false},
		{"New Year's Eve 2026 — SOURCED NORMAL session (only rates settle early)", time.Date(2026, 12, 31, 10, 0, 0, 0, chicago), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCMEOpen(tc.when); got != tc.want {
				t.Errorf("IsCMEOpen(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

// TestIsCMEOpenBoundaries pins the exact open/close edges (Part A). Friday close,
// Sunday reopen, the Mon–Thu daily break, and a DST-month sample so a tz bug can
// never slip in unnoticed.
func TestIsCMEOpenBoundaries(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		// Friday close edge (Jun 19 2026 is Juneteenth → use Jun 12, a normal Friday, CDT)
		{"Fri 15:59 CT open", time.Date(2026, 6, 12, 15, 59, 0, 0, chicago), true},
		{"Fri 16:01 CT closed", time.Date(2026, 6, 12, 16, 1, 0, 0, chicago), false},
		// Sunday reopen edge (Jun 14 2026)
		{"Sun 16:59 CT closed", time.Date(2026, 6, 14, 16, 59, 0, 0, chicago), false},
		{"Sun 17:01 CT open", time.Date(2026, 6, 14, 17, 1, 0, 0, chicago), true},
		{"Sun 17:00 CT open (exact)", time.Date(2026, 6, 14, 17, 0, 0, 0, chicago), true},
		// Mon–Thu daily break edges (Jun 16 2026, a Tuesday)
		{"Tue 15:59 CT open", time.Date(2026, 6, 16, 15, 59, 0, 0, chicago), true},
		{"Tue 16:30 CT break closed", time.Date(2026, 6, 16, 16, 30, 0, 0, chicago), false},
		{"Tue 17:00 CT reopen", time.Date(2026, 6, 16, 17, 0, 0, 0, chicago), true},
		// DST: same wall-clock edges in a CST (winter) month — Jan 9 2026 is a Friday
		{"Fri 15:59 CST open (winter)", time.Date(2026, 1, 9, 15, 59, 0, 0, chicago), true},
		{"Fri 16:01 CST closed (winter)", time.Date(2026, 1, 9, 16, 1, 0, 0, chicago), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCMEOpen(tc.when); got != tc.want {
				t.Errorf("IsCMEOpen(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

// TestCMEClosedReason asserts the reason bool never disagrees with IsCMEOpen
// (the whole gate depends on that) and that each reason string is right.
func TestCMEClosedReason(t *testing.T) {
	chicago, _ := time.LoadLocation("America/Chicago")
	cases := []struct {
		name       string
		when       time.Time
		wantClosed bool
		wantReason string
	}{
		{"Sat weekend", time.Date(2026, 6, 13, 12, 0, 0, 0, chicago), true, "weekend"},
		{"Sun pre-open weekend", time.Date(2026, 6, 14, 12, 0, 0, 0, chicago), true, "weekend"},
		{"Fri close", time.Date(2026, 6, 12, 16, 30, 0, 0, chicago), true, "Friday close"},
		{"Tue daily break", time.Date(2026, 6, 16, 16, 30, 0, 0, chicago), true, "daily break"},
		{"Holiday (Independence Day)", time.Date(2026, 7, 4, 10, 0, 0, 0, chicago), true, "holiday"},
		{"Mon open (no reason)", time.Date(2026, 6, 15, 10, 0, 0, 0, chicago), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			closed, reason := CMEClosedReason(tc.when)
			if closed != tc.wantClosed || reason != tc.wantReason {
				t.Errorf("CMEClosedReason(%v) = (%v,%q), want (%v,%q)", tc.when, closed, reason, tc.wantClosed, tc.wantReason)
			}
			// Invariant: closed == !IsCMEOpen
			if closed == IsCMEOpen(tc.when) {
				t.Errorf("CMEClosedReason closed=%v disagrees with IsCMEOpen=%v at %v", closed, IsCMEOpen(tc.when), tc.when)
			}
		})
	}
}

// TestNextCMEOpen checks the reopen instant is correct from several closed
// states, including a Friday→Sunday jump and a DST fall-back weekend, and that
// the result always satisfies IsCMEOpen.
func TestNextCMEOpen(t *testing.T) {
	chicago, _ := time.LoadLocation("America/Chicago")
	cases := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"Sat → Sun 17:00", time.Date(2026, 6, 13, 12, 0, 0, 0, chicago), time.Date(2026, 6, 14, 17, 0, 0, 0, chicago)},
		{"Fri close → Sun 17:00", time.Date(2026, 6, 12, 16, 30, 0, 0, chicago), time.Date(2026, 6, 14, 17, 0, 0, 0, chicago)},
		{"Daily break → same-day 17:00", time.Date(2026, 6, 16, 16, 20, 0, 0, chicago), time.Date(2026, 6, 16, 17, 0, 0, 0, chicago)},
		// DST fall-back: Fri Oct 30 close → Sun Nov 1 17:00 CST (clocks fell back 02:00 that morning)
		{"DST fall-back weekend", time.Date(2026, 10, 30, 16, 30, 0, 0, chicago), time.Date(2026, 11, 1, 17, 0, 0, 0, chicago)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextCMEOpen(tc.from)
			if !got.Equal(tc.want) {
				t.Errorf("NextCMEOpen(%v) = %v, want %v", tc.from, got.In(chicago), tc.want)
			}
			if !IsCMEOpen(got) {
				t.Errorf("NextCMEOpen(%v) returned %v which is not open", tc.from, got.In(chicago))
			}
		})
	}
}

// m427/1: IsCMEOpen flips mid-hour on an early-close day (2026-11-27 12:15 CT).
// NextCMEOpen / CMEOpenSpanStart must agree with it to the minute, and the
// ordinary 17:00 reopen must still be exact.
func TestM427EarlyCloseMinuteEdges(t *testing.T) {
	ch := CTLocation()
	d := func(y int, m time.Month, dd, h, mi, s, ns int) time.Time { return time.Date(y, m, dd, h, mi, s, ns, ch) }
	lastOpen := d(2026, 11, 27, 12, 14, 59, 999_000_000)
	firstClosed := d(2026, 11, 27, 12, 15, 0, 0)
	if !IsCMEOpen(lastOpen) || IsCMEOpen(firstClosed) {
		t.Fatalf("premise: 12:14:59.999 open=%v 12:15:00 open=%v", IsCMEOpen(lastOpen), IsCMEOpen(firstClosed))
	}
	sun := d(2026, 11, 29, 17, 0, 0, 0)
	for _, from := range []time.Time{firstClosed, d(2026, 11, 27, 12, 40, 0, 0), d(2026, 11, 28, 9, 0, 0, 0)} {
		if got := NextCMEOpen(from); !got.Equal(sun) {
			t.Errorf("NextCMEOpen(%v) = %v want %v", from, got.In(ch), sun)
		}
	}
	// Open span ending on the early close starts at the same instant seen from
	// the last open millisecond and from a minute earlier.
	a, b := CMEOpenSpanStart(lastOpen), CMEOpenSpanStart(lastOpen.Add(-time.Minute))
	if !a.Equal(b) || !IsCMEOpen(a) || IsCMEOpen(a.Add(-time.Minute)) {
		t.Errorf("span start at early-close edge: a=%v b=%v", a.In(ch), b.In(ch))
	}
	// The 17:00 reopen (Sunday evening) is exact from inside the span.
	if got := CMEOpenSpanStart(d(2026, 11, 29, 17, 30, 0, 0)); !got.Equal(sun) {
		t.Errorf("span start after reopen = %v want %v", got.In(ch), sun)
	}
	if got := CMEOpenSpanStart(d(2026, 11, 29, 17, 0, 0, 0)); !got.Equal(sun) {
		t.Errorf("span start at 17:00:00 = %v want %v", got.In(ch), sun)
	}
}
