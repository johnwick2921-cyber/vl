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
	if !eveningSlow(evening, slow, DefaultConfig()) {
		t.Fatalf("a 2-pt night must be slow (5m), got fast")
	}
	strong := []market.Kline{
		{OpenTime: close15, High: 120, Low: 119, Close: 119.5},
		{OpenTime: close15 + 60_000, High: 230, Low: 119, Close: 225}, // 111-pt range
	}
	if eveningSlow(evening, strong, DefaultConfig()) {
		t.Fatalf("a 111-pt night must be strong (2m), got slow")
	}
	// No evening bars yet → not slow (stay 2m).
	if eveningSlow(evening, nil, DefaultConfig()) {
		t.Fatalf("no evening bars must not read slow")
	}
	// Outside the evening window → never slow.
	if eveningSlow(ct(2026, 9, 15, 9, 0), slow, DefaultConfig()) {
		t.Fatalf("the 09:00 RTH minute must never read the evening 5m")
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
