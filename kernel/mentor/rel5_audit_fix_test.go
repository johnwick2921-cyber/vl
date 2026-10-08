package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// REL-5 audit fixes pins: #1 MidRange target-only leak + named refusal, #2 room
// x target-only fall-through, #3 stale ISBArms after a Limits drop. Each is a
// Tick pin (or a direct unit pin where the call is a pure function) with a RED
// mutant.

// rel5ISBEval builds a seeded evaluator whose last pair is a green ISB (long)
// with entry/stop set by the inside candle, ready for a tick.
func rel5ISBEval(t *testing.T, cfg Config, levels []Level, htfPrice float64) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = levels
	bars := []market.Kline{
		rthBars(0, 98, 106, 97, 105),  // mother green → ISB long
		rthBars(1, 103, 104, 99, 100), // inside: entry 104, stop 99 → risk 5
	}
	now := bars[1].CloseTime + 1
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: htfPrice}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

// TestMidRangeIgnoresTargetOnlyLevels — REL-5 #1: a target-only level (4h
// trigger / wick microscalp) and a trendline are never range boundaries.
// Mutant: drop the Kind filter in MidRange → RED.
func TestMidRangeIgnoresTargetOnlyLevels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RangeGapPts = 20
	price := 100.0
	cases := []struct {
		name   string
		levels []Level
		want   bool
	}{
		{
			name:   "key below + 4h trigger above → NOT mid-range (4h excluded)",
			levels: []Level{{Key: "down", Kind: KindKeyLevel, Price: 96}, {Key: "htf_4h_trigger", Kind: KindHTFTrigger, Price: 103}},
			want:   false,
		},
		{
			name:   "key below + wick microscalp above → NOT mid-range",
			levels: []Level{{Key: "down", Kind: KindKeyLevel, Price: 96}, {Key: "wick", Kind: KindWickMicroscalp, Price: 103}},
			want:   false,
		},
		{
			name:   "key below + trendline above → NOT mid-range",
			levels: []Level{{Key: "down", Kind: KindKeyLevel, Price: 96}, {Key: "tl", Kind: KindTrendline, Price: 103}},
			want:   false,
		},
		{
			name:   "two key levels → mid-range",
			levels: []Level{{Key: "down", Kind: KindKeyLevel, Price: 96}, {Key: "up", Kind: KindKeyLevel, Price: 103}},
			want:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MidRange(c.levels, price, cfg); got != c.want {
				t.Fatalf("MidRange(%+v) = %v, want %v", c.levels, got, c.want)
			}
		})
	}
}

// TestPHLMidRangeNamedRefusal — REL-5 #1: a PHL banned by the mid-range filter
// now counts "phl_mid_range" instead of a silent continue. Mutant: revert the
// eval.go:1202 continue to a silent one → RED.
func TestPHLMidRangeNamedRefusal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RangeGapPts = 20
	cfg.PHLTargetShyPts = 6
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false

	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	// L=100 is the touch; 96 and 105 bracket the entry within RangeGapPts; the
	// old high is far so only the mid-range filter matters.
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 100},
		{Key: "below", Kind: KindKeyLevel, Price: 96},
		{Key: "above", Kind: KindKeyLevel, Price: 105},
		{Key: "old-high:130", Kind: KindOldExtreme, Price: 130},
	}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}

	bars := []market.Kline{
		rthBars(0, 130, 130.5, 129.5, 130), // old high
		rthBars(1, 103, 104, 102, 103),
		rthBars(2, 102, 103, 100, 102), // touches L=100 from above, closes above → LONG reject
	}
	now := bars[2].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

	e.Tick(bars, now)
	if e.State.Refusals["phl_mid_range"] == 0 {
		t.Fatalf("a mid-range PHL must count phl_mid_range; refusals=%v", e.State.Refusals)
	}
}

// TestISBRoomFallsThroughTargetOnly — REL-5 #2: a target-only 4h trigger closer
// than 2R is skipped and the room search falls through to the next key level.
// OPTION B: the 4h line is still skipped as a TARGET, but it is the FIRST
// opposing level for the room rule — half the room (1 pt) < 1R (5) → the ISB is
// refused room_vs_target (it cannot clear even 1:1 before the 4h line).
func TestISBRoomFallsThroughTargetOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	// RoomMultiple 2. Entry 104, stop 99 → risk 5, 2R = 10. 4h line at 106 is
	// 2 pts away (< 10) → skipped as a target; the key level at 120 is 16 pts
	// away. Option B: half the room to the 4h line = 1 < 1R → refuse.
	e, bars, now := rel5ISBEval(t, cfg,
		[]Level{{Key: "far", Kind: KindKeyLevel, Price: 120}},
		106,
	)

	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			t.Fatalf("option B must refuse (half the room to the 4h line < 1R); got %+v refusals %v", in, e.State.Refusals)
		}
	}
	if e.State.Refusals["room_vs_target"] == 0 {
		t.Fatalf("room_vs_target refusal not counted; refusals=%v", e.State.Refusals)
	}
}

// TestISBArmsReconciledAfterLimitsDrop — REL-5 #3: an ISB dropped by the leg
// budget must not leave a stale ISBArms entry (a stale arm later suppresses a
// legitimate same-side ISB via isbArmActive). Mutant: remove the reconcile in
// Tick → RED (the arm survives).
func TestISBArmsReconciledAfterLimitsDrop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	e, bars, now := rel5ISBEval(t, cfg,
		[]Level{{Key: "far", Kind: KindKeyLevel, Price: 120}},
		90,
	)
	// Leg full on the long side: the ISB is emitted then dropped by Limits.Apply.
	e.State.Limits.Long = &Leg{Side: SideLong, Extreme: 1e9, Entries: 2}

	e.Tick(bars, now)
	if e.State.Limits.Refusals["leg_budget_full"] == 0 {
		t.Fatalf("precondition: the leg-budget drop must fire; limits refusals=%v", e.State.Limits.Refusals)
	}
	if len(e.State.ISBArms) != 0 {
		t.Fatalf("the dropped ISB's arm must be reconciled away; ISBArms=%v", e.State.ISBArms)
	}
}
