package mentor

import (
	"testing"

	"vl/market"
)

// MENTOR-NY item 5 — funnel visibility at the production call site
// (Evaluator.Tick). The funnel line prints e.State.Refusals as "kernel
// refusals"; these pins assert what reaches it.

// nyTicks ticks e on bars, then (n-1) more times on a new OUTSIDE candle that
// still touches the level and closes above it — the SAME visit (no departure).
func nyTicks(e *Evaluator, bars []market.Kline, now int64, n int) map[string]int {
	e.Tick(bars, now)
	for i := 1; i < n; i++ {
		b := bars[len(bars)-1]
		b.OpenTime += 60_000
		b.CloseTime += 60_000
		b.High += 0.5 // outside the previous candle — never an inside bar
		b.Low -= 0.05
		bars = append(append([]market.Kline{}, bars...), b)
		e.Tick(bars, now+int64(i)*60_000)
	}
	return e.State.Refusals
}

// TestMentorNYFirstTouchInvalidationIsCounted — the first touch closing on the
// wrong side invalidates the level for PHL/PLH [D5.2 p1 @19:51–20:25]. It is
// course-faithful, but the funnel never showed it (RTH 10-12 75, 10-13 65,
// 10-14 25 invalidations). It now counts level_invalid_first_touch, once per
// invalidation. MUTANT: drop the count → RED.
func TestMentorNYFirstTouchInvalidationIsCounted(t *testing.T) {
	e := triggerRetestFixture()
	bars := []market.Kline{
		rthBars(0, 101, 101.5, 100.9, 101.2), // prev close above the line: support
		rthBars(1, 101, 101.2, 98, 98.5),     // touches 100, closes BELOW → wrong way
	}
	e.Tick(bars, bars[1].CloseTime+1)
	if !e.State.ISBOnly[string(KindTriggerRetest)] {
		t.Fatalf("precondition: the wrong-way close must invalidate the line; state=%v", e.State.ISBOnly)
	}
	if n := e.State.Refusals["level_invalid_first_touch"]; n != 1 {
		t.Fatalf("level_invalid_first_touch = %d, want 1 (the funnel must show the invalidation): %v", n, e.State.Refusals)
	}
	// The next candle of the same (still invalid) visit does not count again.
	bars = append(bars, rthBars(2, 98.5, 99, 97.5, 98))
	e.Tick(bars, bars[2].CloseTime+1)
	if n := e.State.Refusals["level_invalid_first_touch"]; n != 1 {
		t.Fatalf("level_invalid_first_touch = %d after a second candle of the same invalid visit, want 1", n)
	}
}

// TestMentorNYPHLRoomCountedOncePerVisit — the PHL room drop re-evaluated and
// re-counted on every tick the touch stood (the report's "room is counted per
// tick"). K's PHL targets the key level 110 (reward 6 ≥ the 4.5 stop, < 2×4.5):
// refused by the room rule on each of three ticks of ONE visit. Counted once
// for K; the other +1 per tick is the ISB candidate's own room check on each
// new candle pair (one per setup, not a visit count). Per tick it read 5.
// MUTANT: e.refuse for the PHL room again → 5 → RED.
func TestMentorNYPHLRoomCountedOncePerVisit(t *testing.T) {
	e, bars, now := b21PairFixtureMode(b21Thin)
	e.Cfg.RoomMultiple = 2
	e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "KL-near", Kind: KindKeyLevel, Price: 110})
	led := nyTicks(e, bars, now, 3)
	if !e.State.PHLRefusedVisit["K|room"] {
		t.Fatalf("precondition: K's PHL must be refused by the room rule: %v", led)
	}
	if led["room"] != 3 {
		t.Fatalf("room = %d over 3 ticks of one visit, want 3 (K once + the ISB pair's per-candle check twice; per tick it read 5): %v", led["room"], led)
	}
}

// TestMentorNYPHLTriggerSideCountedOncePerVisit — the PHL's isb_trigger_side
// drop (a level reject against the 5m trigger side) counts once per visit of
// each level, not per tick. Two levels touch (K and the trigger retest): 2
// marks on tick 1; the +1 per later tick is a non-PHL trigger-side check on
// the new candle (unchanged). Per tick it read 8. MUTANT: e.refuse again → 8 → RED.
func TestMentorNYPHLTriggerSideCountedOncePerVisit(t *testing.T) {
	e, bars, now := b21PairFixtureMode(b21Thin)
	e.Cfg.LocTriggerFilter = true
	e.Cfg.TriggerSchool = 2
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 101}
	led := nyTicks(e, bars, now, 3)
	if !e.State.PHLRefusedVisit["K|isb_trigger_side"] {
		t.Fatalf("precondition: K's PHL must be refused on the trigger side: %v", led)
	}
	if led["isb_trigger_side"] != 4 {
		t.Fatalf("isb_trigger_side = %d over 3 ticks, want 4 (2 levels once each + 1 non-PHL check per later tick; per tick it read 8): %v", led["isb_trigger_side"], led)
	}
}
