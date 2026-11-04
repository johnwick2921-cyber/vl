package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// Release #61 review round 2 (fix/m466-checklist-gaps): the second review's
// P2s and the cheap P3s, each RED against 58cd96e6a before the fix landed.
//
//   - setups lens P2 — a provisional (isb_15m_forming) break lock replaced a
//     standing same-side FTGH/ISB lock; when that 15m closed as no inside bar
//     the release wiped the lock outright, although the earlier break never
//     failed (D4.1-R30 [D4.1 p1 @17:43–18:29] — only a close back through the
//     broken edge fails a break; D4.2-R11 [D4.2 p1 @07:20–08:15] — the
//     provisional box was never a box).
//   - mtf-loss lens P2 — G33 latched on the breakout candle itself: a chop
//     with one 30-pt candle plus a >= 30-pt candle that CLOSES outside the
//     chop is a break (D4.1-R34 [D4.1 p1 @20:36–21:03]), not a too-small range
//     (D4.2-R33 [D4.2 p2 @06:16–06:30] is churn inside the range).
//   - mtf-loss lens P2/P3 — a box return met while the 15m/5m conflict stood
//     (D4.2-03 [D4.2 p1 @13:59–14:53]) was skipped with no counter and NOT
//     consumed, then walked as a fresh entry once the conflict ended: the
//     10-21 00:42 BOX short 29852.75 of the #60 replay was the 00:39 return,
//     emitted 3 minutes late above the 29835.75 market.
//   - course lens P3 — G43 read an un-flipped FTGL hit from below as a
//     "reject FTGH" (D2.2-R71 [D2.2 p3 @14:28–15:58] names the FTGH reject).

// m61rv2FormingLockOver builds a lock `under` and then a provisional long 15m
// box 98–112 (forming bucket 09:45) that escapes on the 09:55 1m close 115,
// setting an isb_15m_forming lock over it. It returns the evaluator and the
// mother 15m candle.
func m61rv2FormingLockOver(t *testing.T, under breakEvent, underClose float64) (*Evaluator, market.Kline) {
	t.Helper()
	e := New(DefaultConfig())
	c0 := m60p1K(m60p1At(9, 50), underClose-1, underClose+1, underClose-2, underClose)
	e.advanceBreakLock(c0, c0.OpenTime+60_000, []breakEvent{under})
	if l := e.State.BreakLock; l == nil || l.Src != under.Src {
		t.Fatalf("fixture: want the %s lock, got %+v", under.Src, l)
	}
	mother := market.Kline{OpenTime: m60p1At(9, 30), CloseTime: m60p1At(9, 45) - 1, Open: 100, High: 105, Low: 99, Close: 104}
	prov := &ISBBox{High: 112, Low: 98, Dir: SideLong, AtTime: m60p1At(9, 45), Provisional: true}
	e.State.ISBBox15m = prov
	e.State.LastForming15mAt = prov.AtTime
	cur := m60p1K(m60p1At(9, 55), 113, 116, 113, 115)
	ev, ok := e.advance15mBox(nil, []market.Kline{mother}, cur)
	if !ok || ev.Src != "isb_15m_forming" {
		t.Fatalf("fixture: the provisional box escapes long, ev %+v ok %v", ev, ok)
	}
	e.advanceBreakLock(cur, cur.OpenTime+60_000, []breakEvent{ev})
	if l := e.State.BreakLock; l == nil || l.Src != "isb_15m_forming" {
		t.Fatalf("fixture: want the isb_15m_forming lock on top, got %+v", l)
	}
	return e, mother
}

// noISB15 is the 09:45 15m candle closing as NO inside bar (high 120 above the
// mother's 105).
var m61rv2NoISB15 = market.Kline{OpenTime: m60p1At(9, 45), CloseTime: m60p1At(10, 0) - 1, Open: 104, High: 120, Low: 95, Close: 118}

// TestM61rv2FormingReleaseRestoresTheLockItReplaced — the setups lens' P2
// repro: an FTGH long break (box 100–108, close 111) stands; a provisional 15m
// box breaks long on top of it; the 15m closes as no inside bar while price is
// still 111 > 108. The FTGH break never failed, so its lock comes back
// (before: BreakLock nil, and the counter-side entries traded again).
func TestM61rv2FormingReleaseRestoresTheLockItReplaced(t *testing.T) {
	ftgh := breakEvent{Side: SideLong, Lo: 100, Hi: 108, Src: "ftgh"}
	e, mother := m61rv2FormingLockOver(t, ftgh, 111)
	cur := m60p1K(m60p1At(10, 0), 111, 112, 110, 111)
	e.advance15mBox(nil, []market.Kline{mother, m61rv2NoISB15}, cur)
	e.advanceBreakLock(cur, cur.OpenTime+60_000, nil)
	l := e.State.BreakLock
	if l == nil || l.Src != "ftgh" || l.Side != SideLong || l.Lo != 100 || l.Hi != 108 || l.Bucket != 0 {
		t.Fatalf("the FTGH lock whose break never failed must come back; got %+v (counters %v)", l, e.State.Counters)
	}
	if e.State.Counters["break_lock_forming_released"] != 1 || e.State.Counters["break_lock_restored"] != 1 {
		t.Fatalf("released 1, restored 1: %v", e.State.Counters)
	}
	// It still refuses the counter side.
	if out := e.courseRangeFilter([]Intent{m61p1Entry(SideShort, 109)}, nil); m61p1Has(out, SideShort) {
		t.Fatalf("the restored long lock refuses a short; refusals %v", e.State.Refusals)
	}
}

// TestM61rv2FormingLockFailureHandsBackTheEarlierLock — the provisional break
// fails (a close back inside 98–112) while the FTGH break (100–108) still
// holds (close 110 > 108): the FTGH lock is what stands.
func TestM61rv2FormingLockFailureHandsBackTheEarlierLock(t *testing.T) {
	ftgh := breakEvent{Side: SideLong, Lo: 100, Hi: 108, Src: "ftgh"}
	e, _ := m61rv2FormingLockOver(t, ftgh, 111)
	cur := m60p1K(m60p1At(9, 56), 113, 113, 109, 110)
	e.advanceBreakLock(cur, cur.OpenTime+60_000, nil)
	if l := e.State.BreakLock; l == nil || l.Src != "ftgh" || l.Hi != 108 {
		t.Fatalf("the failed provisional break hands back the standing FTGH lock; got %+v (counters %v)", l, e.State.Counters)
	}
}

// TestM61rv2BuriedLockFailsOnItsOwnEdge — the lock under a provisional one is
// failed by its own edge: an FTGH 110–113 long break (close 114), then the
// provisional 98–112 break; a close at 112.5 fails the FTGH break (<= 113)
// but not the provisional one (> 112). When the 15m closes as no inside bar
// nothing comes back.
func TestM61rv2BuriedLockFailsOnItsOwnEdge(t *testing.T) {
	ftgh := breakEvent{Side: SideLong, Lo: 110, Hi: 113, Src: "ftgh"}
	e, mother := m61rv2FormingLockOver(t, ftgh, 114)
	cur := m60p1K(m60p1At(9, 56), 114, 114.5, 112.25, 112.5)
	e.advanceBreakLock(cur, cur.OpenTime+60_000, nil)
	if l := e.State.BreakLock; l == nil || l.Src != "isb_15m_forming" || l.Under != nil {
		t.Fatalf("the provisional lock stands with nothing under it; got %+v", l)
	}
	cur = m60p1K(m60p1At(10, 0), 112.5, 113, 112.25, 112.75)
	e.advance15mBox(nil, []market.Kline{mother, m61rv2NoISB15}, cur)
	if e.State.BreakLock != nil || e.State.Counters["break_lock_restored"] != 0 {
		t.Fatalf("a failed buried break is not restored; lock %+v counters %v", e.State.BreakLock, e.State.Counters)
	}
}

// TestM61rv2ConfirmedFormingLockDropsWhatItReplaced — the 15m closes AS an
// inside bar: the newest confirmed break wins (as before) and nothing is kept
// underneath it.
func TestM61rv2ConfirmedFormingLockDropsWhatItReplaced(t *testing.T) {
	ftgh := breakEvent{Side: SideLong, Lo: 100, Hi: 108, Src: "ftgh"}
	e, mother := m61rv2FormingLockOver(t, ftgh, 111)
	isb15 := market.Kline{OpenTime: m60p1At(9, 45), CloseTime: m60p1At(10, 0) - 1, Open: 101, High: 104, Low: 100, Close: 103}
	cur := m60p1K(m60p1At(10, 0), 118, 119, 117, 118)
	e.advance15mBox(nil, []market.Kline{mother, isb15}, cur)
	if l := e.State.BreakLock; l == nil || l.Src != "isb_15m" || l.Under != nil || l.Bucket != 0 {
		t.Fatalf("a confirmed 15m ISB lock replaces outright; got %+v", l)
	}
}

// m61rv2Chop: 14 closed 1m candles of 20 pts (28,990–29,010) with ONE 30-pt
// candle (28,980–29,010) among them, starting at 09:30.
func m61rv2Chop() []market.Kline {
	var bars []market.Kline
	for i := 0; i < 14; i++ {
		o, h, l, c := 29_000.0, 29_010.0, 28_990.0, 29_002.0
		if i == 6 {
			l = 28_980
		}
		bars = append(bars, m60p1K(m60p1At(9, 30)+int64(i)*60_000, o, h, l, c))
	}
	return bars
}

// TestM61rv2SmallRangeNeverLatchesOnTheBreakout — the mtf-loss lens' P2 with
// the two-candle rule: chop + one 30-pt candle + a 34-pt breakout candle that
// closes ABOVE the chop (29,032 > 29,010). The 15-candle window spans 54 pts
// with two candles >= 30, but its newest candle is the break — no range, and
// none in the 14 candles after it either (each window still holds the break
// candle), so the pullback long at 29,015 trades. Before: SmallRange
// 28,980–29,034 latched on the breakout and refused it.
func TestM61rv2SmallRangeNeverLatchesOnTheBreakout(t *testing.T) {
	bars := m61rv2Chop()
	e := New(smallRangeOnConfig())
	e.advanceSmallRange(bars)
	if e.State.SmallRange != nil {
		t.Fatalf("fixture: 14 candles are not a window yet, got %+v", e.State.SmallRange)
	}
	brk := m60p1K(m60p1At(9, 44), 29_003, 29_034, 29_000, 29_032)
	bars = append(bars, brk)
	e.advanceSmallRange(bars)
	if e.State.SmallRange != nil {
		t.Fatalf("the breakout candle latched a too-small range %+v (counters %v)", e.State.SmallRange, e.State.Counters)
	}
	for i := 0; i < 14; i++ {
		bars = append(bars, m60p1K(m60p1At(9, 45)+int64(i)*60_000, 29_020, 29_030, 29_014, 29_022))
		e.advanceSmallRange(bars)
		if e.State.SmallRange != nil {
			t.Fatalf("pullback candle %d: a window holding the breakout latched %+v", i, e.State.SmallRange)
		}
	}
	if out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 29_015)}, nil); !m61p1Has(out, SideLong) {
		t.Fatalf("the pullback long after the break must trade; refusals %v", e.State.Refusals)
	}
}

// TestM61rv2BoxReturnInConflictIsConsumed — a box return met while the 15m/5m
// conflict stands is refused box_tf_conflict ONCE and consumed; when the
// conflict lifts it is NOT walked as a fresh entry. Fixture: the release #30
// day-gate tape (FTGL 95–96, the ONE reject return at bar 7), with a standing
// conflict (5m box short vs 15m box long, both above the close so neither
// escapes nor bans the return).
func TestM61rv2BoxReturnInConflictIsConsumed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 103, 95, 96),
		mk(3, 96.5, 97, 96, 96.8),
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),
		mk(6, 96.2, 97, 95.8, 96.8),
		mk(7, 96.5, 97.1, 95.9, 97),
	}
	boxKey := "ftgl:96.00:95.00"
	now := bars[7].OpenTime + 59_999
	newE := func() *Evaluator {
		e := New(cfg)
		e.seeded = true
		e.State.Seed1mWatermark = maxInt64
		e.State.Seed1HWatermark = maxInt64
		e.State.SeedLevels = []Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}
		e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 80, LastBucket: t0 + 5*60_000}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
		return e
	}
	boxEntries := func(out []Intent) int {
		n := 0
		for _, in := range out {
			if strings.HasPrefix(in.Reason, "box edge return") {
				n++
			}
		}
		return n
	}
	if n := boxEntries(newE().Tick(bars[:8], now)); n != 1 {
		t.Fatalf("control: no conflict → the return emits 1 box entry, got %d", n)
	}

	e := newE()
	e.State.ISBBox = &ISBBox{High: 110, Low: 97, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 97, Dir: SideLong, AtTime: bars[0].OpenTime}
	if n := boxEntries(e.Tick(bars[:8], now)); n != 0 {
		t.Fatalf("the conflict refuses the return, got %d box entries", n)
	}
	if got := e.State.Refusals["box_tf_conflict"]; got != 1 {
		t.Fatalf("box_tf_conflict = %d, want 1 (named, counted) — refusals %v", got, e.State.Refusals)
	}
	if got := e.State.BoxRefs[boxKey]; got != bars[7].OpenTime {
		t.Fatalf("the refused return is consumed: BoxRefs[%s] = %d, want %d", boxKey, got, bars[7].OpenTime)
	}
	// The conflict lifts: the stale return must not be walked as fresh.
	e.State.ISBBox, e.State.ISBBox15m, e.State.TFConflictLatched = nil, nil, false
	if n := boxEntries(e.Tick(bars[:8], now)); n != 0 {
		t.Fatalf("the conflict-refused return was traded after the conflict lifted")
	}
}

// TestM61rv2UnflippedFTGLFromBelowIsNoReject — the course lens' P3: G43's
// side-of-price read made an un-flipped FTGL, hit from below and closed under,
// a short-pointing rejection. It is not an FTGH and never played the ceiling
// role (the G37 test midRangeBoxed uses). A native FTGH (k == FTGH) and a
// FLIPPED FTGH hit from below still reject short.
func TestM61rv2UnflippedFTGLFromBelowIsNoReject(t *testing.T) {
	hit := m60p1K(m60p1At(10, 0), 28_995, 29_002, 28_990, 28_996) // from below, wick into 29,000, close under
	run := func(b Box) *MidRangeReject {
		e := New(DefaultConfig())
		e.advanceMidRangeReject(hit, []Box{b})
		return e.State.MidRangeReject
	}
	if r := run(Box{Kind: FTGL, Top: 29_010, Bottom: 29_000, Key: "ftgl"}); r != nil {
		t.Fatalf("an un-flipped FTGL hit from below is no FTGH reject; got %+v", r)
	}
	if r := run(Box{Kind: FTGH, Top: 29_010, Bottom: 29_000, Key: "ftgh"}); r == nil || r.Side != SideShort {
		t.Fatalf("an FTGH reject points short; got %+v", r)
	}
	if r := run(Box{Kind: FTGH, Top: 29_010, Bottom: 29_000, Key: "ftgh-f", Flipped: true}); r == nil || r.Side != SideShort {
		t.Fatalf("a flipped FTGH (played the ceiling) hit from below rejects short; got %+v", r)
	}
	// Mirror: an un-flipped FTGH hit from above is no floor reject.
	hitUp := m60p1K(m60p1At(10, 0), 29_015, 29_020, 29_008, 29_014)
	e := New(DefaultConfig())
	e.advanceMidRangeReject(hitUp, []Box{{Kind: FTGH, Top: 29_010, Bottom: 29_000, Key: "ftgh"}})
	if r := e.State.MidRangeReject; r != nil {
		t.Fatalf("an un-flipped FTGH hit from above is no FTGL reject; got %+v", r)
	}
}
