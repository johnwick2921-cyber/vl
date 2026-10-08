package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// SCRATCH option B — the faithful D5.3 room rule [@09:16–10:17]: room = the
// distance from entry to the FIRST available opposing level; the take-profit
// is placed at min(current target, half the room), never below 1R; refuse when
// half the room < 1R. Plus the two unchanged gates:
//
//   - rr_floor       — D1.2 p1 @07:41–08:45: target >= stop (1:1 floor).
//   - rr_confluence  — D3.4 p3 @07:52–08:07: 1:2 ONLY for the confluence tier.

// TestRoomFaithfulBPure — the faithful arithmetic on the pure helper.
func TestRoomFaithfulBPure(t *testing.T) {
	// stop 5 / target 20 / room 25: TP = min(20, 12.5) = 12.5, admitted.
	tp, ok, _ := roomFaithfulB(100, 95, 120, 125, 2, SideLong)
	if !ok || tp != 112.5 {
		t.Fatalf("stop 5 / TP 20 / room 25: got tp=%.2f ok=%v, want 112.5 admitted", tp, ok)
	}
	// stop 15 / target 15 / room 30: TP = min(15, 15) = 15, admitted.
	tp, ok, _ = roomFaithfulB(100, 85, 115, 130, 2, SideLong)
	if !ok || tp != 115 {
		t.Fatalf("stop 15 / TP 15 / room 30: got tp=%.2f ok=%v, want 115 admitted", tp, ok)
	}
	// half the room < 1R → refused (stop 10, room 15 → half 7.5 < 10).
	if _, ok, _ := roomFaithfulB(100, 90, 130, 115, 2, SideLong); ok {
		t.Fatal("half the room 7.5 < 1R 10 must refuse")
	}
	// no first level on record → target unchanged.
	if tp, ok, _ := roomFaithfulB(100, 90, 120, 0, 2, SideLong); !ok || tp != 120 {
		t.Fatalf("no first level: got tp=%.2f ok=%v, want 120 unchanged", tp, ok)
	}
	// short side mirrors.
	tp, ok, _ = roomFaithfulB(100, 105, 80, 75, 2, SideShort)
	if !ok || tp != 87.5 {
		t.Fatalf("short: got tp=%.2f ok=%v, want 87.5", tp, ok)
	}
}

// TestRRFloorRefusalPure — the 1:1 floor [D1.2 p1 @07:41–08:45].
func TestRRFloorRefusalPure(t *testing.T) {
	if refuse, _ := rrFloorRefusal(100, 90, 108); !refuse {
		t.Fatal("reward 8 vs risk 10: 0.8R must refuse the floor")
	}
	if refuse, _ := rrFloorRefusal(100, 90, 110); refuse {
		t.Fatal("reward 10 vs risk 10: 1:1 must pass the floor")
	}
	if refuse, _ := rrFloorRefusal(100, 110, 90); refuse {
		t.Fatal("short side, reward 10 vs risk 10: 1:1 must pass")
	}
}

// TestRRConfluenceRefusalPure — the confluence tier's 1:2 [D3.4 p3 @07:52–08:07].
func TestRRConfluenceRefusalPure(t *testing.T) {
	// stop 10 / target 15 → 1:1.5 < 1:2 → refused.
	if refuse, _ := rrConfluenceRefusal(100, 90, 115); !refuse {
		t.Fatal("confluence 1:1.5 must refuse (1:2 demanded)")
	}
	// stop 10 / target 20 → 1:2 → admitted.
	if refuse, _ := rrConfluenceRefusal(100, 90, 120); refuse {
		t.Fatal("confluence 1:2 must pass")
	}
}

// isbRoomFixture builds the seeded evaluator the ISB room tests share: a
// mother+inside pair, a trigger/HTF/ORB that all agree LONG, a normal day, and
// the given target/room key levels above the entry.
func isbRoomFixture(t *testing.T, mother, inside market.Kline, seed []Level) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	// RoomMultiple stays 2 (the default).

	bars := []market.Kline{mother, inside}
	now := bars[1].CloseTime + 1
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = seed
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

// TestISBRoomFaithfulBHalvesTarget — stop 5, first level 16 pts away (entry 104
// → 120): TP = min(16, 8) = 8 ≥ 1R(5) → ADMITTED at entry+8 = 112.
func TestISBRoomFaithfulBHalvesTarget(t *testing.T) {
	mother := rthBars(0, 98, 106, 97, 105)
	inside := rthBars(1, 103, 104, 99, 100) // entry 104, stop 99 → risk 5
	seed := []Level{{Key: "first", Kind: KindKeyLevel, Price: 120}}
	e, bars, now := isbRoomFixture(t, mother, inside, seed)

	ins := e.Tick(bars, now)
	found := false
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			found = true
			if in.Target != 112 {
				t.Fatalf("option B must halve the TP to entry+8=112, got %.2f", in.Target)
			}
		}
	}
	if !found {
		t.Fatalf("option B must admit the ISB (half room 8 >= 1R 5); refusals=%v", e.State.Refusals)
	}
}

// TestISBRoomFaithfulBRefusesHalfRoomUnder1R — stop 15, first level 20 pts away
// (entry 105 → 125): the floor passes (20 >= 15) but half the room (10) < 1R
// (15) → REFUSED room_vs_target.
func TestISBRoomFaithfulBRefusesHalfRoomUnder1R(t *testing.T) {
	mother := rthBars(0, 90, 106, 85, 100)
	inside := rthBars(1, 100, 105, 90, 95) // entry 105, stop 90 → risk 15
	seed := []Level{{Key: "first", Kind: KindKeyLevel, Price: 125}}
	e, bars, now := isbRoomFixture(t, mother, inside, seed)

	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			t.Fatalf("option B must refuse when half the room < 1R; got %+v", in)
		}
	}
	if e.State.Refusals["room_vs_target"] == 0 {
		t.Fatalf("room_vs_target refusal not counted; refusals=%v", e.State.Refusals)
	}
}
