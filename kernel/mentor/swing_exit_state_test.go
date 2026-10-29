package mentor

import (
	"testing"

	"vl/market"
)

// Release #54 (m441) — SWING4H exit-state pins.
//
//   (a) N2  a finished swing must not act on a LATER trade
//   (b) N3  BE and hold-close are owed until the executor reports back
//   (d) P2-3 a close THROUGH the line is never swallowed by the re-arm guard
//
// Every test drives SwingTick — the production entry the Evaluator calls
// (eval.go runSwing); the receipts are the production methods the trader's
// executors call (Evaluator.SwingBEApplied / SwingEnded -> SwingState.*).

// filledShort returns a state holding a FILLED short swing (entry 9955, stop
// 10199.xx via the standard reject) on the shared swingTape, plus the bars.
//
// Tape: line 10168.16; price comes from below, touches, closes back below ->
// sell stop at the touch candle's low (10155). The next bar trades through it.
func swingFilledShort(t *testing.T, cfg SwingCfg) (*SwingState, []market.Kline, Intent) {
	t.Helper()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),      // prev: below the line
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160),  // touches, closes back below -> reject
		mk5m(t, 15, 5, 10, 10158, 10160, 10140, 10145), // trades through 10155 -> FILLED
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	var entry Intent
	for i := 4; i <= len(bars); i++ {
		out := SwingTick(s, bars[:i], cfg, bars[i-1].OpenTime+60_000)
		for _, in := range out {
			if in.Action == PlaceStopEntry {
				entry = in
			}
		}
	}
	if s.Pos == nil {
		t.Fatalf("fixture: the short swing did not fill: pending=%+v", s.Pending)
	}
	return s, bars, entry
}

// (b) The BE is owed until the executor reports it applied: the evaluator
// re-emits it once per closed 5m bar, never twice on the same bar, and stops
// the moment the receipt arrives.
// Mutant: set MovedBE=true at emit again (the single-shot) -> the second bar
// emits nothing -> RED.
func TestSwingBERetriesUntilApplied(t *testing.T) {
	cfg := DefaultSwingCfg()
	s, bars, entry := swingFilledShort(t, cfg)
	risk := s.Pos.Risk
	// A 5m bar closing at/through -1R (short) earns the BE.
	b1 := mk5m(t, 15, 5, 15, 10145, 10146, entry.Price-risk-1, entry.Price-risk-0.5)
	bars = append(bars, b1)
	out := SwingTick(s, bars, cfg, b1.OpenTime+60_000)
	if got := countAction(out, ActionMoveStopBE); got != 1 {
		t.Fatalf("the +1R close must emit one BE, got %d: %+v", got, out)
	}
	// Same bar, a later 1m tick: no duplicate.
	if out = SwingTick(s, bars, cfg, b1.OpenTime+120_000); countAction(out, ActionMoveStopBE) != 0 {
		t.Fatalf("the BE must not be re-emitted twice on the same 5m bar: %+v", out)
	}
	if s.Pos == nil || s.Pos.MovedBE {
		t.Fatalf("MovedBE must stay false until the executor reports applied: %+v", s.Pos)
	}
	// The executor REFUSED (no receipt). The next closed 5m bar re-emits it.
	b2 := mk5m(t, 15, 5, 20, b1.Close, b1.Close+1, b1.Close-1, b1.Close)
	bars = append(bars, b2)
	out = SwingTick(s, bars, cfg, b2.OpenTime+60_000)
	if got := countAction(out, ActionMoveStopBE); got != 1 {
		t.Fatalf("an unapplied BE must be re-emitted on the next 5m bar, got %d: %+v", got, out)
	}
	// Even if price pulled back off +1R, the earned BE stays owed.
	b3 := mk5m(t, 15, 5, 25, b2.Close, b2.Close+8, b2.Close-1, b2.Close+6)
	bars = append(bars, b3)
	out = SwingTick(s, bars, cfg, b3.OpenTime+60_000)
	if got := countAction(out, ActionMoveStopBE); got != 1 {
		t.Fatalf("an earned BE stays owed after a pullback, got %d: %+v", got, out)
	}
	// The receipt lands -> never again.
	if !s.BEApplied(s.Pos.ArmID) {
		t.Fatal("BEApplied for the open swing must record")
	}
	b4 := mk5m(t, 15, 5, 30, b3.Close, b3.Close+1, b3.Close-1, b3.Close)
	bars = append(bars, b4)
	if out = SwingTick(s, bars, cfg, b4.OpenTime+60_000); countAction(out, ActionMoveStopBE) != 0 {
		t.Fatalf("an applied BE must not be re-emitted: %+v", out)
	}
	// A receipt for another arm is ignored.
	if s.BEApplied("swing-other") || s.Ended("swing-other") {
		t.Fatal("a receipt for another arm must be ignored")
	}
}

// (b) The hold-close is owed until the position is confirmed over: re-emitted
// every closed 5m bar past the deadline, and cleared only by Ended.
// Mutant: Pos=nil at emit again -> the second bar emits nothing -> RED.
func TestSwingHoldCloseRetriesUntilEnded(t *testing.T) {
	cfg := DefaultSwingCfg()
	s, bars, _ := swingFilledShort(t, cfg)
	hold, _ := swingHold(s.Pos.EntryBucket, cfg.Hold4hBars)
	// First bar AT the hold deadline (a bar that opens at/after it).
	mk := func(open int64, px float64) market.Kline {
		return market.Kline{OpenTime: open, Open: px, High: px + 1, Low: px - 1, Close: px, CloseTime: open + 299_999}
	}
	b1 := mk(hold, 10150)
	bars = append(bars, b1)
	out := SwingTick(s, bars, cfg, b1.CloseTime+1)
	if got := countAction(out, ActionClosePosition); got != 1 {
		t.Fatalf("the hold deadline must emit one close, got %d: %+v", got, out)
	}
	if s.Pos == nil {
		t.Fatal("the swing must stay open in the evaluator until the executor reports it over")
	}
	// Refused / failed (no receipt): the next 5m bar re-emits it.
	b2 := mk(hold+swingBarMs, 10150)
	bars = append(bars, b2)
	out = SwingTick(s, bars, cfg, b2.CloseTime+1)
	if got := countAction(out, ActionClosePosition); got != 1 {
		t.Fatalf("an unconfirmed hold-close must be re-emitted on the next 5m bar, got %d: %+v", got, out)
	}
	// Confirmed over -> never again.
	if !s.Ended(s.Pos.ArmID) {
		t.Fatal("Ended for the open swing must clear it")
	}
	b3 := mk(hold+2*swingBarMs, 10150)
	bars = append(bars, b3)
	if out = SwingTick(s, bars, cfg, b3.CloseTime+1); len(out) != 0 {
		t.Fatalf("a finished swing must emit nothing: %+v", out)
	}
}

// (a) N2: a swing whose stop/TP already filled (receipt: Ended) must emit
// neither a BE nor a market close hours later — that close is a Flatten of the
// whole instrument and would take another trade with it.
// Mutant: ignore the receipt (Ended a no-op) -> the stale swing emits the
// hold-close at its deadline -> RED.
func TestSwingEndedByStopNeverActsLater(t *testing.T) {
	cfg := DefaultSwingCfg()
	s, bars, entry := swingFilledShort(t, cfg)
	armID := s.Pos.ArmID
	// The stop filled: the trader's reconcile reports the position over.
	if !s.Ended(armID) || s.Pos != nil {
		t.Fatal("Ended must clear Swing.Pos")
	}
	risk := entry.Stop - entry.Price
	// Hours later: a BE-qualifying close, then the hold deadline.
	b1 := mk5m(t, 15, 5, 15, 10145, 10146, entry.Price-risk-1, entry.Price-risk-0.5)
	bars = append(bars, b1)
	if out := SwingTick(s, bars, cfg, b1.OpenTime+60_000); countAction(out, ActionMoveStopBE) != 0 {
		t.Fatalf("a stopped-out swing must not send a BE: %+v", out)
	}
	late := market.Kline{OpenTime: b1.OpenTime + 6*3600_000, Open: 10150, High: 10151, Low: 10149, Close: 10150, CloseTime: b1.OpenTime + 6*3600_000 + 299_999}
	bars = append(bars, late)
	if out := SwingTick(s, bars, cfg, late.CloseTime+1); countAction(out, ActionClosePosition) != 0 {
		t.Fatalf("a stopped-out swing must not send a market close: %+v", out)
	}
}

// (d) P2-3, trade 707 — REAL 2026-10-28 17:10-17:50 CT MNQ 5m bars (data.db
// bars tf=5m, read-only; the 4h EMA34 line 29645.53 is the one the live arms
// 469-475 were stopped from: long stop 29615.5 = line-30, short stop 29675.75 =
// line+30 rounded away). Price came down onto the line from above, two touches
// rejected (arms 469/472 — correct, before any through-close), then the 17:35
// bar touched and CLOSED THROUGH (29633 < 29645.53) while the "not yet
// re-armed" guard (ClearLongAt) was still set — the old code skipped that bar
// without latching Through, so the 17:40 bar read the other side of the line as
// a fresh approach and armed a normal reject SHORT (arm 475 -> position 707,
// -200). The level is invalid for the whole 4h candle [D5.2 p2 @20:41-21:04,
// @20:48, @10:30; D5.2 p3 @21:30]: arm 475 must not exist.
// Mutant: put the guard back in front of the through-close branch -> arm 475
// (PlaceStopEntry swing-1793227200000, short 29625.75) is emitted -> RED.
func TestSwingThroughCloseNotSwallowedReal1028(t *testing.T) {
	const ms5 = 300_000
	mkr := func(open int64, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: open, Open: o, High: h, Low: l, Close: c, CloseTime: open + ms5 - 1}
	}
	bars := []market.Kline{
		// the last real 5m bars of the two previous 4h buckets (09:00 and
		// 13:00 CT) — SwingTick needs two closed buckets to read the line.
		mkr(1793210100000, 29867.25, 29873.75, 29857.0, 29864.0),  // 12:55 CT
		mkr(1793220900000, 29760.00, 29770.00, 29750.00, 29765.0), // 15:55 CT (pad: level only needs to be far from the line)
		mkr(1793225700000, 29730.0, 29730.75, 29684.25, 29704.75), // 17:15
		mkr(1793226000000, 29704.75, 29710.0, 29665.25, 29667.75), // 17:20  prev (above the line)
		mkr(1793226300000, 29668.5, 29688.5, 29635.25, 29673.5),   // 17:25  touch, closes back above  -> arm 469
		mkr(1793226600000, 29673.0, 29675.25, 29642.75, 29651.5),  // 17:30  2nd touch, closes back    -> arm 472
		mkr(1793226900000, 29652.25, 29653.25, 29624.0, 29633.0),  // 17:35  touch, CLOSES THROUGH     -> cancel, Through latched
		mkr(1793227200000, 29632.75, 29644.25, 29625.75, 29637.5), // 17:40  old code: normal reject short (arm 475)
		mkr(1793227500000, 29637.5, 29659.0, 29616.25, 29619.25),  // 17:45
		mkr(1793227800000, 29618.25, 29660.0, 29616.5, 29655.25),  // 17:50
	}
	cfg := DefaultSwingCfg()
	s := &SwingState{
		Line:        29645.53,
		BucketStart: 1793224800000, // 17:00 CT bucket
		EmaCount:    FourHEMA34Min, // warm: the line is the seeded one
	}
	var entries, cancels []Intent
	for i := 2; i <= len(bars); i++ {
		now := bars[i-1].CloseTime + 1
		for _, in := range SwingTick(s, bars[:i], cfg, now) {
			switch in.Action {
			case PlaceStopEntry:
				entries = append(entries, in)
			case CancelArm:
				cancels = append(cancels, in)
			}
		}
	}
	armed := map[string]Intent{}
	for _, in := range entries {
		armed[in.ArmID] = in
	}
	// arms 469 / 472: legitimately emitted BEFORE the through-close.
	a469, ok := armed["swing-1793226300000"]
	if !ok || a469.Side != SideLong || a469.Price != 29688.5 {
		t.Fatalf("the 17:25 reject long (arm 469) must still arm at 29688.5: %+v", entries)
	}
	a472, ok := armed["swing-1793226600000"]
	if !ok || a472.Side != SideLong || a472.Price != 29675.25 {
		t.Fatalf("the 17:30 second-touch long (arm 472) must still arm at 29675.25: %+v", entries)
	}
	// arm 475: the short that came out of the swallowed through-close.
	if in, bad := armed["swing-1793227200000"]; bad {
		t.Fatalf("arm 475 (normal reject SHORT %.2f, stop %.2f) was armed at a level the 17:35 close-through made invalid for the whole 4h candle", in.Price, in.Stop)
	}
	if len(entries) != 2 {
		t.Fatalf("exactly the two pre-through arms are allowed in this window, got %d: %+v", len(entries), entries)
	}
	if len(cancels) == 0 {
		t.Fatal("the 17:35 close through the line must cancel the resting order (R31)")
	}
	if s.FirstTouch == nil || !s.FirstTouch.Through {
		t.Fatalf("Through must be latched by the 17:35 close: %+v", s.FirstTouch)
	}
}

// (d) the same ordering fault, synthetic and mirrored (short side): the guard
// is set by a prior setup, the next touching bar closes THROUGH -> the level is
// invalid, a later opposite-side bar arms nothing.
func TestSwingThroughCloseNotSwallowedByGuardShort(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),      // prev: below the line
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160),  // touch 1, closes back below -> reject (guard set)
		mk5m(t, 15, 5, 10, 10160, 10172, 10156, 10158), // touch 2 (leeway), closes back below, does not fill
		mk5m(t, 15, 5, 15, 10160, 10185, 10160, 10180), // touch, CLOSES THROUGH (above) - window spent, guard still set
		mk5m(t, 15, 5, 20, 10180, 10181, 10165, 10172), // back down onto the line from above: a "fresh" approach
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	var entries int
	for i := 4; i <= len(bars); i++ {
		for _, in := range SwingTick(s, bars[:i], cfg, bars[i-1].OpenTime+60_000) {
			if in.Action == PlaceStopEntry {
				entries++
			}
		}
	}
	if entries != 2 {
		t.Fatalf("only the two pre-through arms are allowed; got %d entries (a third is the swallowed through-close re-arming)", entries)
	}
	if s.FirstTouch == nil || !s.FirstTouch.Through {
		t.Fatalf("Through must be latched by the close through the line: %+v", s.FirstTouch)
	}
}

// (e) doc truth — MaxStopPts is DECLARED and NEVER READ: a clean-reject swing
// with a ~102-pt stop (trades 709/710, the 10-28 19:05 bar, a 72-pt touch
// candle) is armed today, and the 100-pt behaviour waits for mentor Q43. The
// comment on swingRejectIntent used to claim a refusal at MaxStopPts; this pin
// states what the code really does so the comment cannot drift back into a lie.
func TestSwingMaxStopPtsIsDeclaredButNotRead(t *testing.T) {
	cfg := DefaultSwingCfg()
	if cfg.MaxStopPts != 100 {
		t.Fatalf("fixture: MaxStopPts default = %v, want 100", cfg.MaxStopPts)
	}
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10300, 10310, 10290, 10300),    // prev: above the line
		mk5m(t, 15, 5, 5, 10200, 10240.41, 10160, 10230), // 72-pt candle: touches, closes back above -> long reject
	}
	s := &SwingState{}
	out := SwingTick(s, swingTape(t, cur), cfg, cur[1].OpenTime+60_000)
	if len(out) != 1 || out[0].Action != PlaceStopEntry || out[0].Side != SideLong {
		t.Fatalf("a ~100-pt-stop clean reject must still arm (MaxStopPts is not read): %+v", out)
	}
	if d := out[0].Price - out[0].Stop; d < 100 {
		t.Fatalf("fixture: the stop distance %.2f should reach MaxStopPts", d)
	}
}
