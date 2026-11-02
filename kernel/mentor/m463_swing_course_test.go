package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// m463 — release #59, swing-4h exactly as D5.2 (owner order 2026-11-01 "full of
// it"). Rule ids are the FINAL note's ([~/mm-course/notes-video/final/Ngày 5.2
// Cách trade khung 4H chuyển xuống 5M.md], R1-R38). Every test drives SwingTick
// or runSwing — the production call sites (Evaluator.Tick → runSwing →
// SwingTick). The synthetic tape is swingTape (line 10168.16).

func m463Entries(out []Intent) []Intent {
	var es []Intent
	for _, in := range out {
		if in.Action == PlaceStopEntry {
			es = append(es, in)
		}
	}
	return es
}

func m463Refused(s *SwingState, name string) int {
	n := 0
	for _, r := range s.refused {
		if r.Name == name {
			n++
		}
	}
	return n
}

// walk ticks SwingTick bar by bar (live cadence) from the first current bar.
func m463Walk(t *testing.T, s *SwingState, bars []market.Kline, from int, cfg SwingCfg) []Intent {
	t.Helper()
	var out []Intent
	for i := from; i <= len(bars); i++ {
		out = append(out, SwingTick(s, bars[:i], cfg, bars[i-1].OpenTime+60_000)...)
	}
	return out
}

// ── D5.2-R12 / R13 — the touch must be LITERAL ─────────────────────────────

// TestM463R12NearTouchIsNoTouch — trade 707's shape on a FRESH level: a 5m
// candle whose high stops 1.28 pts under the resistance line and closes back
// below is NOT a touch: "Không, phải đụng nha, không được gần đụng. Bắt buộc là
// phải đụng" [D5.2 p2 @09:12-09:27, @18:27-18:48]. The old 5-pt LineOffsetPts
// allowance armed arm 475 (position 707, -$200) off exactly this candle. The
// near miss is NAMED (swing_near_touch), never silent.
func TestM463R12NearTouchIsNoTouch(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105),                              // prev: below the line
		mk5m(t, 15, 5, 5, 10150, swingLineGolden-1.28, 10140, 10150),               // high 1.28 short, closes back below
		mk5m(t, 15, 5, 10, 10150, swingLineGolden-0.01, 10145, 10148),              // high a hair short — still no touch
		mk5m(t, 15, 5, 15, 10148, swingLineGolden-4.99, swingLineGolden-20, 10149), // inside the old 5-pt band
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := m463Walk(t, s, bars, 4, cfg)
	if es := m463Entries(out); len(es) != 0 {
		t.Fatalf("a near touch armed a swing (the removed 5-pt allowance): %+v", es)
	}
	if n := m463Refused(s, SwingRefusedNearTouch); n != 3 {
		t.Fatalf("each near-touch candle must be NAMED swing_near_touch: got %d (%+v)", n, s.refused)
	}
}

// TestM463R13WickIntoTheLineCounts — "Đụng ngay cái nến đỏ, rút râu… Đụng ít
// đụng nhiều gì vẫn phải đụng" [D5.2 p2 @13:56-14:05]: a wick that reaches the
// line by a tick is a touch; the close decides reject vs through (R21/R22).
func TestM463R13WickIntoTheLineCounts(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105),
		mk5m(t, 15, 5, 5, 10150, swingLineGolden+0.25, 10140, 10150), // wick pokes the line, closes below
	}
	s := &SwingState{}
	out := SwingTick(s, swingTape(t, cur), cfg, cur[1].OpenTime+60_000)
	es := m463Entries(out)
	if len(es) != 1 || es[0].Side != SideShort || es[0].Price != 10140 {
		t.Fatalf("a wick into the line must arm the sell stop at the candle low 10140: %+v", out)
	}
}

// ── D5.2-R19 / R20 — only the FIRST touching candle (2 candles of leeway) ──

// TestM463R19ThirdTouchNeverArms — "Cây nến đầu tiên chạm... lúc nào cũng phải
// lấy cây nến đầu tiên. Đừng bao giờ lấy cây nến thứ 2" [D5.2 p1 @13:23-13:40],
// with "em sẽ du di là 2 cây nến" [p1 @15:37-15:51]. After the two-candle
// window, a third candle wicks 38 pts through the support and closes back above
// IN THE SAME 4h candle: the old "left the line by the stop distance → re-arm"
// guard (ClearLongAt = line − 30, cleared by that very wick) armed a third
// reject — the 709/710 re-arm mechanism. The course never does. Named
// swing_not_first_touch.
func TestM463R19ThirdTouchNeverArms(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10210, 10195, 10205),  // prev above (support)
		mk5m(t, 15, 5, 5, 10180, 10185, 10160, 10180),  // touch 1, reject → buy stop 10185
		mk5m(t, 15, 5, 10, 10180, 10183, 10165, 10178), // touch 2 (leeway), reject → re-base 10183
		mk5m(t, 15, 5, 15, 10178, 10182, 10175, 10180), // no touch, no fill
		mk5m(t, 15, 5, 20, 10180, 10182, 10130, 10175), // touch 3: wick to line−38, closes back above → NOT armed
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := m463Walk(t, s, bars, 4, cfg)
	es := m463Entries(out)
	if len(es) != 2 {
		t.Fatalf("only the two leeway candles may arm; got %d entries: %+v", len(es), es)
	}
	if n := m463Refused(s, SwingRefusedNotFirstTouch); n != 1 {
		t.Fatalf("the third touching candle must be NAMED swing_not_first_touch once: got %d (%+v)", n, s.refused)
	}
}

// TestM463R27NewCandleNewFirstTouch — "Nếu bây giờ nó ra cây nến mới thì chị
// phải base on cây nến mới luôn… flip liên tục" [D5.2 p3 @12:16-13:18]: the
// first-touch window belongs to ONE 4h candle; the next 4h candle has its own.
func TestM463R27NewCandleNewFirstTouch(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105),
		mk5m(t, 15, 5, 5, 10150, 10175, 10140, 10150),  // touch 1
		mk5m(t, 15, 5, 10, 10150, 10172, 10145, 10148), // touch 2
		mk5m(t, 15, 5, 15, 10150, 10175, 10141, 10150), // touch 3 → refused (same 4h candle)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	_ = m463Walk(t, s, bars, 4, cfg)
	if m463Refused(s, SwingRefusedNotFirstTouch) != 1 {
		t.Fatalf("setup: touch 3 must be refused in the 05:00 candle: %+v", s.refused)
	}
	// 09:00 CT — a new 4h candle: the window resets (the line re-bases).
	nb := []market.Kline{
		mk5m(t, 15, 9, 0, 10060, 10070, 10055, 10065),
		mk5m(t, 15, 9, 5, 10100, 10300, 10090, 10120), // a fresh first touch of the NEW line
	}
	bars = append(bars, nb...)
	line, _, _ := swingLine(bars, nb[1].OpenTime+60_000, cfg, ctime())
	nb[1] = mk5m(t, 15, 9, 5, line-20, line+5, line-25, line-10)
	bars[len(bars)-1] = nb[1]
	out := m463Walk(t, s, bars, len(bars)-1, cfg)
	if es := m463Entries(out); len(es) != 1 {
		t.Fatalf("the new 4h candle's first touch must arm: %+v (line %.2f)", out, line)
	}
}

// ── D5.2-R22 / R23 / R24 / R25 — once closed through: ONLY the inside bar ───

// TestM463R22PlainRejectImpossibleAfterThrough — trades 709/710's shape
// (release #51 armed a plain reject on a line already closed through in the
// same 4h candle). Once the first touching candle closed through, "level đó
// invalid, không chạm vô nữa, không đụng vô nữa" [D5.2 p3 @19:15-21:56];
// "touch + close above + close back below is NOT a setup by itself" [p1
// @16:24-19:12]. Every later reject-shaped touch — from EITHER side, however
// far price travelled in between — is refused and NAMED swing_level_invalid.
func TestM463R22PlainRejectImpossibleAfterThrough(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105),  // prev below
		mk5m(t, 15, 5, 5, 10150, 10220, 10145, 10210),  // first touch CLOSES THROUGH → invalid
		mk5m(t, 15, 5, 10, 10210, 10300, 10205, 10290), // runs 120 above
		mk5m(t, 15, 5, 15, 10290, 10295, 10160, 10190), // back down: wick into the line, closes above (support-reject shape)
		mk5m(t, 15, 5, 20, 10190, 10195, 10060, 10070), // through again, below
		mk5m(t, 15, 5, 25, 10070, 10075, 9990, 10000),  // 170 below
		mk5m(t, 15, 5, 30, 10000, 10175, 9995, 10150),  // resistance-reject shape (green, closes back below)
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := m463Walk(t, s, bars, 4, cfg)
	for _, in := range m463Entries(out) {
		if !strings.Contains(in.Reason, "inside bar") {
			t.Fatalf("a plain reject was armed on a level closed through in this 4h candle: %+v", in)
		}
	}
	if n := m463Refused(s, SwingRefusedLevelInvalid); n < 2 {
		t.Fatalf("both reject-shaped touches must be NAMED swing_level_invalid: got %d (%+v)", n, s.refused)
	}
}

// TestM463R22GapThroughInvalidatesTheWindow — the level is invalid once a 5m
// candle CLOSES beyond it, whether or not that candle touched it (a gap over
// the line). The old code only cancelled the resting order there; the window
// stayed "open", and the next touch from the other side armed a reject in the
// opposite direction. A later reject-shaped touch must be refused.
func TestM463R22GapThroughInvalidatesTheWindow(t *testing.T) {
	cfg := DefaultSwingCfg()
	m61PreP4Swing(&cfg) // m466 P4 (release #61): pins mentor_swing_isb_close_back=required + keep OFF — the strict R24 read this test was written against; the #61 defaults (D5.2-R26 p3 @04:49–05:09, D5.2-R25 p2 @10:23–10:51) are pinned in m61_p4_swing_test.go. The default half is asserted at the end.
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105),
		mk5m(t, 15, 5, 5, 10150, 10175, 10140, 10150),  // touch 1, reject (armed 10140)
		mk5m(t, 15, 5, 10, 10150, 10152, 10148, 10151), // below, no touch
		mk5m(t, 15, 5, 15, 10180, 10200, 10175, 10195), // opens above (gap), never touches, closes above
		mk5m(t, 15, 5, 20, 10195, 10199, 10165, 10190), // support-reject shape from above
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := m463Walk(t, s, bars, 4, cfg)
	if es := m463Entries(out); len(es) != 1 {
		t.Fatalf("only touch 1 may arm; a closed-through level arms nothing more: %+v", es)
	}
	if s.FirstTouch == nil || !s.FirstTouch.Through {
		t.Fatalf("a close beyond the line must latch the level invalid: %+v", s.FirstTouch)
	}
	// m466 P4 default (not_required, D5.2-R26 p3 @04:49–05:09): the 05:20 bar is
	// an INSIDE BAR of the 05:15 gap candle whose low (10,165) is back under the
	// resistance, so it is the way back in on the 4H line's side — a SHORT
	// inside-bar order with the stop AT the line, never the "fresh approach"
	// long reject the old window armed (and never a reject, stop 30 beyond).
	s2 := &SwingState{}
	es := m463Entries(m463Walk(t, s2, bars, 4, DefaultSwingCfg()))
	if len(es) != 2 || es[1].Side != SideShort || es[1].Price != 10165 || abs(es[1].Stop-swingLineGolden) > 0.01 {
		t.Fatalf("default: the reject, then the 05:20 inside-bar short AT the line; got %+v", es)
	}
}

// TestM463R24CandleColourIrrelevant — "Chị vô được thì stop đặt ngay line…
// không quan trọng màu" [D5.2 p3 @06:24-06:52]; R24/R26: after the through
// close and the close back, the next 5m INSIDE BAR enters — candle 1's colour
// does not matter (the D1.4 colour clause added in c52-w5 A7 is removed; the
// rest of A6/A7 stays: the latched invalid marker and "close back, THEN one
// more 5m beat for the inside bar"). A doji candle 1 has no colour and is
// therefore also fine for the swing.
func TestM463R24CandleColourIrrelevant(t *testing.T) {
	cfg := DefaultSwingCfg()
	cases := []struct {
		name    string
		candle1 market.Kline
		isb     market.Kline
	}{
		{"green candle 1, short swing", mk5m(t, 15, 5, 10, 10152, 10180, 10150, 10160), mk5m(t, 15, 5, 15, 10155, 10158, 10152, 10157)},
		{"red candle 1, short swing", mk5m(t, 15, 5, 10, 10175, 10180, 10150, 10160), mk5m(t, 15, 5, 15, 10165, 10167, 10150, 10160)},
		{"doji candle 1, short swing", mk5m(t, 15, 5, 10, 10160, 10180, 10150, 10160), mk5m(t, 15, 5, 15, 10158, 10162, 10152, 10156)},
	}
	for _, c := range cases {
		s := &SwingState{}
		out := SwingTick(s, w5ShortTape(t, c.candle1, c.isb), cfg, c.isb.OpenTime+60_000)
		es := m463Entries(out)
		if len(es) != 1 || es[0].Side != SideShort || abs(es[0].Stop-swingLineGolden) > 0.01 {
			t.Fatalf("%s: want ONE short inside-bar entry, stop AT the line: %+v", c.name, out)
		}
	}
	// mirror: support (long) swing with a RED candle 1 enters long.
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10200, 10220, 10190, 10210),  // prev above the line
		mk5m(t, 15, 5, 5, 10200, 10200, 10140, 10150),  // touches, closes THROUGH (below)
		mk5m(t, 15, 5, 10, 10190, 10195, 10140, 10180), // RED, closes back above
		mk5m(t, 15, 5, 15, 10185, 10190, 10170, 10182), // inside bar
	}
	s := &SwingState{}
	out := SwingTick(s, swingTape(t, cur), cfg, cur[3].OpenTime+60_000)
	if es := m463Entries(out); len(es) != 1 || es[0].Side != SideLong || abs(es[0].Stop-swingLineGolden) > 0.01 {
		t.Fatalf("a RED candle 1 at a support swing must enter LONG, stop at the line: %+v", out)
	}
}

// ── D5.2-R31 — a ~100-pt stop: "đừng vô" ───────────────────────────────────

// TestM463R31SkipAboveNinety — "mà stop loss ở level gần cả trăm điểm thì không
// ngon nữa… Phải chi 30-40, 50-60 ok, vô 2 contracts được… nhưng 100 điểm quá
// nhiều rủi ro" [D5.2 p2 @19:34-21:43] (his example: 91.75 pts). Owner order
// 2026-11-01 ("full of it", shown this quote) REVERSES m186: a swing whose stop
// is ABOVE mentor_swing_max_stop_pts (default 90) is refused and NAMED
// swing_max_stop. 90.00 exactly is still taken.
func TestM463R31SkipAboveNinety(t *testing.T) {
	cfg := DefaultSwingCfg()
	if cfg.SkipStopPts != 90 {
		t.Fatalf("default skip = %v, want 90", cfg.SkipStopPts)
	}
	// reject short: entry = candle low, stop = line + 30 → stop pts = line+30-low.
	mkTape := func(low float64) []market.Kline {
		return swingTape(t, []market.Kline{
			mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
			mk5m(t, 15, 5, 5, low+5, 10175, low, low+10),
		})
	}
	line, _, ok := swingLine(swingTape(t, nil), mk5m(t, 15, 5, 5, 0, 0, 0, 0).OpenTime+60_000, cfg, ctime())
	if !ok {
		t.Fatal("line setup")
	}
	stopLine := line + 30 // the exact line, not the rounded golden
	// 709/710's size: 102.25 pts → refused.
	s := &SwingState{}
	bars := mkTape(stopLine - 102.25)
	if es := m463Entries(SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+60_000)); len(es) != 0 {
		t.Fatalf("a 102.25-pt swing stop must be refused (đừng vô): %+v", es)
	}
	if m463Refused(s, SwingRefusedMaxStop) != 1 {
		t.Fatalf("the refusal must be NAMED swing_max_stop: %+v", s.refused)
	}
	// exactly 90 → taken
	s = &SwingState{}
	bars = mkTape(stopLine - 90)
	if es := m463Entries(SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+60_000)); len(es) != 1 {
		t.Fatalf("a 90.00-pt stop is at the ceiling, not above it — must be taken: %+v", es)
	}
	// the knob raises it
	cfg.SkipStopPts = 110
	s = &SwingState{}
	bars = mkTape(stopLine - 102.25)
	if es := m463Entries(SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+60_000)); len(es) != 1 {
		t.Fatalf("mentor_swing_max_stop_pts=110 must take a 102.25-pt stop: %+v", es)
	}
}

// TestM463R31InsideBarStopAtTheLineAlsoSkips — E11 (Wed 17 Jun, MNQU2026): the
// inside-bar long at 30,337.25 with the stop AT the level 30,245.25 = 91.75 pts
// "Rejected as ~100-pt stop" [D5.2 p2 @18:49-21:40]. The ISB is not marked
// spent by a refusal: R25 (no time limit) lets a later inside bar with a stop
// under the ceiling enter.
func TestM463R31InsideBarStopAtTheLineAlsoSkips(t *testing.T) {
	cfg := DefaultSwingCfg()
	const L = 30245.25
	s := &SwingState{Line: L, BucketStart: m463Ms(t, 6, 17, 9, 0), EmaCount: FourHEMA34Min}
	bars := []market.Kline{
		m463Bar(t, 6, 17, 1, 0, 30400, 30410, 30380, 30390),
		m463Bar(t, 6, 17, 5, 0, 30390, 30395, 30385, 30392),
		m463Bar(t, 6, 17, 9, 0, 30392, 30400, 30380, 30390),     // prev: above (support)
		m463Bar(t, 6, 17, 9, 5, 30380, 30385, 30200, 30210),     // touch, closes THROUGH (below)
		m463Bar(t, 6, 17, 9, 10, 30250, 30340, 30240, 30320),    // closes back above (green)
		m463Bar(t, 6, 17, 9, 15, 30330, 30337.25, 30300, 30310), // inside bar → entry 30337.25, stop 30245.25
	}
	s.LastBarTime = bars[2].OpenTime
	out := SwingTick(s, bars, cfg, bars[5].OpenTime+60_000)
	if es := m463Entries(out); len(es) != 0 {
		t.Fatalf("E11: a 91.75-pt inside-bar stop must be refused: %+v", es)
	}
	// m466 P4 default (not_required, D5.2-R26 p3 @04:49–05:09): the 09:10
	// close-back bar is itself an inside bar of the 09:05 through candle (entry
	// 30,340, 94.75 pts) — refused too; two named refusals, none spends the level.
	if m463Refused(s, SwingRefusedMaxStop) != 2 || s.FirstTouch == nil || s.FirstTouch.ISBFired {
		t.Fatalf("E11: named swing_max_stop and the ISB NOT spent: refused=%+v touch=%+v", s.refused, s.FirstTouch)
	}
	// a later, tighter inside bar still enters (R25).
	bars = append(bars,
		m463Bar(t, 6, 17, 9, 20, 30300, 30310, 30260, 30280), // closes back above
		m463Bar(t, 6, 17, 9, 25, 30285, 30290, 30275, 30288), // inside bar → entry 30290, stop 44.75
	)
	out = SwingTick(s, bars, cfg, bars[len(bars)-1].OpenTime+60_000)
	if es := m463Entries(out); len(es) != 1 || es[0].Price != 30290 || es[0].Stop != L {
		t.Fatalf("a later inside bar with a 44.75-pt stop at the line must enter: %+v", out)
	}
}

// ── D5.2-R27 — the 4h candle closes → the instance is over ─────────────────

// TestM463R27ExpiryNeverCrossesAClose — a swing order lives until the close of
// its 4h candle [D5.2 p3 @12:16-13:18 "9h đó là xong rồi… mình không có lệnh
// nào hết"]. The 13:00 CT candle's last bar is 15:55 — the CME closes at 16:00 —
// so the order's expiry is the CLOSE (15:59:59.999), never 16:59:59.999 inside
// the break: trade 678 (row 410) filled at 17:00:40 on the reopen off an expiry
// left in the break. Same for a Friday (weekend) and an early close.
func TestM463R27ExpiryNeverCrossesAClose(t *testing.T) {
	ct := ctime()
	at := func(y int, mo time.Month, d, h, mi int) int64 {
		return time.Date(y, mo, d, h, mi, 0, 0, ct).UnixMilli()
	}
	cases := []struct {
		name string
		now  int64
		want int64
	}{
		{"Wed 13:00 bucket → the 16:00 close", at(2026, 10, 21, 15, 20), at(2026, 10, 21, 16, 0) - 1},
		{"Fri 13:00 bucket → the 16:00 weekend close", at(2026, 10, 30, 14, 5), at(2026, 10, 30, 16, 0) - 1},
		{"Thu 11-26 Thanksgiving early close 12:00", at(2026, 11, 26, 10, 0), m463EarlyCloseMs(t, at(2026, 11, 26, 10, 0)) - 1},
		{"Wed 09:00 bucket → its own end 12:59:59.999", at(2026, 10, 21, 9, 40), at(2026, 10, 21, 13, 0) - 1},
		{"Tue 21:00 bucket → its own end 00:59:59.999", at(2026, 10, 20, 22, 0), at(2026, 10, 21, 1, 0) - 1},
	}
	for _, c := range cases {
		if got := swingExpiry(c.now); got != c.want {
			t.Fatalf("%s: expiry %s, want %s", c.name,
				time.UnixMilli(got).In(ct).Format("01-02 15:04:05.000"), time.UnixMilli(c.want).In(ct).Format("01-02 15:04:05.000"))
		}
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func m463Ms(t *testing.T, mo time.Month, d, h, mi int) int64 {
	t.Helper()
	return time.Date(2026, mo, d, h, mi, 0, 0, ctime()).UnixMilli()
}

// m463Bar: a 5m bar opening at 2026-mo-d h:mi CT.
func m463Bar(t *testing.T, mo time.Month, d, h, mi int, o, hi, lo, c float64) market.Kline {
	t.Helper()
	ot := m463Ms(t, mo, d, h, mi)
	return market.Kline{OpenTime: ot, Open: o, High: hi, Low: lo, Close: c, CloseTime: ot + 5*60_000 - 1}
}

func m463EarlyCloseMs(t *testing.T, now int64) int64 {
	t.Helper()
	for m := now; m < now+12*3600_000; m += 60_000 {
		if !kernel.IsCMEOpen(time.UnixMilli(m)) {
			return m
		}
	}
	t.Fatal("no close found")
	return 0
}

// ── the D5.2 worked examples (final note, WORKED EXAMPLES table) ───────────

// m463Example runs the mentor's own tape through SwingTick bar by bar, with the
// line seeded WARM at the value the note gives (the 4h EMA 34 he drew). Two
// far-away bars in the two previous 4h buckets let swingLine read a bucket
// series (the warm line is authoritative, eval.go seed path).
func m463Example(t *testing.T, mo time.Month, d, bucketH int, line float64, cur []market.Kline) ([]Intent, *SwingState) {
	t.Helper()
	return m463ExampleCfg(t, DefaultSwingCfg(), mo, d, bucketH, line, cur)
}

// m463ExampleCfg is m463Example under an explicit swing config (m466 P4).
func m463ExampleCfg(t *testing.T, cfg SwingCfg, mo time.Month, d, bucketH int, line float64, cur []market.Kline) ([]Intent, *SwingState) {
	t.Helper()
	bs := m463Ms(t, mo, d, bucketH, 0)
	pre := []market.Kline{
		{OpenTime: bs - 8*3600_000, CloseTime: bs - 8*3600_000 + 299_999, Open: line - 500, High: line - 499, Low: line - 501, Close: line - 500},
		{OpenTime: bs - 4*3600_000, CloseTime: bs - 4*3600_000 + 299_999, Open: line - 500, High: line - 499, Low: line - 501, Close: line - 500},
	}
	bars := append(pre, cur...)
	s := &SwingState{Line: line, BucketStart: bs, EmaCount: FourHEMA34Min, LastBarTime: pre[1].OpenTime}
	var out []Intent
	for i := 3; i <= len(bars); i++ {
		out = append(out, SwingTick(s, bars[:i], cfg, bars[i-1].CloseTime+1)...)
	}
	return m463Entries(out), s
}

func TestM463CourseWorkedExamples(t *testing.T) {
	// E2 — Thu 3 Sep, MNQZ2026 [D5.2 p1 @13:12–14:05, @19:44–20:25]: line
	// 29,575.50; price creeps up from below; the FIRST touching 5m candle (03:00,
	// O29,556.50 H29,582.75 L29,543.50 C29,543.50) closes back below → "sell stop,
	// nằm bên dưới cây nến chạm reject" at 29,543.50; stop ~30 above the line
	// (he drew 29,609.00; the bot's line+30 = 29,605.50).
	t.Run("E2 Sep 3 first-touch reject short", func(t *testing.T) {
		es, _ := m463Example(t, 9, 3, 1, 29575.50, []market.Kline{
			m463Bar(t, 9, 3, 2, 50, 29520, 29532, 29515, 29530),
			m463Bar(t, 9, 3, 2, 55, 29530, 29560, 29528, 29556.50),
			m463Bar(t, 9, 3, 3, 0, 29556.50, 29582.75, 29543.50, 29543.50),
		})
		if len(es) != 1 || es[0].Side != SideShort || es[0].Price != 29543.50 || es[0].Stop != 29605.50 {
			t.Fatalf("E2: want SHORT sell stop 29,543.50, stop 29,605.50; got %+v", es)
		}
	})
	// E12 — Thu 3 Sep, MNQU2026 [D5.2 p3 @17:32–18:01]: line 29,281.25 (falling
	// 4h EMA = resistance); first 5m touch 03:05 O29,257.75 H29,293.00
	// L29,240.50 C29,244.75 closed below → "đặt sell stop" under it.
	t.Run("E12 Sep 3 MNQU reject short", func(t *testing.T) {
		es, _ := m463Example(t, 9, 3, 1, 29281.25, []market.Kline{
			m463Bar(t, 9, 3, 2, 55, 29240, 29262, 29236, 29257.75),
			m463Bar(t, 9, 3, 3, 0, 29257.75, 29270, 29250, 29257.75),
			m463Bar(t, 9, 3, 3, 5, 29257.75, 29293.00, 29240.50, 29244.75),
		})
		if len(es) != 1 || es[0].Side != SideShort || es[0].Price != 29240.50 || es[0].Stop != 29311.25 {
			t.Fatalf("E12: want SHORT sell stop 29,240.50, stop 29,311.25; got %+v", es)
		}
	})
	// E3 — Wed 26 Aug, MNQZ2026 [D5.2 p1 @22:34–24:06]: line 29,658.75; the
	// first touch CLOSED ABOVE → INVALID, no order; price ran ~+280 (a short
	// would lose). Every later reject-shaped touch in that 4h candle is refused.
	t.Run("E3 Aug 26 first touch closes through: no order", func(t *testing.T) {
		e3 := []market.Kline{
			m463Bar(t, 8, 26, 14, 0, 29620, 29640, 29615, 29635),
			m463Bar(t, 8, 26, 14, 5, 29635, 29690, 29630, 29685), // first touch closes ABOVE
			m463Bar(t, 8, 26, 14, 10, 29685, 29720, 29680, 29715),
			m463Bar(t, 8, 26, 14, 15, 29715, 29718, 29655, 29690), // pulls back onto the line from above, closes above
			m463Bar(t, 8, 26, 14, 20, 29690, 29760, 29688, 29755),
		}
		// m466 P4 default (not_required, D5.2-R26 p3 @04:49–05:09): the 14:15
		// bar is an inside bar of 14:10 whose low dips under the line — the ONLY
		// order is that inside-bar SHORT with the stop AT the line (no reject
		// order), and price runs away from it: it never fills.
		des, ds := m463Example(t, 8, 26, 13, 29658.75, e3)
		if len(des) != 1 || des[0].Side != SideShort || des[0].Stop != 29658.75 || ds.Pos != nil {
			t.Fatalf("E3 default: only the inside-bar short AT the line, never filled; got %+v pos %+v", des, ds.Pos)
		}
		strict := DefaultSwingCfg()
		m61PreP4Swing(&strict)
		es, s := m463ExampleCfg(t, strict, 8, 26, 13, 29658.75, e3)
		if len(es) != 0 {
			t.Fatalf("E3: the level is invalid — no order at all; got %+v", es)
		}
		if m463Refused(s, SwingRefusedLevelInvalid) == 0 {
			t.Fatalf("E3: the 14:15 reject-shaped touch must be NAMED swing_level_invalid: %+v", s.refused)
		}
	})
	// E7 — Tue 11 Aug, MNQU2026, long [D5.2 p2 @10:01–11:29]: level 29,553.50;
	// the 5m closes below (low ~29,533), closes back above, then an inside bar
	// → entry 29,577.75 at the inside bar's high, stop AT the level ("đây mới gọi
	// là setup").
	t.Run("E7 Aug 11 inside bar long, stop at the level", func(t *testing.T) {
		e7 := []market.Kline{
			m463Bar(t, 8, 11, 13, 20, 29600, 29605, 29590, 29595),
			m463Bar(t, 8, 11, 13, 25, 29595, 29598, 29533, 29540),    // touches, closes BELOW → invalid
			m463Bar(t, 8, 11, 13, 30, 29540, 29585, 29535, 29575),    // closes back above (candle 1)
			m463Bar(t, 8, 11, 13, 35, 29572, 29577.75, 29560, 29570), // inside bar
		}
		// strict read (required): the mentor's own entry, "đây mới gọi là setup".
		strict := DefaultSwingCfg()
		m61PreP4Swing(&strict)
		es, _ := m463ExampleCfg(t, strict, 8, 11, 13, 29553.50, e7)
		if len(es) != 1 || es[0].Side != SideLong || es[0].Price != 29577.75 || es[0].Stop != 29553.50 {
			t.Fatalf("E7: want LONG 29,577.75 stop 29,553.50; got %+v", es)
		}
		// m466 P4 default (not_required, D5.2-R26 p3 @04:49–05:09): in this
		// fixture the 13:30 close-back candle sits inside the 13:25 through
		// candle, so the inside-bar long arms one beat earlier at its high
		// 29,585, stop AT the level — "tùy góc nhìn" [p3 @07:42–08:36].
		des, _ := m463Example(t, 8, 11, 13, 29553.50, e7)
		if len(des) != 1 || des[0].Side != SideLong || des[0].Price != 29585 || des[0].Stop != 29553.50 {
			t.Fatalf("E7 default: want LONG 29,585 stop 29,553.50; got %+v", des)
		}
	})
	// E8 — Fri 11 Sep, MNQU2026, long [D5.2 p2 @12:34–14:05; p3 @04:49–05:57]:
	// line 29,405.25; the 5m dips under the line (11:15–11:50, lows 29,381–
	// 29,395), closes back above, inside bar → entry 29,435.50, stop AT the line
	// (18.5–22 pts; the 46.25 stop on the inside bar is rejected).
	t.Run("E8 Sep 11 inside bar long, stop at the line", func(t *testing.T) {
		es, _ := m463Example(t, 9, 11, 9, 29405.25, []market.Kline{
			m463Bar(t, 9, 11, 11, 10, 29440, 29445, 29420, 29425),
			m463Bar(t, 9, 11, 11, 15, 29425, 29428, 29381, 29395),    // closes BELOW → invalid
			m463Bar(t, 9, 11, 11, 20, 29395, 29440, 29389.25, 29430), // closes back above (candle 1)
			m463Bar(t, 9, 11, 11, 25, 29428, 29435.50, 29415, 29426), // inside bar → 29,435.50
		})
		if len(es) != 1 || es[0].Side != SideLong || es[0].Price != 29435.50 || es[0].Stop != 29405.25 {
			t.Fatalf("E8: want LONG 29,435.50 stop 29,405.25; got %+v", es)
		}
	})
}

// TestM463InsideBarNeverDoublesAnOpenSwing — review finding (course-swing P2,
// release #59 fix round): ONE setup per level per 4h candle [D5.2-R19/R23 p1
// @13:23–13:40; p3 @19:15–21:56 "không chạm vô nữa, không đụng vô nữa"]. Since
// m463 ANY 5m close beyond the line latches the level invalid — also AFTER the
// reject order FILLED. The inside-bar branch then armed a SECOND swing stop
// entry on the same level, and its fill replaced s.Pos, losing the first leg's
// BE/hold tracking. With a swing position open, the inside bar is refused and
// NAMED swing_position_open, never armed.
// Mutant: drop the `s.Pos != nil` guard in the ISB branch → a second
// place_stop_entry short (swing-…) → RED.
func TestM463InsideBarNeverDoublesAnOpenSwing(t *testing.T) {
	cfg := DefaultSwingCfg()
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 10100, 10110, 10095, 10105),  // prev: below the line
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160),  // touch, closes back below → sell stop 10155
		mk5m(t, 15, 5, 10, 10158, 10160, 10150, 10152), // trades through 10155 → FILLED
		mk5m(t, 15, 5, 15, 10152, 10180, 10150, 10175), // closes ABOVE the line (short of the stop) → invalid
		mk5m(t, 15, 5, 20, 10175, 10178, 10150, 10160), // closes back below
		mk5m(t, 15, 5, 25, 10165, 10170, 10152, 10160), // 5m inside bar, closes below
	}
	bars := swingTape(t, cur)
	s := &SwingState{}
	out := m463Walk(t, s, bars, 4, cfg)
	es := m463Entries(out)
	if len(es) != 1 {
		t.Fatalf("one setup per level: only the reject may arm while its position is open, got %d entries: %+v", len(es), es)
	}
	if s.Pos == nil || s.Pos.ArmID != es[0].ArmID {
		t.Fatalf("the first leg must stay the managed position, Pos=%+v first=%s", s.Pos, es[0].ArmID)
	}
	if s.Pending != nil {
		t.Fatalf("no second swing order may rest beside the open position: %+v", s.Pending)
	}
	if n := m463Refused(s, SwingRefusedPositionOpen); n != 1 {
		t.Fatalf("the inside bar must be refused and NAMED %s once, got %d (%+v)", SwingRefusedPositionOpen, n, s.refused)
	}
}
