package mentor

import (
	"testing"

	"vl/market"
)

// m473 round-2 B3 coverage: long side, 15m/30m boxes under the DEFAULT mode,
// sticky Escaped, and a newer pair replacing a broken box. Course rule: the
// box is deleted only when a candle of its OWN timeframe closes outside; the
// 1m close is permission [Oct D4.3 @ 65:00-66:13] (OctD4.3-R67).

func b3k(openMs int64, w int64, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: openMs, CloseTime: openMs + w - 1, Open: o, High: h, Low: l, Close: c}
}

// Long escape, every box TF (5m/15m/30m all use isbBoxStep with their own cb).
func TestM473B3LongEscapeKeepsBoxUntilOwnTFClose(t *testing.T) {
	for _, tf := range []int64{5, 15, 30} {
		w := tf * 60_000
		box := ISBBox{High: 28_700, Low: 28_650, Dir: SideLong, AtTime: m60p1At(9, 0)}
		oneMin := b3k(m60p1At(10, 1), 60_000, 28_698, 28_706, 28_697, 28_705) // 1m CLOSE above
		// own-TF candle opened after the box candle, closing INSIDE: box stays
		inside := b3k(m60p1At(10, 0), w, 28_690, 28_706, 28_680, 28_690)
		nb, dir, gone := isbBoxStep(&box, oneMin, ISBBoxEscapeCloseBoxTF, []market.Kline{inside})
		if !nb || dir != SideLong || gone || box.Escaped != SideLong {
			t.Fatalf("tf=%dm: 1m close above = permission, box stays: newBreak=%v dir=%q gone=%v escaped=%q", tf, nb, dir, gone, box.Escaped)
		}
		// own-TF candle closing outside: gone
		outside := b3k(m60p1At(10, 0), w, 28_690, 28_712, 28_680, 28_708)
		_, dir, gone = isbBoxStep(&box, oneMin, ISBBoxEscapeCloseBoxTF, []market.Kline{outside})
		if !gone || dir != SideLong {
			t.Fatalf("tf=%dm: own-TF close above must delete: gone=%v dir=%q", tf, gone, dir)
		}
	}
}

// A 1m close back inside does NOT re-arm the box ban (the video states only
// the exit and the TF-close delete; re-arming is a mentor question).
func TestM473B3EscapedIsStickyAfterBackInside(t *testing.T) {
	box := ISBBox{High: 28_700, Low: 28_650, Dir: SideLong, AtTime: m60p1At(9, 0)}
	out := b3k(m60p1At(10, 1), 60_000, 28_698, 28_706, 28_697, 28_705)
	in := b3k(m60p1At(10, 2), 60_000, 28_700, 28_701, 28_690, 28_695)
	isbBoxStep(&box, out, ISBBoxEscapeCloseBoxTF, nil)
	nb, _, gone := isbBoxStep(&box, in, ISBBoxEscapeCloseBoxTF, nil)
	if nb || gone || box.Escaped != SideLong {
		t.Fatalf("back inside must leave Escaped set and not re-break: newBreak=%v gone=%v escaped=%q", nb, gone, box.Escaped)
	}
	if liveBox(&box) != nil {
		t.Fatal("a broken box must stay out of the gates")
	}
}

// 15m box, default mode, through Tick: the 1m close gives a SHORT lock but the
// box stands; a closed 15m candle below deletes it.
func TestM473B3FifteenMinBoxDefaultModeThroughTick(t *testing.T) {
	e := m60p1Eval(t, SideShort, 29_100, SideShort, SideShort, m60p1Levels())
	e.Cfg.Early15mRead = false
	if e.Cfg.ISBBoxEscapeMode != ISBBoxEscapeCloseBoxTF {
		t.Fatalf("default must be close5m, got %q", e.Cfg.ISBBoxEscapeMode)
	}
	bx := ISBBox{High: 29_026, Low: 28_970.75, Dir: SideLong, AtTime: m60p1At(9, 30)}
	e.State.ISBBox15m = &bx
	e.State.LastISBBox15mAt = bx.AtTime
	var bars []market.Kline
	for m := 0; m < 15; m++ { // the 10:00 15m bucket: inside until the 1m at 10:02, then below
		c := 28_985.0
		if m >= 2 {
			c = 28_966
		}
		bars = append(bars, m60p1K(m60p1At(10, m), c, c+4, c-4, c))
	}
	for i := range bars {
		m60p1Run(e, bars[:i+1])
		if i == 3 {
			if e.State.ISBBox15m == nil {
				t.Fatal("a 1m close below must NOT delete the 15m box")
			}
			if l := e.State.BreakLock; l == nil || l.Side != SideShort || l.Src != "isb_15m" {
				t.Fatalf("1m close below = SHORT permission, got %+v", l)
			}
		}
	}
	bars = append(bars, m60p1K(m60p1At(10, 15), 28_966, 28_970, 28_962, 28_966)) // 10:00 bucket now closed below
	m60p1Run(e, bars)
	if e.State.ISBBox15m != nil {
		t.Fatalf("a 15m close below must delete the box, got %+v", e.State.ISBBox15m)
	}
}

// A broken-but-standing 15m / 30m box never blocks a NEWER inside-bar pair.
func TestM473B3NewerPairReplacesBrokenBox(t *testing.T) {
	m15 := int64(15 * 60_000)
	e := m60p1Eval(t, SideShort, 29_100, SideShort, SideShort, m60p1Levels())
	e.Cfg.Early15mRead = false
	old := ISBBox{High: 29_026, Low: 28_970.75, Dir: SideLong, AtTime: m60p1At(9, 30), Escaped: SideShort}
	e.State.ISBBox15m = &old
	e.State.LastISBBox15mAt = old.AtTime
	mother := b3k(m60p1At(9, 45), m15, 29_000, 29_020, 28_980, 28_985) // red
	inside := b3k(m60p1At(10, 0), m15, 28_990, 29_000, 28_990, 28_995) // body inside mother
	cur := m60p1K(m60p1At(10, 15), 28_992, 28_996, 28_990, 28_994)     // inside the old box
	e.advance15mBox(nil, []market.Kline{mother, inside}, cur)
	if e.State.ISBBox15m == nil || e.State.ISBBox15m.AtTime != inside.OpenTime || e.State.ISBBox15m.Escaped != "" {
		t.Fatalf("a newer 15m pair must replace the broken box, got %+v", e.State.ISBBox15m)
	}
}
