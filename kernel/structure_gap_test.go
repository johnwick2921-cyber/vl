package kernel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestBarsHoleWarningFindsOpenMinuteGap pins m134 (c): a gap inside RTH (all
// open minutes) must warn — the restart chart hole.
func TestBarsHoleWarningFindsOpenMinuteGap(t *testing.T) {
	ct := time.Date(2026, 10, 20, 10, 0, 0, 0, CTLocation()) // a Tuesday, RTH open
	a := market.Kline{OpenTime: ct.UnixMilli()}
	b := market.Kline{OpenTime: ct.Add(10 * time.Minute).UnixMilli()}
	w := BarsHoleWarning([]market.Kline{a, b}, 1)
	if w == "" {
		t.Fatalf("expected a hole warning for a 10-minute gap inside RTH")
	}
	if strings.Contains(w, "CT CT") {
		t.Fatalf("double CT suffix: %q", w)
	}
}

// TestBarsHoleWarningNoSessionBreak pins the false-positive guard: the
// 15:59 (last pre-break bar) → 17:00 CT maintenance break is NOT a hole.
func TestBarsHoleWarningNoSessionBreak(t *testing.T) {
	ct := time.Date(2026, 10, 20, 15, 59, 0, 0, CTLocation())  // last pre-break bar
	next := time.Date(2026, 10, 20, 17, 0, 0, 0, CTLocation()) // reopen
	a := market.Kline{OpenTime: ct.UnixMilli()}
	b := market.Kline{OpenTime: next.UnixMilli()}
	if w := BarsHoleWarning([]market.Kline{a, b}, 1); w != "" {
		t.Fatalf("session break must not warn, got %q", w)
	}
}

// TestBarsHoleWarningCountsOpenMinutesOnly pins m149: a 15:50→17:00 gap must
// count only the CME-open missing minutes (9), not the raw 69-minute gap.
func TestBarsHoleWarningCountsOpenMinutesOnly(t *testing.T) {
	ct := time.Date(2026, 10, 20, 15, 50, 0, 0, CTLocation())
	next := time.Date(2026, 10, 20, 17, 0, 0, 0, CTLocation())
	w := BarsHoleWarning([]market.Kline{{OpenTime: ct.UnixMilli()}, {OpenTime: next.UnixMilli()}}, 1)
	if w == "" {
		t.Fatal("expected a warning")
	}
	expected := 0
	for m := ct.UnixMilli() + 60_000; m < next.UnixMilli(); m += 60_000 {
		if IsCMEOpen(time.UnixMilli(m)) {
			expected++
		}
	}
	want := fmt.Sprintf("(%d open minute(s) missing)", expected)
	if !strings.Contains(w, want) {
		t.Fatalf("count must be CME-open minutes %q, got %q", want, w)
	}
	if strings.Contains(w, "69") {
		t.Fatalf("must not report the raw 69-minute gap, got %q", w)
	}
}

// TestBarsHoleWarningLatestHoleOnly pins m149: only a hole inside the last 6h
// is reported — a hole older than 6h (stale) must not warn for hours.
func TestBarsHoleWarningLatestHoleOnly(t *testing.T) {
	ct := time.Date(2026, 10, 20, 10, 0, 0, 0, CTLocation()) // a Tuesday, RTH open
	old := market.Kline{OpenTime: ct.UnixMilli()}            // a hole 10:00→10:10
	oldNext := market.Kline{OpenTime: ct.Add(10 * time.Minute).UnixMilli()}
	// ... then 7 hours of contiguous bars up to 17:20.
	recent := []market.Kline{old, oldNext}
	last := oldNext.OpenTime
	for m := last + 60_000; m <= oldNext.OpenTime+7*60*60*1000; m += 60_000 {
		recent = append(recent, market.Kline{OpenTime: m})
	}
	// The 10:00 hole is now older than 6h (newest is ~17:20) → no warning.
	if w := BarsHoleWarning(recent, 1); w != "" {
		t.Fatalf("a hole older than 6h must not warn, got %q", w)
	}
}

// TestBarsHoleWarningNoEarlyClose pins m168 (b): a shortened-day gap from the
// last pre-close bar to the daily reopen is NOT a hole — the early-close tail is
// halted, so no OPEN minute is missing. BarsHoleWarning must respect early-close
// days, not just the 16:00/17:00 daily break.
func TestBarsHoleWarningNoEarlyClose(t *testing.T) {
	// 2026-12-24 is Christmas Eve — a Thursday, shortened, close 12:15 CT, then
	// the ordinary daily reopen at 17:00 CT.
	ct := time.Date(2026, 12, 24, 12, 14, 0, 0, CTLocation())  // last pre-close bar
	next := time.Date(2026, 12, 24, 17, 0, 0, 0, CTLocation()) // daily reopen
	a := market.Kline{OpenTime: ct.UnixMilli()}
	b := market.Kline{OpenTime: next.UnixMilli()}
	if w := BarsHoleWarning([]market.Kline{a, b}, 1); w != "" {
		t.Fatalf("an early-close gap must not warn (no open minute missing), got %q", w)
	}
}

// TestBarsHoleWarningContiguous pins the no-hole case.
func TestBarsHoleWarningContiguous(t *testing.T) {
	ct := time.Date(2026, 10, 20, 10, 0, 0, 0, CTLocation())
	bars := []market.Kline{
		{OpenTime: ct.UnixMilli()},
		{OpenTime: ct.Add(time.Minute).UnixMilli()},
		{OpenTime: ct.Add(2 * time.Minute).UnixMilli()},
	}
	if w := BarsHoleWarning(bars, 1); w != "" {
		t.Fatalf("contiguous series must not warn, got %q", w)
	}
}
