package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestB14SkipNeverFallsBackToFartherExtreme — B14b [D2.2 p2 @03:07–03:14,
// 06:43–07:02]: a failed NEAREST old extreme is a SKIP, never replaced by a
// farther one. The B15 obstacle cap makes the fallback invisible through the
// target (the nearer extreme is always the first obstacle), so the pin uses
// the DISTANCE gate: the nearest old high by price sits one bar from the
// touch (too close), the farther one is 5 bars away and would pass. Correct:
// 0 entries, the too-close drop named, the floor never reached. Mutant
// (farthest-wins): the far extreme passes the distance gate, its target is
// capped at the near level, the floor fails and rr_floor is counted instead.
func TestB14SkipNeverFallsBackToFartherExtreme(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 2
	cfg.PHLMinCandlesFromExtreme = 3
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 101, 130.5, 100.5, 129.8), // old high 130.5 @0 (5 bars away: passes)
		mk(1, 102, 103.0, 101.0, 102.5),
		mk(2, 98, 99.5, 96.0, 98.8), // tape swing low 96 @2 — prior low under the 97.5 stop
		mk(3, 103, 104.0, 102.0, 103.6),
		mk(4, 103, 106.2, 101.8, 103.5), // old high 106.2 @4 (1 bar from the touch: too close)
		mk(5, 102, 104.2, 97.5, 103.9),  // reject touch; 97.5 > 96 → higher-low passes
	}
	now := bars[5].OpenTime + 59_999
	levels := []Level{
		{Key: "K", Kind: KindKeyLevel, Price: 97.5},
		{Key: "old-high:130.5", Kind: KindOldExtreme, Price: 130.5},
		{Key: "old-high:106.2", Kind: KindOldExtreme, Price: 106.2},
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = now
	e.State.Seed1HWatermark = now
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 140, Low: 90, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Touches = map[string]Touch{
		"K": {LevelKey: "K", Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: bars[5], PriceAtTouch: 97.5},
	}
	ins := e.Tick(bars, now)
	if n := b14Entries(ins); n != 0 {
		t.Fatalf("too-close nearest emitted %d entries, want 0: %+v", n, ins)
	}
	if e.State.Refusals["phl_too_close_to_extreme"] == 0 {
		t.Fatalf("ledger = %v, want phl_too_close_to_extreme counted", e.State.Refusals)
	}
}
