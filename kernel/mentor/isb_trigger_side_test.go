package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// I1 (release #3b): the ISB ignored the 5m trigger SIDE. The trigger direction
// was discarded at eval.go:622 (`TriggerVerdict(e.State.Trigger, cur.Close)`
// kept only `dirOK`), so the ISB side (candle-1 colour) was compared with the
// 4h `htfSide` only. A short ISB could sit ABOVE a 5m buy line (or a long
// below a sell line) and still trade. Course: after a buy trigger, entries are
// long only and above the line, "BẤT KỲ TRƯỜNG HỢP NÀO" [D3.4 p1 @09:30–10:38,
// @17:39; p3 @02:24 "nằm ở dưới đường line này em đánh inside bar SHORT";
// D2.2 p3 @00:39, @04:19; D4.3 @14:40].
//
// These pins run through the PRODUCTION CALL SITE Evaluator.Tick.

// shortISBFixture is the 3-bar ISB fixture with a RED candle 1 (→ short), the
// mirror of isbFixture(): the head bar keeps every EMA target away, candle 1
// is red (candle-1 colour → short), and candle 2's body sits inside candle 1's
// full range.
func shortISBFixture() []market.Kline {
	return []market.Kline{
		rthBars(0, 121, 121.5, 120.5, 120),
		rthBars(1, 106, 108, 97, 99),    // red candle 1 → short
		rthBars(2, 101, 104, 100, 102), // body inside candle 1
	}
}

// newShortISBEval returns an evaluator with a LONG trigger line and a SHORT 4h
// — the pin-1 shape: 5m buy line + 4h short.
func newShortISBEval(cfg Config) *Evaluator {
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 90}}
	bars := shortISBFixture()
	now := bars[len(bars)-1].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideShort}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e
}

// TestISBTriggerSideMismatchRefused — pin 1: 5m buy line + 4h short + a short
// ISB above the line → refused with the counted reason isb_trigger_side_mismatch.
func TestISBTriggerSideMismatchRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := newShortISBEval(cfg)
	bars := shortISBFixture()

	// Sanity: the pair IS an ISB, candle-1 colour is short, and the close sits
	// ABOVE the 5m buy line — the wrong-side ISB the fix refuses.
	prev, cur := bars[1], bars[2]
	if !IsISB(prev, cur) {
		t.Fatal("fixture: candle 2 is not an inside bar")
	}
	if d := ISBDirection(prev); d != SideShort {
		t.Fatalf("fixture: candle-1 colour = %q, want short", d)
	}
	if ok, _, _ := TriggerVerdict(e.State.Trigger, cur.Close); !ok || cur.Close < 90 {
		t.Fatalf("fixture: close %.2f is not above the 5m buy line", cur.Close)
	}

	ints := e.Tick(bars, bars[2].CloseTime+1)
	if e.State.Refusals["isb_trigger_side_mismatch"] == 0 {
		t.Fatalf("the wrong-side ISB must be refused by name; ledger = %v", e.State.Refusals)
	}
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry {
			t.Fatalf("no ISB entry may survive the trigger-side refusal; got %+v", in)
		}
	}
}

// TestISBTriggerSideMatchPlaced — pin 2: 5m buy line + a long ISB → placed
// (no trigger-side refusal).
func TestISBTriggerSideMatchPlaced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := newISBEval(cfg) // trigger long, 4h long, ORB escaped long
	bars := isbFixture()

	ints := e.Tick(bars, bars[2].CloseTime+1)
	if e.State.Refusals["isb_trigger_side_mismatch"] != 0 {
		t.Fatalf("a long ISB on a buy line must not be refused; ledger = %v", e.State.Refusals)
	}
	placed := false
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry {
			placed = true
		}
	}
	if !placed {
		t.Fatalf("the long ISB did not place; intents = %+v, ledger = %v", ints, e.State.Refusals)
	}
}

// TestISBNoTriggerLineUnchanged — pin 3: no 5m line → the ISB places exactly as
// before (the trigger-side check is skipped, no refusal).
func TestISBNoTriggerLineUnchanged(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := newISBEval(cfg)
	e.State.Trigger = TriggerLine{} // no live 5m trigger line
	bars := isbFixture()

	ints := e.Tick(bars, bars[2].CloseTime+1)
	if e.State.Refusals["isb_trigger_side_mismatch"] != 0 {
		t.Fatalf("with no trigger line there is no side to mismatch; ledger = %v", e.State.Refusals)
	}
	placed := false
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry {
			placed = true
		}
	}
	if !placed {
		t.Fatalf("the long ISB did not place without a trigger line; intents = %+v, ledger = %v", ints, e.State.Refusals)
	}
}

// TestISBBoxDoesNotHideTheTriggerLine — isbTF-F1 (October overrides the R5
// box clause of Sept D4.1-23): a standing 5m ISB box no longer hides the
// trigger line. A short box + a short ISB under a LONG 5m trigger line is
// refused isb_trigger_side_mismatch — a trade inside the 5m box needs BOTH the
// trigger and the box [OctD4.3-R69, Oct D4.3 @ 67:26]; no short while the 5m
// has a BUY trigger [OctD4.3-R28/R29, Oct D4.3 @ 22:00, 23:20]. The box gate
// itself still passes (same direction, OctD4.3-R70).
//
// Mutant: the pre-F1 seam (box hides the trigger) → no refusal → RED.
func TestISBBoxDoesNotHideTheTriggerLine(t *testing.T) {
	run := func() *Evaluator {
		cfg := DefaultConfig()
		cfg.Enabled = true
		e := newISBEval(cfg) // trigger long
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 90}}
		e.State.ISBBox = &ISBBox{High: 200, Low: 50, Dir: SideShort, AtTime: auditMs(2026, 9, 15, 9, 0, 0)}
		bars := shortISBFixture()
		e.Tick(bars, bars[2].CloseTime+1)
		return e
	}
	e := run()
	if e.State.Refusals["isb_box_blocked"] != 0 {
		t.Fatalf("a short ISB matching a short box must not be box-blocked; ledger = %v", e.State.Refusals)
	}
	if e.State.Refusals["isb_trigger_side_mismatch"] != 1 {
		t.Fatalf("OctD4.3-R28/R69: the 5m BUY trigger still governs inside the short box — want isb_trigger_side_mismatch; ledger = %v", e.State.Refusals)
	}
	prev := isbTriggerHiddenByBox5m
	isbTriggerHiddenByBox5m = true
	defer func() { isbTriggerHiddenByBox5m = prev }()
	if e := run(); e.State.Refusals["isb_trigger_side_mismatch"] != 0 {
		t.Fatalf("pre-F1 seam: the box hid the trigger (Sept D4.1-23); ledger = %v", e.State.Refusals)
	}
}
