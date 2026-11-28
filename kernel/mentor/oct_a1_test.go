package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// OCT-EXACT build step A1 (CTO-decisions.md section A1, owner order 2026-11-14
// "mentor mode = the October course exactly"). Every gate below ships OFF in
// DefaultConfig; the revert is the old value (true / 0.80).

// TestOctA1_Defaults pins the October defaults of every A1 row that lives in
// the kernel Config.
func TestOctA1_Defaults(t *testing.T) {
	d := DefaultConfig()
	off := map[string]bool{
		"HTFGateEnabled (row 1)":      d.HTFGateEnabled,
		"DayGateEnabled (row 2)":      d.DayGateEnabled,
		"Swing4HEnabled (row 3)":      d.Swing4HEnabled,
		"OrbPart2Enabled (row 4)":     d.OrbPart2Enabled,
		"Open5mGateEnabled (row 5)":   d.Open5mGateEnabled,
		"NewsWaitBothLines (row 6)":   d.NewsWaitBothLines,
		"NewsBarsExcluded (row 6)":    d.NewsBarsExcluded,
		"LegBudgetEnabled (row 9)":    d.LegBudgetEnabled,
		"LossBoxEnabled (row 10)":     d.LossBoxEnabled,
		"MidRangeRejectSide (row 11)": d.MidRangeRejectSide,
		"BreakDirectionLock (row 12)": d.BreakDirectionLock,
	}
	for name, v := range off {
		if v {
			t.Errorf("%s default = true, want false (October is silent or contrary)", name)
		}
	}
	if d.OrbISBFractionOutside != 0 {
		t.Errorf("OrbISBFractionOutside default = %v, want 0 (orb_isb_mostly_inside OFF, row 4)", d.OrbISBFractionOutside)
	}
	if !d.OrbGateEnabled {
		t.Errorf("OrbGateEnabled default = false, want true: ORB part 1 (orb_inside) must stay ON [Oct D4.3 @ 61:44 R63]")
	}
}

// Row 1: the 4h/1h gate is inactive by default at every hour; the revert knob
// (mentor_htf_gate_enabled=true) restores the m462 all-session gate.
func TestOctA1_HTFGateOff(t *testing.T) {
	now := auditMs(2026, 11, 2, 9, 0, 0)
	cfg := DefaultConfig()
	if HTFGateActive(now, cfg) {
		t.Fatalf("HTFGateActive under the October default = true, want false [Oct D4.3-R9 @07:28: the only big frame is the 5m]")
	}
	cfg.HTFGateEnabled, cfg.HTFGateNewsOnly = true, false
	if !HTFGateActive(now, cfg) {
		t.Fatalf("revert (mentor_htf_gate_enabled=true, news_only=false) must restore the all-session gate")
	}
	// The shared check allows a with-nothing entry when the gate is off.
	h := HTF{GateOff: true}
	if r, _ := HTFEntryCheck(h, SideLong); r != "" {
		t.Fatalf("HTFEntryCheck with GateOff refused %q", r)
	}
}

// Row 2: D5.1 @19:22 day: spent (run 265+) AND 4h/1h conflict was day_off.
func TestOctA1_DayGateOff(t *testing.T) {
	d := DefaultConfig()
	dg := DayGate{SpentPts: d.DayGateSpentPts, TargetCapPts: d.DayGateTargetCapPts, Off: !d.DayGateEnabled}
	if v, _ := DayGateVerdict(300, true, true, dg); v != DayTrade {
		t.Fatalf("spent+conflict under the October default = %v, want DayTrade (gate OFF)", v)
	}
	if v, _ := DayGateVerdict(300, false, false, dg); v != DayTrade {
		t.Fatalf("unmeasured run under the October default = %v, want DayTrade (day_not_measured OFF)", v)
	}
	dg.Off = false // the revert
	if v, _ := DayGateVerdict(300, true, true, dg); v != DayOff {
		t.Fatalf("revert: spent+conflict = %v, want DayOff", v)
	}
	if v, _ := DayGateVerdict(300, true, false, dg); v != DaySpent {
		t.Fatalf("revert: spent = %v, want DaySpent", v)
	}
	// The spent-day target cap is inert on a DayTrade verdict.
	in := Intent{Side: SideLong, Price: 100, Target: 160}
	if got := CapTargetForDay(in, DayTrade, dg); got.Target != 160 {
		t.Fatalf("target capped on DayTrade: %v", got.Target)
	}
}

// Row 4: ORB part 1 (orb_inside) stays; part 2 (not drawn / not escaped / wrong
// side) is OFF. Video: OctD4.3-R63 @1:01:44 "nến đang nằm trong vòng ORB 2 phút, đừng trade".
func TestOctA1_ORBPart2OffPart1Stays(t *testing.T) {
	cfg := DefaultConfig()
	orb := ORB{Drawn: true, High: 100, Low: 90}
	// Part 1: inside the 2-minute range is refused, escaped or not.
	if ok, _ := ORBVerdict(orb, SideLong, 95, cfg); ok {
		t.Fatalf("entry priced inside the ORB must stay refused (orb_inside, part 1)")
	}
	if r := orbRefusalStage(orb, SideLong, 95, cfg); r != "orb_inside" {
		t.Fatalf("refusal stage = %q, want orb_inside", r)
	}
	// Part 2: not escaped / wrong side / not drawn no longer refuse.
	if ok, why := ORBVerdict(orb, SideLong, 101, cfg); !ok {
		t.Fatalf("outside the range, not yet escaped: refused (%s), want allowed (orb_not_escaped OFF)", why)
	}
	orb.Escaped = SideShort
	if ok, why := ORBVerdict(orb, SideLong, 101, cfg); !ok {
		t.Fatalf("wrong side after a short escape: refused (%s), want allowed (orb_wrong_side OFF)", why)
	}
	if ok, why := ORBVerdict(ORB{}, SideLong, 101, cfg); !ok {
		t.Fatalf("ORB not drawn: refused (%s), want allowed (orb_not_drawn OFF)", why)
	}
	// Revert: part 2 back on.
	cfg.OrbPart2Enabled = true
	if ok, _ := ORBVerdict(ORB{}, SideLong, 101, cfg); ok {
		t.Fatalf("revert: orb_not_drawn must refuse again")
	}
	if ok, _ := ORBVerdict(orb, SideLong, 101, cfg); ok {
		t.Fatalf("revert: orb_wrong_side must refuse again")
	}
}

// Row 5: the Sept open-5m range break gate is OFF by default; the fixture is
// TestOpen5mRangeBreak's pre-break case, which refused before.
func TestOctA1_Open5mGateOff(t *testing.T) {
	mk := func(hh, mm int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, hh, mm, 0)
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(8, 30, 100.0, 100.5, 99.5, 100.2), mk(8, 31, 100.2, 100.4, 99.6, 100.1),
		mk(8, 32, 100.1, 100.3, 99.2, 100.0), mk(8, 33, 100.0, 100.2, 99.0, 99.8),
		mk(8, 34, 99.8, 100.1, 99.4, 100.0),
		mk(8, 35, 100.0, 100.2, 99.8, 100.0), mk(8, 36, 100.0, 100.1, 99.7, 99.9),
		mk(8, 37, 99.9, 100.0, 99.6, 99.8), mk(8, 38, 99.8, 100.0, 99.7, 99.9),
		mk(8, 39, 99.9, 100.1, 99.8, 100.0),
	}
	now := auditMs(2026, 9, 15, 9, 0, 0)
	h := HTF{FourH: TriggerLine{Dir: SideLong, Price: 102.0}}
	in := Intent{Action: PlaceStopEntry, Side: SideLong, Reason: "PHL", Price: 100.6, Stop: 99.5, Target: 102.0}

	cfg := DefaultConfig()
	kept, refused, _ := open5mRangeFilter([]Intent{in}, h, bars, now, cfg)
	if len(kept) != 1 || len(refused) != 0 {
		t.Fatalf("October default: kept=%d refused=%v, want 1 kept / none", len(kept), refused)
	}
	cfg.Open5mGateEnabled = true // the revert
	kept, refused, _ = open5mRangeFilter([]Intent{in}, h, bars, now, cfg)
	if len(kept) != 0 || len(refused) != 1 || refused[0] != "open_5m_range_not_broken" {
		t.Fatalf("revert: kept=%d refused=%v, want 0 / [open_5m_range_not_broken]", len(kept), refused)
	}
}

// Row 3: the SWING 4H→5M setup is OFF by default — fixture is
// TestEvaluatorTickWarmSwingEntryUsesSeededLine (one short swing entry under the
// revert), which emitted before; October: "the 5m is not traded because the stop
// is too big" [Oct D4.3 @ 07:58 R10].
func TestOctA1_Swing4HOff(t *testing.T) {
	loc := ctime()
	bar := func(day, hour, minute int, o, h, l, c float64) market.Kline {
		open := time.Date(2026, time.September, day, hour, minute, 0, 0, loc)
		return market.Kline{OpenTime: open.UnixMilli(), CloseTime: open.UnixMilli() + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		bar(14, 17, 5, 1000, 1000, 1000, 1000), bar(14, 21, 5, 1600, 1600, 1600, 1600),
		bar(15, 1, 5, 1000, 1000, 1000, 1000), bar(15, 5, 0, 900, 905, 895, 900),
		bar(15, 5, 5, 1100, 1100, 900, 900), bar(15, 5, 10, 970, 1020, 930, 940),
		bar(15, 5, 15, 940, 945, 935, 940),
	}
	now := bars[len(bars)-1].CloseTime + 1
	run := func(cfg Config) (swings int, e *Evaluator) {
		e = New(cfg)
		e.Cfg.Enabled = true
		e.State.Swing = SwingState{Line: 950, BucketStart: fourHBucketStart(now, loc), LastBarTime: bars[3].OpenTime,
			EmaCount: FourHEMA34Min, FirstTouch: &swingTouch{Approach: SideShort, Through: true}}
		for _, in := range e.Tick(bars, now) {
			if in.Setup == "SWING4H" {
				swings++
			}
		}
		return swings, e
	}
	if n, e := run(DefaultConfig()); n != 0 || e.State.Swing.Pending != nil || e.State.Swing.Pos != nil {
		t.Fatalf("October default: %d SWING4H intents, pending=%v pos=%v, want none (no NEW swing arm)", n, e.State.Swing.Pending, e.State.Swing.Pos)
	}
	if n, _ := run(preOctCfg()); n != 1 {
		t.Fatalf("revert (mentor_swing4h_enabled=true): %d SWING4H intents, want 1", n)
	}
}

// Rows 9 and 10: with the G1 leg budget and the G2 loss box OFF, a stop-out does
// not box its band and the third entry in a leg is not refused. Fixture:
// TestLimitsISBLossBoxesTheBand (the in-band re-entry was refused).
func TestOctA1_LossBoxAndLegBudgetOff(t *testing.T) {
	run := func(cfg Config) int {
		var l Limits
		cfg.Enabled = true
		cfg.ISBLossBox = ISBLossBoxBand
		prev := limitsK(94, 96, 94, 95, 0)
		exp := limitsNow(60) + 86400_000
		applyAtG2(&l, []Intent{limitsISB(92, 89, 98, exp)}, prev, limitsK(93, 94, 92, 93, 1), 1, cfg)
		applyAtG2(&l, nil, limitsK(93, 94, 92, 93, 1), limitsK(92, 93, 88, 89, 2), 2, cfg) // fill + stop
		return len(applyAtG2(&l, []Intent{limitsISB(91, 88, 97, exp)}, limitsK(92, 93, 88, 89, 2), limitsK(90, 91.5, 89.5, 91, 3), 3, cfg))
	}
	if n := run(DefaultConfig()); n != 1 {
		t.Fatalf("October default: in-band re-entry after a stop-out emitted %d, want 1 (loss box OFF)", n)
	}
	rev := preOctCfg()
	rev.LegBudgetEnabled = false // isolate G2 (the leg budget would also refuse)
	if n := run(rev); n != 0 {
		t.Fatalf("revert (mentor_loss_box_enabled=true): in-band re-entry emitted %d, want 0 (loss_box_blocked)", n)
	}
}
