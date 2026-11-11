package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// round-2 D3-G7 — the D14 flip is reversible. After a close above the FTGH the
// mentor gives two ways [Oct D3.3 @ 37:40] (OctD3.3-R46): an ISB long, or "đợi
// nó quay về nó bắt test, nó đóng ngược lại level, thì level này lại tiếp tục là
// Resistance"; and R48 [@ 39:20]: let it drop, retest the box from below, sell
// (polyline 28,802.75 -> 28,731.25 -> 28,812.50 -> 28,686.25). A permanent flip made
// both unreachable. Tape: the m473 FTGH (peak 1 28,818, bottom 28,800), a body
// wholly ABOVE it (flip), then a body wholly BACK BELOW it (restore).

func round2FlipTape() (pre []market.Kline, escape, back market.Kline, at func(n int) time.Time) {
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, ctime())
	pre = m473Tape(t0)
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := t0.UnixMilli() + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	escape = mk(len(pre), 28819, 28826, 28818.5, 28825)
	back = mk(len(pre)+1, 28795, 28796, 28788, 28790)
	return pre, escape, back, func(n int) time.Time { return time.UnixMilli(t0.UnixMilli() + int64(n)*60_000).In(ctime()) }
}

func round2FlipRun(t *testing.T, cfg Config) (afterEscape, afterBack bool) {
	t.Helper()
	pre, escape, back, _ := round2FlipTape()
	e := New(cfg)
	bars := append([]market.Kline(nil), pre...)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	bars = append(bars, escape)
	e.Tick(append([]market.Kline(nil), bars...), escape.CloseTime+1)
	b, ok := e.State.LatchedBoxes["ftgh"]
	if !ok {
		t.Fatal("setup: no FTGH latched")
	}
	afterEscape = b.Flipped
	bars = append(bars, back)
	e.Tick(append([]market.Kline(nil), bars...), back.CloseTime+1)
	afterBack = e.State.LatchedBoxes["ftgh"].Flipped
	return afterEscape, afterBack
}

func TestRound2D3G7FlipIsReversible(t *testing.T) {
	esc, back := round2FlipRun(t, m473Cfg())
	if !esc {
		t.Fatal("a body wholly above the FTGH must flip it (D14)")
	}
	if back {
		t.Fatal("a body wholly back below the FTGH must restore its role [Oct D3.3 @ 37:40, R46 way 2]")
	}
}

func TestRound2D3G7KnobOffFlipIsPermanent(t *testing.T) {
	cfg := m473Cfg()
	cfg.BoxFlipReversible = false
	esc, back := round2FlipRun(t, cfg)
	if !esc || !back {
		t.Fatalf("knob off: the flip must stay for the box's life (esc=%v back=%v)", esc, back)
	}
}
