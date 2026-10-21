package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestEveningWindowAndSlowNight pins the X11 evening/overnight 5m schedule
// (m186 DS-108): the evening/overnight window runs 15:00 CT → 03:00 CT, and a
// SLOW night (1m range since the RTH close under 100 pts) drops the read to 5m
// while a strong night (>= 100 pts) stays on the 2m. MUTANT: raise the cutoff
// or widen the window → RED.
func TestEveningWindowAndSlowNight(t *testing.T) {
	ct := func(y int, mo time.Month, d, h, m int) int64 {
		return time.Date(y, mo, d, h, m, 0, 0, ctime()).UnixMilli()
	}

	// inEveningWindow: after 15:00 CT and before 03:00 CT.
	for _, c := range []struct {
		now  int64
		want bool
	}{
		{ct(2026, 9, 15, 18, 0), true},  // evening
		{ct(2026, 9, 15, 23, 59), true}, // late evening
		{ct(2026, 9, 16, 2, 30), true},  // overnight, before Europe open
		{ct(2026, 9, 15, 9, 0), false},  // RTH day
		{ct(2026, 9, 16, 3, 30), false}, // after Europe open
	} {
		if got := inEveningWindow(c.now); got != c.want {
			t.Fatalf("inEveningWindow(%s) = %v, want %v",
				time.UnixMilli(c.now).In(ctime()).Format("15:04"), got, c.want)
		}
	}

	// eveningSlow: a night whose 1m range since the 15:00 CT close is < 100 pts.
	evening := ct(2026, 9, 15, 18, 0)
	close15 := ct(2026, 9, 15, 15, 0)
	slow := []market.Kline{
		{OpenTime: close15, High: 120, Low: 119, Close: 119.5},
		{OpenTime: close15 + 60_000, High: 121, Low: 118, Close: 120},
	}
	if !eveningSlow(evening, slow) {
		t.Fatalf("a 2-pt night must be slow (5m), got fast")
	}
	strong := []market.Kline{
		{OpenTime: close15, High: 120, Low: 119, Close: 119.5},
		{OpenTime: close15 + 60_000, High: 230, Low: 119, Close: 225}, // 111-pt range
	}
	if eveningSlow(evening, strong) {
		t.Fatalf("a 111-pt night must be strong (2m), got slow")
	}
	// No evening bars yet → not slow (stay 2m).
	if eveningSlow(evening, nil) {
		t.Fatalf("no evening bars must not read slow")
	}
	// Outside the evening window → never slow.
	if eveningSlow(ct(2026, 9, 15, 9, 0), slow) {
		t.Fatalf("the 09:00 RTH minute must never read the evening 5m")
	}
}

// TestPullbackWarning5m pins the X11 pullback-warning size cut (m186 DS-108):
// the latest closed 5m candle breaking the PRIOR bullish 5m candle's low warns
// of a pullback ("giảm size lại cũng được"). MUTANT: drop the uptrend guard or
// the low-break → RED.
func TestPullbackWarning5m(t *testing.T) {
	b := func(o, h, l, c float64) market.Kline {
		return market.Kline{Open: o, High: h, Low: l, Close: c}
	}
	// Bullish prior candle, latest candle breaks its low → warning.
	if !pullbackWarning5m([]market.Kline{b(100, 110, 99, 105), b(105, 108, 97, 101)}) {
		t.Fatal("a 5m candle breaking the prior bullish low must warn")
	}
	// Prior candle not bullish → no warning.
	if pullbackWarning5m([]market.Kline{b(105, 110, 99, 100), b(100, 108, 97, 101)}) {
		t.Fatal("no uptrend (prior not bullish) must not warn")
	}
	// Latest low above the prior low → no warning.
	if pullbackWarning5m([]market.Kline{b(100, 110, 99, 105), b(105, 108, 100, 101)}) {
		t.Fatal("a candle holding above the prior low must not warn")
	}
	// Fewer than two candles → no warning.
	if pullbackWarning5m([]market.Kline{b(100, 110, 99, 105)}) {
		t.Fatal("one candle must not warn")
	}
}
