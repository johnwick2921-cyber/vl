package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// FIX-DAYOFF-RECHECK (owner ruling 2026-10-09, D5.1 p1 @19:22): a DayOff latch
// freezes at the 08:30 read; from then on the evaluator re-reads the 4h/1h
// directions on every CLOSED 1h bar and clears the latch the moment they agree
// (one-way). These pins drive the production Tick call site and assert the
// transition, the knob, the still-conflicting case and the one-way guarantee.

// dayoffRecheckTape builds 1m bars from 09:00 CT (2026-09-15, CDT), one flat
// hour block per entry: hours[i] = {high, low, close}. The 1h bucket
// aggregation takes the min low / max high across the hour, so a later hour's
// low below the prior hour's low flips the 1h trigger line SHORT; a later
// hour's high above the prior hour's high flips it LONG.
func dayoffRecheckTape(hours ...[3]float64) []market.Kline {
	t0 := auditMs(2026, 9, 15, 9, 0, 0)
	var bars []market.Kline
	for h, spec := range hours {
		hi, lo, cl := spec[0], spec[1], spec[2]
		for m := 0; m < 60; m++ {
			ot := t0 + int64(h*60+m)*60_000
			bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_000, Open: cl, High: hi, Low: lo, Close: cl})
		}
	}
	return bars
}

// dayoffRecheckEval seeds an evaluator with a conflicting 4h/1h (4h SHORT, 1h
// LONG drawn later) and a latched DayOff for 2026-09-15 — the 10-08 shape.
func dayoffRecheckEval(recheck bool) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false
	cfg.DayOffRecheck = recheck
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	e := New(cfg)
	now9 := auditMs(2026, 9, 15, 9, 0, 0)
	// 4h SHORT, drawn at the prior bucket; LastBucket pins the tape's own 4h
	// bucket (09:00 CT) so the tape never moves the 4h line.
	fh := fourHBucketStart(now9, ctime())
	e.State.HTF.FourH = TriggerLine{Dir: SideShort, Price: 500, MovedAt: fh - 240*60_000, LastBucket: fh, LastBar: market.Kline{OpenTime: fh - 240*60_000, High: 501, Low: 499, Close: 500}}
	// 1h LONG, drawn at 08:00 (after the 4h) → HTFConflict.
	h1 := (now9 / 3_600_000) * 3_600_000 // UTC-aligned 1h bucket open of 09:00 CT
	e.State.HTF.OneH = TriggerLine{Dir: SideLong, Price: 95, MovedAt: h1 - 3_600_000, LastBucket: h1 - 3_600_000, LastBar: market.Kline{OpenTime: h1 - 3_600_000, High: 105, Low: 100, Close: 102}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now9).In(ctime())), Verdict: DayOff}
	return e
}

// (a) 10-08 shape: DayOff latched at the open; a later 1h close where the 1h
// flips to agree with the 4h CLEARS the latch — the day lands on DaySpent
// (still spent, now agreeing) and the day gate reopens.
func TestDayOffRecheckClearsOnAgreement(t *testing.T) {
	e := dayoffRecheckEval(true)
	bars := dayoffRecheckTape(
		[3]float64{104, 100, 103}, // 09:00 — no break vs the 08:00 low 100
		[3]float64{99, 90, 94},    // 10:00 — low 90 breaks the 09:00 low 100 → 1h flips SHORT
	)
	now10 := auditMs(2026, 9, 15, 10, 0, 0)
	e.Tick(bars[:60], now10)
	if e.State.Day.Verdict != DayOff {
		t.Fatalf("pre-flip verdict = %v, want DayOff (still conflicting)", e.State.Day.Verdict)
	}
	if r := dayGateRefusal(e.State.Day.Verdict); r != "day_off" {
		t.Fatalf("pre-flip day-gate refusal = %q, want day_off", r)
	}

	now11 := auditMs(2026, 9, 15, 11, 0, 0)
	e.Tick(bars, now11)
	if e.State.Day.Verdict != DaySpent {
		t.Fatalf("post-flip verdict = %v, want DaySpent (spent + agree)", e.State.Day.Verdict)
	}
	rc := e.State.DayRecheck
	if rc.Clears != 1 || rc.ClearedDir != SideShort {
		t.Fatalf("recheck = %+v, want Clears=1 ClearedDir=short", rc)
	}
	if rc.ClearedAt == 0 {
		t.Fatalf("recheck.ClearedAt must be the closed 1h bar's CloseTime, got 0")
	}
	if r := dayGateRefusal(e.State.Day.Verdict); r != "" {
		t.Fatalf("post-flip day-gate still refuses: %q", r)
	}
}

// (b) knob false → the DayOff stays latched all day (proves the recheck is not
// a no-op — the same tape clears when the knob is ON).
func TestDayOffRecheckKnobFalseStaysLatched(t *testing.T) {
	e := dayoffRecheckEval(false)
	bars := dayoffRecheckTape(
		[3]float64{104, 100, 103},
		[3]float64{99, 90, 94},
	)
	e.Tick(bars[:60], auditMs(2026, 9, 15, 10, 0, 0))
	e.Tick(bars, auditMs(2026, 9, 15, 11, 0, 0))
	if e.State.Day.Verdict != DayOff {
		t.Fatalf("knob=false verdict = %v, want DayOff (whole-day latch)", e.State.Day.Verdict)
	}
	if e.State.DayRecheck.Clears != 0 {
		t.Fatalf("knob=false recheck = %+v, want zero clears", e.State.DayRecheck)
	}
}

// (c) still-conflicting 1h closes → the DayOff stays latched.
func TestDayOffRecheckStillConflictingStaysOff(t *testing.T) {
	e := dayoffRecheckEval(true)
	bars := dayoffRecheckTape(
		[3]float64{104, 100, 103}, // 09:00
		[3]float64{104, 100, 103}, // 10:00 — no break: the 1h stays LONG, conflict remains
	)
	e.Tick(bars[:60], auditMs(2026, 9, 15, 10, 0, 0))
	e.Tick(bars, auditMs(2026, 9, 15, 11, 0, 0))
	if e.State.Day.Verdict != DayOff {
		t.Fatalf("still-conflicting verdict = %v, want DayOff", e.State.Day.Verdict)
	}
	if e.State.DayRecheck.Clears != 0 {
		t.Fatalf("still-conflicting recheck = %+v, want zero clears", e.State.DayRecheck)
	}
}

// (d) one-way: once cleared, a later conflict return does NOT re-latch.
func TestDayOffRecheckOneWay(t *testing.T) {
	e := dayoffRecheckEval(true)
	bars := dayoffRecheckTape(
		[3]float64{104, 100, 103}, // 09:00
		[3]float64{99, 90, 94},    // 10:00 — 1h flips SHORT (agree) → clear
		[3]float64{110, 95, 108},  // 11:00 — high 110 breaks the 10:00 high 99 → 1h flips LONG (conflict returns)
	)
	e.Tick(bars[:60], auditMs(2026, 9, 15, 10, 0, 0))
	e.Tick(bars[:120], auditMs(2026, 9, 15, 11, 0, 0))
	if e.State.Day.Verdict != DaySpent {
		t.Fatalf("after clear verdict = %v, want DaySpent", e.State.Day.Verdict)
	}
	e.Tick(bars, auditMs(2026, 9, 15, 12, 0, 0))
	if e.State.Day.Verdict != DaySpent {
		t.Fatalf("conflict-return verdict = %v, want DaySpent (one-way, never re-latched)", e.State.Day.Verdict)
	}
	if e.State.DayRecheck.Clears != 1 {
		t.Fatalf("recheck = %+v, want exactly one clear (one-way)", e.State.DayRecheck)
	}
}

// shortISBEntryCount drives a SHORT inside-bar tape through Tick and counts
// emitted ISB stop-limit entries. The day verdict is the only variable — a
// DayOff refuses (isb_day_off), a DaySpent day emits (capped), proving the
// gate reopens in the agreed direction once the DayOff clears.
func shortISBEntryCount(verdict DayVerdict) int {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // pin the day gate + direction, not the room rule
	// head low so the EMA 34 target sits BELOW the short entry.
	head := rthBars(0, 70, 71, 69.5, 70)
	prev := rthBars(1, 106, 106.5, 97, 97.5) // red candle 1 → short
	cur := rthBars(2, 104, 105, 102, 103)    // body inside prev → ISB
	if !IsISB(prev, cur) || ISBDirection(prev) != SideShort {
		panic("fixture must be a short ISB")
	}
	bars := []market.Kline{head, prev, cur}
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 200}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 200}, OneH: TriggerLine{Dir: SideShort, Price: 200, MovedAt: 1}}
	e.State.ORB = ORB{Day: dayStartCT(cur.CloseTime + 1), High: 150, Low: 140, Drawn: true, Escaped: SideShort} // entry 101.5 sits below the range (short side)
	now := cur.CloseTime + 1
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: verdict}
	n := 0
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopLimitEntry {
			n++
		}
	}
	return n
}

// (a, cont.) "box/ISB in that direction evaluate again": a short ISB is refused
// on a DayOff day and emitted once the recheck has cleared the day.
func TestDayOffRecheckReopensEntries(t *testing.T) {
	if n := shortISBEntryCount(DayOff); n != 0 {
		t.Fatalf("DayOff short ISB emitted %d entries, want 0 (day gate)", n)
	}
	if n := shortISBEntryCount(DaySpent); n == 0 {
		t.Fatalf("cleared (DaySpent) short ISB emitted no entries — the day gate must reopen")
	}
}

// TestDayGateReportRecordedOnceAtFreeze — owner ruling 2026-10-09: the
// once-per-day day-gate line reads the gate's OWN inputs, captured the moment
// the 08:30 latch freezes. A spent+conflict overnight (run 310) with a 4h short
// / 1h long conflict must record DayOff, run 310, hi/lo and their bar times, the
// 300-pt line and the two directions — exactly once (a second tick leaves it).
func TestDayGateReportRecordedOnceAtFreeze(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	e := New(cfg)
	// Freeze the HTF lines: LastBucket pins the tape's own buckets so the
	// overnight bars below never re-draw the pre-seeded short/long conflict.
	e.State.HTF.FourH = TriggerLine{Dir: SideShort, Price: 400, MovedAt: auditMs(2026, 10, 7, 21, 0, 0), LastBucket: fourHBucketStart(auditMs(2026, 10, 8, 8, 30, 0), ctime())}
	e.State.HTF.OneH = TriggerLine{Dir: SideLong, Price: 300, MovedAt: auditMs(2026, 10, 8, 7, 0, 0), LastBucket: (auditMs(2026, 10, 8, 8, 30, 0) / 3_600_000) * 3_600_000}

	mk := func(y int, mo time.Month, d, hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(y, mo, d, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(2026, 10, 7, 17, 0, 497, 500, 495, 497),  // overnight hi 500
		mk(2026, 10, 8, 8, 29, 193, 198, 190, 193),  // overnight lo 190 → run 310
		mk(2026, 10, 8, 8, 30, 194, 199, 191, 194),  // the latch tick (after the session close)
	}
	now := auditMs(2026, 10, 8, 8, 31, 0)

	e.Tick(bars, now)
	rep := e.State.DayGateReport
	if rep.Key != "2026-10-08" {
		t.Fatalf("report key = %q, want 2026-10-08", rep.Key)
	}
	if rep.Verdict != DayOff {
		t.Fatalf("verdict = %v, want DayOff (spent 310 + conflict)", rep.Verdict)
	}
	if !rep.HaveRun || rep.RunPts != 310 {
		t.Fatalf("run = %.2f haveRun=%v, want 310 true", rep.RunPts, rep.HaveRun)
	}
	if rep.HiPx != 500 || rep.LoPx != 190 {
		t.Fatalf("hi/lo = %.2f/%.2f, want 500/190", rep.HiPx, rep.LoPx)
	}
	if rep.FourHDir != SideShort || rep.OneHDir != SideLong || !rep.Conflict {
		t.Fatalf("dirs = %s/%s conflict=%v, want short/long true", rep.FourHDir, rep.OneHDir, rep.Conflict)
	}
	if rep.SpentPts != 300 {
		t.Fatalf("spent line = %.0f, want 300", rep.SpentPts)
	}

	// once: a second tick (same day) must not overwrite the report.
	bars2 := append(append([]market.Kline{}, bars...), mk(2026, 10, 8, 9, 0, 194, 199, 191, 194))
	e.Tick(bars2, auditMs(2026, 10, 8, 9, 1, 0))
	if e.State.DayGateReport != rep {
		t.Fatalf("report changed on the second tick:\n  first %+v\n  now   %+v", rep, e.State.DayGateReport)
	}
}

// TestDayOffRecheckReal1008HourlySequence — the real 10-08 hourly shape the CTO
// settled (RECONCILE SETTLED, 2026-10-09): 4h SHORT all day; 1h LONG drawn at
// 08:00, then the 10:00 hour's low 31169.75 breaks the 09:00 hour's low
// 31189.50 (no high break: 10:00 high 31273 ≤ 09:00 high 31280) → the 1h flips
// SHORT at the 10:00 bar's CLOSE (11:00 CT). The recheck must clear there, dir
// SHORT — the production e.State.HTF (with the broke-both tie-break) path.
func TestDayOffRecheckReal1008HourlySequence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false
	cfg.DayOffRecheck = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	e := New(cfg)

	// 4h SHORT since 10-07 21:00, frozen at the tape's own 4h bucket.
	now9 := auditMs(2026, 10, 8, 9, 0, 0)
	fh := fourHBucketStart(now9, ctime())
	e.State.HTF.FourH = TriggerLine{Dir: SideShort, Price: 31371.75, MovedAt: auditMs(2026, 10, 7, 21, 0, 0), LastBucket: fh, LastBar: market.Kline{OpenTime: fh - 240*60_000, High: 31466, Low: 31119, Close: 31120}}
	// 1h LONG drawn at 08:00 (after the 4h) → conflict.
	h8 := (auditMs(2026, 10, 8, 8, 0, 0) / 3_600_000) * 3_600_000
	e.State.HTF.OneH = TriggerLine{Dir: SideLong, Price: 31210.75, MovedAt: h8, LastBucket: h8, LastBar: market.Kline{OpenTime: h8, High: 31300, Low: 31100, Close: 31210}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now9).In(ctime())), Verdict: DayOff}

	mkHour := func(hh int, hi, lo, cl float64) []market.Kline {
		t0 := auditMs(2026, 10, 8, hh, 0, 0)
		out := make([]market.Kline, 0, 60)
		for m := 0; m < 60; m++ {
			ot := t0 + int64(m)*60_000
			out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 59_000, Open: cl, High: hi, Low: lo, Close: cl})
		}
		return out
	}
	hour9 := mkHour(9, 31280, 31189.50, 31220)  // 09:00 — no low break vs 08:00 low 31100
	hour10 := mkHour(10, 31273, 31169.75, 31170) // 10:00 — low breaks 31189.50, high 31273 ≤ 31280
	bars0910 := append(append([]market.Kline{}, hour9...), hour10...)

	// Tick 1: the 09:00 1h bar closes (now 10:00 CT) — the 1h is still LONG.
	e.Tick(hour9, auditMs(2026, 10, 8, 10, 0, 0))
	if e.State.Day.Verdict != DayOff {
		t.Fatalf("after the 09:00 close verdict = %v, want DayOff (still conflicting)", e.State.Day.Verdict)
	}

	// Tick 2: the 10:00 1h bar closes (now 11:00 CT) — the 1h flips SHORT and
	// agrees with the 4h → the recheck clears, direction SHORT.
	e.Tick(bars0910, auditMs(2026, 10, 8, 11, 0, 0))
	if e.State.Day.Verdict != DaySpent {
		t.Fatalf("after the 10:00 close verdict = %v, want DaySpent (cleared)", e.State.Day.Verdict)
	}
	rc := e.State.DayRecheck
	if rc.Clears != 1 || rc.ClearedDir != SideShort {
		t.Fatalf("recheck = %+v, want Clears=1 ClearedDir=short (11:00 CT, dir SHORT)", rc)
	}
	// The clear fired on the 10:00 bar's close — 11:00 CT.
	if got := time.UnixMilli(rc.ClearedAt).In(ctime()).Format("15:04"); got != "11:00" {
		t.Fatalf("ClearedAt = %s CT, want 11:00", got)
	}
}
