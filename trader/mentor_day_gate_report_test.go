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
	// MENTOR-NY item 6: the seed ran after 08:30, so this is a BOOT re-read —
	// labelled as such, with the 08:30 decision's own 4h/1h (replayed) shown
	// beside it (here the same, so "same 4h/1h as the 08:30 decision").
	if !strings.Contains(line, "re-read at boot 10:19 CT, not the 08:30 decision") || !strings.Contains(line, "trading day 2026-10-09") {
		t.Fatalf("the boot re-latch line must say it is the boot's re-read, with its instant and day: %q", line)
	}
	if !strings.Contains(line, "same 4h/1h as the 08:30 decision") && !strings.Contains(line, "the 08:30 decision read 4h ") {
		t.Fatalf("the boot re-read line must carry the 08:30 decision's 4h/1h: %q", line)
	}
	if rep := e.State.DayGateReport; !rep.BootRead || rep.OpenFourHDir != rep.FourHDir || rep.OpenOneHDir != rep.OneHDir {
		t.Fatalf("seed after 08:30 must record the 08:30 read (flat tape: same as now): %+v", rep)
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

// TestMentorDayGateLineBootShowsBothValuesWhenTheyDiffer — MENTOR-NY item 6
// (S2), the renderer the evaluator's report feeds: 10-13 08:30 read "4h short ·
// 1h short", the 08:49 boot re-read "4h long · 1h long" and printed it as the
// day gate with no label. The boot line now says it is the boot's re-read and
// shows the 08:30 decision's values beside it when they differ.
// MUTANT: drop the 08:30 values → RED.
func TestMentorDayGateLineBootShowsBothValuesWhenTheyDiffer(t *testing.T) {
	open := ctMs(2026, 10, 13, 8, 30)
	line := mentorDayGateLine(mentor.DayGateReport{
		Key: "2026-10-13", Verdict: mentor.DayTrade, HaveRun: true, RunPts: 180,
		FourHDir: mentor.SideLong, OneHDir: mentor.SideLong, SpentPts: 300,
		ReadAt: open + 19*60_000 + 30_000, BootRead: true,
		OpenFourHDir: mentor.SideShort, OpenOneHDir: mentor.SideShort,
	})
	for _, want := range []string{
		"day gate re-read at boot 08:49 CT, not the 08:30 decision",
		"· 4h long · 1h long",
		"the 08:30 decision read 4h short · 1h short (DIFFERENT",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, "day gate @08:30 CT") {
		t.Fatalf("a boot re-read must never carry the @08:30 label: %q", line)
	}
}
