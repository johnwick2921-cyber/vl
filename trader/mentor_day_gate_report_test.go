package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
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
		ReadAt:   ctMs(2026, 10, 8, 8, 30),
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

// TestMentorDayGateLineSpent — a spent+agree day reads SPENT (the owner's
// three-way label: TRADE | DAY OFF | SPENT).
func TestMentorDayGateLineSpent(t *testing.T) {
	line := mentorDayGateLine(mentor.DayGateReport{
		Key:      "2026-10-08",
		Verdict:  mentor.DaySpent,
		RunPts:   346.25,
		HaveRun:  true,
		HiPx:     31465.75,
		HiAt:     ctMs(2026, 10, 7, 21, 34),
		LoPx:     31119.5,
		LoAt:     ctMs(2026, 10, 8, 5, 34),
		FourHDir: mentor.SideShort,
		OneHDir:  mentor.SideShort,
		Conflict: false,
		SpentPts: 300,
	})
	for _, want := range []string{"SPENT", "conflict no", "4h short", "1h short"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, "DAY OFF") || strings.Contains(line, "TRADE ") {
		t.Fatalf("a spent day must read SPENT, not TRADE/DAY OFF: %q", line)
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

// TestMentorDayGateLineRestartLabelsRealReadTime — release #22 P2-3, at the
// production call sites: the evaluator's Tick captures the report, the trader
// renders it. Live 10-09: the process booted at 10:19 printed "day gate @08:30
// CT: SPENT … 4h short · 1h short" — but the 4h/1h were read at 10:19, not at
// 08:30. A restart re-latch must name its real read instant. Control: an
// evaluator that ticked through 08:30 keeps the "@08:30 CT" label.
func TestMentorDayGateLineRestartLabelsRealReadTime(t *testing.T) {
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	start := ctMs(2026, 10, 8, 17, 0)
	bars := func(until int64) []market.Kline {
		var out []market.Kline
		for ms := start; ms < until; ms += 60_000 {
			p := 30000 + float64((ms-start)/60_000%40)
			out = append(out, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: p, High: p + 2, Low: p - 2, Close: p + 1, Final: true})
		}
		return out
	}
	// RESTART at 10:19: seed + the first tick.
	tape := bars(ctMs(2026, 10, 9, 10, 19))
	e := mentor.New(cfg)
	now := mentor.BarCloseInstant(tape[len(tape)-1])
	mentor.Seed(e, tape, now)
	e.Tick(tape, now) // the 17:00→10:19 tape is under the 1500-bar live window
	line := mentorDayGateLine(e.State.DayGateReport)
	if strings.Contains(line, "day gate @08:30 CT") {
		t.Fatalf("a 10:19 restart re-latch is labelled as the 08:30 read: %q", line)
	}
	if !strings.Contains(line, "RE-LATCHED @10:19 CT") || !strings.Contains(line, "trading day 2026-10-09") {
		t.Fatalf("the re-latch line must name its real read instant and day: %q", line)
	}
	// CONTROL: a bot that ticked through the open reads at 08:30.
	tape2 := bars(ctMs(2026, 10, 9, 9, 0))
	c := mentor.New(cfg)
	i0 := len(tape2) - 60 // boot at 08:00, tick every bar through 08:59
	mentor.Seed(c, tape2[:i0], mentor.BarCloseInstant(tape2[i0-1]))
	for i := i0; i < len(tape2); i++ {
		lo := i + 1 - 1500
		if lo < 0 {
			lo = 0
		}
		c.Tick(tape2[lo:i+1], mentor.BarCloseInstant(tape2[i]))
	}
	if l := mentorDayGateLine(c.State.DayGateReport); !strings.Contains(l, "day gate @08:30 CT") {
		t.Fatalf("control: the live 08:30 latch must keep its @08:30 label: %q", l)
	}
}
