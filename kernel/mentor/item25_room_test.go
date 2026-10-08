package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// Item 25 (RELEASE #4, re-scoped by FIX-ROOM-RULE-LESSON): the swing reject's
// room rule lives in swingRoomRefused — the obstacle (the on-side 5m EMA34)
// must be at least RoomMultiple × risk from the entry. The swing's leg-1 target
// is "at least 1:1" (R43), so under the D5.3 lesson (room >= RoomMultiple ×
// target) the planned target distance equals the risk and the two forms
// coincide. The ISB / reverse-ISB / box / PHL room checks are pinned in
// room_rule_lesson_test.go with the new room_vs_target / rr_floor /
// rr_confluence keys.

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
