package mentor

import (
	"strconv"
	"testing"

	"vl/market"
)

// Release #59 review round 2 (course-swing lens, 2026-11-01) — the swing
// findings, pinned RED-first on the swingTape harness (line 10,168.16 in the
// 05:00 CT 4h candle; the 09:00 candle's line folds the 05:00 close).

// TestRV59LastBarOfCandleReadAgainstItsOwnLine — course-swing P2-a. The tick
// that first sees a 4h candle's LAST 5m bar runs at now = the candle's end
// (m437), i.e. already in the NEXT 4h candle. SwingTick flipped the line and
// wiped the touch window at the top of the tick, then walked that last bar
// against the NEW line: the 08:55 bar below never reaches the 05:00 candle's
// line (10,168.16; high 10,167.50, 0.66 short) but does reach the 09:00
// candle's line (10,166.84), so a short stop entry armed off a bar that never
// touched the line of its own candle, and lived into the next candle. R27 [D5.2
// p3 @12:16–13:18 "9h đó là xong rồi… mình không có lệnh nào hết"]: the 4h close
// ends the instance — a bar is read against the line of the 4h candle it
// belongs to, and nothing arms once that candle has closed.
// Mutant: walk the old candle's bars after the flip (the pre-fix order) → a
// place_stop_entry short swing-<08:55> → RED.
func TestRV59LastBarOfCandleReadAgainstItsOwnLine(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9950),
		mk5m(t, 15, 8, 50, 9950, 9960, 9945, 9950),
		mk5m(t, 15, 8, 55, 10150, 10167.5, 10140, 10145), // 0.66 under the OLD line; touches the NEW one
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	_ = SwingTick(s, bars[:len(bars)-1], cfg, bars[len(bars)-2].OpenTime+5*60_000) // the 08:55 tick: 08:50 is final
	if abs(s.Line-swingLineGolden) > 0.01 {
		t.Fatalf("setup: the 05:00 candle's line must be %.2f, got %.2f", swingLineGolden, s.Line)
	}
	now := bars[len(bars)-1].OpenTime + 5*60_000 // 09:00:00 CT — the tick that first sees 08:55
	out := SwingTick(s, bars, cfg, now)
	if es := m463Entries(out); len(es) != 0 {
		t.Fatalf("the 08:55 bar never touched its own candle's line %.2f; nothing may arm, got %+v", swingLineGolden, es)
	}
	if s.Pending != nil {
		t.Fatalf("no swing order may rest into the 09:00 candle: %+v", s.Pending)
	}
	if n := m463Refused(s, SwingRefusedNearTouch); n != 1 {
		t.Fatalf("08:55 is read against the 05:00 line: a 0.66-pt near miss, NAMED %s once; got %d (%+v)", SwingRefusedNearTouch, n, s.refused)
	}
	if abs(s.Line-10166.84) > 0.01 || s.FirstTouch != nil {
		t.Fatalf("after the walk the line flips to the 09:00 candle's 10,166.84 with a fresh window, got line %.2f touch %+v", s.Line, s.FirstTouch)
	}
}

// TestRV59RejectOnTheClosingBarIsNamedCandleClosed — course-swing P2-a, the
// other half: a REAL reject of the old line on the candle's last 5m bar. The
// order could only be placed at the 4h close, when that candle's instance is
// over (R27) — it is refused and NAMED swing_candle_closed, never armed into
// the next candle with the next candle's expiry.
// Mutant: drop the candleClosed check before the reject arm → a
// place_stop_entry short swing-<08:55> → RED.
func TestRV59RejectOnTheClosingBarIsNamedCandleClosed(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9950),
		mk5m(t, 15, 8, 50, 9950, 9960, 9945, 9950),
		mk5m(t, 15, 8, 55, 10150, 10170, 10140, 10145), // touches 10,168.16, closes back below
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	_ = SwingTick(s, bars[:len(bars)-1], cfg, bars[len(bars)-2].OpenTime+5*60_000)
	out := SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+5*60_000)
	if es := m463Entries(out); len(es) != 0 {
		t.Fatalf("a reject on the 4h candle's closing bar arms nothing (R27), got %+v", es)
	}
	if n := m463Refused(s, SwingRefusedCandleClosed); n != 1 {
		t.Fatalf("the closing-bar reject must be NAMED %s once, got %d (%+v)", SwingRefusedCandleClosed, n, s.refused)
	}
	if s.Pending != nil {
		t.Fatalf("nothing may rest: %+v", s.Pending)
	}
}

// TestRV59InsideBarAfterACancelledInsideBarArms — course-swing P2-b. The
// inside-bar way back has no time limit [D5.2-R25 p1 @20:31–21:57 "xuất hiện
// inside bar, tôi vô lệnh"]. ISBFired latched on the FIRST inside-bar arm and
// stayed latched after that order was CANCELLED unfilled (a later close back
// through the line, R31): every later inside bar of the 4h candle was then
// refused with the misleading "reject shape … only a 5m inside bar may enter"
// text on a bar that IS an inside bar (the 10-28 19:00 CT bar). An inside-bar
// order that dies unfilled does not spend the level.
// Mutant: keep ISBFired latched on the R31 cancel → one entry, refused
// swing_level_invalid → RED.
func TestRV59InsideBarAfterACancelledInsideBarArms(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10210, 10195, 10200),  // prev: above → support
		mk5m(t, 15, 5, 5, 10190, 10192, 10160, 10150),  // touch, closes THROUGH → invalid
		mk5m(t, 15, 5, 10, 10150, 10185, 10148, 10180), // closes back above (candle 1)
		mk5m(t, 15, 5, 15, 10170, 10182, 10160, 10175), // inside bar → buy stop 10,182
		mk5m(t, 15, 5, 20, 10175, 10176, 10140, 10150), // no fill; closes through → cancel
		mk5m(t, 15, 5, 25, 10150, 10192, 10149, 10190), // closes back above (candle 1)
		mk5m(t, 15, 5, 30, 10175, 10188, 10160, 10180), // inside bar of 05:25, closes back
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	es := m463Entries(m463Walk(t, s, bars, 4, cfg))
	if len(es) != 2 {
		t.Fatalf("the cancelled inside bar does not spend the level: want 2 inside-bar entries, got %d %+v (refused %+v)", len(es), es, s.refused)
	}
	second := es[1]
	if second.Side != SideLong || second.Price != 10188 || abs(second.Stop-swingLineGolden) > 0.01 ||
		second.ArmID != "swing-"+strconv.FormatInt(cur[6].OpenTime, 10) {
		t.Fatalf("second inside bar = %+v, want LONG 10,188 stop AT the line %.2f off the 05:30 bar", second, swingLineGolden)
	}
	if n := m463Refused(s, SwingRefusedLevelInvalid); n != 0 {
		t.Fatalf("an inside bar must never be refused as a reject shape, got %d (%+v)", n, s.refused)
	}
}

// TestRV59SpentInsideBarIsNamedForWhatItIs — course-swing P2-b, the naming
// half: once the level's inside-bar order FILLED, the level gave its trade (one
// setup per level, D5.2-R19/R23) — a later inside bar after that position ended
// is refused, but NAMED swing_isb_spent, not swing_level_invalid with the
// "only a 5m inside bar may enter" text.
func TestRV59SpentInsideBarIsNamedForWhatItIs(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10210, 10195, 10200),  // prev: above → support
		mk5m(t, 15, 5, 5, 10190, 10192, 10160, 10150),  // touch, closes THROUGH → invalid
		mk5m(t, 15, 5, 10, 10150, 10185, 10148, 10180), // closes back above
		mk5m(t, 15, 5, 15, 10170, 10182, 10160, 10175), // inside bar → buy stop 10,182
		mk5m(t, 15, 5, 20, 10178, 10186, 10176, 10184), // fills 10,182; low 7.84 above the line (no near touch)
		mk5m(t, 15, 5, 25, 10180, 10200, 10175, 10198), // not an inside bar; no touch
		mk5m(t, 15, 5, 30, 10190, 10199, 10180, 10195), // inside bar of 05:25, closes back
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	es := m463Entries(m463Walk(t, s, bars[:8], 4, cfg)) // through 05:20
	if len(es) != 1 || s.Pos == nil {
		t.Fatalf("setup: the inside bar must arm and fill, entries %+v pos %+v", es, s.Pos)
	}
	s.Pos = nil // the trader reported the leg ended (SwingEnded)
	if es := m463Entries(m463Walk(t, s, bars, 8, cfg)); len(es) != 0 {
		t.Fatalf("the level's inside bar already traded — no second arm, got %+v", es)
	}
	if n := m463Refused(s, SwingRefusedISBSpent); n != 1 {
		t.Fatalf("the later inside bar must be NAMED %s once, got %d (%+v)", SwingRefusedISBSpent, n, s.refused)
	}
	if n := m463Refused(s, SwingRefusedLevelInvalid); n != 0 {
		t.Fatalf("an inside bar must never be refused as a reject shape, got %d (%+v)", n, s.refused)
	}
}

// TestRV59ColdBacklogOfClosedCandlesIsNotWalked — found while regenerating the
// goldens for the P2-a fix: a state whose line was never readable (fewer than 2
// closed 4h buckets — the 09-15 RTH fixture until 13:00) got its FIRST line on a
// later tick and then walked the whole backlog of OLDER 4h candles against it.
// A 08:40 reject was read against the 13:00 line; its entry was dropped as
// stale (S2) but its Pending stayed registered, "filled", and the bot emitted
// break-even intents for a swing it never placed (17 on 09-15). Bars of 4h
// candles that closed before the tick could read them are not walked for
// entries (fill / cancel checks only); the skip is NAMED once
// (swing_candle_closed).
// Mutant: drop the swingBarUnseenCandle classification → Pending registered → RED.
func TestRV59ColdBacklogOfClosedCandlesIsNotWalked(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105), // 05:00 candle: below the coming line
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // a reject shape at ~10,168 — in a CLOSED candle by the time a line exists
		mk5m(t, 15, 9, 0, 10100, 10110, 10095, 10105), // 09:00 candle
	}
	bars := swingTape(t, cur)
	// Cold state: the 05:00 candle was in force with NO readable line.
	s := &SwingState{BucketStart: fourHBucketStart(cur[0].OpenTime, ctime())}
	out := SwingTick(s, bars, cfg, cur[2].OpenTime+60_000)
	if es := m463Entries(out); len(es) != 0 || s.Pending != nil {
		t.Fatalf("a closed candle's backlog must not arm: entries %+v pending %+v", es, s.Pending)
	}
	if n := m463Refused(s, SwingRefusedCandleClosed); n != 1 {
		t.Fatalf("the skipped backlog must be NAMED %s once, got %d (%+v)", SwingRefusedCandleClosed, n, s.refused)
	}
}
