package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// FIX-ROOM-RULE-LESSON (release #13): the room rule, the R:R floor and the
// confluence tier are THREE separate checks, each with its own funnel key:
//
//   - rr_floor       — D1.2 p1 @07:41–08:45: target >= stop (1:1 is the
//     universal minimum).
//   - room_vs_target — D5.3 p1 @09:16–10:17: the free room from entry to the
//     NEXT opposing level must be >= RoomMultiple × the planned target
//     distance ("dù target của em là 15 điểm, từ chỗ em xuống target nó phải
//     từ 30 điểm đổ lên").
//   - rr_confluence  — D3.4 p3 @07:52–08:07: ONLY the confluence tier demands
//     1:2 ("risk reward 1-2 … trường hợp đặc biệt … cộng hưởng").
//
// The old single "room" key (reward >= 2× risk, everywhere) is GONE.

// TestRoomVsTargetRefusalPure — the exact lesson arithmetic, on the pure helper.
func TestRoomVsTargetRefusalPure(t *testing.T) {
	cases := []struct {
		name   string
		entry  float64
		target float64
		room   float64 // the next opposing level (0 = none)
		want   bool
	}{
		{"stop 5 / target 20 / room 25 → refused (25 < 40)", 100, 120, 125, true},
		{"stop 15 / target 15 / room 30 → admitted (30 = 2×15)", 100, 115, 130, false},
		{"no next level on record → room unbounded → admitted", 100, 120, 0, false},
		{"roomMultiple off → admitted", 100, 120, 125, false}, // exercised with rm=0 below
	}
	for _, c := range cases {
		rm := 2.0
		if c.name == "roomMultiple off → admitted" {
			rm = 0
		}
		if refuse, _ := roomVsTargetRefusal(c.entry, c.target, c.room, rm); refuse != c.want {
			t.Fatalf("%s: got refuse=%v, want %v", c.name, refuse, c.want)
		}
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

// TestISBRoomVsTargetRefusedAtCallSite — stop 5 / target 20 / room 25: the
// free room (25) is less than 2× the target (40) → refused with the
// room_vs_target key. The OLD code (reward >= 2× risk = 10) ADMITTED this —
// the named RED.
func TestISBRoomVsTargetRefusedAtCallSite(t *testing.T) {
	// mother green, inside: entry 104, stop 99 → risk 5.
	mother := rthBars(0, 98, 106, 97, 105)
	inside := rthBars(1, 103, 104, 99, 100)
	// target 124 (entry + 20), room 129 (entry + 25).
	seed := []Level{
		{Key: "target", Kind: KindKeyLevel, Price: 124},
		{Key: "room", Kind: KindKeyLevel, Price: 129},
	}
	e, bars, now := isbRoomFixture(t, mother, inside, seed)

	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			t.Fatalf("the ISB must be refused by the room rule (room 25 < 2× target 20); got %+v; refusals=%v", in, e.State.Refusals)
		}
	}
	if e.State.Refusals["room_vs_target"] == 0 {
		t.Fatalf("room_vs_target refusal not counted; refusals=%v", e.State.Refusals)
	}
}

// TestISBRoomVsTargetAdmittedAtCallSite — stop 15 / target 15 / room 30: the
// 1:1 floor passes and the room is exactly 2× the target → ADMITTED. The OLD
// code (reward 15 < 2× risk 30) REFUSED this — the named RED.
func TestISBRoomVsTargetAdmittedAtCallSite(t *testing.T) {
	// mother green, inside: entry 105, stop 90 → risk 15.
	mother := rthBars(0, 90, 106, 85, 100)
	inside := rthBars(1, 100, 105, 90, 95)
	// target 120 (entry + 15), room 135 (entry + 30).
	seed := []Level{
		{Key: "target", Kind: KindKeyLevel, Price: 120},
		{Key: "room", Kind: KindKeyLevel, Price: 135},
	}
	e, bars, now := isbRoomFixture(t, mother, inside, seed)

	ins := e.Tick(bars, now)
	found := false
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			found = true
			if in.Target != 120 {
				t.Fatalf("admitted ISB target = %.2f, want 120", in.Target)
			}
		}
	}
	if !found {
		t.Fatalf("the ISB must be admitted (1:1 floor + room 30 = 2× target 15); refusals=%v", e.State.Refusals)
	}
}
