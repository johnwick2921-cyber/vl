package mentor

import (
	"fmt"
	"math"

	"vl/market"
)

// ISB at a level it just broke — oct-exact D1 (knob mentor_isb_just_broken_level,
// default ON; OFF reverts to today: nothing here runs).
//
// October D3.3: price broke the level, reversed back through it, and an inside
// bar formed right there is "hoan toan khong duoc ... khi ma no break level xong
// no dong nguoc lai, dung bao gio bat kieu nay" [OctD3.3-R47 @38:10, the 10-30
// ~09:04 FTGH break-and-reverse ISB, closes 28,802.75]; "do NOT take the inside-bar
// short when the inside bar sits too close to the level ... wait: price drops away,
// comes back UP to retest the level from below, and only then sell down; a bare
// close below the level is not the entry" [R2 @00:46, R3 @01:00]. The way back in
// is the retest [R48 @39:20]. The mentor gives NO points distance, so none is
// invented here.
//
// The rule, on the two candles the ISB is read on: the MOTHER candle (prev) just
// crossed the box with its BODY (open at or beyond the near edge, close through
// it), and the inside bar (cur) has not retested the box since. A box is a zone:
// the edge facing the ISB is the one price must get back to (a short ISB under the
// box retests its BOTTOM edge, a long ISB above it its TOP edge). A retest (the
// inside bar's own wick reaching the edge) lifts it, and the check is stateless (it
// reads only prev and cur), so the next candle and the session roll reset it too.
//
// SCOPE of THIS function (mentor_isb_just_broken_scope=box, the revert): the
// FTGH/FTGL box edges only. It was built box-only because the mentor's own D2.2 E9
// trade (a short ISB at 28,709.5 on 10-30 08:34) came right after a candle closed
// through the 28,743.9 trendline. ISB-M1 (scope all, the default since the 11-16
// full re-read) separates E9 from the refused pictures WITHOUT a number: E9's
// inside bar (H 28,735.75) is clear of the line, while Oct D3.3 @00:50 (the line
// runs through the 21:18 inside candle) and the 11-16 19:13 inside candle (on key
// 27,359) sit ON it. See justBrokenAll.
func justBrokenLevel(side Side, prev, cur market.Kline, boxes []Box) string {
	if side != SideShort && side != SideLong {
		return ""
	}
	for _, b := range boxes {
		p := b.Bottom
		if side == SideLong {
			p = b.Top
		}
		if p <= 0 {
			continue
		}
		var crossed, retested bool
		if side == SideShort {
			crossed = prev.Open >= p && prev.Close < p
			retested = cur.High >= p
		} else {
			crossed = prev.Open <= p && prev.Close > p
			retested = cur.Low <= p
		}
		if crossed && !retested {
			return fmt.Sprintf("%s box edge %.2f", b.Kind, p)
		}
	}
	return ""
}

// ISB-M1 scope (knob mentor_isb_just_broken_scope).
const (
	// JustBrokenScopeAll (default): every level the mother's BODY broke — key
	// level, old extreme, valid trendline, FTGH/FTGL box edge — refuses an inside
	// bar that sits ON it (justBrokenLevelAt), plus the box break-and-reverse
	// (justBrokenBoxReverse).
	JustBrokenScopeAll = "all"
	// JustBrokenScopeBox reverts to the oct-exact D1 build: box edges only,
	// mother-crosses + no wick retest (justBrokenLevel).
	JustBrokenScopeBox = "box"
)

// justBrokenCtx is what the hook reads on one ISB candidate.
type justBrokenCtx struct {
	// hist: the execution-frame candles, oldest first, ending with the inside
	// bar (hist[len-1]) after its mother (hist[len-2]).
	hist  []market.Kline
	boxes []Box
	// levels: the tick's level set; only key levels and old extremes are read
	// here (box edges come from boxes, trendlines from trendlines).
	levels     []Level
	trendlines []Trendline
}

// jbClose is a candle's close instant for a sloping line (its open when the
// close time is not on the candle).
func jbClose(c market.Kline) int64 {
	if c.CloseTime > c.OpenTime {
		return c.CloseTime
	}
	return c.OpenTime
}

// justBrokenAt — ISB-M1 (+ J-BOX-14, TL-M10), oct-exact. Oct D3.3 @00:16-02:56
// (frame 00:50: 01 Nov 21:18, the yellow TRENDLINE runs through the inside
// candle): "no la INSABA cho lenh soc ... khong sai. Nhung ... Insider 3 qua gan
// ... minh dang o giua cai level ... dung vo, doi ... no bat test nguoc lai" (it
// IS an ISB, but it is too close, IN the middle of the level: don't enter, wait for
// the retest); "dung bat INSABA ngay cho no vua break" (@02:40, don't take the ISB
// right where it just broke); restated for the box @39:20-39:52 "Dung bao gio no
// vua break level ra, no vua quay lai level ... cai level ngay day no gan qua".
//
// The level at[] (a price function of time, so a sloping trendline reads its own
// value on each candle) is refused when:
//   - it lies INSIDE the inside bar's [Low, High] (the inside bar sits ON it —
//     "o giua cai level"); the inside bar's own wick reaching it is NOT a retest;
//   - and the most recent candle whose BODY crossed it (open on one side, close
//     on the other) is the mother, or an earlier candle with every candle since
//     still on the level (J-BOX-14: the ISB one or more candles after the break,
//     still at the edge, no retest).
//
// It lifts — no number, the mentor gives none — when:
//   - the inside bar forms CLEAR of the level ("thà là anh chi du in sai ba o xa
//     level" @01:40; the drop-away ISB "no cu drop drop drop, thi khi nao xuat hien
//     Insaba thi minh vo" @40:07-40:20; the ISB long wholly above the box @37:00-51);
//   - or a candle between the break and the inside bar was clear of the level
//     (price moved away), so the inside bar is the return to it — the retest the
//     mentor waits for (@01:00, @39:20).
//
// The break DIRECTION is part of the rule (review P1-1, 11-17): D3.3 @00:16-02:47
// refuses the ISB in the direction of the break ("no dong o duoi ... INSABA cho
// lenh sóc ... dung vo" — the short after a close under; the trap is that price
// "dut nguoc len", rips back the other way). A cross counts only when it went
// the trade's way: a long refuses on an UP cross (open at/below the level,
// close above), a short on a DOWN cross. If the most recent body cross went the
// OTHER way, the inside bar is a trade against the break — the reverse ISB read
// as the with-trend PHL/PLH (D5.3 @17:40-18:30, the 11-16 19:17 PHL long) — and
// no October source refuses that here: allowed.
//
// from: the instant the level exists (a candle before it cannot have broken it).
func justBrokenAt(side Side, hist []market.Kline, at func(int64) float64, from int64) bool {
	n := len(hist)
	if n < 2 {
		return false
	}
	touches := func(c market.Kline) bool {
		a, b := at(c.OpenTime), at(jbClose(c))
		lo, hi := math.Min(a, b), math.Max(a, b)
		return c.Low <= hi && c.High >= lo
	}
	// crossed: 0 no body cross, -1 down through the level, +1 up through it.
	crossed := func(c market.Kline) int {
		vo, vc := at(c.OpenTime), at(jbClose(c))
		switch {
		case c.Open >= vo && c.Close < vc:
			return -1
		case c.Open <= vo && c.Close > vc:
			return 1
		}
		return 0
	}
	want := 1
	if side == SideShort {
		want = -1
	} else if side != SideLong {
		return false
	}
	if !touches(hist[n-1]) {
		return false // the inside bar is clear of the level
	}
	for i := n - 2; i >= 0; i-- {
		c := hist[i]
		if c.OpenTime < from {
			return false
		}
		if d := crossed(c); d != 0 {
			return d == want // the most recent break: refused only in the trade's direction
		}
		if !touches(c) {
			return false // price had moved away: the inside bar is the return (retest)
		}
	}
	return false
}

// justBrokenBoxReverse — the box BREAK-AND-REVERSE, Oct D3.3 E7 (10-30 09:03,
// frame 38:40/38:58): the 08:58-08:59 candles broke ABOVE the FTGH box
// 28,812.50-28,819.00, the 09:02 candle came straight back down through it and
// the 09:03 inside bar formed just under it (H 28,807.50, clear of the edge):
// "Hoan toan khong duoc ... Khi ma no break level xong roi no dong nguoc lai, dung
// bao gio bat kieu nay" (@38:58) / "Dung bao gio no vua break level ra, no vua
// quay lai level" (@39:20). It is refused even though the inside bar is clear of
// the edge. A plain break is NOT a reverse: the 08:59 candle that broke UP out of
// the box (price came from below) is followed by the allowed ISB long @37:00-51.
//
// Read: the mother's BODY crossed out of the box (down through the bottom / up
// through the top), the latest candle before it that CLOSED outside the box closed
// on the OTHER side (the mother traversed the box), and price had got to that
// other side by a traversal too: an earlier close (since the box formed) on the
// mother's side. That earlier traversal is the break, the mother's is the reverse.
// E7: 08:57 closed under the box, 08:59-09:01 over it (the break), 09:02 back under
// (the reverse) — refused. 09:00: 08:57 under, 08:59 over, and nothing over the box
// before since it formed — one traversal, a break, not a reverse — allowed.
//
// Side (review P1-1, 11-17): the reverse refuses the ISB in the mother's
// direction only (E7: the mother closed out UNDER the box -> the short). A trade
// against the mother's traversal is not the E7 picture and is not refused here.
func justBrokenBoxReverse(side Side, hist []market.Kline, b Box) bool {
	n := len(hist)
	if n < 3 || b.Top <= 0 || b.Bottom <= 0 || b.Top < b.Bottom {
		return false
	}
	m := hist[n-2]
	var mSide int // -1 the mother closed out under the box, +1 over it
	switch {
	case m.Open >= b.Bottom && m.Close < b.Bottom:
		mSide = -1
	case m.Open <= b.Top && m.Close > b.Top:
		mSide = 1
	default:
		return false
	}
	if (mSide == -1 && side != SideShort) || (mSide == 1 && side != SideLong) {
		return false
	}
	sideOf := func(c market.Kline) int {
		switch {
		case c.Close > b.Top:
			return 1
		case c.Close < b.Bottom:
			return -1
		}
		return 0
	}
	want := -mSide // first: where the mother came from (the other side)
	for i := n - 3; i >= 0; i-- {
		c := hist[i]
		if b.FormedAtMs > 0 && c.OpenTime < b.FormedAtMs {
			return false
		}
		switch s := sideOf(c); {
		case s == 0:
			continue
		case s == want && want == -mSide:
			want = mSide // then: the break that got price there came from the mother's side
		case s == want:
			return true
		case want == -mSide:
			return false // the latest outside close is on the mother's own side: no traversal
		}
	}
	return false
}

// justBrokenAll is the scope=all read for a trade on side: "" = allowed, else
// what refused it. Only a break in the trade's direction refuses (review P1-1).
func justBrokenAll(side Side, jb justBrokenCtx) string {
	n := len(jb.hist)
	if n < 2 {
		return ""
	}
	cur := jb.hist[n-1]
	flat := func(p float64) func(int64) float64 { return func(int64) float64 { return p } }
	for _, b := range jb.boxes {
		if justBrokenBoxReverse(side, jb.hist, b) {
			return fmt.Sprintf("%s box %.2f-%.2f broke and reversed back through", b.Kind, b.Bottom, b.Top)
		}
		for _, p := range []float64{b.Bottom, b.Top} {
			if p > 0 && p >= cur.Low && p <= cur.High && justBrokenAt(side, jb.hist, flat(p), b.FormedAtMs) {
				return fmt.Sprintf("%s box edge %.2f", b.Kind, p)
			}
		}
	}
	for _, l := range jb.levels {
		if l.Kind != KindKeyLevel && l.Kind != KindOldExtreme {
			continue
		}
		if l.Price <= 0 || l.Price < cur.Low || l.Price > cur.High {
			continue
		}
		if justBrokenAt(side, jb.hist, flat(l.Price), l.AtTime) {
			return fmt.Sprintf("%s %.2f", l.Kind, l.Price)
		}
	}
	for _, tl := range jb.trendlines {
		if tl.ValidAt == 0 || tl.Dead || tl.P1T == tl.P0T {
			continue
		}
		if justBrokenAt(side, jb.hist, tl.priceAt, tl.P1T) {
			return fmt.Sprintf("trendline %s %.2f", tl.key(), tl.priceAt(cur.OpenTime))
		}
	}
	return ""
}

// isbJustBrokenRefusal is the evaluator hook: "" = allowed (knob OFF included).
// Both the normal ISB and the reverse ISB call it (eval.go).
func (e *Evaluator) isbJustBrokenRefusal(side Side, jb justBrokenCtx) string {
	if !e.Cfg.ISBJustBrokenLevel || len(jb.hist) < 2 {
		return ""
	}
	prev, cur := jb.hist[len(jb.hist)-2], jb.hist[len(jb.hist)-1]
	if e.Cfg.ISBJustBrokenScope == JustBrokenScopeBox {
		what := justBrokenLevel(side, prev, cur, jb.boxes)
		if what == "" {
			return ""
		}
		e.note("ISB %s refused: it sits at a box that just broke (%s) with no retest yet — wait for the retest [OctD3.3-R47 @38:10, R2/R3 @00:46-01:00]", side, what)
		return "isb_just_broken_level"
	}
	what := justBrokenAll(side, jb)
	if what == "" {
		return ""
	}
	e.note("ISB %s refused: the inside bar sits at a level just broken (%s), no retest yet — wait for price to move away and retest, or an ISB clear of the level [Oct D3.3 @00:46-01:00, @01:40, @02:40, @38:58-39:52; mentor_isb_just_broken_scope=box reverts]", side, what)
	return "isb_just_broken_level"
}
