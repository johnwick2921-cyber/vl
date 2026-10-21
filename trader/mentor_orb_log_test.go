package trader

import (
	"strings"
	"testing"

	"vl/kernel/mentor"
)

// TestMentorNoteORBLogsRangeOnceAndEachEscape — MENTOR-NY item 5. 10-14 had
// ZERO ORB lines while the ORB latch held every short out 08:33–15:00. The
// production note (called from mentorEvalOnce on every tick) READS the
// evaluator's ORB: the range once per CT day, the escape side + read time on
// its first appearance and on every side change; nothing else repeats.
// MUTANT: drop the range dedup → the range repeats → RED.
func TestMentorNoteORBLogsRangeOnceAndEachEscape(t *testing.T) {
	at := &AutoTrader{}
	day := ctMs(2026, 10, 14, 0, 0)
	t0 := ctMs(2026, 10, 14, 8, 32)
	orb := mentor.ORB{Day: day}
	if got := at.mentorNoteORB(orb, t0); len(got) != 0 {
		t.Fatalf("an undrawn ORB logs nothing, got %v", got)
	}
	orb = mentor.ORB{Day: day, High: 30583.5, Low: 30518.25, Drawn: true}
	got := at.mentorNoteORB(orb, t0)
	if len(got) != 1 || !strings.Contains(got[0], "ORB 2026-10-14: range 30518.25–30583.50 (65.25 pts)") {
		t.Fatalf("the draw must log the range once: %v", got)
	}
	if got := at.mentorNoteORB(orb, t0+60_000); len(got) != 0 {
		t.Fatalf("the range must not repeat: %v", got)
	}
	orb.Escaped = mentor.SideLong
	got = at.mentorNoteORB(orb, ctMs(2026, 10, 14, 8, 34))
	if len(got) != 1 || !strings.Contains(got[0], "escape long read @08:34 CT") {
		t.Fatalf("the first escape must log side + time: %v", got)
	}
	if got := at.mentorNoteORB(orb, ctMs(2026, 10, 14, 8, 35)); len(got) != 0 {
		t.Fatalf("an unchanged escape must not repeat: %v", got)
	}
	orb.Escaped = mentor.SideShort
	got = at.mentorNoteORB(orb, ctMs(2026, 10, 14, 8, 59))
	if len(got) != 1 || !strings.Contains(got[0], "escape short (was long) read @08:59 CT") {
		t.Fatalf("a side change must log (was …): %v", got)
	}
	next := mentor.ORB{Day: ctMs(2026, 10, 15, 0, 0), High: 1, Low: 0, Drawn: true, Escaped: mentor.SideShort}
	got = at.mentorNoteORB(next, ctMs(2026, 10, 15, 8, 33))
	if len(got) != 2 || !strings.Contains(got[0], "ORB 2026-10-15: range") || strings.Contains(got[1], "(was") {
		t.Fatalf("a new day logs its range and a fresh first escape: %v", got)
	}
}
