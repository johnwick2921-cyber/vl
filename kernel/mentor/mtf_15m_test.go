package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// Item 15 (RELEASE #4): the 15m ISB box (D4.1-25), the 15m direction (D4.2-02)
// and the PERSISTENT 15m/5m conflict that cancels arms BY ArmID (D4.2-03).

// mtfFixture seeds an evaluator whose last 1m pair is an ISB (candle-1 colour
// driven by red) with a long trigger and a level above at 110.
func mtfFixture(t *testing.T, red bool) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	cfg.RoomMultiple = 0 // item 25: this fixture pins the MTF box gates, not the room rule
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{}
	for i := 0; i < 40; i++ {
		bars = append(bars, mk(i, 101, 101.5, 100.5, 101))
	}
	if red {
		bars = append(bars,
			mk(40, 106, 106.5, 99.5, 100), // red candle 1 → ISB short
			mk(41, 102, 104.5, 99.5, 104), // inside bar 2
		)
	} else {
		bars = append(bars,
			mk(40, 99.5, 106.5, 99.5, 106), // green candle 1 → ISB long
			mk(41, 102, 104.5, 100.5, 104), // inside bar 2
		)
	}
	now := bars[len(bars)-1].CloseTime
	levels := []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 90},
		{Key: "K-above", Kind: KindKeyLevel, Price: 110},
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 101}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

func entryIntents(ins []Intent) []Intent {
	var out []Intent
	for _, in := range ins {
		if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
			out = append(out, in)
		}
	}
	return out
}

// TestISB15mBoxBlocksCounterDirection — D4.1-25: a LONG 1m ISB is refused while
// a SHORT 15m box stands ("allow only entries in the 15m ISB direction"). The
// fixture passes every other gate (trigger long, 4h long, target above), so the
// 15m box is the ONLY thing blocking the emit. Mutant: drop the box15Blocked
// gate → the long ISB emits → RED.
func TestISB15mBoxBlocksCounterDirection(t *testing.T) {
	e, bars, now := mtfFixture(t, false) // long ISB
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	if got := entryIntents(ins); len(got) != 0 {
		t.Fatalf("counter-direction ISB must be blocked by the 15m box, got %+v", got)
	}
	if e.State.Refusals["isb_box_blocked_15m"] == 0 {
		t.Fatalf("ledger = %v, want isb_box_blocked_15m counted", e.State.Refusals)
	}
}

// TestISB15mBoxAllowsSameDirection — D4.1-25/D4.2-02: inside a LONG 15m box a
// LONG 1m ISB still fires. Mutant: gate on the wrong side → RED.
func TestISB15mBoxAllowsSameDirection(t *testing.T) {
	e, bars, now := mtfFixture(t, false) // long ISB
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	ins := e.Tick(bars, now)
	var longISB bool
	for _, in := range entryIntents(ins) {
		if in.Setup == "ISB" && in.Side == SideLong {
			longISB = true
		}
	}
	if !longISB {
		t.Fatalf("same-direction ISB must fire inside the 15m box; intents %+v refusals %v", ins, e.State.Refusals)
	}
}

// TestMTFConflictCancelsArmsAndRefusesAll — D4.2-03: with the 5m box SHORT and
// the 15m box LONG the conflict is a standing STATE: every live arm is cancelled
// BY ArmID (the trader refuses a no-ArmID cancel) and no setup (ISB, PHL/PLH,
// box return) trades. Mutant: cancel with no ArmID → RED.
func TestMTFConflictCancelsArmsAndRefusesAll(t *testing.T) {
	e, bars, now := mtfFixture(t, false) // long ISB would otherwise fire
	e.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.ISBBox15m = &ISBBox{High: 110, Low: 95, Dir: SideLong, AtTime: bars[0].OpenTime}
	e.State.ISBArms["isb-7"] = ISBArm{FirstBar: bars[len(bars)-1], Inside: 0, Side: SideShort}
	e.State.LevelArms = map[string]LevelArm{"K-above": {ArmID: "lvl-3", Side: SideLong, LevelPrice: 110, PlacedAt: bars[len(bars)-2].CloseTime}}

	ins := e.Tick(bars, now)

	var cancels []Intent
	for _, in := range ins {
		if in.Action == CancelArm {
			cancels = append(cancels, in)
			if in.ArmID == "" {
				t.Fatalf("the conflict cancel must carry an ArmID; got %+v", in)
			}
		}
	}
	if len(cancels) != 2 {
		t.Fatalf("conflict must cancel BOTH arms, got %+v", cancels)
	}
	if got := entryIntents(ins); len(got) != 0 {
		t.Fatalf("conflict must refuse every setup, got entries %+v", got)
	}
	if len(e.State.ISBArms) != 0 || len(e.State.LevelArms) != 0 {
		t.Fatalf("conflict must clear the live arms: ISBArms=%v LevelArms=%v", e.State.ISBArms, e.State.LevelArms)
	}
}

// TestMTFConflictNoConflictWhenSameDirection — the same-direction pair is NOT a
// conflict (the 5m box and 15m box agree → normal trading).
func TestMTFConflictNoConflictWhenSameDirection(t *testing.T) {
	if mtfConflict(&ISBBox{Dir: SideShort}, &ISBBox{Dir: SideLong}) != true {
		t.Fatal("opposite boxes must conflict")
	}
	if mtfConflict(&ISBBox{Dir: SideLong}, &ISBBox{Dir: SideLong}) != false {
		t.Fatal("same-direction boxes must NOT conflict")
	}
	if mtfConflict(nil, &ISBBox{Dir: SideLong}) != false {
		t.Fatal("a lone 15m box must NOT conflict")
	}
	if mtfConflict(&ISBBox{Dir: SideShort}, nil) != false {
		t.Fatal("a lone 5m box must NOT conflict")
	}
}
