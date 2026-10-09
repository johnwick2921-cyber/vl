package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
)

func ctMs(y int, mo time.Month, d, hh, mm int) int64 {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CT", -6*3600)
	}
	return time.Date(y, mo, d, hh, mm, 0, 0, loc).UnixMilli()
}

// TestMentorDayGateLineRealInputs — owner ruling 2026-10-09: the once-per-day
// line is rendered from the report the evaluator captured at the 08:30 latch,
// with the gate's OWN inputs (10-08's real shape).
func TestMentorDayGateLineRealInputs(t *testing.T) {
	rep := mentor.DayGateReport{
		Key:      "2026-10-08",
		Verdict:  mentor.DayOff,
		RunPts:   346.25,
		HaveRun:  true,
		HiPx:     31465.75,
		HiAt:     ctMs(2026, 10, 7, 21, 34),
		LoPx:     31119.5,
		LoAt:     ctMs(2026, 10, 8, 5, 34),
		FourHDir: mentor.SideShort,
		OneHDir:  mentor.SideLong,
		Conflict: true,
		SpentPts: 300,
	}
	line := mentorDayGateLine(rep)
	for _, want := range []string{
		"day gate @08:30 CT",
		"DAY OFF",
		"overnight run 346 pts",
		"hi 31465.75 @21:34",
		"lo 31119.50 @05:34",
		"line 300",
		"4h short",
		"1h long",
		"conflict yes",
		"[D5.1 p1 @19:22]",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q missing %q", line, want)
		}
	}
}

// TestMentorDayGateLineUnknownReadsNA — a not-measured gate prints n/a for
// every value it did not know (never fabricated zeros).
func TestMentorDayGateLineUnknownReadsNA(t *testing.T) {
	line := mentorDayGateLine(mentor.DayGateReport{
		Key:      "2026-10-08",
		Verdict:  mentor.DayNotMeasured,
		SpentPts: 300,
	})
	for _, want := range []string{"n/a", "overnight run n/a pts", "4h n/a", "1h n/a", "conflict no"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, "hi n/a @") || strings.Contains(line, "lo n/a @") {
		t.Fatalf("an unmeasured hi/lo must print plain n/a, not a fake clock: %q", line)
	}
}
