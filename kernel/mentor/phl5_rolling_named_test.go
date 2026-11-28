package mentor

import "testing"

// PHL-5 (phl audit, 2026-11-10) — "Counters record, never infer" (repo canon,
// checklist class 35). The rolling ladder used to drop a candle SILENTLY in two
// places: no old extreme on the trend side (rolling_phl.go step 2) and a level
// touch owning the entry (step 3, levelTouchRejectActive) — both called
// rollingCancel, which records nothing, so the counters could not say why a
// course setup did not trade (10-30 13:21–13:27 WE7, 10-29 19:48–19:53 WE10:
// no rolling intent and no rolling refusal). Each is now named once per exec
// candle with the same once-per-candle key as rollingSkipRung. The cancel
// itself is unchanged.
//
// RED before PHL-5: the ledger stays empty for both.

// TestPHL5NoOldExtremeIsNamedOncePerCandle — the pre-PHL-1 source (level-set
// old extremes, PHLOldExtreme1mK 0) with no old high: named once per candle,
// however many ticks re-read it.
func TestPHL5NoOldExtremeIsNamedOncePerCandle(t *testing.T) {
	e, bars, now := rollingFixture()
	e.State.SeedLevels = nil // no old high on the trend side
	for i := 0; i < 3; i++ {
		e.Tick(bars, now) // the same closed candle, re-read
	}
	if got := e.State.Refusals["rolling_phl_no_old_extreme_on_side"]; got != 1 {
		t.Fatalf("no old extreme: rolling_phl_no_old_extreme_on_side = %d, want 1 (once per exec candle); ledger=%v", got, e.State.Refusals)
	}
	if e.State.RollingPHL.Ladder || e.State.RollingPHL.ArmID != "" {
		t.Fatalf("the legacy source still ends the ladder (cancel unchanged), state=%+v", e.State.RollingPHL)
	}
}

// TestPHL5LevelTouchOwnsIsNamedOncePerCandle — a live level touch-reject on
// the trend side owns the entry (m216); the rolling cancel is now named.
func TestPHL5LevelTouchOwnsIsNamedOncePerCandle(t *testing.T) {
	for _, k := range []int{0, PHLOldExtreme1mKDefault} {
		e, bars, now := rollingFixture()
		e.Cfg.PHLOldExtreme1mK = k
		e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "support", Kind: KindKeyLevel, Price: 112})
		e.State.Touches["support"] = Touch{LevelKey: "support", Outcome: TouchReject, ApproachedFrom: SideLong, RefBar: bars[5], PriceAtTouch: 112}
		e.Tick(bars, now)
		e.Tick(bars, now)
		if got := e.State.Refusals["rolling_level_touch_owns"]; got != 1 {
			t.Fatalf("k=%d: a level touch owning the entry: rolling_level_touch_owns = %d, want 1; ledger=%v", k, got, e.State.Refusals)
		}
	}
}

// TestPHL5SeamKeepsTheFrozenGoldensSilent — the frozen tick-identity goldens
// predate PHL-5: under rollingSilentCancelSeam nothing is recorded.
func TestPHL5SeamKeepsTheFrozenGoldensSilent(t *testing.T) {
	prev := rollingSilentCancelSeam
	rollingSilentCancelSeam = true
	defer func() { rollingSilentCancelSeam = prev }()
	e, bars, now := rollingFixture()
	e.State.SeedLevels = nil
	e.Tick(bars, now)
	if got := e.State.Refusals["rolling_phl_no_old_extreme_on_side"]; got != 0 {
		t.Fatalf("seam: the pre-PHL-5 cancel records nothing, got %d", got)
	}
}
