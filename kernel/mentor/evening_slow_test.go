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

	// eveningSlow (m408): rolling 60m window inside the 17:00 CT session only.
	mk := func(d, fromH, fromM, toH, toM int, lo, hi float64) []market.Kline {
		var out []market.Kline
		for ts := ct(2026, 9, d, fromH, fromM); ts < ct(2026, 9, d, toH, toM); ts += 60_000 {
			out = append(out, market.Kline{OpenTime: ts, Low: lo, High: hi, Close: lo})
		}
		return out
	}
	join := func(parts ...[]market.Kline) []market.Kline {
		var out []market.Kline
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	now2100 := ct(2026, 9, 15, 21, 0)

	// A quiet 20:00-21:00 after a busy 17:00-18:00 IS slow (impossible before).
	quietAfterBusy := join(mk(15, 17, 0, 18, 0, 0, 300), mk(15, 20, 0, 21, 0, 100, 110))
	if !eveningSlow(now2100, quietAfterBusy) {
		t.Fatal("quiet 20:00-21:00 after a busy 17:00-18:00 must be slow")
	}
	// Busy inside the window -> not slow.
	if eveningSlow(now2100, join(mk(15, 20, 0, 21, 0, 100, 110), mk(15, 20, 30, 20, 31, 100, 230))) {
		t.Fatal("a 130-pt range inside the last 60m must not be slow")
	}
	// 15:00-16:59 bars never count.
	dayHour := mk(15, 15, 0, 17, 0, 0, 500)
	if !eveningSlow(now2100, join(dayHour, mk(15, 20, 0, 21, 0, 100, 110))) {
		t.Fatal("15:00-16:59 bars must never count toward the evening range")
	}
	// Just after the 17:00 reopen: under one full window of evening -> 2m, even
	// when the pre-17:00 bars are flat (they must not fill the window).
	if eveningSlow(ct(2026, 9, 15, 17, 30), join(mk(15, 16, 0, 17, 0, 100, 101), mk(15, 17, 0, 17, 30, 100, 101))) {
		t.Fatal("before a full 17:00-based window has elapsed the read stays on the 2m")
	}
	if eveningSlow(ct(2026, 9, 15, 16, 30), mk(15, 15, 30, 16, 30, 100, 101)) {
		t.Fatal("the 16:00-17:00 break is never slow")
	}
	// After midnight the session is anchored to yesterday's 17:00 reopen.
	if !eveningSlow(ct(2026, 9, 16, 1, 0), mk(16, 0, 0, 1, 0, 100, 110)) {
		t.Fatal("a quiet 00:00-01:00 must be slow")
	}
	// 03:00 Europe open forces the 2m even on a dead tape.
	dead := mk(16, 1, 30, 3, 30, 100, 101)
	if eveningSlow(ct(2026, 9, 16, 3, 1), dead) || eveningSlow(ct(2026, 9, 16, 3, 30), dead) {
		t.Fatal("03:00 CT onward must force the 2m")
	}
	// Threshold: range exactly 100 is NOT slow and stays so every tick while
	// that bar is in the window (no flip-flop); 99.75 is slow.
	edge := join(mk(15, 20, 0, 21, 0, 100, 150), []market.Kline{{OpenTime: ct(2026, 9, 15, 20, 10), Low: 50, High: 150}})
	for _, off := range []int64{0, 60_000, 120_000} {
		if eveningSlow(now2100+off, edge) {
			t.Fatal("a 100-pt window range must not read slow (strictly under)")
		}
	}
	edge[len(edge)-1].Low = 50.25
	if !eveningSlow(now2100, edge) {
		t.Fatal("a 99.75-pt window range must read slow")
	}
	if eveningSlow(now2100, nil) {
		t.Fatal("no bars must not read slow")
	}
	if eveningSlow(ct(2026, 9, 15, 9, 0), mk(15, 8, 0, 9, 0, 1, 2)) {
		t.Fatal("the 09:00 RTH minute must never read the evening 5m")
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

// TestX8TrapDodge pins the X8 trap-dodge exit (m186 DS-108): a 2m close through
// the EMA34 against the 5m trigger trend, with the 5m trigger swept against the
// position, is a fake breakout → exit. MUTANTS: drop the trigger-side guard, the
// EMA-side guard, or the empty-trigger guard → RED.
func TestX8TrapDodge(t *testing.T) {
	// LONG trap: 5m trigger SELL (swept the prior low) + 2m closed ABOVE the EMA34.
	if !X8TrapDodge("long", SideShort, 100, 101) {
		t.Fatal("a 2m close above the EMA34 against a 5m sell trigger must exit the long")
	}
	// SHORT trap: 5m trigger BUY + 2m closed BELOW the EMA34.
	if !X8TrapDodge("short", SideLong, 100, 99) {
		t.Fatal("a 2m close below the EMA34 against a 5m buy trigger must exit the short")
	}
	// The 2m close THROUGH the EMA alone never fires — the 5m sweep is the trigger.
	if X8TrapDodge("long", SideShort, 100, 99) {
		t.Fatal("a long with the 2m close below the EMA must not fire")
	}
	if X8TrapDodge("short", SideLong, 100, 101) {
		t.Fatal("a short with the 2m close above the EMA must not fire")
	}
	// The 5m trigger on the same side as the position is not a trap.
	if X8TrapDodge("long", SideLong, 100, 101) {
		t.Fatal("a long with a 5m buy trigger is not a trap")
	}
	if X8TrapDodge("short", SideShort, 100, 99) {
		t.Fatal("a short with a 5m sell trigger is not a trap")
	}
	// No trigger / no EMA / no close → no exit (fail-closed).
	if X8TrapDodge("long", "", 100, 101) {
		t.Fatal("an empty trigger must never fire")
	}
	if X8TrapDodge("long", SideShort, 0, 101) {
		t.Fatal("a zero EMA must never fire")
	}
	if X8TrapDodge("long", SideShort, 100, 0) {
		t.Fatal("a zero 2m close must never fire")
	}
}
