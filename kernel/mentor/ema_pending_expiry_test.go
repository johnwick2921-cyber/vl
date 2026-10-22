package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// #7 — the EMA-34 loss block (E2/CE-6) was dead in production: a level ('lvl-')
// order carries ExpiryMs=0, and the emit site copied that 0 into
// EmaPendingExpiry, so emaLossTick saw `now > 0` (always true) and cleared the
// pending as 'expired unfilled' on the next tick. The fix resolves the pending's
// expiry through pendExpiry (LevelArmExpiry for level arms). This pin goes
// through the production Tick path — a real EMA34HTF PHL emission — and asserts
// the pending expiry is non-zero.
// MUTANT: restore `EmaPendingExpiry = in.ExpiryMs` at the emit site → the pin
// sees expiry 0 → RED.
func TestEmaPendingExpiryNonZeroForLevelArm(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.RoomMultiple = 2
	cfg.PHLMinCandlesFromExtreme = 2
	cfg.PHLTargetShyPts = 0
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	// The E4 cross-count gate is orthogonal to this bug; the #43 base still
	// defaults it ON (2), so isolate E2/CE-6 by disabling it here.
	cfg.EmaMaxCross30m = 0

	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 101, 101.5, 101, 101.3),
		mk(1, 101, 101.4, 100, 100.5),
		mk(2, 100, 101, 98, 99),        // the left structural low 98
		mk(3, 99.5, 130, 129.5, 129.7), // the far old high 130 — the PHL target
		// Crossing noise (closes 101/99/101/99) — but E4 is OFF by default, so
		// the EMA setup EMITS.
		mk(4, 100.5, 101.5, 99, 101),
		mk(5, 99.5, 100.5, 99, 99),
		mk(6, 100.5, 101.5, 99, 101),
		mk(7, 99.5, 100.5, 99, 99),
	}
	refBar := mk(8, 100.1, 100.8, 99.6, 100.5)
	bars = append(bars, refBar)
	now := bars[len(bars)-1].OpenTime + 59_999

	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = []Level{
		{Key: string(KindEMA34HTF), Kind: KindEMA34HTF, Price: 100},
		{Key: "old-high:130", Kind: KindOldExtreme, Role: OldHigh, Price: 130},
	}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Touches = map[string]Touch{
		string(KindEMA34HTF): {LevelKey: string(KindEMA34HTF), Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: refBar, PriceAtTouch: 100},
	}

	ins := e.Tick(bars, now)
	emitted := false
	for _, in := range ins {
		if in.Action == PlaceStopEntry {
			emitted = true
		}
	}
	if !emitted {
		t.Fatal("the EMA34HTF setup must emit a PHL (E4 is OFF by default)")
	}
	if e.State.EmaPendingSide == "" {
		t.Fatal("the EMA pending must be recorded on a real emission")
	}
	if e.State.EmaPendingExpiry == 0 {
		t.Fatalf("EmaPendingExpiry must be non-zero for a level arm (pendExpiry), got 0 — the E2/CE-6 block would drop it as expired-unfilled")
	}
	if e.State.EmaPendingExpiry != LevelArmExpiry(now) {
		t.Fatalf("EmaPendingExpiry = %d, want LevelArmExpiry(now) = %d", e.State.EmaPendingExpiry, LevelArmExpiry(now))
	}
}
