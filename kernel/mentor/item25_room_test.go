package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// Item 25 (RELEASE #4): the D5.3 room rule [@09:16] on the ISB path, the swing
// reject and the reverse ISB — reward to the target must be at least
// RoomMultiple × risk. One shared helper roomRefusal, one counter "room".
// These three Evaluator.Tick pins each refuse a sub-2R setup and count "room".

// TestISBRoomRuleRefused — an ISB whose target sits between the 1:1 floor and
// 2R emits NO intent and counts "room" = 1. Mutant: delete the ISB room check
// in eval.go → RED (the ISB emits).
func TestISBRoomRuleRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0 // the target is the seeded key level, not the EMA
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	// RoomMultiple stays 2 (the default).

	bars := []market.Kline{
		rthBars(0, 98, 106, 97, 105),  // mother green → ISB long
		rthBars(1, 103, 104, 99, 100), // inside: entry 104, stop 99 → risk 5
	}
	now := bars[1].CloseTime + 1
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	// Target 110 → reward 6 >= risk 5 (floor ok) but 6 < 2R (10) → room refuses.
	e.State.SeedLevels = []Level{{Key: "target", Kind: KindKeyLevel, Price: 110}}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			t.Fatalf("the ISB must be refused by the room rule; got %+v; refusals=%v", in, e.State.Refusals)
		}
	}
	if e.State.Refusals["room"] == 0 {
		t.Fatalf("room refusal not counted; refusals=%v", e.State.Refusals)
	}
}

// TestSwingRejectRoomRule (item 25 + R68, CTO fold) — the room is measured to
// the OBSTACLE (the on-side 5m EMA34), never to the swing's own 1:1 first
// target (R43). With no on-side obstacle the 1R-target swing reject trades.
// Mutant: compare the 1R target against 2R (the pre-fold rule) → the swing is
// refused → RED.
func TestSwingRejectRoomRule(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cur := []market.Kline{
		mk5m(t, 15, 5, 0, 9950, 9960, 9945, 9955),
		mk5m(t, 15, 5, 5, 10160, 10175, 10155, 10160), // touches the line, closes back below
	}
	bars := swingTape(t, cur)
	now := cur[1].OpenTime + 5*60_000
	e := New(cfg)
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	seededWith(e, fullDepth, now)

	ins := e.Tick(bars, now)
	if entriesOf(ins, "SWING4H") != 1 {
		t.Fatalf("a 1:1-target swing reject with no on-side obstacle must trade; got %+v; refusals=%v", ins, e.State.Refusals)
	}
}

// TestSwingRoomRefusedAgainstTheObstacle — the helper the swing filter calls:
// an on-side obstacle closer than RoomMultiple × risk refuses; 2R+ away, an
// off-side obstacle, or none at all does not. Mutant: drop the filter → RED.
func TestSwingRoomRefusedAgainstTheObstacle(t *testing.T) {
	short := Intent{Side: SideShort, Price: 100, Stop: 110} // risk 10
	cases := []struct {
		name     string
		obstacle float64
		want     bool
	}{
		{"on-side 15 pts (1.5R) → refused", 85, true},
		{"on-side 25 pts (2.5R) → allowed", 75, false},
		{"off-side → allowed", 120, false},
		{"none → allowed", 0, false},
	}
	for _, c := range cases {
		if got := swingRoomRefused(short, c.obstacle, 2); got != c.want {
			t.Fatalf("%s: got %v", c.name, got)
		}
	}
}

// TestReverseISBRoomRuleRefused — a reverse ISB whose target sits between the
// floor and 2R emits NO reverse entry and counts "room". Mutant: delete the
// reverse-ISB room check in eval.go → RED (the reverse emits).
func TestReverseISBRoomRuleRefused(t *testing.T) {
	e, bars, now := isbReverseFixture(t)
	e.Cfg.RoomMultiple = 2 // default room; the reverse target (110) is 5.5 pts on a 5-pt risk → < 2R
	ins := e.Tick(bars, now)
	for _, in := range ins {
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == SideLong {
			t.Fatalf("the reverse ISB must be refused by the room rule; got %+v; refusals=%v", in, e.State.Refusals)
		}
	}
	if e.State.Refusals["room"] == 0 {
		t.Fatalf("room refusal not counted; refusals=%v", e.State.Refusals)
	}
}
