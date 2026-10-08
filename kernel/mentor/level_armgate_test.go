package mentor

import (
	"math"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestLevelOrderRestsAndCancels — B6 + B16 (10-03 rulings): a level order
// RESTS (no one-candle expiry — that is the ISB rule only [D1.4 p1 @18:32]);
// repeated touches while it rests do NOT re-emit it (one intent per
// reference [D5.3 p1 @20:40–22:12]); a LATER candle closing through the
// level cancels it ("Mình cancel" [D2.3 p1 @18:01–19:12, recovered @23:48]).
func TestLevelOrderRestsAndCancels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PHLTargetShyPts = 6
	cfg.RoomMultiple = 0 // this pin is about arm rest/cancel, not the room rule

	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = 0
	e.State.EMA9 = 0
	e.State.SeedLevels = []Level{
		{Key: "L", Kind: KindKeyLevel, Price: 29385},
		{Key: "old-high:29431.75", Kind: KindOldExtreme, Price: 29431.75},
	}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 29300}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 29300}}

	bars := []market.Kline{
		rthBars(-1, 29340, 29420, 29335, 29415),   // D2-28: the low the leg to the old high started from (29335, the course's own example)
		rthBars(0, 29420, 29431.75, 29410, 29425), // the old high (extreme)
		rthBars(1, 29405, 29408, 29395, 29402),
		rthBars(2, 29392, 29402, 29370, 29396),
		rthBars(3, 29390, 29392, 29385, 29391), // reject touch of L=29385 (low touches, close above)
	}
	now := bars[4].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}

	ints := e.Tick(bars, now)
	armed := false
	for _, in := range ints {
		if in.Action == PlaceStopEntry && in.Setup == "PHL" && in.Side == SideLong && in.Price == 29393 {
			armed = true
			break
		}
	}
	if !armed {
		t.Fatalf("B6 precondition: expected a LONG PHL stop-entry at 29393 (high 29392 + 1.0 buffer) before testing rest/cancel behavior; intents=%+v refusals=%v", ints, e.State.Refusals)
	}
	phl := 0
	for _, in := range ints {
		if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "PHL") {
			phl++
			if in.ExpiryMs != 0 {
				t.Fatalf("B6: a level order RESTS — ExpiryMs must be 0, got %+v", in)
			}
			if in.ArmID == "" {
				t.Fatalf("B6: a resting level order needs an ArmID for the cancel sweep, got %+v", in)
			}
		}
	}
	if phl != 1 {
		t.Fatalf("want exactly one PHL intent, got %d (%+v)", phl, ints)
	}
	arm := e.State.LevelArms["L"]
	if arm.ArmID == "" || arm.Side != SideLong || arm.LevelPrice != 29385 {
		t.Fatalf("resting arm not recorded: %+v", e.State.LevelArms)
	}

	// B16: a second reject touch while the order rests — no re-emission.
	b4 := rthBars(4, 29392, 29395, 29384, 29393)
	ints2 := e.Tick(append(append([]market.Kline{}, bars...), b4), b4.CloseTime+1)
	for _, in := range ints2 {
		if in.Action == PlaceStopEntry && strings.HasPrefix(in.Reason, "PHL") {
			t.Fatalf("B16: a resting level order must not re-emit on a repeated touch: %+v", in)
		}
	}

	// B6: a LATER candle closing THROUGH the level cancels the rest.
	b5 := rthBars(5, 29388, 29389, 29360, 29365)
	ints3 := e.Tick(append(append(append([]market.Kline{}, bars...), b4), b5), b5.CloseTime+1)
	cancelled := false
	for _, in := range ints3 {
		if in.Action == CancelArm && in.ArmID == arm.ArmID {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatalf("B6: a close through the level must cancel the resting order; intents = %+v", ints3)
	}
	if len(e.State.LevelArms) != 0 {
		t.Fatalf("the resting arm must be gone after the cancel: %+v", e.State.LevelArms)
	}
}

// TestLevelArmCancelAtWindowEnd — B6: at the RTH window end (15:00 CT) the
// resting order is cancelled even without a close-through.
func TestLevelArmCancelAtWindowEnd(t *testing.T) {
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

	// bars at 15:04–15:05 CT — past the 15:00 window end.
	b0 := rthBars(0, 29390, 29395, 29380, 29390)
	b0.OpenTime = auditMs(2026, 9, 15, 15, 4, 0)
	b0.CloseTime = auditMs(2026, 9, 15, 15, 5, 0) - 1
	b1 := rthBars(1, 29391, 29394, 29386, 29392)
	b1.OpenTime = auditMs(2026, 9, 15, 15, 5, 0)
	b1.CloseTime = auditMs(2026, 9, 15, 15, 6, 0) - 1
	bars := []market.Kline{b0, b1}
	now := b1.CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 29300, Low: 29200, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.LevelArms = map[string]LevelArm{
		"L": {ArmID: "lvl-9", Side: SideLong, LevelPrice: 29385, PlacedAt: auditMs(2026, 9, 15, 10, 0, 0)},
	}

	ints := e.Tick(bars, now)
	for _, in := range ints {
		if in.Action == CancelArm && in.ArmID == "lvl-9" {
			return // cancelled at the window end even though no candle closed through
		}
	}
	t.Fatalf("B6: the window end must cancel the resting order; intents = %+v", ints)
}
