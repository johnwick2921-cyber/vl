package mentor

import (
	"testing"

	"vl/market"
)

// Release #61 review round (fix/m466-checklist-gaps): the reviewers' P1 (boxes,
// breaks, ranges) findings, each pinned from the course example it cites and
// run RED against ea6006a84 before the fix landed.
//
//   - G33 D4.2-R33 [D4.2 p2 @06:16–06:30] "cây nến vừa vậy 30-40 điểm" — the
//     CANDLES are 30-40 pts (the video measures TWO: 33.75 and 34.50). One
//     outlier wick in a chop of teens-sized candles is the normal 50-pt range
//     of D4.2-R31 [@05:03–05:37] ("nến tầm mười mấy điểm"), not a too-small one.
//   - G33 + D4.1-R34 [D4.1 p1 @20:36–21:03] — the guard ENDS on a closed 1m
//     candle that closes outside it. The breakout candle must not re-latch a
//     widened range on the same tick; a new range needs a window built wholly
//     from candles after the break, and a standing range stays the box it
//     latched (it does not slide with the window).
//   - G37 D3.2-R40 [D3.2 p2 @07:50–09:14] + D1.3-R21 [D1.3 p1 @12:31–12:58] —
//     a floor is a box below price that plays SUPPORT: an FTGL, or a box that
//     flipped (Uno). An un-flipped FTGH below price was only wicked through.
//   - D4.2-R9/R11 [D4.2 p1 @07:20–08:15] — a lock set by a PROVISIONAL 15m box
//     ends when that 15m closes as no inside bar (there never was one).

// m61rvSmallRangeBreakout: the D4.2-R33 fixture with its top at 29,000 (the
// window spans 28,960–29,010, 50 pts, candles 33.75 and 34.50) and a 1m
// breakout that CLOSES below it at 28,955 — the post-break window
// 28,953–29,010 (57 pts) would still qualify.
func m61rvSmallRangeBreakout() (pre []market.Kline, exit market.Kline) {
	pre = m61p1SmallRange(29_000, 33.75, 34.50)
	exit = m60p1K(m60p1At(9, 59), 28_962, 28_963, 28_953, 28_955)
	return pre, exit
}

// TestM61rvSmallRangeBreakCloseEndsTheGuard — the setups + course lenses' P2:
// the break close clears the range and NOTHING re-latches on that tick, so the
// break-direction short at 28,954 trades (before the fix: range_small_set 2,
// SmallRange 28,953–29,010, the short refused).
func TestM61rvSmallRangeBreakCloseEndsTheGuard(t *testing.T) {
	pre, exit := m61rvSmallRangeBreakout()
	e := New(DefaultConfig())
	e.advanceSmallRange(pre)
	if r := e.State.SmallRange; r == nil || r.Lo != 28_960 || r.Hi != 29_010 {
		t.Fatalf("fixture: want the too-small range 28,960–29,010, got %+v", r)
	}
	bars := append(append([]market.Kline(nil), pre...), exit)
	e.advanceSmallRange(bars)
	if e.State.SmallRange != nil {
		t.Fatalf("the 1m close at 28,955 (outside) ends the guard; got a re-latched %+v (counters %v)", e.State.SmallRange, e.State.Counters)
	}
	if e.State.Counters["range_small_cleared"] != 1 || e.State.Counters["range_small_set"] != 1 {
		t.Fatalf("one set, one clear: %v", e.State.Counters)
	}
	if out := e.courseRangeFilter([]Intent{m61p1Entry(SideShort, 28_954)}, nil); !m61p1Has(out, SideShort) {
		t.Fatalf("the break-direction short at 28,954 must trade after the break; refusals %v", e.State.Refusals)
	}
	// 14 more quiet candles: every window still holds the break candle (or a
	// candle before it) → no new range.
	for i := 1; i <= 14; i++ {
		bars = append(bars, m60p1K(m60p1At(9, 59)+int64(i)*60_000, 28_958, 28_962, 28_954, 28_958))
		e.advanceSmallRange(bars)
		if e.State.SmallRange != nil {
			t.Fatalf("candle %d after the break: a window that still holds pre-break candles must not latch, got %+v", i, e.State.SmallRange)
		}
	}
	// A fresh small range built wholly AFTER the break (two 30-40 pt candles)
	// latches again.
	fresh := bars
	for i := 0; i < 15; i++ {
		o, h, l, c := 28_958.0, 28_962.0, 28_954.0, 28_958.0
		switch i {
		case 4:
			o, h, l, c = 28_960, 28_985, 28_985-33.75, 28_980
		case 9:
			o, h, l, c = 28_975, 28_986, 28_986-34.50, 28_960
		}
		fresh = append(fresh, m60p1K(m60p1At(10, 14)+int64(i)*60_000, o, h, l, c))
	}
	e.advanceSmallRange(fresh)
	if r := e.State.SmallRange; r == nil || e.State.Counters["range_small_set"] != 2 {
		t.Fatalf("a too-small range built wholly after the break latches: %+v counters %v", r, e.State.Counters)
	}
}

// TestM61rvSmallRangeStaysTheLatchedBox — the mtf-loss lens' P3: a standing
// range is the box it latched. When the oldest candle (the 28,960 low) leaves
// the 15-candle window the range does NOT shrink to the window, so a close at
// 28,965 is still inside it (the sliding read would have cleared on it).
func TestM61rvSmallRangeStaysTheLatchedBox(t *testing.T) {
	bars := m61p1SmallRange(29_021.50, 33.75, 34.50)
	e := New(DefaultConfig())
	e.advanceSmallRange(bars)
	r0 := e.State.SmallRange
	if r0 == nil {
		t.Fatal("fixture: the D4.2-R33 window latches")
	}
	bars = append(bars, m60p1K(m60p1At(9, 59), 28_990, 28_996, 28_986, 28_992))
	e.advanceSmallRange(bars)
	if r := e.State.SmallRange; r == nil || *r != *r0 {
		t.Fatalf("a candle inside keeps the latched box %+v, got %+v", *r0, r)
	}
	bars = append(bars, m60p1K(m60p1At(10, 0), 28_975, 28_978, 28_963, 28_965))
	e.advanceSmallRange(bars)
	if r := e.State.SmallRange; r == nil || *r != *r0 {
		t.Fatalf("a close at 28,965 is inside the latched 28,960–29,021.50: the guard stands, got %+v", r)
	}
	if e.State.Counters["range_small_set"] != 1 || e.State.Counters["range_small_cleared"] != 0 {
		t.Fatalf("no churn: %v", e.State.Counters)
	}
}

// m61rvTape1021 is the real 2026-10-21 18:01–18:15 CT tape (MNQ 1m, OHLC as
// recorded): a 46.5-pt chop whose candles are 5.5–18.25 pts except ONE
// 45.25-pt wick at 18:10.
func m61rvTape1021() []market.Kline {
	rows := [][4]float64{
		{29_849.50, 29_851.00, 29_833.25, 29_843.50},
		{29_843.25, 29_844.25, 29_826.00, 29_829.25},
		{29_829.00, 29_831.75, 29_817.50, 29_826.50},
		{29_826.00, 29_826.00, 29_820.50, 29_823.50},
		{29_824.50, 29_828.25, 29_820.25, 29_822.50},
		{29_822.00, 29_822.50, 29_808.75, 29_818.00},
		{29_819.00, 29_820.00, 29_809.50, 29_812.00},
		{29_812.50, 29_822.50, 29_806.25, 29_821.50},
		{29_821.50, 29_834.00, 29_821.50, 29_831.25},
		{29_831.00, 29_852.75, 29_807.50, 29_827.50},
		{29_827.50, 29_837.50, 29_823.00, 29_835.25},
		{29_835.50, 29_837.00, 29_828.25, 29_830.50},
		{29_830.00, 29_833.75, 29_826.75, 29_833.00},
		{29_832.00, 29_833.50, 29_827.50, 29_828.75},
		{29_828.50, 29_832.50, 29_825.75, 29_827.50},
	}
	var bars []market.Kline
	for i, r := range rows {
		bars = append(bars, m60p1K(m60p1At(18, 1)+int64(i)*60_000, r[0], r[1], r[2], r[3]))
	}
	return bars
}

// TestM61rvSmallRangeOneWickDoesNotArm — the mtf-loss lens' P2: on 10-21 the
// 18:15 ISB short 29,824.25 (stop 29,835, target 29,787 — reached 18:21) was
// refused range_too_small_big_candles because of ONE 45.25-pt wick. D4.2-R33
// is about the candles being 30-40 pts; R31 calls teens-sized candles in a
// 50-pt range normal. Two candles >= 30 pts (the video's 33.75 and 34.50) arm
// the guard; one does not.
func TestM61rvSmallRangeOneWickDoesNotArm(t *testing.T) {
	bars := m61rvTape1021()
	if _, _, ok := smallRangeWindow(bars); ok {
		t.Fatal("10-21 18:01–18:15: one 45.25-pt wick among 5–18-pt candles is not a too-small range")
	}
	e := New(DefaultConfig())
	e.advanceSmallRange(bars)
	if e.State.SmallRange != nil {
		t.Fatalf("no range latched, got %+v", e.State.SmallRange)
	}
	if out := e.courseRangeFilter([]Intent{m61p1Entry(SideShort, 29_824.25)}, nil); !m61p1Has(out, SideShort) {
		t.Fatalf("the 18:15 ISB short at 29,824.25 trades; refusals %v", e.State.Refusals)
	}
	// The same window with a SECOND 30-pt candle is the too-small range.
	bars[3] = m60p1K(bars[3].OpenTime, 29_826, 29_840, 29_809, 29_823.5)
	if _, _, ok := smallRangeWindow(bars); !ok {
		t.Fatal("two candles >= 30 pts inside a <= 61.50-pt span is the too-small range")
	}
}

// TestM61rvMidRangeFloorIsSupport — the course lens' P3 on G37: two UN-flipped
// FTGH boxes, one below and one above price, are not a floor and a ceiling —
// the lower FTGH was only wicked through (no body escape), so it never became
// support (D1.3-R21 needs the break). Pre-#61 false; the side-of-price read
// said true. The flipped cases stay as TestM61MidRangeUsesFlippedRole pins.
func TestM61rvMidRangeFloorIsSupport(t *testing.T) {
	twoFTGH := []Box{
		{Kind: FTGH, Top: 29_010, Bottom: 29_000, Key: "ftgh:29010:29000"},
		{Kind: FTGH, Top: 29_110, Bottom: 29_100, Key: "ftgh:29110:29100"},
	}
	if midRangeBoxed(twoFTGH, 29_050) {
		t.Fatal("an un-flipped FTGH below price is no floor: price 29,050 is not between a floor and a ceiling")
	}
	twoFTGL := []Box{
		{Kind: FTGL, Top: 29_010, Bottom: 29_000, Key: "ftgl:29010:29000"},
		{Kind: FTGL, Top: 29_110, Bottom: 29_100, Key: "ftgl:29110:29100"},
	}
	if midRangeBoxed(twoFTGL, 29_050) {
		t.Fatal("an un-flipped FTGL above price is no ceiling")
	}
	// The lower FTGH escaped UP (flipped): now support → mid-range.
	twoFTGH[0].Flipped = true
	if !midRangeBoxed(twoFTGH, 29_050) {
		t.Fatal("a flipped FTGH below price is the floor")
	}
}

// TestM61rvFormingBreakLockReleasedWhenNoInsideBar — the mtf-loss lens' P3: a
// lock set by the PROVISIONAL 15m box's escape rests on an inside bar that is
// only real when its 15m closes as one (D4.2-R9/R11). It closes as NO inside
// bar → the lock is released and counted; it closes AS one → the lock stays,
// now a real isb_15m lock.
func TestM61rvFormingBreakLockReleasedWhenNoInsideBar(t *testing.T) {
	mother := market.Kline{OpenTime: m60p1At(9, 30), CloseTime: m60p1At(9, 45) - 1, Open: 100, High: 105, Low: 99, Close: 104}
	run := func(closed15 market.Kline, close1m float64) *Evaluator {
		e := New(DefaultConfig())
		prov := &ISBBox{High: 112, Low: 98, Dir: SideLong, AtTime: m60p1At(9, 45), Provisional: true}
		e.State.ISBBox15m = prov
		e.State.LastForming15mAt = prov.AtTime
		cur := m60p1K(m60p1At(9, 55), 113, 116, 113, 115)
		ev, ok := e.advance15mBox(nil, []market.Kline{mother}, cur)
		if !ok || ev.Src != "isb_15m_forming" {
			t.Fatalf("fixture: the provisional box escapes long, ev %+v ok %v", ev, ok)
		}
		e.advanceBreakLock(cur, cur.OpenTime+60_000, []breakEvent{ev})
		if l := e.State.BreakLock; l == nil || l.Side != SideLong || l.Src != "isb_15m_forming" {
			t.Fatalf("fixture: a long isb_15m_forming lock, got %+v", l)
		}
		cur = m60p1K(m60p1At(10, 0), close1m, close1m+1, close1m-1, close1m)
		e.advance15mBox(nil, []market.Kline{mother, closed15}, cur)
		return e
	}
	// The 15m closes as NO inside bar (high 120 above the mother's 105).
	e := run(market.Kline{OpenTime: m60p1At(9, 45), CloseTime: m60p1At(10, 0) - 1, Open: 104, High: 120, Low: 95, Close: 118}, 118)
	if e.State.BreakLock != nil || e.State.Counters["break_lock_forming_released"] != 1 {
		t.Fatalf("no 15m inside bar → the forming lock is released and counted; lock %+v counters %v", e.State.BreakLock, e.State.Counters)
	}
	// The 15m closes AS an inside bar → the lock now rests on a real 15m ISB.
	e = run(market.Kline{OpenTime: m60p1At(9, 45), CloseTime: m60p1At(10, 0) - 1, Open: 101, High: 104, Low: 100, Close: 103}, 118)
	if l := e.State.BreakLock; l == nil || l.Src != "isb_15m" || l.Bucket != 0 || e.State.Counters["break_lock_forming_released"] != 0 {
		t.Fatalf("a confirmed 15m inside bar keeps the lock as a real isb_15m lock; lock %+v counters %v", l, e.State.Counters)
	}
}
