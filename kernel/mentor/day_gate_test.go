package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// §7 — the pre-session routine table [D5.1 p1 @ 18:36, 19:22, 15:57]:
//   normal run      + agree    → TRADE
//   300–400+ pts    + conflict → "TẮT MÁY NGHỈ LUÔN CHO EM" (off)
//   300–400+ pts    + agree    → trade, small targets only (15/10 pt sales)
//
// CTO review E1: a NORMAL day with a conflict is NOT off — §5.4 case 3 is a
// per-tick sit-out until the 1h flips. DayOff = spent AND conflict only.

// TestDayGateVerdictTable — the table rows plus the E1 correction.
func TestDayGateVerdictTable(t *testing.T) {
	g := DefaultDayGate()

	if v, reason := DayGateVerdict(120, true, false, g); v != DayTrade || reason != "" {
		t.Fatalf("normal run + agree: v=%v reason=%q, want DayTrade", v, reason)
	}
	// E1: normal + conflict → DayTrade; the per-tick sit-out is HTFVerdict's
	// job (case 3), not the day gate's.
	if v, _ := DayGateVerdict(120, true, true, g); v != DayTrade {
		t.Fatalf("normal run + conflict: v=%v, want DayTrade (§5.4 case 3 sits out per tick)", v)
	}
	if v, reason := DayGateVerdict(350, true, true, g); v != DayOff || reason == "" {
		t.Fatalf("spent + conflict: v=%v, want DayOff 'TẮT MÁY NGHỈ LUÔN CHO EM'", v)
	}
	if v, reason := DayGateVerdict(350, true, false, g); v != DaySpent || reason == "" {
		t.Fatalf("spent + agree: v=%v, want DaySpent (trade, don't target big)", v)
	}
	// 1h silent is not a conflict — a spent day with the 4h alone is still
	// "agree" (case 2: follow the 4h).
	if v, _ := DayGateVerdict(350, true, false, g); v != DaySpent {
		t.Fatalf("spent + 1h-silent: v=%v, want DaySpent", v)
	}
}

// TestDayGateVerdictUnmeasuredFailClosed — E3: fewer than 2 bars in the
// window fail CLOSED, with a reason ("any trade you are vague about — don't"
// [§12]).
func TestDayGateVerdictUnmeasuredFailClosed(t *testing.T) {
	g := DefaultDayGate()
	if v, reason := DayGateVerdict(0, false, false, g); v != DayNotMeasured || reason == "" {
		t.Fatalf("unmeasured run: v=%v reason=%q, want DayNotMeasured with a reason", v, reason)
	}
	if v, _ := DayGateVerdict(0, false, true, g); v != DayNotMeasured {
		t.Fatalf("unmeasured run + conflict: v=%v, want DayNotMeasured", v)
	}
}

// TestDayLatchSpentConflictLatchesAllDay — E1: a spent+conflict read at the
// 08:30 boundary latches DayOff for the rest of the RTH day; the 1h flipping
// at 10:00 does not reopen the machine.
func TestDayLatchSpentConflictLatchesAllDay(t *testing.T) {
	g := DefaultDayGate()
	loc := time.FixedZone("CT", -5*3600)
	open := time.Date(2026, 9, 15, 8, 30, 0, 0, loc)

	l := LatchDay(DayLatch{}, open.UnixMilli(), loc, 350, true, true, g)
	if l.Verdict != DayOff || l.Key != "2026-09-15" {
		t.Fatalf("spent+conflict at 08:30: v=%v key=%q, want DayOff keyed 2026-09-15", l.Verdict, l.Key)
	}
	// 10:00 same day, the 1h flips → conflict gone; the latch must hold.
	l = LatchDay(l, time.Date(2026, 9, 15, 10, 0, 0, 0, loc).UnixMilli(), loc, 350, true, false, g)
	if l.Verdict != DayOff {
		t.Fatalf("after the 1h flip at 10:00: v=%v, want the latched DayOff (the machine stays off)", l.Verdict)
	}
}

// TestDayLatchFreezesAtOpen — E1: at/after 08:30 the verdict freezes as
// read; a conflict appearing mid-day does not downgrade a DaySpent day.
func TestDayLatchFreezesAtOpen(t *testing.T) {
	g := DefaultDayGate()
	loc := time.FixedZone("CT", -5*3600)
	open := time.Date(2026, 9, 15, 8, 30, 0, 0, loc)

	l := LatchDay(DayLatch{}, open.UnixMilli(), loc, 350, true, false, g)
	if l.Verdict != DaySpent {
		t.Fatalf("spent+agree at 08:30: v=%v, want DaySpent", l.Verdict)
	}
	l = LatchDay(l, time.Date(2026, 9, 15, 10, 0, 0, 0, loc).UnixMilli(), loc, 350, true, true, g)
	if l.Verdict != DaySpent {
		t.Fatalf("mid-day conflict after the frozen read: v=%v, want the frozen DaySpent", l.Verdict)
	}
}

// TestDayLatchNormalConflictIsNotOff — E1: a NORMAL day with a conflict reads
// DayTrade (live, pre-open, never latched); the sit-out until the flip is
// §5.4 case 3.
func TestDayLatchNormalConflictIsNotOff(t *testing.T) {
	g := DefaultDayGate()
	loc := time.FixedZone("CT", -5*3600)
	l := LatchDay(DayLatch{}, time.Date(2026, 9, 15, 7, 0, 0, 0, loc).UnixMilli(), loc, 120, true, true, g)
	if l.Verdict != DayTrade || l.Key != "" {
		t.Fatalf("normal run + conflict pre-open: v=%v key=%q, want live DayTrade with no latch", l.Verdict, l.Key)
	}
}

// TestDayLatchPreOpenDoesNotLatch — L1: §7 is a pre-session read taken AT
// the open. A pre-open spent+conflict does NOT latch: if the 1h agrees by
// 08:30, the day is DaySpent, not off. Only the first tick at/after 08:30
// latches.
func TestDayLatchPreOpenDoesNotLatch(t *testing.T) {
	g := DefaultDayGate()
	loc := time.FixedZone("CT", -5*3600)

	l := LatchDay(DayLatch{}, time.Date(2026, 9, 15, 7, 0, 0, 0, loc).UnixMilli(), loc, 350, true, true, g)
	if l.Verdict != DayOff || l.Key != "" {
		t.Fatalf("07:00 spent+conflict: v=%v key=%q, want LIVE DayOff with no latch", l.Verdict, l.Key)
	}
	// 08:30: the 1h agrees by the open → DaySpent, latched for the day.
	l = LatchDay(l, time.Date(2026, 9, 15, 8, 30, 0, 0, loc).UnixMilli(), loc, 350, true, false, g)
	if l.Verdict != DaySpent || l.Key != "2026-09-15" {
		t.Fatalf("08:30 agree after pre-open conflict: v=%v key=%q, want DaySpent latched 2026-09-15", l.Verdict, l.Key)
	}
}

// TestDayLatchKeysByTradingDay — L2: the latch key is the trading day
// (17:00 CT → 16:00 CT next calendar day), not the calendar date. From
// 17:00 the overnight session belongs to TOMORROW's trading day and the
// verdict is live again; a Sunday 17:00 open belongs to Monday.
func TestDayLatchKeysByTradingDay(t *testing.T) {
	g := DefaultDayGate()
	loc := time.FixedZone("CT", -5*3600)

	// Latch at 08:30 on 09-15 → keyed 2026-09-15.
	l := LatchDay(DayLatch{}, time.Date(2026, 9, 15, 8, 30, 0, 0, loc).UnixMilli(), loc, 350, true, false, g)
	if l.Key != "2026-09-15" {
		t.Fatalf("08:30 latch key = %q, want 2026-09-15", l.Key)
	}
	// 18:00 on the SAME calendar date: the new session (09-16's trading
	// day) is pre-open → live, not the frozen 09-15 verdict.
	l = LatchDay(l, time.Date(2026, 9, 15, 18, 0, 0, 0, loc).UnixMilli(), loc, 350, true, true, g)
	if l.Verdict != DayOff || l.Key != "" {
		t.Fatalf("18:00 same date: v=%v key=%q, want LIVE DayOff for the next trading day", l.Verdict, l.Key)
	}

	// Sunday 17:05 (2026-09-20): the open belongs to Monday's trading day,
	// so a Monday-keyed latch must NOT freeze the Sunday evening.
	l = LatchDay(DayLatch{Key: "2026-09-21", Verdict: DayOff}, time.Date(2026, 9, 20, 17, 5, 0, 0, loc).UnixMilli(), loc, 120, true, false, g)
	if l.Verdict != DayTrade || l.Key != "" {
		t.Fatalf("Sunday 17:05 with a Monday-keyed latch: v=%v key=%q, want LIVE DayTrade", l.Verdict, l.Key)
	}
	// Monday 08:30 latches under Monday's key.
	l = LatchDay(DayLatch{}, time.Date(2026, 9, 21, 8, 30, 0, 0, loc).UnixMilli(), loc, 350, true, true, g)
	if l.Verdict != DayOff || l.Key != "2026-09-21" {
		t.Fatalf("Monday 08:30: v=%v key=%q, want DayOff keyed 2026-09-21", l.Verdict, l.Key)
	}
}

// TestGlobexRunOnRecorded5mGlobex — canon 53: the recorded Globex session
// 2026-09-14 17:00 CT → 2026-09-15 08:30 CT (db-copy, MNQ 12-26, 5m, 186
// bars). Golden run = 264.5 pts (high 29495.75 − low 29231.25, computed from
// the same fixture rows on extraction, 2026-10-03).
func TestGlobexRunOnRecorded5mGlobex(t *testing.T) {
	bars := loadFixture(t, "mnq_5m_2026-09-15_globex", "5m")
	loc := ctime()
	// 09:00 CT on 2026-09-15: the most recent 08:30 boundary is today's, so
	// the window is 09-14 17:00 CT → 09-15 08:30 CT.
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, loc).UnixMilli()
	run, ok := GlobexRun(bars, now, loc)
	if !ok {
		t.Fatal("Globex window found fewer than 2 bars")
	}
	if abs(run-264.5) > 0.01 {
		t.Fatalf("run = %.2f, want the golden 264.5", run)
	}
}

// TestGlobexRunLivePreOpen — E2: before 08:30 the window is the CURRENT
// session from 17:00 CT to NOW, so the run grows as overnight bars arrive;
// after 08:30 the 17:00→08:30 value freezes. Synthetic time arithmetic for
// the window logic (the recorded fixture covers the value path).
func TestGlobexRunLivePreOpen(t *testing.T) {
	loc := time.FixedZone("CT", -5*3600)
	mk := func(day int, hour, minute int, h, l float64) market.Kline {
		ot := time.Date(2026, 9, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: ot.UnixMilli(), High: h, Low: l}
	}
	// Session A (09-13 17:00 → 09-14 08:30, run 10) and session B
	// (09-14 17:00 → 09-15 08:30, run 40).
	bars := []market.Kline{
		mk(13, 17, 30, 100, 98),
		mk(13, 20, 0, 101, 99),  // A hi
		mk(14, 6, 0, 100.5, 91), // A lo → run 10
		mk(14, 17, 30, 130, 110),
		mk(14, 20, 0, 135, 105), // B hi
		mk(15, 7, 0, 115, 95),   // B lo → run 40
	}
	// 03:30 CT on 09-15: session B has only the first two bars → run 30.
	if run, ok := GlobexRun(bars, time.Date(2026, 9, 15, 3, 30, 0, 0, loc).UnixMilli(), loc); !ok || abs(run-30) > 1e-9 {
		t.Fatalf("pre-open 03:30: run=%.2f ok=%v, want 30 (session B, live to now)", run, ok)
	}
	// 07:30 CT: the 07:00 bar joined → run 40.
	if run, ok := GlobexRun(bars, time.Date(2026, 9, 15, 7, 30, 0, 0, loc).UnixMilli(), loc); !ok || abs(run-40) > 1e-9 {
		t.Fatalf("pre-open 07:30: run=%.2f ok=%v, want 40", run, ok)
	}
	// 09:00 CT: frozen 17:00→08:30 → 40, and the pre-open value does not grow.
	if run, ok := GlobexRun(bars, time.Date(2026, 9, 15, 9, 0, 0, 0, loc).UnixMilli(), loc); !ok || abs(run-40) > 1e-9 {
		t.Fatalf("post-open 09:00: run=%.2f ok=%v, want the frozen 40", run, ok)
	}
}

// TestGlobexRunUndersizedFeedRefused — A6 [D5.1 p1 @17:44-17:56]: the run
// is the FULL Globex session (Asia high -> pre-market low). A feed that
// starts mid-window (the live 240-bar fetch starts ~04:30 at the 08:30
// latch) under-measures it and must read not-measured — never a fabricated
// "normal" day. Within the alignment tolerance the feed counts as full.
func TestGlobexRunUndersizedFeedRefused(t *testing.T) {
	loc := time.FixedZone("CT", -5*3600)
	mk := func(day int, hour, minute int, h, l float64) market.Kline {
		ot := time.Date(2026, 9, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: ot.UnixMilli(), High: h, Low: l}
	}
	// The 240-bar live feed at the 08:30 latch: earliest bar 04:30.
	undersized := []market.Kline{
		mk(15, 4, 30, 120, 100),
		mk(15, 5, 30, 125, 105),
		mk(15, 6, 30, 130, 110),
	}
	if run, ok := GlobexRun(undersized, time.Date(2026, 9, 15, 9, 0, 0, 0, loc).UnixMilli(), loc); ok {
		t.Fatalf("frozen read on a 04:30-start feed: run=%.2f ok=true, want ok=false (under-measured)", run)
	}
	// Same during the pre-open live phase: a 03:30 read with a feed that
	// starts 23:30 misses 17:00-23:30 — under-measured, fail closed.
	undersizedLive := []market.Kline{
		mk(14, 23, 30, 120, 100),
		mk(15, 1, 30, 125, 105),
		mk(15, 2, 30, 130, 110),
	}
	if run, ok := GlobexRun(undersizedLive, time.Date(2026, 9, 15, 3, 30, 0, 0, loc).UnixMilli(), loc); ok {
		t.Fatalf("pre-open read on a 23:30-start feed: run=%.2f ok=true, want ok=false", run)
	}
	// A feed starting inside the alignment tolerance (17:05) is full.
	full := []market.Kline{
		mk(14, 17, 5, 100, 98),
		mk(14, 20, 0, 105, 99),
		mk(15, 7, 0, 106, 90),
	}
	if run, ok := GlobexRun(full, time.Date(2026, 9, 15, 9, 0, 0, 0, loc).UnixMilli(), loc); !ok || abs(run-16) > 1e-9 {
		t.Fatalf("17:05-start feed: run=%.2f ok=%v, want 16 true", run, ok)
	}
}

// TestGlobexRunSundayOpen — E2: at Sunday 17:05 CT the current session
// opened at 17:00 the same day; Saturday bars are excluded (the Globex
// week does not have a Saturday session).
func TestGlobexRunSundayOpen(t *testing.T) {
	loc := time.FixedZone("CT", -5*3600)
	mk := func(day int, hour, minute int, h, l float64) market.Kline {
		ot := time.Date(2026, 9, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: ot.UnixMilli(), High: h, Low: l}
	}
	// 2026-09-20 is a Sunday. now = 17:06 CT: both bars closed and inside
	// the current session's window [17:00, now).
	bars := []market.Kline{
		mk(19, 12, 0, 200, 180), // Saturday — must be excluded
		mk(20, 17, 0, 130, 120),
		mk(20, 17, 5, 132, 122),
	}
	run, ok := GlobexRun(bars, time.Date(2026, 9, 20, 17, 6, 0, 0, loc).UnixMilli(), loc)
	if !ok || abs(run-12) > 1e-9 {
		t.Fatalf("Sunday 17:06: run=%.2f ok=%v, want 12 (132−120, Saturday excluded)", run, ok)
	}
}

// TestCapTargetForDay — on a spent day the target distance is capped at
// TargetCapPts ("15 điểm bán, 10 điểm bán" [D5.1 p1 @ 15:57]); on a normal
// day the target is untouched.
func TestCapTargetForDay(t *testing.T) {
	g := DefaultDayGate()
	long := Intent{Side: SideLong, Price: 30000, Stop: 29990, Target: 30040}
	if got := CapTargetForDay(long, DayTrade, g); got.Target != 30040 {
		t.Fatalf("DayTrade must not move the target: %+v", got)
	}
	got := CapTargetForDay(long, DaySpent, g)
	if got.Target != 30015 {
		t.Fatalf("DaySpent long target = %.2f, want the 15-pt cap 30015", got.Target)
	}
	if got.Stop != long.Stop || got.Price != long.Price {
		t.Fatalf("the cap must only touch the target: %+v", got)
	}
	short := Intent{Side: SideShort, Price: 30000, Stop: 30010, Target: 29950}
	gotS := CapTargetForDay(short, DaySpent, g)
	if gotS.Target != 29985 {
		t.Fatalf("DaySpent short target = %.2f, want the 15-pt cap 29985", gotS.Target)
	}
	// an already-small target is not moved further away
	if got2 := CapTargetForDay(Intent{Side: SideLong, Price: 30000, Target: 30010}, DaySpent, g); got2.Target != 30010 {
		t.Fatalf("a 10-pt target must stay (never pushed further): %+v", got2)
	}
}
