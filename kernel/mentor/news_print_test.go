package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// ── item 18 part 1 — the 07:30 print candle must not move the 1h/4h trigger
// lines [D4.4 p1 @18:13, @22:15], and ONLY on a red-folder print day. The
// trader plumbs the day's print windows into Config.PrintWindows; empty = no
// print today = the HTF feed is byte-identical. ───────────────────────────────

// printWindowCT returns one 07:20–07:35 CT window (print 07:30, −10m/+5m).
func printWindowCT(y int, mo time.Month, d int) PrintWindow {
	return PrintWindow{
		FromMs: auditMs(y, mo, d, 7, 20, 0),
		ToMs:   auditMs(y, mo, d, 7, 35, 0),
	}
}

// TestInPrintWindows pins the range compare (FromMs inclusive, ToMs exclusive).
func TestInPrintWindows(t *testing.T) {
	w := printWindowCT(2026, 9, 15)
	cases := []struct {
		ms int64
		in bool
	}{
		{auditMs(2026, 9, 15, 7, 19, 0), false},
		{auditMs(2026, 9, 15, 7, 20, 0), true},
		{auditMs(2026, 9, 15, 7, 30, 0), true},
		{auditMs(2026, 9, 15, 7, 34, 0), true},
		{auditMs(2026, 9, 15, 7, 35, 0), false},
		{auditMs(2026, 9, 15, 8, 0, 0), false},
	}
	for _, c := range cases {
		if got := inPrintWindows(c.ms, []PrintWindow{w}); got != c.in {
			t.Fatalf("inPrintWindows(%d) = %v, want %v", c.ms, got, c.in)
		}
	}
	// CST date (Jan): the trader computes the window in real-UTC ms via the
	// calendar's Time, so the evaluator's range compare is zone-free.
	wCST := printWindowCT(2026, 1, 15)
	if !inPrintWindows(auditMs(2026, 1, 15, 7, 30, 0), []PrintWindow{wCST}) {
		t.Fatal("07:30 CST must be inside its window")
	}
	if inPrintWindows(auditMs(2026, 1, 15, 7, 30, 0), []PrintWindow{w}) {
		t.Fatal("a Sep window must not cover a Jan instant")
	}
}

// TestHTFFeedBarsDropsPrintWindow pins the filter directly: with the day's
// window, the 07:20–07:34 bars leave the HTF feed and 07:19/07:35 stay. With
// no windows the input slice is returned untouched (byte-identical).
func TestHTFFeedBarsDropsPrintWindow(t *testing.T) {
	w := printWindowCT(2026, 9, 15)
	bars := []market.Kline{
		{OpenTime: auditMs(2026, 9, 15, 7, 19, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 20, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 30, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 34, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 35, 0)},
	}
	got := htfFeedBars(bars, []PrintWindow{w})
	if len(got) != 2 {
		t.Fatalf("htfFeedBars kept %d bars, want 2 (07:19 and 07:35)", len(got))
	}
	if got[0].OpenTime != bars[0].OpenTime || got[1].OpenTime != bars[4].OpenTime {
		t.Fatalf("htfFeedBars kept the wrong bars: %+v", got)
	}

	// No windows (non-print day / no calendar) → the input slice itself.
	if got := htfFeedBars(bars, nil); &got[0] != &bars[0] || len(got) != len(bars) {
		t.Fatal("no windows must return the input slice untouched")
	}
}

// newsTape builds 91 contiguous 1m bars: 60 bars at high 100, 30 bars at high
// 99, then one spike bar at high `spikeHigh` — the spike is the 90th bar after
// the base instant (base+90m). At base 06:00 CT the spike lands on the 07:30
// print candle; the same tape with no print window is the NON-print-day control.
func newsTape(baseHH, baseMM int, spikeHigh float64) []market.Kline {
	base := auditMs(2026, 9, 15, baseHH, baseMM, 0)
	bars := make([]market.Kline, 0, 91)
	add := func(i int, o, h, l, c float64) {
		ot := base + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c})
	}
	for i := 0; i < 60; i++ {
		add(i, 99, 100, 95, 99)
	}
	for i := 60; i < 90; i++ {
		add(i, 99, 99, 95, 99)
	}
	add(90, 102, spikeHigh, 98, spikeHigh-1)
	return bars
}

// TestPrintCandleDoesNotMoveHTFOneHour is the Tick pin: on a PRINT day the
// 07:30 spike (high 105 above the prior 1h bucket's high 100) must NOT draw
// the 1h trigger line — the evaluator drops the print-window bars.
func TestPrintCandleDoesNotMoveHTFOneHour(t *testing.T) {
	e := seedEmptySeeded()
	e.Cfg.EMALocationTFMinutes = 0
	e.Cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}
	bars := newsTape(6, 0, 105)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if e.State.HTF.OneH.Dir != "" || e.State.HTF.OneH.Price != 0 {
		t.Fatalf("the 07:30 print candle moved the 1h trigger line: %+v", e.State.HTF.OneH)
	}
}

// TestPrintCandleMovesHTFWhenNoPrintWindow is the control: the SAME tape on a
// NON-print day (no windows — also the no-calendar case) is a real break and
// draws the 1h line at the broken extreme.
func TestPrintCandleMovesHTFWhenNoPrintWindow(t *testing.T) {
	e := seedEmptySeeded()
	e.Cfg.EMALocationTFMinutes = 0
	bars := newsTape(6, 0, 105)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if e.State.HTF.OneH.Dir != SideLong || e.State.HTF.OneH.Price != 100 {
		t.Fatalf("a non-print break must draw the 1h line (long @ 100), got %+v", e.State.HTF.OneH)
	}
}

// TestSeedDropsPrintWindowAt1730 (REL10-PRINT-SEED) — the seed at 17:30 CT must
// drop the 07:30 print candle from the HTF feed, the SAME as a live tick. The
// old session-key scoping (htfFeedBarsToday) dropped NOTHING at a 17:30 boot:
// the 07:30 bar's session key is "09-15" while now's is "09-16", so the print
// candle moved the seeded 1h line and the seed disagreed with the live tick.
func TestSeedDropsPrintWindowAt1730(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PrintWindows = []PrintWindow{printWindowCT(2026, 9, 15)}
	e := New(cfg)
	bars := newsTape(6, 0, 105) // spike at 07:30, inside the 07:20–07:35 window
	now := auditMs(2026, 9, 15, 17, 30, 0)
	Seed(e, bars, now)
	if e.State.HTF.OneH.Dir != "" || e.State.HTF.OneH.Price != 0 {
		t.Fatalf("the 07:30 print candle moved the seeded 1h line at a 17:30 boot: %+v", e.State.HTF.OneH)
	}
}

// TestSeedNoWindowUnchanged — with no window the seed's HTF feed is the input
// itself (htfFeedBars returns it untouched), so the 07:30 spike is a real break
// and draws the 1h line — the no-window path is unchanged.
func TestSeedNoWindowUnchanged(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	bars := newsTape(6, 0, 105)
	now := auditMs(2026, 9, 15, 17, 30, 0)
	Seed(e, bars, now)
	if e.State.HTF.OneH.Dir != SideLong || e.State.HTF.OneH.Price != 100 {
		t.Fatalf("with no window the 07:30 spike must move the seeded 1h line (long @ 100), got %+v", e.State.HTF.OneH)
	}
}
