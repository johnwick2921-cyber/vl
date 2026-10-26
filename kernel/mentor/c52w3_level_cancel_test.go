package mentor

import (
	"testing"

	"vl/market"
)

// C52 W3 (course-alignment plan A4): the close-through "cancel" of a resting
// level order.
//
//	Course [Ngày 2.3 Trendline part1.recovered.txt @23:48-24:02]: a later candle
//	that closes through the level -> "Mình cancel. Chẳng làm gì khác hết...
//	Đơn giản" - cancel, nothing else. "Áp dụng trên mọi khung thời gian"
//	[part1.srt @21:24]: the rule runs on the frame you trade.
//
// (a) the cancel SPENDS the touch reference (the DropArm mechanism): the
// through-candle still "touches" a support from above, so without this the PHL
// loop re-placed the SAME buy stop on the next tick.
// (b) the close-through is read on the arm's own exec frame (LevelArm.ExecTFMin),
// not on the last 1m bar.

func countPHLPlace(ints []Intent) (n int) {
	for _, in := range ints {
		if in.Action == PlaceStopEntry && in.Setup == "PHL" {
			n++
		}
	}
	return
}

func hasCancel(ints []Intent, armID string) bool {
	for _, in := range ints {
		if in.Action == CancelArm && in.ArmID == armID {
			return true
		}
	}
	return false
}

// TestC52W3CancelSpendsReference - (a). A resting long PHL at L=29385 is
// cancelled by a later candle closing below it; the NEXT tick must not place a
// stop entry for the same reference. Mutant: drop the RefusedLevelTouch write in
// levelArmCancels -> the next tick re-places the order -> RED.
func TestC52W3CancelSpendsReference(t *testing.T) {
	e, bars, in := m425aLevelFixture(t, KindKeyLevel, "L")
	arm := e.State.LevelArms["L"]
	if arm.ArmID != in.ArmID {
		t.Fatalf("precondition: resting arm %+v vs intent %q", arm, in.ArmID)
	}
	e.State.ISBBox, e.State.ISBBox15m, e.State.ISBBox30m = nil, nil, nil

	// a later candle closes THROUGH the level (below 29385) -> cancel.
	b5 := rthBars(5, 29388, 29389, 29360, 29365)
	t1 := append(append([]market.Kline{}, bars...), b5)
	if !hasCancel(e.Tick(t1, b5.CloseTime+1), arm.ArmID) {
		t.Fatal("precondition: the close-through must cancel the resting order")
	}
	if len(e.State.LevelArms) != 0 {
		t.Fatalf("precondition: arm must be gone: %+v", e.State.LevelArms)
	}
	// the next minutes: price stays under the level, the touch (RefBar) is
	// unchanged and still TouchReject - the old reference is SPENT, nothing may
	// be re-placed off it (base: lvl-3 re-placed at 29393 on the very next tick).
	cur := t1
	for i := 6; i <= 8; i++ {
		b := rthBars(i, 29365, 29368, 29360, 29366)
		cur = append(append([]market.Kline{}, cur...), b)
		if got := countPHLPlace(e.Tick(cur, b.CloseTime+1)); got != 0 {
			t.Fatalf("tick bar %d: the cancelled reference was re-placed (%d PHL stop entries) - a close-through is cancel and NOTHING else [Ngày 2.3 @23:48-24:02]; refusals=%v", i, got, e.State.Refusals)
		}
	}
}

// TestC52W3CancelRecordsReferenceBar - the spend is by REFERENCE: it is keyed to
// the touch's RefBar, so a NEW reference candle at the level may trade again
// (the course cancels the order, not the level).
func TestC52W3CancelRecordsReferenceBar(t *testing.T) {
	e, bars, _ := m425aLevelFixture(t, KindKeyLevel, "L")
	ref := e.State.Touches["L"].RefBar.CloseTime
	if ref == 0 {
		t.Fatal("precondition: no touch reference")
	}
	b5 := rthBars(5, 29388, 29389, 29360, 29365)
	e.Tick(append(append([]market.Kline{}, bars...), b5), b5.CloseTime+1)
	if got := e.State.RefusedLevelTouch["L"]; got != ref {
		t.Fatalf("the close-through cancel must spend the touch reference %d, RefusedLevelTouch[L]=%d", ref, got)
	}
}

// TestC52W3ExpiryCancelDoesNotSpend - the 15:00 expiry is a different cancel
// (owner window, not a close-through): it must NOT spend the reference.
func TestC52W3ExpiryCancelDoesNotSpend(t *testing.T) {
	e, bars, in := m425aLevelFixture(t, KindKeyLevel, "L")
	// age the arm so it is past its own expiry but no candle closed through.
	arm := e.State.LevelArms["L"]
	arm.PlacedAt = auditMs(2026, 9, 14, 9, 0, 0)
	e.State.LevelArms["L"] = arm
	// re-tick the SAME tape: the touch (RefBar) is alive, only the arm expired.
	if e.State.Touches["L"].RefBar.CloseTime == 0 {
		t.Fatal("precondition: the touch must still carry its reference bar")
	}
	ints := e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if !hasCancel(ints, in.ArmID) {
		t.Fatalf("precondition: the aged arm must be expiry-cancelled; intents=%+v", ints)
	}
	if _, spent := e.State.RefusedLevelTouch["L"]; spent {
		t.Fatalf("an EXPIRY cancel must not spend the reference: %+v", e.State.RefusedLevelTouch)
	}
}

// 2m frame tape: f2PHLBars + two more minutes (bars 6, 7 = one more 2m bucket).
func c52w3TwoMinTape(close6, close7 float64) ([]market.Kline, []market.Kline, []market.Kline) {
	base := auditMs(2026, 9, 15, 9, 0, 0)
	bars, _ := f2PHLBars(base)
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := base + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	b6 := mk(6, 100.5, 101, 98.5, close6)
	b7 := mk(7, close6, 102, 98.5, close7)
	t6 := append(append([]market.Kline{}, bars...), b6)
	t7 := append(append([]market.Kline{}, t6...), b7)
	return bars, t6, t7
}

func c52w3TwoMinEval(t *testing.T, bars []market.Kline) (*Evaluator, LevelArm) {
	t.Helper()
	cfg := f2PHLCfg()
	cfg.Exec2mAfter30m = true
	now := bars[len(bars)-1].CloseTime + 1
	e := f2PHLEval(cfg, bars, now)
	var placed bool
	for _, in := range e.Tick(bars, now) {
		if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong {
			placed = true
		}
	}
	arm, ok := e.State.LevelArms["L"]
	if !placed || !ok {
		t.Fatalf("precondition: no 2m PHL arm at L (placed=%v arms=%+v refusals=%v)", placed, e.State.LevelArms, e.State.Refusals)
	}
	return e, arm
}

// TestC52W3ArmCarriesItsFrame - (b) storage: the arm records the exec frame it
// was read on.
func TestC52W3ArmCarriesItsFrame(t *testing.T) {
	bars, _, _ := c52w3TwoMinTape(100.2, 100.4)
	_, arm := c52w3TwoMinEval(t, bars)
	if arm.ExecTFMin != 2 {
		t.Fatalf("a 2m-read level order must record ExecTFMin 2, got %+v", arm)
	}
}

// TestC52W3CloseThroughReadOnTheArmsFrame - (b). A 1m wick-close below L inside
// a FORMING 2m candle that closes back above L must NOT cancel a 2m order; a 2m
// candle that closes below L must. Mutant: levelArmCancels reads the last 1m
// bar again -> the first half goes RED.
func TestC52W3CloseThroughReadOnTheArmsFrame(t *testing.T) {
	// 2m bucket [bar6,bar7): 1m bar 6 closes 99 (< L=100), bar 7 closes 101 -> the
	// 2m candle closes ABOVE L.
	bars, t6, t7 := c52w3TwoMinTape(99, 101)
	e, arm := c52w3TwoMinEval(t, bars)
	if hasCancel(e.Tick(t6, t6[len(t6)-1].CloseTime+1), arm.ArmID) {
		t.Fatal("a 1m close below L inside a still-forming 2m candle cancelled a 2m order - the close-through is read on the frame you trade [Ngày 2.3 @21:24 'mọi khung thời gian']")
	}
	if hasCancel(e.Tick(t7, t7[len(t7)-1].CloseTime+1), arm.ArmID) {
		t.Fatal("the 2m candle closed back ABOVE L - the order must still rest")
	}
	if _, ok := e.State.LevelArms["L"]; !ok {
		t.Fatal("the resting 2m order was dropped without a 2m close-through")
	}

	// mirror: the 2m candle closes below L -> cancel, on the tick that closes it.
	bars2, u6, u7 := c52w3TwoMinTape(99.5, 98.8)
	e2, arm2 := c52w3TwoMinEval(t, bars2)
	if hasCancel(e2.Tick(u6, u6[len(u6)-1].CloseTime+1), arm2.ArmID) {
		t.Fatal("the cancel fired before the 2m candle closed")
	}
	if !hasCancel(e2.Tick(u7, u7[len(u7)-1].CloseTime+1), arm2.ArmID) {
		t.Fatalf("a 2m candle closing below L must cancel the 2m order; arms=%+v", e2.State.LevelArms)
	}
}
