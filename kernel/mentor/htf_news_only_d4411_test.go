package mentor

// D4.4-11 [D4.4 p1 @13:44–14:06, @24:48]: the 4h/1h direction gate is for NEWS
// first, not ordinary trading yet ("đánh news NÊN SỬ DỤNG CHO NEWS TRƯỚC ĐI.
// ĐỪNG SỬ DỤNG CHO [trade] THƯỜNG" / "đánh ĐẦU GIỜ hay là đánh THƯỜNG — CHƯA
// TỚI LÚC ĐÂU"). The gate applied all day; now HTFGateNewsOnly (default true)
// applies it only inside the 07:20–07:35 CT news window.

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// TestHTFGateActiveNewsOnly pins the pure window test.
func TestHTFGateActiveNewsOnly(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HTFGateNewsOnly {
		t.Fatal("HTFGateNewsOnly must default OFF (the all-day gate; U-6 open) — CTO 2026-10-04")
	}
	cfg.HTFGateNewsOnly = true
	cases := []struct {
		hh, mm int
		want   bool
	}{
		{7, 19, false},
		{7, 20, true},
		{7, 30, true},
		{7, 35, true},
		{7, 36, false},
		{10, 0, false},
	}
	for _, c := range cases {
		now := auditMs(2026, 9, 15, c.hh, c.mm, 0)
		if got := HTFGateActive(now, cfg); got != c.want {
			t.Fatalf("%02d:%02d CT: gate active = %v, want %v", c.hh, c.mm, got, c.want)
		}
	}
	// The knob off = the legacy all-day gate.
	cfg.HTFGateNewsOnly = false
	if got := HTFGateActive(auditMs(2026, 9, 15, 10, 0, 0), cfg); !got {
		t.Fatal("HTFGateNewsOnly=false must apply the gate all day")
	}
}

// TestHTFGateNewsOnlyTick pins the Tick call site: at 09:05 CT (outside the news
// window) a 4h/1h CONFLICT no longer refuses a long PHL — the gate is off under
// the course default. With HTFGateNewsOnly=false (the legacy all-day gate) the
// same tick refuses it (case 3).
// MUTANT: drop the GateOff set in Tick (or the GateOff check in HTFVerdict) →
// the 09:05 conflict PHL is refused under the default → RED.
func TestHTFGateNewsOnlyTick(t *testing.T) {
	run := func(allDay bool) bool {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.PHLTargetShyPts = 6
		cfg.EMALocationTFMinutes = 0
		cfg.HTFGateNewsOnly = !allDay

		e := New(cfg)
		e.seeded = true
		e.State.Seed1mWatermark = math.MaxInt64
		e.State.Seed1HWatermark = math.MaxInt64
		e.State.EMA34 = 0
		e.State.EMA9 = 0
		e.State.SeedLevels = []Level{
			{Key: "L", Kind: KindKeyLevel, Price: 29386},
			{Key: "old-low:29335", Kind: KindOldExtreme, Price: 29335},
			{Key: "old-high:29431.75", Kind: KindOldExtreme, Price: 29431.75},
		}
		e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
		// CONFLICT: 4h long vs 1h short — case 3 sits out when the gate is on.
		e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}, OneH: TriggerLine{Dir: SideShort, Price: 29200}}

		bars := []market.Kline{
			rthBars(0, 29400, 29410, 29335, 29405),
			rthBars(1, 29420, 29430, 29415, 29425),
			rthBars(2, 29425, 29431.75, 29420, 29428),
			rthBars(3, 29410, 29415, 29400, 29408),
			rthBars(4, 29400, 29405, 29395, 29398),
			rthBars(5, 29395, 29397, 29386, 29392),
		}
		now := bars[5].CloseTime + 1 // 09:05 CT — outside the 07:20–07:35 news window
		e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
		e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

		ints := e.Tick(bars, now)
		for _, in := range ints {
			if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong {
				return true
			}
		}
		return false
	}

	if !run(false) {
		t.Fatal("news-only (switch ON): the 09:05 conflict PHL must EMIT — the HTF gate is off outside the news window [D4.4-11]")
	}
	if run(true) {
		t.Fatal("all-day (switch OFF, the default): the same 09:05 conflict PHL must be REFUSED (case 3)")
	}
}
