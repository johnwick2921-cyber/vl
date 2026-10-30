package ninjatrader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
)

// ── 101 D3(b) — ONE HORIZON WARN PER (symbol,tf) PER WINDOW, CALLERS AGGREGATED ──
//
// Measured on the running rev: 8,258 🕳 SHORT lines since the 22:15 boot, in a
// 5.1 GB log. The dedupe key carried the CALLER, the REQUESTED count and the
// SERVED count — served grows by one every bar and requested differs per
// caller (5000 / 400 / 220), so the key was nearly unique per call and the
// dedupe suppressed almost nothing. The condition ("this ring is short") is one
// fact about one ring; the callers that noticed it are a list, not a reason to
// say it again.

func TestFourCallersInOneWindowEmitOneLineWithCallersAggregated(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	now := bhCT(2026, time.September, 16, 9, 30)

	// four callers, the same short ring, served climbing as it would live
	callers := []string{"api/handler_klines.go:377", "trader/fade_facts.go:73", "market/data.go:250", "trader/auto_trader_planner.go:2275"}
	for i, c := range callers {
		h := kernel.BarHorizon{Requested: 2400 - i*100, Served: 154 + i, GapCount: 0} // m452: inside the 2500 ring (a request beyond it is no longer a SHORT)
		barHorizonWarn(now.Add(time.Duration(i)*20*time.Second), h, "MNQ", "5m", c, 2500)
	}
	lines := get()
	if n := bhCountLines(lines, "bar horizon"); n != 1 {
		t.Fatalf("four callers inside one window must produce ONE line, got %d: %v", n, lines)
	}
	if !strings.Contains(lines[0], "callers=[api/handler_klines.go:377]") {
		t.Errorf("the first line names the caller that opened the window: %s", lines[0])
	}
	// The line that CLOSES the window is the one that can name everyone in it —
	// a line cannot list callers that have not arrived yet. Past the window a
	// fifth read re-arms the key, and that line carries the four plus itself
	// and says how many it ate.
	barHorizonWarn(now.Add(barHorizonWarnWindow+time.Second),
		kernel.BarHorizon{Requested: 2400, Served: 160}, "MNQ", "5m", "api/handler_klines.go:377", 2500)
	lines = get()
	if n := bhCountLines(lines, "bar horizon"); n != 2 {
		t.Fatalf("past the window: %d lines, want 2: %v", n, lines)
	}
	closing := lines[len(lines)-1]
	for _, c := range callers {
		if !strings.Contains(closing, c) {
			t.Errorf("the closing line must name caller %s: %s", c, closing)
		}
	}
	if !strings.Contains(closing, "callers=[") || !strings.Contains(closing, "suppressed=3") {
		t.Errorf("the closing line carries callers=[...] and suppressed=3: %s", closing)
	}
	if !strings.Contains(closing, "api/handler_klines.go:377×2") {
		t.Errorf("a caller seen twice is counted, not listed twice: %s", closing)
	}
}

// The window is FIVE minutes (D3b), and the re-armed line names what it ate
// and which callers were talking.
func TestHorizonWindowIsFiveMinutesAndReArmsWithCallers(t *testing.T) {
	resetBarHorizonWarnsForSessionDay(0)
	get := bhWarnCapture(t)
	start := bhCT(2026, time.September, 16, 9, 30)
	h := func(served int) kernel.BarHorizon {
		return kernel.BarHorizon{Requested: 2400, Served: served} // m452: inside the 2500 ring - a request BEYOND the ring is no longer a SHORT
	}
	barHorizonWarn(start, h(154), "MNQ", "5m", "api/handler_klines.go:377", 2500)
	for i := 1; i < 12; i++ { // 4m40s of further reads from two callers
		c := "market/data.go:250"
		if i%2 == 0 {
			c = "trader/fade_facts.go:73"
		}
		barHorizonWarn(start.Add(time.Duration(i)*25*time.Second), h(154+i), "MNQ", "5m", c, 2500)
	}
	if n := bhCountLines(bhFilter(get(), "api/handler_klines.go:377"), "bar horizon"); n != 1 {
		t.Fatalf("inside five minutes: %d lines, want 1", n)
	}
	barHorizonWarn(start.Add(5*time.Minute+1*time.Second), h(170), "MNQ", "5m", "api/handler_klines.go:377", 2500)
	lines := bhFilter(get(), "api/handler_klines.go:377")
	if n := bhCountLines(lines, "bar horizon"); n != 2 {
		t.Fatalf("past five minutes the key must speak again: %d lines: %v", n, lines)
	}
	// "suppressed=11 since" — the re-armed line's form. The bare substring
	// also matches the boot-totals tail ("… suppressed=11") once -count=N
	// reuses the binary and totals carry over from the prior iteration.
	if bhCountLines(lines, "suppressed=11 since") != 1 {
		t.Errorf("the re-armed line must say how many it ate: %v", lines)
	}
}
