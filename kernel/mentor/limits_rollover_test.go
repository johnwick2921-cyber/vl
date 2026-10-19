package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// TestLimitsDiscardPendingAcrossTradingDayRoll pins the CORRECT day-reset
// semantics at the exact instant the r40 race caught (16:59:38 → 17:00:38 CT):
// a pend seeded on the 16:59 candle must NOT fill on the 17:00 candle, because
// 17:00 rolls the trading day and Limits.Apply discards the unfinished
// simulation with the day (limits.go day-reset). The same-session arm proves
// the machinery WOULD fill inside one day — so the discard is the roll, not a
// dead fill path. Production behavior, unchanged; this pin only asserts it.
func TestLimitsDiscardPendingAcrossTradingDayRoll(t *testing.T) {
	loc := ctime()
	cfg := DefaultConfig()
	levels := []Level{{Kind: KindOldExtreme, Price: 29650}}
	prev := market.Kline{Open: 29500, High: 29590, Low: 29450, Close: 29580}
	cur := market.Kline{Open: 29580, High: 29590, Low: 29500, Close: 29550}
	fillCur := market.Kline{Open: 29590, High: 29620, Low: 29540, Close: 29610}

	newSeed := func(expiryMs int64) Intent {
		return Intent{
			Action: PlaceStopLimitEntry, ArmID: "x-07", Setup: "ISB",
			Side: SideLong, Price: 29600, Stop: 29595, Target: 29610,
			StopPts: 5, TargetPts: 10, ExpiryMs: expiryMs,
		}
	}

	t.Run("same_session_pend_fills", func(t *testing.T) {
		now := time.Date(2026, 10, 19, 16, 58, 38, 0, loc).UnixMilli() // both before 17:00
		ev := New(cfg)
		ev.State.Limits.Apply([]Intent{newSeed(now + 60_000)}, prev, cur, now, levels, cfg)
		ev.State.Limits.Apply(nil, prev, fillCur, now+60_000, levels, cfg) // 16:59:38, same day
		if ev.State.Limits.Long == nil {
			t.Fatalf("same-session: the seeded pend must fill and create the G1 leg")
		}
	})

	t.Run("trading_day_roll_discards_pend", func(t *testing.T) {
		now := time.Date(2026, 10, 19, 16, 59, 38, 0, loc).UnixMilli() // 16:59:38
		ev := New(cfg)
		ev.State.Limits.Apply([]Intent{newSeed(now + 60_000)}, prev, cur, now, levels, cfg)
		ev.State.Limits.Apply(nil, prev, fillCur, now+60_000, levels, cfg) // 17:00:38, new trading day
		if ev.State.Limits.Long != nil {
			t.Fatalf("the 17:00 trading-day roll must discard the pending sim (leg created across the roll)")
		}
	})
}
