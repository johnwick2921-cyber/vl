package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// SWING-RISK (release #22 fix wave) — call-site pins for the swing's risk and
// exit. The production chain is Evaluator.Tick → runSwing → SwingTick →
// manageSwingPosition; every tick here goes through Evaluator.Tick.
//
// Release #30: the swing runner holds by the 4h with its hard stop — the
// bot-only break-even at +1R and the 2nd-4h-candle forced close are REMOVED
// [D5.2 p1 @05:48–06:18 "gồng theo khung 4 giờ"; no swing BE / hold length in
// the course]. The first partial (the 5m EMA 34) is the swing's own target via
// the CX-2 trader split.
//
// The fixture is TestEvaluatorTickWarmSwingEntryUsesSeededLine's: a warm 4h
// line at 950 and a through-close first touch from below, so the 05:10 CT 5m
// inside bar (930–1020) arms a SHORT stop entry at 930 with the stop AT the
// line, 950 — risk 20. Then:
//
//	05:20 bar  trades through 930 (the fill) and closes at 929.50 = +0.025R
//	05:25 bar  closes at 909.50 = +1.025R (the first 5m close at ≥ +1R)
//
// Neither the fill bar nor the +1R close moves the stop to break-even — the
// runner keeps the hard stop by the 4h.

func swingRiskBar(day, hour, minute int, o, h, l, c float64) market.Kline {
	open := time.Date(2026, time.September, day, hour, minute, 0, 0, ctime())
	return market.Kline{OpenTime: open.UnixMilli(), CloseTime: open.UnixMilli() + 59_999, Open: o, High: h, Low: l, Close: c}
}

// swingRiskTape returns the entry tape (through the 05:15 1m bar), the fill
// bar and the +1R bar.
func swingRiskTape() (entryTape []market.Kline, fill, oneR market.Kline) {
	entryTape = []market.Kline{
		swingRiskBar(14, 17, 5, 1000, 1000, 1000, 1000),
		swingRiskBar(14, 21, 5, 1600, 1600, 1600, 1600),
		swingRiskBar(15, 1, 5, 1000, 1000, 1000, 1000),
		swingRiskBar(15, 5, 0, 900, 905, 895, 900),
		swingRiskBar(15, 5, 5, 900, 1100, 900, 1100),
		swingRiskBar(15, 5, 10, 970, 1020, 930, 940),
		swingRiskBar(15, 5, 15, 940, 941, 935, 940), // high 941: never reaches the 945 placed line
	}
	// each on the LAST minute of its 5m bucket, so the bucket closes with it
	fill = swingRiskBar(15, 5, 24, 935, 938, 928, 929.5)
	oneR = swingRiskBar(15, 5, 29, 929, 931, 909, 909.5)
	return entryTape, fill, oneR
}

// swingWarmState is the warm 4h-line state the fixture seeds (the seed path's
// EmaCount ≥ FourHEMA34Min keeps the line authoritative within the bucket).
func swingWarmState(bucket, lastBar int64, touch *swingTouch) SwingState {
	return SwingState{Line: 950, BucketStart: bucket, LastBarTime: lastBar, EmaCount: FourHEMA34Min, FirstTouch: touch}
}

// swingIntentsOf runs ONE production tick and returns its swing intents.
func swingIntentsOf(e *Evaluator, bars []market.Kline) []Intent {
	now := bars[len(bars)-1].CloseTime + 1
	var out []Intent
	for _, in := range e.Tick(bars, now) {
		if len(in.ArmID) >= 6 && in.ArmID[:6] == "swing-" {
			out = append(out, in)
		}
	}
	return out
}

func countAction(ins []Intent, a Action) int {
	n := 0
	for _, in := range ins {
		if in.Action == a {
			n++
		}
	}
	return n
}

// TestEvaluatorTickSwingNeverMovesStopBE — the live-fill path, release #30:
// the swing runner holds by the 4h with its hard stop, so neither the fill bar
// nor a +1R close moves the stop to break-even (the bot-only BE-at-+1R is
// removed [D5.2: no swing break-even rule]). The position still FILLS.
func TestEvaluatorTickSwingNeverMovesStopBE(t *testing.T) {
	tape, fill, oneR := swingRiskTape()
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	now := tape[len(tape)-1].CloseTime + 1
	e.State.Swing = swingWarmState(fourHBucketStart(now, ctime()), tape[3].OpenTime, &swingTouch{Approach: SideShort, Through: true})

	entries := swingIntentsOf(e, tape)
	if len(entries) != 1 || entries[0].Action != PlaceStopEntry || entries[0].Side != SideShort {
		t.Fatalf("the fixture must arm one short swing entry, got %+v", entries)
	}
	in := entries[0]
	if in.Price != 930 || in.Stop != 950 {
		t.Fatalf("fixture drift: swing entry %.2f stop %.2f, want 930 / 950", in.Price, in.Stop)
	}

	atFill := swingIntentsOf(e, append(append([]market.Kline{}, tape...), fill))
	if e.State.Swing.Pos == nil {
		t.Fatalf("the 05:20 bar trades through 930: the swing must be FILLED, pos=nil (intents %+v)", atFill)
	}
	if n := countAction(atFill, ActionMoveStopBE); n != 0 {
		t.Fatalf("release #30: no BE on the fill bar; intents %+v", atFill)
	}

	atOneR := swingIntentsOf(e, append(append([]market.Kline{}, tape...), fill, oneR))
	if n := countAction(atOneR, ActionMoveStopBE); n != 0 {
		t.Fatalf("release #30: no BE at +1R — the runner holds by the 4h with its hard stop; got %d (%+v)", n, atOneR)
	}
}

// TestEvaluatorTickRebuiltSwingNeverMovesStopBE — the restart path, release
// #30: the same swing restored by RebuildSwingPos holds by the 4h with its hard
// stop — no break-even at +1R after the rebuild either.
func TestEvaluatorTickRebuiltSwingNeverMovesStopBE(t *testing.T) {
	tape, fill, oneR := swingRiskTape()
	e := New(DefaultConfig())
	e.Cfg.Enabled = true
	refBar := tape[5] // the 05:10 inside bar the entry was placed on
	afterFill := append(append([]market.Kline{}, tape...), fill)
	// the restarted evaluator has already walked the 5m tape through the fill
	// bucket's predecessor; the fill is in the records, not re-walked.
	e.State.Swing = swingWarmState(fourHBucketStart(refBar.OpenTime, ctime()), tape[6].OpenTime, nil)
	if !e.State.Swing.RebuildSwingPos(SwingRebuild{Side: SideShort, Entry: 930, EntryStop: 950, LiveStop: 950, Target: 890,
		EntryBucket: fourHBucketStart(refBar.OpenTime, ctime()), ArmID: "swing-rebuilt", MovedBE: false}) {
		t.Fatal("RebuildSwingPos refused a valid short swing (entry 930, stop 950)")
	}
	if got := e.State.Swing.Pos.Risk; got != 20 {
		t.Fatalf("the rebuilt risk is %.2f, want 20 (the ONE SwingRisk: stop − entry for a short)", got)
	}
	atFill := swingIntentsOf(e, afterFill)
	if n := countAction(atFill, ActionMoveStopBE); n != 0 {
		t.Fatalf("release #30: no BE on the fill bar; intents %+v", atFill)
	}
	atOneR := swingIntentsOf(e, append(afterFill, oneR))
	if n := countAction(atOneR, ActionMoveStopBE); n != 0 {
		t.Fatalf("release #30: the rebuilt swing must not move the stop to BE at +1R: got %d (%+v)", n, atOneR)
	}
}

// TestSwingRiskIsPositiveForBothSides — the ONE definition: positive for a
// well-formed swing on either side, ≤ 0 for a stop on the wrong side (the
// rebuild refuses that).
func TestSwingRiskIsPositiveForBothSides(t *testing.T) {
	cases := []struct {
		side        Side
		entry, stop float64
		want        float64
	}{
		{SideLong, 30654, 30599, 55},
		{SideShort, 31147, 31156.2, 9.2},
		{SideLong, 100, 110, -10},
		{SideShort, 100, 90, -10},
	}
	for _, c := range cases {
		if got := SwingRisk(c.side, c.entry, c.stop); abs(got-c.want) > 1e-9 {
			t.Fatalf("SwingRisk(%s, %.2f, %.2f) = %.4f, want %.4f", c.side, c.entry, c.stop, got, c.want)
		}
	}
	var s SwingState
	if s.RebuildSwingPos(SwingRebuild{Side: SideLong, Entry: 100, EntryStop: 110, LiveStop: 110, ArmID: "swing-bad"}) {
		t.Fatal("a long whose entry stop sits ABOVE the entry must be refused (risk ≤ 0)")
	}
}
