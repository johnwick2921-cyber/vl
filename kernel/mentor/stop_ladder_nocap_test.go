package mentor

import (
	"math"
	"strings"
	"testing"

	"vl/market"
)

// Stop-size ladder, NO CAP (owner order 2026-11-15: "tarde all isb or any
// thing just need adjust contact"; "what u mean all sto loss just change the
// size of contact"). With the ladder ON and mentor_wide_stop_max_pts unset
// (0, the new default) no non-swing stop is refused for its SIZE — the
// trader's size table sizes it down (above 30 → mentor_wide_stop_contracts,
// 1 MNQ "Trade vẫn được. 1 MNQ" [OctD2.3-R47 @48:12]). The setups' other
// rules (1:1 floor, room, isb_trigger_side, the ISB boxes, orb_inside,
// just-broken, near_box) are NOT touched. A stored positive value restores
// the refusal above it (revert = 40, the #70 behaviour — pinned by the
// 40-explicit tests in stop_size_ladder_test.go).

// TestStopLadderNoCapDefault — the production default config: the ladder
// ceiling is +Inf, so ISB / PHL/PLH / BOX at 45..150 pts are never refused
// for stop size; the ladder OFF path keeps the pre-ladder 20/25 ceilings.
// RED on #70: the default max is 40, so 45+ is refused.
func TestStopLadderNoCapDefault(t *testing.T) {
	d := DefaultConfig()
	if d.CourseExits.WideStopMaxPts != 0 {
		t.Errorf("CourseExits.WideStopMaxPts default = %g, want 0 = no cap (owner order 2026-11-15)", d.CourseExits.WideStopMaxPts)
	}
	for _, off := range []float64{20, 25} {
		if got := StopLadderCeiling(d, off); !math.IsInf(got, 1) {
			t.Errorf("StopLadderCeiling(DefaultConfig(), %g) = %g, want +Inf (no stop-size cap)", off, got)
		}
	}
	// Ladder OFF + 0 → the setup's own pre-ladder ceiling, unchanged.
	offCfg := d
	offCfg.CourseExits.StopSizeLadder = false
	for _, off := range []float64{20, 25} {
		if got := StopLadderCeiling(offCfg, off); got != off {
			t.Fatalf("ladder OFF + max 0: StopLadderCeiling(%g) = %g, want %g", off, got, off)
		}
	}

	// ISB: the production stop gate (ISBStopVerdict — ISB, reverse ISB and
	// the stack read all call it) passes every wide stop.
	cfg := preOctCfg()
	if !cfg.CourseExits.StopSizeLadder {
		t.Fatal("fixture: preOctCfg must carry the production ladder default (ON)")
	}
	buf2 := 2 * cfg.ISBBufferPts
	for _, s := range []float64{45, 60, 100, 150} {
		stop, ok, why := ISBStopVerdict(ladderISBCandle(s-buf2), cfg)
		if !ok || stop != s {
			t.Fatalf("no cap: %.0f-pt ISB ok=%v stop=%.2f why=%q, want taken (sized by the trader, never refused)", s, ok, stop, why)
		}
	}

	// PHL/PLH: the production builder emits; no *_stop_ceiling reason.
	pcfg := cfg
	pcfg.Enabled = true
	pad := pcfg.PHLEntryBufferPts + pcfg.PHLStopBufferPts
	touch := func(risk float64) Touch {
		return Touch{Outcome: TouchReject, RefBar: market.Kline{High: 100 + risk - pad, Low: 100}, ApproachedFrom: SideLong}
	}
	for _, s := range []float64{45, 100} {
		in, ok, why := PHLPLH(touch(s), Level{Kind: KindOldExtreme, Price: 100 + 10*s}, 0, 3, pcfg)
		if !ok || strings.Contains(why, "mentor_wide_stop_max_pts") || strings.Contains(why, "ceiling") {
			t.Fatalf("no cap: %.0f-pt PHL ok=%v why=%q, want taken (no stop ceiling)", s, ok, why)
		}
		if r := in.Price - in.Stop; math.Abs(r-s) > 0.01 {
			t.Fatalf("fixture: PHL risk %.2f, want %.0f", r, s)
		}
	}

	// BOX: the production box entry emits; never box_stop_ceiling.
	bcfg := cfg
	bcfg.Enabled = true
	bcfg.RoomMultiple = 0.05
	floor := Box{Kind: FTGL, Top: 100, Bottom: 50, Key: "ftgl:100.00:50.00"}
	far := []Level{{Key: "k", Kind: KindKeyLevel, Price: 2000}}
	buy := TriggerLine{Dir: SideLong, Price: 60}
	for _, s := range []float64{45, 100} {
		ref := market.Kline{High: 101, Low: 101 - s, Close: 101}
		out, why := boxEntryIntentWhy(ref, floor, []Box{floor}, far, buy, nil, bcfg)
		if why == "box_stop_ceiling" || why != "" || len(out) != 1 {
			t.Fatalf("no cap: %.0f-pt box got %d intents why=%q, want 1 intent (never box_stop_ceiling)", s, len(out), why)
		}
		if r := out[0].Price - out[0].Stop; r != s {
			t.Fatalf("box risk %.2f, want %.0f", r, s)
		}
	}
}

// TestStopLadderCeilingBadValueFailsClosed — a NEGATIVE or NaN
// WideStopMaxPts that reaches the kernel without the trader's resolver (a
// future caller, a hand-built Config) must FAIL CLOSED to the #70 cap 40,
// never mean "no cap" (only an exact 0 is no cap) and never fall to the
// ladder-OFF ceiling (ladder ON is still ON). Reviewer P2-1 on 7e790a741:
// -5 returned +Inf and NaN returned the OFF ceiling — two bad values, two
// different wrong answers. RED before the fix.
func TestStopLadderCeilingBadValueFailsClosed(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.CourseExits.StopSizeLadder {
		t.Fatal("fixture: DefaultConfig must carry the ladder ON")
	}
	for _, bad := range []float64{-5, -0.25, math.NaN(), math.Inf(-1)} {
		c := cfg
		c.CourseExits.WideStopMaxPts = bad
		for _, off := range []float64{20, 25} {
			got := StopLadderCeiling(c, off)
			if got != WideStopMaxFailClosedPts {
				t.Errorf("WideStopMaxPts=%g off=%g: StopLadderCeiling = %g, want fail-closed %g", bad, off, got, WideStopMaxFailClosedPts)
			}
		}
		// A setup ceiling stored ABOVE 40 still wins (the ladder never narrows).
		if got := StopLadderCeiling(c, 55); got != 55 {
			t.Errorf("WideStopMaxPts=%g off=55: StopLadderCeiling = %g, want 55", bad, got)
		}
		// ISB production gate: a 45-pt stop is refused, a 40-pt stop is taken.
		ic := preOctCfg()
		ic.CourseExits.WideStopMaxPts = bad
		buf2 := 2 * ic.ISBBufferPts
		if _, ok, _ := ISBStopVerdict(ladderISBCandle(45-buf2), ic); ok {
			t.Errorf("WideStopMaxPts=%g: 45-pt ISB taken, want refused (fail closed to 40)", bad)
		}
		if stop, ok, why := ISBStopVerdict(ladderISBCandle(40-buf2), ic); !ok || stop != 40 {
			t.Errorf("WideStopMaxPts=%g: 40-pt ISB ok=%v stop=%.2f why=%q, want taken", bad, ok, stop, why)
		}
	}
	// +Inf stored: also not a finite cap — fails closed the same way.
	c := cfg
	c.CourseExits.WideStopMaxPts = math.Inf(1)
	if got := StopLadderCeiling(c, 20); got != WideStopMaxFailClosedPts {
		t.Errorf("WideStopMaxPts=+Inf: StopLadderCeiling = %g, want fail-closed %g", got, WideStopMaxFailClosedPts)
	}
	// Ladder OFF ignores the wide max entirely, bad or not.
	c.CourseExits.StopSizeLadder = false
	c.CourseExits.WideStopMaxPts = -5
	if got := StopLadderCeiling(c, 20); got != 20 {
		t.Errorf("ladder OFF + -5: StopLadderCeiling = %g, want 20", got)
	}
}
