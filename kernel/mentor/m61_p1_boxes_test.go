package mentor

import (
	"bufio"
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"vl/market"
)

// m466 P1 (release #61) — boxes, breaks, ranges. Every pin below is written
// from the course example it cites (the rule id + the timestamp) and was run
// RED against the #60 base (45053f78a) before the fix landed.
//
//   - G20 D4.1-R34 / G49 D3.4-R34 — the ISB rest-box escape is a 1m CLOSE
//     outside the box: "Break là sao? Là một cây nến một phút anh chị trade, nó
//     đóng bên ngoài cái box là at least" [D4.1 p1 @20:36–21:03]; "nó đóng một
//     cái nến một phút ở ngoài" [D3.4 p2 @05:37–06:01].
//   - G28 D4.1-R37 / G32 D4.2-R10 — the 5m box is the NEWEST 5m inside bar:
//     "cái nến inside bar nào gần với thời điểm đó nhất, bọc lại cho em" [D4.1
//     p1 @22:31–22:49]; a newer OPPOSITE one is the 15m/5m conflict, "messed
//     up… đừng trade. Đợi nó thoát cả luôn" [D4.2 p1 @05:34–08:33].
//   - G21 D4.1-R31/R33 / G29 D4.1-R55 — after a break only setups in the break
//     direction: "Break ra phải có setup… break UP → inside bar long hoặc
//     potential high-low" [D4.1 p1 @18:29–20:33]; "thoát [đầu] nào thì mình
//     trade [đầu] đó" [D4.1 p1 @07:47–08:07].
//   - G33 D4.2-R33 — a range too small for its candles: "từ đây trên đây có
//     50-60 điểm, mà cây nến vừa vậy 30-40 điểm… đừng bao giờ đụng tới" [D4.2
//     p2 @06:16–06:30] (measured 61.50 pts, a 33.75-pt candle).
//   - G37 D3.2-R40 — the mid-range ban reads the box's EFFECTIVE (Uno) role
//     [D3.2 p2 @07:50–09:14; D1.3 p1 @12:31–12:58].
//   - G43 D2.2-R71 — mid-range, trade WITH a fresh rejection: "Nó mới reject
//     thì đánh theo hướng nó reject" [D2.2 p3 @14:28–15:58].

func m61p1Entry(side Side, price float64) Intent {
	stop := price - 10
	if side == SideShort {
		stop = price + 10
	}
	return Intent{Action: PlaceStopLimitEntry, Setup: "ISB", Side: side, Price: price, Limit: price, Stop: stop, ArmID: "isb-t-" + string(side), Reason: "test"}
}

func m61p1Has(out []Intent, side Side) bool {
	for _, in := range out {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Side == side {
			return true
		}
	}
	return false
}

// ── G20 / G49 ───────────────────────────────────────────────────────────────

// TestM61ISBBoxEscapesOnOneMinuteClose — D3.4-R30 box H29,046.25 L29,031.25; a
// 1m candle OPENS inside (29,040) and CLOSES outside (29,047): the course's
// minimum "nó đóng bên ngoài cái box là at least" [D4.1 p1 @20:36–21:03] — the
// box escapes long. Mutant body (the #25–#60 default) keeps it standing. The
// provisional (forming) 15m box is pinned the same way.
func TestM61ISBBoxEscapesOnOneMinuteClose(t *testing.T) {
	// m473 B3 [Oct D4.3 @ 65:00-66:13] OctD4.3-R67 supersedes the September default: the 1m close
	// still gives permission, but the DEFAULT now keeps the box until its own TF closes outside.
	if d := DefaultConfig().ISBBoxEscapeMode; d != ISBBoxEscapeCloseBoxTF {
		t.Fatalf("m473 B3 D4.3-R67: the default ISB rest-box escape must be close5m, got %q", d)
	}
	box := ISBBox{High: 29_046.25, Low: 29_031.25, Dir: SideShort, AtTime: m60p1At(9, 45)}
	bar := m60p1K(m60p1At(9, 56), 29_040, 29_048, 29_039, 29_047)
	if esc, dir := ISBBoxEscapeWith(box, bar, DefaultConfig().ISBBoxEscapeMode); !esc || dir != SideLong {
		t.Fatalf("O29,040 C29,047 must escape the 5m box long, got escaped=%v dir=%q", esc, dir)
	}
	if esc, _ := ISBBoxEscapeWith(box, bar, ISBBoxEscapeBody); esc {
		t.Fatal("mutant body: the open is inside, so the body test keeps the box (the pre-#61 read)")
	}

	tape := []market.Kline{
		m60p1K(m60p1At(9, 55), 29_038, 29_040, 29_035, 29_039),
		bar,
	}
	for _, mode := range []string{ISBBoxEscapeClose, ISBBoxEscapeBody} {
		e := m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
		e.Cfg.Early15mRead = false
		e.Cfg.ISBBoxEscapeMode = mode // "close" = the explicit September 1m-delete mode (m466 G20)
		b := box
		e.State.ISBBox = &b
		e.State.LastISBBoxAt = box.AtTime
		e.State.LastISBBoxDir = box.Dir
		m60p1Run(e, tape)
		if mode == ISBBoxEscapeClose {
			if e.State.ISBBox != nil || e.State.LastISBBoxEscape != SideLong {
				t.Fatalf("default: the 5m box must escape long on the 1m close, box %+v escape %q", e.State.ISBBox, e.State.LastISBBoxEscape)
			}
		} else if e.State.ISBBox == nil {
			t.Fatal("mutant body: the box must still stand")
		}
	}

	// The #60 provisional forming-15m box escapes the same way.
	for _, mode := range []string{ISBBoxEscapeClose, ISBBoxEscapeBody} {
		e := m60p1Eval(t, SideLong, 28_980, SideLong, SideLong, m60p1Levels())
		e.Cfg.ISBBoxEscapeMode = mode
		pb := ISBBox{High: 29_046.25, Low: 29_031.25, Dir: SideLong, AtTime: m60p1At(9, 45), Provisional: true}
		e.State.ISBBox15m = &pb
		e.State.LastForming15mAt = pb.AtTime
		m60p1Run(e, tape)
		if mode == ISBBoxEscapeClose {
			if e.State.ISBBox15m != nil || e.State.Counters["isb_15m_forming_escaped"] != 1 {
				t.Fatalf("default: the provisional 15m box must escape on the 1m close, box %+v counters %v", e.State.ISBBox15m, e.State.Counters)
			}
		} else if e.State.ISBBox15m == nil {
			t.Fatal("mutant body: the provisional box must still stand")
		}
	}
}

// TestM61PHLBanAndBoxEscapeAgree — G49 D3.4-R34: the PHL/PLH 5m-box ban reads
// the 1m CLOSE (phlLocationBan), so the box escape must too — one candle, one
// verdict. O inside / C outside both escapes the box AND frees the ban; O
// outside / C inside does neither.
func TestM61PHLBanAndBoxEscapeAgree(t *testing.T) {
	box := ISBBox{High: 29_046.25, Low: 29_031.25, Dir: SideShort, AtTime: m60p1At(9, 45)}
	e := New(DefaultConfig())
	for _, c := range []struct {
		name string
		o, c float64
	}{
		{"open inside, close above", 29_040, 29_047},
		{"open inside, close below", 29_040, 29_030},
		{"open above, close inside", 29_050, 29_040},
		{"both above", 29_050, 29_048},
		{"both inside", 29_035, 29_040},
	} {
		cur := m60p1K(m60p1At(9, 56), c.o, max(c.o, c.c)+1, min(c.o, c.c)-1, c.c)
		b := box
		e.State.ISBBox = &b
		name, _ := e.phlLocationBan(Level{Key: "k", Kind: KindKeyLevel, Price: c.c}, SideLong, c.c, cur, nil, nil)
		banned := name == "phl_5m_box_ban"
		escaped, _ := ISBBoxEscapeWith(box, cur, e.Cfg.ISBBoxEscapeMode)
		if banned == escaped {
			t.Fatalf("%s: the PHL ban (banned=%v) and the box escape (escaped=%v) must agree on the same candle", c.name, banned, escaped)
		}
	}
}

// ── G28 / G32 ───────────────────────────────────────────────────────────────

// m61p1NewestTape: a red 09:40 5m mother, a doji 09:45 inside bar (the 5m
// SHORT box 29,024–29,052), two doji 5m candles that make no ISB, the 10:00
// mother (red → short, green → long when `long`) and the 10:05 inside bar —
// the NEWEST 5m ISB, AtTime 10:05. Every 1m close stays inside the 09:45 box.
func m61p1NewestTape(long bool) []market.Kline {
	mother := [6]float64{10, 0, 29_046, 29_050, 29_028, 29_030}
	if long {
		mother = [6]float64{10, 0, 29_030, 29_050, 29_028, 29_046}
	}
	return m60p1Tape([][6]float64{
		{9, 35, 29_000, 29_010, 28_990, 29_008},
		{9, 40, 29_060, 29_066, 29_020, 29_026},
		{9, 45, 29_040, 29_052, 29_024, 29_040},
		{9, 50, 29_040, 29_045, 29_035, 29_040},
		{9, 55, 29_040, 29_045, 29_035, 29_040},
		mother,
		{10, 5, 29_034, 29_044, 29_032, 29_042},
	})
}

// TestM61FiveMinBoxIsNewestISB — G28 D4.1-R37 [D4.1 p1 @22:31–22:49]: the 09:45
// short box stands (no 1m close outside it) when the 10:05 5m pair closes as a
// short inside bar → the box is REPLACED by the 10:05 candle (AtTime 10:05),
// counted isb_5m_box_replaced. Mutant first → the 09:45 box stays.
func TestM61FiveMinBoxIsNewestISB(t *testing.T) {
	// ISB5-F1: the default is merge since the ISB5 audit (OctD4.3-R59); newest
	// stays a knob value and is pinned here explicitly.
	for _, mode := range []string{FiveMinBoxNewest, FiveMinBoxFirst, FiveMinBoxMerge} {
		e := m60p1Eval(t, SideShort, 29_100, SideShort, SideShort, m60p1Levels())
		e.Cfg.Early15mRead = false
		e.Cfg.FiveMinBoxSource = mode
		bars := m61p1NewestTape(false)
		for i := range bars {
			e.Tick(bars[:i+1], BarCloseInstant(bars[i]))
			if bars[i].OpenTime == m60p1At(9, 59) {
				if bx := e.State.ISBBox; bx == nil || bx.AtTime != m60p1At(9, 45) || bx.Dir != SideShort {
					t.Fatalf("10:00: want the 09:45 short box standing, got %+v", bx)
				}
			}
		}
		bx := e.State.ISBBox
		if mode == FiveMinBoxMerge {
			// OctD4.3-R59: the 10:05 candle closed inside the 09:45 box → merged,
			// never a replacement.
			if bx == nil || bx.AtTime != m60p1At(9, 45) || bx.Dir != SideShort || e.State.Counters["isb_5m_box_replaced"] != 0 {
				t.Fatalf("merge: the 09:45 box must stand (merged), got %+v counters %v", bx, e.State.Counters)
			}
			continue
		}
		if mode == FiveMinBoxNewest {
			if bx == nil || bx.AtTime != m60p1At(10, 5) || bx.Dir != SideShort || bx.High != 29_044 || bx.Low != 29_032 {
				t.Fatalf("newest: the 10:05 inside bar must replace the 09:45 box, got %+v", bx)
			}
			if e.State.LastISBBoxAt != m60p1At(10, 5) || e.State.LastISBBoxDir != SideShort || e.State.LastISBBoxEscape != "" {
				t.Fatalf("newest: LastISBBox* must follow the replacement, got at=%d dir=%q esc=%q", e.State.LastISBBoxAt, e.State.LastISBBoxDir, e.State.LastISBBoxEscape)
			}
			if e.State.Counters["isb_5m_box_replaced"] != 1 || e.State.Counters["isb_5m_box_replaced_opposite"] != 0 {
				t.Fatalf("newest: want isb_5m_box_replaced=1, counters %v", e.State.Counters)
			}
		} else if bx == nil || bx.AtTime != m60p1At(9, 45) {
			t.Fatalf("mutant first: the 09:45 box must stay, got %+v", bx)
		}
	}
}

// TestM61NewerOppositeFiveMinISBConflicts — G32 D4.2-R10 [D4.2 p1 @05:34–08:33]:
// a 15m SHORT box stands; the 5m short box 09:45 stands; the 10:05 5m pair is
// a LONG inside bar → the 5m box turns long (isb_5m_box_replaced_opposite) and
// the 15m-short / 5m-long conflict refuses the next 1m ISB (isb_tf_conflict).
// Mutant first → the 5m box stays short, no conflict (the pre-#61 read).
func TestM61NewerOppositeFiveMinISBConflicts(t *testing.T) {
	for _, mode := range []string{FiveMinBoxNewest, FiveMinBoxFirst} {
		e := m60p1Eval(t, SideShort, 29_100, SideShort, SideShort, m60p1Levels())
		e.Cfg.Early15mRead = false
		e.Cfg.FiveMinBoxSource = mode
		box15 := ISBBox{High: 29_100, Low: 28_950, Dir: SideShort, AtTime: m60p1At(9, 30)}
		e.State.ISBBox15m = &box15
		e.State.LastISBBox15mAt = m60p1At(9, 45) // no real 15m rebuild from the tape
		bars := append(m61p1NewestTape(true),
			m60p1K(m60p1At(10, 10), 29_042, 29_043.5, 29_034, 29_036), // red 1m mother → short
			m60p1K(m60p1At(10, 11), 29_037, 29_040, 29_035, 29_039),   // inside
		)
		m60p1Run(e, bars)
		if mode == FiveMinBoxNewest {
			if bx := e.State.ISBBox; bx == nil || bx.Dir != SideLong || bx.AtTime != m60p1At(10, 5) {
				t.Fatalf("newest: the newer LONG 5m inside bar must replace the short box, got %+v", bx)
			}
			if e.State.Counters["isb_5m_box_replaced_opposite"] != 1 {
				t.Fatalf("newest: want isb_5m_box_replaced_opposite=1, counters %v", e.State.Counters)
			}
			if e.State.Refusals["isb_tf_conflict"]+e.State.Refusals["isb_tf_conflict_both_boxes"] == 0 {
				t.Fatalf("newest: 15m short + 5m long must refuse the 1m ISB as the TF conflict; refusals %v", e.State.Refusals)
			}
		} else {
			if bx := e.State.ISBBox; bx == nil || bx.Dir != SideShort {
				t.Fatalf("mutant first: the short 09:45 box must stay, got %+v", bx)
			}
			if e.State.Refusals["isb_tf_conflict"]+e.State.Refusals["isb_tf_conflict_both_boxes"] != 0 {
				t.Fatalf("mutant first: no conflict (today); refusals %v", e.State.Refusals)
			}
		}
	}
}

// ── G21 / G29 ───────────────────────────────────────────────────────────────

// TestM61BreakLocksCounterSetups — G21 D4.1-R31/R33 [D4.1 p1 @18:29–20:33]:
// FTGH 29,463.00–29,478.50 (D2.2-R70); a 1m O29,480 C29,485 escapes it with
// the BODY (box.go boxEscapeIndex) → the lock goes LONG; a short ISB at 29,482
// is refused break_counter_setup, a long ISB passes; a 1m close back at 29,470
// (inside the box: the break FAILED, D4.1-R30) clears it. Case 2: a 5m ISB box
// escapes DOWN through Tick → the long ISB is refused. Knob off → today.
func TestM61BreakLocksCounterSetups(t *testing.T) {
	if !DefaultConfig().BreakDirectionLock {
		t.Fatal("G21: mentor_break_direction_lock must default ON")
	}
	b0 := m60p1K(m60p1At(10, 0), 29_470, 29_475, 29_465, 29_468)
	b1 := m60p1K(m60p1At(10, 1), 29_468, 29_477, 29_460, 29_470)
	b2 := m60p1K(m60p1At(10, 2), 29_480, 29_487, 29_479, 29_485) // the body escape
	b3 := m60p1K(m60p1At(10, 3), 29_484, 29_486, 29_468, 29_470) // closes back inside
	ftgh := Box{Kind: FTGH, Top: 29_478.50, Bottom: 29_463.00, Key: "ftgh:29478.5:29463", FormedAt: 0, FormedAtMs: b0.OpenTime}
	bars := []market.Kline{b0, b1, b2}

	ev := boxBreakEvents(bars, []Box{ftgh}, DefaultBoxCfg(), b2)
	if len(ev) != 1 || ev[0].Side != SideLong || ev[0].Lo != 29_463 || ev[0].Hi != 29_478.5 {
		t.Fatalf("the FTGH body escape on 10:02 must be one LONG break 29,463–29,478.50, got %+v", ev)
	}
	if ev := boxBreakEvents(append(bars, b3), []Box{ftgh}, DefaultBoxCfg(), b3); len(ev) != 0 {
		t.Fatalf("only the FIRST escape candle is a break event, got %+v on 10:03", ev)
	}

	e := New(DefaultConfig())
	e.advanceBreakLock(b2, BarCloseInstant(b2), ev)
	if l := e.State.BreakLock; l == nil || l.Side != SideLong || l.At != b2.OpenTime {
		t.Fatalf("want a LONG break lock at 10:02, got %+v", l)
	}
	out := e.courseRangeFilter([]Intent{m61p1Entry(SideShort, 29_482), m61p1Entry(SideLong, 29_488)}, nil)
	if m61p1Has(out, SideShort) || !m61p1Has(out, SideLong) {
		t.Fatalf("after the up-break only longs pass: got %+v", out)
	}
	if e.State.Refusals["break_counter_setup"] != 1 {
		t.Fatalf("want break_counter_setup=1, refusals %v", e.State.Refusals)
	}
	e.advanceBreakLock(b3, BarCloseInstant(b3), nil)
	if e.State.BreakLock != nil || e.State.Counters["break_lock_failed"] != 1 {
		t.Fatalf("a 1m close back inside the box must clear the lock (failed break), lock %+v counters %v", e.State.BreakLock, e.State.Counters)
	}
	if out := e.courseRangeFilter([]Intent{m61p1Entry(SideShort, 29_465)}, nil); !m61p1Has(out, SideShort) {
		t.Fatal("after the failed break the short passes again")
	}
	// A one-candle reversal straight THROUGH the box (close below 29,463) is
	// the same failed break.
	e.advanceBreakLock(b2, BarCloseInstant(b2), ev)
	thru := m60p1K(m60p1At(10, 3), 29_484, 29_486, 29_440, 29_450)
	e.advanceBreakLock(thru, BarCloseInstant(thru), nil)
	if e.State.BreakLock != nil {
		t.Fatalf("a close through the box to the far side also fails the up-break, got %+v", e.State.BreakLock)
	}

	// An opposite break replaces the lock; the day roll clears it.
	e.advanceBreakLock(b2, BarCloseInstant(b2), ev)
	b4 := m60p1K(m60p1At(10, 4), 29_490, 29_495, 29_488, 29_492) // closes outside: the lock stands into the flip
	e.advanceBreakLock(b4, BarCloseInstant(b4), []breakEvent{{Side: SideShort, Lo: 29_400, Hi: 29_420, Src: "isb_5m"}})
	if l := e.State.BreakLock; l == nil || l.Side != SideShort || e.State.Counters["break_lock_flipped"] != 1 {
		t.Fatalf("an opposite break flips the lock, got %+v counters %v", l, e.State.Counters)
	}
	next := m60p1K(m60p1At(10, 3)+24*3_600_000, 29_300, 29_301, 29_299, 29_300)
	e.advanceBreakLock(next, BarCloseInstant(next), nil)
	if e.State.BreakLock != nil {
		t.Fatalf("the day roll clears the lock, got %+v", e.State.BreakLock)
	}

	// Case 2 through Tick: the 5m ISB box (105–110) escapes DOWN on the
	// fixture's last 1m close (104) → lock short → the long ISB is refused.
	for _, on := range []bool{true, false} {
		f, fb, now := mtfFixture(t, false)
		f.Cfg.BreakDirectionLock = on
		f.State.ISBBox = &ISBBox{High: 110, Low: 105, Dir: SideShort, AtTime: fb[0].OpenTime}
		f.State.LastISBBoxAt = fb[0].OpenTime
		got := f.Tick(fb, now)
		if on {
			if m60p1ISB(got, SideLong) != nil || f.State.Refusals["break_counter_setup"] == 0 {
				t.Fatalf("knob on: the long ISB after a DOWN escape must be refused break_counter_setup; got %+v refusals %v", entryIntents(got), f.State.Refusals)
			}
			if l := f.State.BreakLock; l == nil || l.Side != SideShort || l.Src != "isb_5m" {
				t.Fatalf("knob on: want a short isb_5m lock, got %+v", l)
			}
		} else {
			if m60p1ISB(got, SideLong) == nil || f.State.BreakLock != nil {
				t.Fatalf("knob off: today — the long ISB emits and no lock exists; got %+v lock %+v refusals %v", entryIntents(got), f.State.BreakLock, f.State.Refusals)
			}
		}
	}
}

// TestM61FifteenMinExitSetsSide — G29 D4.1-R55 [D4.1 p1 @07:47–08:07] + D3.4-R32
// [D3.4 p2 @03:13–04:30]: the WE-1 15m box 29,026.00 / 28,970.75 exits DOWN (a
// 1m close 28,968) → the lock goes short: a long 1m ISB is refused
// break_counter_setup, a short one passes; a 1m close back at 28,980 clears it.
func TestM61FifteenMinExitSetsSide(t *testing.T) {
	e := m60p1Eval(t, SideShort, 29_100, SideShort, SideShort, m60p1Levels())
	e.Cfg.Early15mRead = false
	e.Cfg.ISBBoxEscapeMode = ISBBoxEscapeClose // m473 B3: explicit 1m-delete mode; the default close5m keeps the 15m box until a 15m close outside [Oct D4.3 @ 65:00-66:13]
	bx := ISBBox{High: 29_026, Low: 28_970.75, Dir: SideLong, AtTime: m60p1At(9, 30)}
	e.State.ISBBox15m = &bx
	e.State.LastISBBox15mAt = bx.AtTime
	bars := []market.Kline{
		m60p1K(m60p1At(10, 0), 28_990, 28_995, 28_980, 28_985),
		m60p1K(m60p1At(10, 1), 28_985, 28_986, 28_972, 28_975),
		m60p1K(m60p1At(10, 2), 28_975, 28_976, 28_966, 28_968), // the exit: close 28,968
	}
	m60p1Run(e, bars)
	if e.State.ISBBox15m != nil {
		t.Fatalf("the 15m box must have escaped on the 1m close 28,968, got %+v", e.State.ISBBox15m)
	}
	if l := e.State.BreakLock; l == nil || l.Side != SideShort || l.Src != "isb_15m" || l.Lo != 28_970.75 || l.Hi != 29_026 {
		t.Fatalf("want a SHORT isb_15m lock over the 15m box, got %+v", l)
	}
	out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 28_970), m61p1Entry(SideShort, 28_960)}, nil)
	if m61p1Has(out, SideLong) || !m61p1Has(out, SideShort) {
		t.Fatalf("after the 15m exit down only shorts trade, got %+v", out)
	}
	m60p1Run(e, append(bars, m60p1K(m60p1At(10, 3), 28_969, 28_982, 28_967, 28_980)))
	if e.State.BreakLock != nil {
		t.Fatalf("a 1m close back inside (28,980) clears the lock, got %+v", e.State.BreakLock)
	}
}

// ── G33 ─────────────────────────────────────────────────────────────────────

// m61p1SmallRange: 15 closed 1m bars inside 28,960.00–29,021.50 (61.50 pts, the
// D4.2-R33 measurement) with TWO big candles of `big` and `big2` points (the
// video measures 33.75 and 34.50 — "cây nến vừa vậy 30-40 điểm").
func m61p1SmallRange(top, big, big2 float64) []market.Kline {
	var bars []market.Kline
	for i := 0; i < 15; i++ {
		o, h, l, c := 28_990.0, 28_996.0, 28_986.0, 28_992.0
		switch i {
		case 0:
			o, h, l, c = 28_965, 28_970, 28_960, 28_968
		case 1:
			o, h, l, c = 29_015, top, top-9.5, 29_018
		case 7:
			o, h, l, c = 28_990, 29_010, 29_010-big, 29_005
		case 10:
			o, h, l, c = 28_990, 29_008, 29_008-big2, 29_000
		}
		bars = append(bars, m60p1K(m60p1At(9, 44)+int64(i)*60_000, o, h, l, c))
	}
	return bars
}

// TestM61SmallRangeBigCandlesRefused — G33 D4.2-R33 [D4.2 p2 @06:16–06:30]: span
// 61.50 with 33.75- and 34.50-pt candles → the long ISB at 29,000 is refused
// range_too_small_big_candles; span 62.00 → allowed; only ONE candle >= 30
// (the other 29.75) → allowed (review round: one outlier is not "the candles");
// a closed 1m CLOSE outside (28,955) clears it (D4.1-R34).
func TestM61SmallRangeBigCandlesRefused(t *testing.T) {
	// Release #61 final: the default is OFF (the course gives no candle count
	// or breakout margin; three review rounds of mis-latches) — the behaviour
	// below is the knob set ON explicitly.
	if DefaultConfig().SmallRangeGuard {
		t.Fatal("G33: mentor_small_range_guard must default OFF (release #61 final)")
	}
	bars := m61p1SmallRange(29_021.50, 33.75, 34.50)
	lo, hi, ok, _ := smallRangeWindow(bars)
	if !ok || lo != 28_960 || hi != 29_021.5 {
		t.Fatalf("want the too-small range 28,960.00–29,021.50, got %v %v %v", lo, hi, ok)
	}
	off := New(DefaultConfig())
	off.advanceSmallRange(bars)
	if off.State.SmallRange != nil {
		t.Fatalf("default (knob OFF) never builds the state, got %+v", off.State.SmallRange)
	}
	onCfg := DefaultConfig()
	onCfg.SmallRangeGuard = true
	e := New(onCfg)
	e.advanceSmallRange(bars)
	out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 29_000)}, nil)
	if m61p1Has(out, SideLong) || e.State.Refusals["range_too_small_big_candles"] != 1 {
		t.Fatalf("the long ISB at 29,000 inside the small range must be refused; out %+v refusals %v", out, e.State.Refusals)
	}
	if out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 29_030)}, nil); !m61p1Has(out, SideLong) {
		t.Fatal("an entry priced outside the range is not touched")
	}
	if _, _, ok, _ := smallRangeWindow(m61p1SmallRange(29_022, 33.75, 34.50)); ok {
		t.Fatal("span 62.00 is not the too-small range")
	}
	if _, _, ok, _ := smallRangeWindow(m61p1SmallRange(29_021.50, 29.75, 34.50)); ok {
		t.Fatal("one candle >= 30 pts (the other 29.75) is not the too-small range")
	}
	exit := m60p1K(m60p1At(9, 59), 28_962, 28_963, 28_950, 28_955)
	e.advanceSmallRange(append(bars, exit))
	if e.State.SmallRange != nil || e.State.Counters["range_small_cleared"] != 1 {
		t.Fatalf("a 1m close at 28,955 (outside) clears the range, got %+v counters %v", e.State.SmallRange, e.State.Counters)
	}

	// Wired in Tick: a standing small range over the fixture refuses its ISB;
	// knob off → today.
	for _, on := range []bool{true, false} {
		f, fb, now := mtfFixture(t, false)
		f.Cfg.SmallRangeGuard = on
		f.State.SmallRange = &SmallRange{Lo: 95, Hi: 110, At: fb[0].OpenTime}
		got := f.Tick(fb, now)
		if on && (m60p1ISB(got, SideLong) != nil || f.State.Refusals["range_too_small_big_candles"] == 0) {
			t.Fatalf("knob on: the ISB inside the standing small range must be refused; got %+v refusals %v", entryIntents(got), f.State.Refusals)
		}
		if !on && m60p1ISB(got, SideLong) == nil {
			t.Fatalf("knob off: today — the ISB emits; refusals %v", f.State.Refusals)
		}
	}
}

// ── G37 / G43 ───────────────────────────────────────────────────────────────

// TestM61MidRangeUsesFlippedRole — G37 D3.2-R40 [D3.2 p2 @07:50–09:14] + the
// Uno flip D1.3-R21 [D1.3 p1 @12:31–12:58]: an FTGH escaped UP (Flipped →
// effective FTGL, support) below price + an FTGH above → price is between two
// boxes and the PHL is refused phl_mid_range_boxed. Before the fix the flipped
// box was read as a ceiling → no mid-range.
func TestM61MidRangeUsesFlippedRole(t *testing.T) {
	boxes := []Box{
		{Kind: FTGH, Top: 29_380.50, Bottom: 29_368.50, Flipped: true, Key: "ftgh:29380.5:29368.5"},
		{Kind: FTGH, Top: 29_478.50, Bottom: 29_463.00, Key: "ftgh:29478.5:29463"},
	}
	if !midRangeBoxed(boxes, 29_420) {
		t.Fatal("a flipped FTGH below price is a FLOOR: price 29,420 sits between two boxes")
	}
	e := New(DefaultConfig())
	cur := m60p1K(m60p1At(10, 0), 29_418, 29_422, 29_415, 29_420)
	if name, _ := e.phlLocationBan(Level{Key: "k", Kind: KindKeyLevel, Price: 29_420}, SideLong, 29_420, cur, nil, boxes); name != "phl_mid_range_boxed" {
		t.Fatalf("the PHL between the flipped floor and the ceiling must be phl_mid_range_boxed, got %q", name)
	}
	// The sticky flip: the real 10-13 08:44 tape — FTGL 30,591.25–30,754.50
	// below, an FTGH 30,894.25–30,901.75 that once escaped UP (Flipped) and
	// that price has since fallen back under (close 30,774.25). The box above
	// is the ceiling by the side of price, so this is still mid-range (the
	// pre-#61 b.Kind read said so too; a boxEffectiveKind read would not).
	real1013 := []Box{
		{Kind: FTGL, Top: 30_754.50, Bottom: 30_591.25, Key: "ftgl:30754.50:30591.25"},
		{Kind: FTGH, Top: 30_901.75, Bottom: 30_894.25, Flipped: true, Key: "ftgh:30901.75:30894.25"},
	}
	if !midRangeBoxed(real1013, 30_774.25) {
		t.Fatal("10-13 08:44: price between the FTGL below and the (flipped) FTGH above is mid-range")
	}
	// The pre-#61 hook reproduces the old read (the identity pins use it).
	midRangeLegacyRole.Store(true)
	defer midRangeLegacyRole.Store(false)
	boxConflictLegacySkip.Store(true) // release #61 review round 2 (no knob): the pre-#61 conflict skip of the box-return walk
	defer boxConflictLegacySkip.Store(false)
	if midRangeBoxed(boxes, 29_420) {
		t.Fatal("legacy hook: the pre-#61 b.Kind read must say no mid-range here")
	}
}

// TestM61MidRangeTradesWithRejection — G43 D2.2-R71 [D2.2 p3 @14:28–15:58]:
// FTGL 29,368.50–29,380.50, FTGH 29,463.00–29,478.50; the 10:00 candle H29,465
// C29,455 rejects the FTGH → a LONG ISB at 29,420 (mid-range) is refused
// mid_range_against_rejection, a SHORT one passes; once a candle touches the
// FTGL top (L29,379) the long passes again. Knob off → today.
func TestM61MidRangeTradesWithRejection(t *testing.T) {
	if !DefaultConfig().MidRangeRejectSide {
		t.Fatal("G43: mentor_mid_range_reject_side must default ON")
	}
	boxes := []Box{
		{Kind: FTGL, Top: 29_380.50, Bottom: 29_368.50, Key: "ftgl:29380.5:29368.5"},
		{Kind: FTGH, Top: 29_478.50, Bottom: 29_463.00, Key: "ftgh:29478.5:29463"},
	}
	rej := m60p1K(m60p1At(10, 0), 29_450, 29_465, 29_448, 29_455)
	for _, on := range []bool{true, false} {
		cfg := DefaultConfig()
		cfg.MidRangeRejectSide = on
		e := New(cfg)
		e.advanceMidRangeReject(rej, boxes)
		out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 29_420), m61p1Entry(SideShort, 29_430)}, boxes)
		if !on {
			if !m61p1Has(out, SideLong) || e.State.MidRangeReject != nil {
				t.Fatalf("knob off: today — the long passes and no state; out %+v", out)
			}
			continue
		}
		if r := e.State.MidRangeReject; r == nil || r.Side != SideShort {
			t.Fatalf("the FTGH reject must record a SHORT rejection, got %+v", r)
		}
		if m61p1Has(out, SideLong) || !m61p1Has(out, SideShort) || e.State.Refusals["mid_range_against_rejection"] != 1 {
			t.Fatalf("mid-range after an FTGH reject: long refused, short allowed; out %+v refusals %v", out, e.State.Refusals)
		}
		// Not mid-range (above the FTGH) → not this rule's business.
		if out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 29_490)}, boxes); !m61p1Has(out, SideLong) {
			t.Fatal("an entry outside the two boxes is not refused by the mid-range rejection")
		}
		// The touch candle closes back above the FTGL top: it ends the short
		// rejection AND is itself an FTGL reject (the mirror) → long.
		touch := m60p1K(m60p1At(10, 5), 29_400, 29_402, 29_379, 29_385)
		e.advanceMidRangeReject(touch, boxes)
		if r := e.State.MidRangeReject; r == nil || r.Side != SideLong || e.State.Counters["mid_range_reject_done"] != 1 {
			t.Fatalf("a candle touching the FTGL top ends the short rejection (and rejects the floor → long), got %+v counters %v", r, e.State.Counters)
		}
		if out := e.courseRangeFilter([]Intent{m61p1Entry(SideLong, 29_420)}, boxes); !m61p1Has(out, SideLong) {
			t.Fatal("after the FTGL touch the long passes")
		}
	}
}

// TestM61BreakLockRefusesRollingRungAfterFiveMinBreakDown — G21 D4.1-R31/R33
// meets the D2.2 rolling ladder. The m464 worked-buy tape as first written
// interpolated a 10:05 5m candle that is an inside bar of the 10:00 one (5m box
// 29,410.00–29,433.25, long); the 10:11 pullback CLOSES 29,400 below it — a
// break DOWN ("break DOWN → inside bar short / PLH" [D4.1 p1 @18:29–20:33]) —
// so the 10:14 buy rung is refused break_counter_setup until a close back at
// or above 29,410. The m464 pins now open the interpolated 10:05 candle above
// the 10:00 high (no 5m inside bar, as on the lesson's 1m chart); this pin
// keeps the original interpolation. Knob off → the rung places (today).
func TestM61BreakLockRefusesRollingRungAfterFiveMinBreakDown(t *testing.T) {
	for _, on := range []bool{true, false} {
		bars := m464CourseTape()
		bars[m464Idx(10, 5)].Open, bars[m464Idx(10, 5)].Low = 29_424, 29_420 // the original interpolation
		e := m464Evaluator(SideLong, Level{Key: "old-high:29433.25", Kind: KindOldExtreme, Role: OldHigh, Price: 29433.25}, bars[m464Idx(10, 7)].OpenTime)
		e.Cfg.BreakDirectionLock = on
		steps := m464Run(e, bars, m464Idx(10, 7), m464Idx(10, 14))
		last := steps[len(steps)-1]
		if on {
			if len(last.places) != 0 || e.State.Refusals["break_counter_setup"] == 0 {
				t.Fatalf("knob on: the 10:14 buy rung after the 5m box broke DOWN must be refused break_counter_setup; places %+v refusals %v", last.places, e.State.Refusals)
			}
			if l := e.State.BreakLock; l == nil || l.Side != SideShort || l.Src != "isb_5m" || l.Lo != 29_410 {
				t.Fatalf("knob on: want the short isb_5m lock from 29,410, got %+v", l)
			}
		} else if len(last.places) != 1 {
			t.Fatalf("knob off: the 10:14 rung places as before, steps %+v", steps)
		}
	}
}

// ── the knob-honesty proof ──────────────────────────────────────────────────

// m466P1PreValues sets every m466 P1 knob to its pre-#61 value (the identity
// pins chain it after m465PreValues; G37 has no knob — its test-only hook
// midRangeLegacyRole is set by the caller).
func m466P1PreValues(cfg *Config) {
	cfg.ISBBoxEscapeMode = ISBBoxEscapeBody
	cfg.FiveMinBoxSource = FiveMinBoxFirst
	cfg.BreakDirectionLock = false
	cfg.SmallRangeGuard = false
	cfg.MidRangeRejectSide = false
}

const m466Pre61GoldenPath = "testdata/m466_pre61_tick_identity.golden.gz"

// TestM61PreValuesReproduceThePre61Golden — every m466 P1 knob at its pre-#61
// value (+ the G37 legacy hook) — at the release #61 merged head chained with
// the P2/P3/P4 pre-values (m466Rel61PreValues) — reproduces the #60 m434 golden (45053f78a,
// saved gzipped before the regeneration) byte for byte — intents, state,
// notes, levels and trendlines on every tick of the four real-tape cases. So
// each behaviour change of this package is exactly its knob (G37: its hook).
func TestM61PreValuesReproduceThePre61Golden(t *testing.T) {
	// frozen pre-October golden: pin the September 15:00 last-candle end (m472 A7 [Oct D5.2 @ 02:14-02:40] moved it to 15:15)
	defer func(v int) { keyLevelLastCandleEndMin = v }(keyLevelLastCandleEndMin)
	keyLevelLastCandleEndMin = 15 * 60
	boxPeak1Level = false    // pre-m473 B2 construction: the frozen golden predates peak-1 / level pairing
	firstRTH5mNotISB = false // pre-m473 B4: the frozen golden predates the 08:30-candle-is-not-ISB rule [Oct D4.3 @ 57:20-57:35]
	t.Cleanup(func() { boxPeak1Level = true; firstRTH5mNotISB = true })
	raw, err := os.Open(m466Pre61GoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	gz, err := gzip.NewReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		ln := sc.Text()
		name := strings.SplitN(ln, " ", 2)[0]
		want[name] = append(want[name], ln)
	}
	midRangeLegacyRole.Store(true)
	defer midRangeLegacyRole.Store(false)
	boxConflictLegacySkip.Store(true) // release #61 review round 2 (no knob): the pre-#61 conflict skip of the box-return walk
	defer boxConflictLegacySkip.Store(false)
	for _, c := range m434Cases() {
		c := c
		base := c.cfg
		c.cfg = func() Config { cfg := base(); m466Rel61PreValues(&cfg); return cfg }
		t.Run(c.name, func(t *testing.T) {
			got := m434Digest(t, c)
			w := want[c.name]
			if len(got) != len(w) || len(w) == 0 {
				t.Fatalf("%s: %d ticks, the pre-#61 golden has %d", c.name, len(got), len(w))
			}
			for i := range got {
				if got[i] != w[i] {
					t.Fatalf("%s: pre-#61 knob values diverge from the #60 golden at row %d\n got  %s\n want %s", c.name, i, got[i], w[i])
				}
			}
		})
	}
}

// smallRangeOnConfig is DefaultConfig with the G33 too-small-range guard
// switched ON. Release #61 final made the guard default OFF (D4.2-R33
// [D4.2 p2 @06:16–06:30] gives no candle count or breakout margin), so every
// test of the guard's own behaviour sets the knob explicitly.
func smallRangeOnConfig() Config {
	cfg := legacy2mCfg()
	cfg.SmallRangeGuard = true
	return cfg
}
