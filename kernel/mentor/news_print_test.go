package mentor

import (
	"testing"

	"vl/market"
)

// ── item 18 part 1 — the 07:30 print candle must not move the 1h/4h trigger
// lines [D4.4 p1 @18:13, @22:15]. The evaluator skips HTF breaks from a bucket
// whose open falls inside the 07:20–07:35 CT print window. ───────────────────

// TestNewsPrintWindowOpen pins the window edges on a CDT date (Sep) AND a CST
// date (Jan) — the CT read goes through America/Chicago, never raw epoch math.
func TestNewsPrintWindowOpen(t *testing.T) {
	cases := []struct {
		name string
		ms   int64
		in   bool
	}{
		{"07:19 CDT out", auditMs(2026, 9, 15, 7, 19, 0), false},
		{"07:20 CDT in", auditMs(2026, 9, 15, 7, 20, 0), true},
		{"07:30 CDT in (the print)", auditMs(2026, 9, 15, 7, 30, 0), true},
		{"07:34 CDT in", auditMs(2026, 9, 15, 7, 34, 0), true},
		{"07:35 CDT out (exclusive end)", auditMs(2026, 9, 15, 7, 35, 0), false},
		{"08:00 CDT out", auditMs(2026, 9, 15, 8, 0, 0), false},
		{"07:30 CST in", auditMs(2026, 1, 15, 7, 30, 0), true},
		{"07:20 CST in", auditMs(2026, 1, 15, 7, 20, 0), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := newsPrintWindowOpen(c.ms); got != c.in {
				t.Fatalf("newsPrintWindowOpen(%d) = %v, want %v", c.ms, got, c.in)
			}
		})
	}
}

// TestHTFFeedBarsDropsPrintWindow pins the filter directly: the 07:20–07:34
// bars leave the HTF feed, the 07:19 and 07:35 bars stay. A tape with no print
// bar returns the input slice untouched (no allocation, no reordering).
func TestHTFFeedBarsDropsPrintWindow(t *testing.T) {
	bars := []market.Kline{
		{OpenTime: auditMs(2026, 9, 15, 7, 19, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 20, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 30, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 34, 0)},
		{OpenTime: auditMs(2026, 9, 15, 7, 35, 0)},
	}
	got := htfFeedBars(bars)
	if len(got) != 2 {
		t.Fatalf("htfFeedBars kept %d bars, want 2 (07:19 and 07:35)", len(got))
	}
	if got[0].OpenTime != bars[0].OpenTime || got[1].OpenTime != bars[4].OpenTime {
		t.Fatalf("htfFeedBars kept the wrong bars: %+v", got)
	}

	// No print bar → the input slice itself, untouched.
	plain := []market.Kline{
		{OpenTime: auditMs(2026, 9, 15, 8, 0, 0)},
		{OpenTime: auditMs(2026, 9, 15, 8, 1, 0)},
	}
	if got := htfFeedBars(plain); &got[0] != &plain[0] {
		t.Fatal("a non-print tape must return the input slice untouched")
	}
}

// newsTape builds 91 contiguous 1m bars: 60 bars at high 100, 30 bars at high
// 99, then one spike bar at high `spikeHigh` — the spike is the 90th bar after
// the base instant (base+90m). At base 06:00 CT the spike lands on the 07:30
// print candle; at base 08:00 CT it lands on 09:30 (outside the window).
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

// TestPrintCandleDoesNotMoveHTFOneHour is the Tick pin: the 07:30 print spike
// (high 105 above the prior 1h bucket's high 100) must NOT draw the 1h trigger
// line — the evaluator drops the print-window bars from the HTF feed.
func TestPrintCandleDoesNotMoveHTFOneHour(t *testing.T) {
	e := seedEmptySeeded()
	e.Cfg.EMALocationTFMinutes = 0
	bars := newsTape(6, 0, 105)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if e.State.HTF.OneH.Dir != "" || e.State.HTF.OneH.Price != 0 {
		t.Fatalf("the 07:30 print candle moved the 1h trigger line: %+v", e.State.HTF.OneH)
	}
}

// TestNonPrintCandleMovesHTFOneHour is the control: the SAME spike outside the
// window (09:30) is a real break and draws the 1h line at the broken extreme.
func TestNonPrintCandleMovesHTFOneHour(t *testing.T) {
	e := seedEmptySeeded()
	e.Cfg.EMALocationTFMinutes = 0
	bars := newsTape(8, 0, 105)
	e.Tick(bars, bars[len(bars)-1].CloseTime+1)
	if e.State.HTF.OneH.Dir != SideLong || e.State.HTF.OneH.Price != 100 {
		t.Fatalf("a non-print break must draw the 1h line (long @ 100), got %+v", e.State.HTF.OneH)
	}
}
