package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestRel9BoxRefsAdvancesAcrossSlidingWindow — UR-FIX REL9. BoxRefs stored the
// last return's bar INDEX; the live window slides (the provider returns the
// last ~1500 closed bars), so after the newest bar was recorded the next tick's
// walk started one past the end and BoxRefs never advanced again — "every
// return trades" degraded to at most one return per box per day. The fix stores
// the return's OpenTime (ms) and re-resolves the index each tick. RED on dev:
// return #2 is never reached (BoxRefs stays at return #1's stale index); GREEN
// on the fix (BoxRefs advances to return #2's OpenTime).
func TestRel9BoxRefsAdvancesAcrossSlidingWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.OrbGateEnabled = false
	cfg.LocTriggerFilter = false
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100.5),     // pad
		mk(1, 100, 101, 99, 100.5),     // pad
		mk(2, 100, 101, 99, 100.5),     // pad
		mk(3, 100, 101, 99, 100.5),     // (was bar 0)
		mk(4, 100, 103, 99, 101),       // (was bar 1)
		mk(5, 104, 106, 102, 104.5),    // top 1: extreme high 106
		mk(6, 104, 105, 103.5, 104),    // confirms top 1
		mk(7, 104, 104.5, 102.5, 103.2),
		mk(8, 103.5, 106, 102.5, 103.2), // top 2: wick 106
		mk(9, 102.5, 103.5, 101, 102.2), // top 2 confirming bar → box born (FormedAt 9)
		mk(10, 102.5, 103, 102, 102.5),  // no touch
		mk(11, 102, 103.6, 101.5, 102.6), // return #1 (touches bottom, closes below)
		mk(12, 103, 103.8, 101.5, 102.4), // return #2
	}
	boxKey := "ftgh:106.00:103.50"

	e := New(cfg)
	e.State.Day = DayLatch{Key: "2026-09-15", Verdict: DayTrade}

	// Tick 1: return #1 is the NEWEST bar — BoxRefs records its OpenTime.
	e.Tick(bars[:12], bars[11].OpenTime+59_999)
	if got := e.State.BoxRefs[boxKey]; got != bars[11].OpenTime {
		t.Fatalf("tick1 BoxRefs[%s] = %d, want return #1 OpenTime %d", boxKey, got, bars[11].OpenTime)
	}

	// Tick 2: SLIDE the window (drop the oldest, append return #2).
	e.Tick(bars[1:13], bars[12].OpenTime+59_999)
	if got := e.State.BoxRefs[boxKey]; got != bars[12].OpenTime {
		t.Fatalf("REL9: return #2 never evaluated — BoxRefs[%s] = %d, want return #2 OpenTime %d (dev kept return #1's stale index)", boxKey, got, bars[12].OpenTime)
	}
}
