package kernel

import (
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
	if w := BarsHoleWarning([]market.Kline{a, b}, 1); w == "" {
		t.Fatalf("expected a hole warning for a 10-minute gap inside RTH")
	}
}

// TestBarsHoleWarningNoSessionBreak pins the false-positive guard: the
// 16:00→17:00 CT maintenance break is NOT a hole.
func TestBarsHoleWarningNoSessionBreak(t *testing.T) {
	ct := time.Date(2026, 10, 20, 16, 0, 0, 0, CTLocation()) // globex close
	next := ct.Add(60 * time.Minute)                         // 17:00 CT reopen, same day
	a := market.Kline{OpenTime: ct.UnixMilli()}
	b := market.Kline{OpenTime: next.UnixMilli()}
	if w := BarsHoleWarning([]market.Kline{a, b}, 1); w != "" {
		t.Fatalf("session break must not warn, got %q", w)
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
