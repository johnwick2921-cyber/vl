package mentor

import (
	"testing"

	"vl/market"
)

// TestM473B3FiveMinBoxDeletedOnlyOnFiveMinuteClose — m473 B3, the worked
// example of [Oct D4.3 @ 65:00-66:13] (OctD4.3-R67): "Nhưng mình sẽ chỉ xóa
// box khi nến 5 phút đóng bên ngoài cái box... cái nến 1 phút nó đóng bên ngoài
// rồi mình xóa cái box không hợp lý". Frame: oct/scenes/Ngày 4.3 Multi Time
// Frame/s_03927.jpg (Mon 02 Nov 5m box top 28,727.50 bottom 28,639.75, the 5m
// close below at ~11:20). Goes through the production path: DefaultConfig +
// Evaluator.Tick, nothing hand-built but the standing box.
//
//   - a 1m CLOSE below 28,639.75 mid-bucket -> trade permission (the break lock
//     says SHORT) AND the box still exists, no longer gating;
//   - the 5m candle (11:15-11:19) closing below -> the box is deleted.
func TestM473B3FiveMinBoxDeletedOnlyOnFiveMinuteClose(t *testing.T) {
	if d := DefaultConfig().ISBBoxEscapeMode; d != ISBBoxEscapeCloseBoxTF {
		t.Fatalf("B3 D4.3-R67: default ISB box escape mode must be %q, got %q", ISBBoxEscapeCloseBoxTF, d)
	}
	e := m60p1Eval(t, SideShort, 28_800, SideShort, SideShort, m60p1Levels())
	e.Cfg.Early15mRead = false
	box := ISBBox{High: 28_727.50, Low: 28_639.75, Dir: SideShort, AtTime: m60p1At(10, 55)}
	e.State.ISBBox = &box
	e.State.LastISBBoxAt = box.AtTime
	e.State.LastISBBoxDir = box.Dir

	k := func(h, m int, o, hi, lo, c float64) market.Kline { return m60p1K(m60p1At(h, m), o, hi, lo, c) }
	tape := []market.Kline{
		k(11, 10, 28_700, 28_705, 28_690, 28_695),
		k(11, 11, 28_695, 28_700, 28_680, 28_685),
		k(11, 12, 28_685, 28_690, 28_670, 28_675),
		k(11, 13, 28_675, 28_680, 28_655, 28_660),
		k(11, 14, 28_660, 28_665, 28_645, 28_650),
		k(11, 15, 28_650, 28_655, 28_642, 28_645), // 5m bucket 11:15 opens inside
		k(11, 16, 28_645, 28_646, 28_636, 28_638), // 1m CLOSE 28,638 < 28,639.75, mid-bucket
	}
	for i := range tape {
		e.Tick(tape[:i+1], BarCloseInstant(tape[i]))
	}
	if e.State.ISBBox == nil {
		t.Fatal("D4.3-R67: a 1m close below the box must NOT delete it - only a 5m close does")
	}
	if e.State.BreakLock == nil || e.State.BreakLock.Side != SideShort || e.State.BreakLock.Src != "isb_5m" {
		t.Fatalf("D4.3-R67: the 1m close below must give trade permission (break lock SHORT isb_5m), got %+v", e.State.BreakLock)
	}
	if e.box5() != nil {
		t.Fatal("a box already broken by a 1m close must no longer gate trades")
	}

	// the rest of the 11:15 bucket; its 5m candle CLOSES at 28,628 < 28,639.75.
	rest := []market.Kline{
		k(11, 17, 28_638, 28_640, 28_630, 28_632),
		k(11, 18, 28_632, 28_634, 28_626, 28_630),
		k(11, 19, 28_630, 28_632, 28_624, 28_628),
		k(11, 20, 28_628, 28_630, 28_622, 28_626),
	}
	for i := range rest {
		tape = append(tape, rest[i])
		e.Tick(tape, BarCloseInstant(rest[i]))
	}
	if e.State.ISBBox != nil {
		t.Fatalf("D4.3-R67: the 5m candle closed below the box - it must be deleted, got %+v", e.State.ISBBox)
	}
	if e.State.LastISBBoxEscape != SideShort {
		t.Fatalf("the resolved box direction must read short, got %q", e.State.LastISBBoxEscape)
	}
}
