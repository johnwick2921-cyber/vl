package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// TestTriggerRetestWrongWayRefused — row 56 (a) [D3.2 p1 @ 21:53–23:08]: a
// trigger-retest level trades ONLY in the trigger's direction, whatever the
// school. A BUY trigger line whose retest rejects from BELOW would map to a
// SELL (RejectEntry: resistance reject → short) — school 1 skips the trigger
// side filter, so without the row-56 gate a SHORT is emitted against a buy
// line. Pin: a buy-line retest rejecting from below → NO short, refused
// "trigger_retest_wrong_way" [CTO ruling 2026-10-04].
func TestTriggerRetestWrongWayRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6
	// Drop the EMA-34 location line from the level set so it does not become
	// the SHORT target's first obstacle and cap the room below 2R — the pin
	// is the direction gate, not the target geometry.
	cfg.EMALocationTFMinutes = 0
	// TriggerSchool stays 1 (the default) — exactly the school that skips the
	// LocTriggerFilter side check and needs the row-56 gate.

	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	// An old LOW below the entry — the SHORT target for the (would-be) PLH.
	e.State.SeedLevels = []Level{
		{Key: "old-low:29200", Kind: KindOldExtreme, Price: 29200},
	}
	// BUY trigger line at 29300 (its retest is a location).
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	// 4h SELL so the SHORT passes the HTF gate — the ONLY thing that should
	// stop the short is the row-56 direction gate against the buy trigger.
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 29350}}

	// Bar 0 is the old low; bar 2 spikes to 29320 (a prior swing HIGH above
	// the line, so the PLH passes the lower-high check and the ONLY remaining
	// blocker is the row-56 direction gate); bar 3 pulls back below 29300; bar
	// 4 touches 29300 and closes BACK below it → resistance reject → SELL.
	bars := []market.Kline{
		rthBars(0, 29250, 29260, 29200, 29240), // the old low (extreme)
		rthBars(1, 29260, 29280, 29240, 29270),
		rthBars(2, 29270, 29320, 29310, 29315), // prior swing high ABOVE the line
		rthBars(3, 29312, 29295, 29285, 29287), // pull back below 29300
		rthBars(4, 29287, 29300, 29280, 29290), // high touches 29300, close back below
	}
	now := bars[4].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

	ints := e.Tick(bars, now)
	for _, in := range ints {
		if in.Action == PlaceStopEntry && in.Side == SideShort {
			t.Fatalf("row 56: a buy-line retest rejecting from below must NOT emit a short; got %+v; refusals=%v", in, e.State.Refusals)
		}
	}
	if got := e.State.Refusals["trigger_retest_wrong_way"]; got < 1 {
		t.Fatalf("row 56: expected a trigger_retest_wrong_way refusal; refusals=%v", e.State.Refusals)
	}
}
