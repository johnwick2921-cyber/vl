package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// FIX-LEVEL-WINDOW (owner order 2026-10-13 ~21:2x CT, "fix all"): a level
// (PHL/PLH) order RESTS until ITS OWN 15:00 CT expiry — LevelArmExpiry, the
// ONE definition the trader's injector and the leg budget already read. The
// old wall-clock read (ctOf(now) ≥ 15:00) was TRUE every minute 15:00–23:59
// and cancelled an evening-placed level order in the SAME Tick it was
// registered. These tests pin the fix at the production call site (Tick →
// levelArmCancels), not at the helper.
//
// MUTANT (CTO): restore the wall-clock windowEnd (hh*60+mm >= rthWindowEndMin)
// → the evening survival assertion fails (the arm is cancelled at 16:12) → RED.

// levelWindowBar builds one real-UTC CT bar: opens at hh:mm:00 and closes at
// hh:mm:59.999 (CloseTime + 1 is the instant the bar is evaluated as CLOSED,
// per BarCloseInstant).
func levelWindowBar(y int, mo time.Month, d, hh, mm int, close float64) market.Kline {
	ot := auditMs(y, mo, d, hh, mm, 0)
	return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: close - 5, High: close + 5, Low: close - 10, Close: close}
}

// levelWindowEval builds a seeded evaluator with one resting long level arm.
func levelWindowEval(placedAt int64, now int64) *Evaluator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = []Level{{Key: "L", Kind: KindKeyLevel, Price: 29385}}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	// The arm was PLACED at `placedAt` (its placement candle's CloseTime).
	// Side long at 29385; the bars below close at 29390 — ABOVE the level, so
	// the close-through branch never fires and only the expiry can cancel.
	e.State.LevelArms = map[string]LevelArm{
		"L": {ArmID: "lvl-9", Side: SideLong, LevelPrice: 29385, PlacedAt: placedAt},
	}
	return e
}

// TestLevelArmEveningPlacementRestsUntilNextDay1500 — an arm placed at 16:10 CT
// must survive the evening ticks and be cancelled at 15:00 CT the NEXT day,
// never at birth.
func TestLevelArmEveningPlacementRestsUntilNextDay1500(t *testing.T) {
	placed := auditMs(2026, 9, 15, 16, 10, 0)
	// 16:11 and 16:12 CT ticks — both after placement, both below the next
	// 15:00 expiry.
	b1611 := levelWindowBar(2026, 9, 15, 16, 11, 29390)
	b1612 := levelWindowBar(2026, 9, 15, 16, 12, 29390)
	e := levelWindowEval(placed, b1612.CloseTime+1)

	ints := e.Tick([]market.Kline{b1611, b1612}, b1612.CloseTime+1)
	for _, in := range ints {
		if in.Action == CancelArm && in.ArmID == "lvl-9" {
			t.Fatalf("evening placement: the arm must NOT be cancelled at 16:12 CT (old wall-clock read cancelled it at birth); intents=%+v", ints)
		}
	}
	if _, ok := e.State.LevelArms["L"]; !ok {
		t.Fatalf("evening placement: the arm must still rest after the evening ticks; State.LevelArms=%+v", e.State.LevelArms)
	}

	// The 14:59 CT bar the NEXT day closes at 14:59:59.999 → now = 15:00:00.000,
	// the arm's own expiry. The close (29390) does not go through the level.
	b1459 := levelWindowBar(2026, 9, 16, 14, 59, 29390)
	ints = e.Tick([]market.Kline{b1612, b1459}, b1459.CloseTime+1)
	cancelled := false
	for _, in := range ints {
		if in.Action == CancelArm && in.ArmID == "lvl-9" {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatalf("evening placement: the arm must cancel at its own 15:00 CT expiry the NEXT day; intents=%+v", ints)
	}
	if len(e.State.LevelArms) != 0 {
		t.Fatalf("the resting arm must be gone after the expiry cancel: %+v", e.State.LevelArms)
	}
}

// TestLevelArmCancelsExactlyAtOwn1500 — an arm placed at 10:00 CT survives at
// 14:59 CT and cancels at 15:00 CT the SAME day (the boundary is the arm's own
// LevelArmExpiry, one definition).
func TestLevelArmCancelsExactlyAtOwn1500(t *testing.T) {
	placed := auditMs(2026, 9, 15, 10, 0, 0)
	// 14:58 CT: now = 14:59:00.000 < 15:00 → survives.
	b1458 := levelWindowBar(2026, 9, 15, 14, 58, 29390)
	e := levelWindowEval(placed, b1458.CloseTime+1)
	ints := e.Tick([]market.Kline{b1458, b1458}, b1458.CloseTime+1)
	for _, in := range ints {
		if in.Action == CancelArm && in.ArmID == "lvl-9" {
			t.Fatalf("morning placement: the arm must survive 14:59 CT (before its 15:00 expiry); intents=%+v", ints)
		}
	}

	// 14:59 CT: now = 15:00:00.000 ≥ 15:00 → cancels.
	b1459 := levelWindowBar(2026, 9, 15, 14, 59, 29390)
	ints = e.Tick([]market.Kline{b1458, b1459}, b1459.CloseTime+1)
	cancelled := false
	for _, in := range ints {
		if in.Action == CancelArm && in.ArmID == "lvl-9" {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatalf("morning placement: the arm must cancel at 15:00 CT the same day; intents=%+v", ints)
	}
}

// TestLevelArmExpiryAgreesWithLegBudget — the leg budget's pend simulation and
// the evaluator's cancel sweep read the SAME LevelArmExpiry: pendExpiry at the
// placement tick (now ≈ PlacedAt + 1) equals the evaluator's expiry of the arm.
func TestLevelArmExpiryAgreesWithLegBudget(t *testing.T) {
	for _, c := range []struct {
		label string
		at    int64
	}{
		{"morning 10:00", auditMs(2026, 9, 15, 10, 0, 0)},
		{"evening 16:10", auditMs(2026, 9, 15, 16, 10, 0)},
	} {
		want := LevelArmExpiry(c.at)
		if got := pendExpiry(Intent{ArmID: "lvl-9"}, c.at+1); got != want {
			t.Fatalf("%s: leg-budget pendExpiry = %d, want %d (the evaluator cancels at LevelArmExpiry(PlacedAt))", c.label, got, want)
		}
	}
}
