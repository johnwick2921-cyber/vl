package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// Item 26 (RELEASE #4): the reverse ISB at EMA 9 never set a Target, so the
// trader geometry gate refused every one ("bad geometry"). Now it carries the
// normal ISB target rule (next level beyond + 1:1 floor + spent cap), and R85
// suppresses the normal ISB's counter-trend arm on the same candle pair.

// TestSuppressSamePairCounterISB (R85 helper): a counter-trend normal ISB of the
// opposite side on the SAME reference candle is dropped and its ArmID returned
// for unregistration. Mutant: skip the filter → RED.
func TestSuppressSamePairCounterISB(t *testing.T) {
	ref := int64(1760000000000)
	normalShort := Intent{Action: PlaceStopEntry, Setup: "ISB", Side: SideShort, RefBarMs: ref, ArmID: "isb-1"}
	reverseLong := Intent{Action: PlaceStopEntry, Setup: "ISB", Side: SideLong, RefBarMs: ref}
	kept, suppressed := suppressSamePairCounterISB([]Intent{normalShort}, reverseLong, ref)
	if len(kept) != 0 {
		t.Fatalf("the counter-trend normal ISB must be dropped, got %+v", kept)
	}
	if len(suppressed) != 1 || suppressed[0] != "isb-1" {
		t.Fatalf("the dropped arm id must be returned, got %+v", suppressed)
	}
	// a same-side or other-pair intent is untouched.
	sameSide := Intent{Action: PlaceStopEntry, Setup: "ISB", Side: SideLong, RefBarMs: ref}
	kept, suppressed = suppressSamePairCounterISB([]Intent{sameSide}, reverseLong, ref)
	if len(kept) != 1 || len(suppressed) != 0 {
		t.Fatalf("a same-side ISB must survive, got kept=%+v suppressed=%+v", kept, suppressed)
	}
	otherPair := Intent{Action: PlaceStopEntry, Setup: "ISB", Side: SideShort, RefBarMs: ref + 60_000}
	kept, suppressed = suppressSamePairCounterISB([]Intent{otherPair}, reverseLong, ref)
	if len(kept) != 1 || len(suppressed) != 0 {
		t.Fatalf("a different-pair ISB must survive, got kept=%+v suppressed=%+v", kept, suppressed)
	}
}

// isbReverseFixture seeds a LONG-trend evaluator whose last 1m pair is a SHORT
// ISB with the EMA 9 inside the ISB candle and a key level above — so the
// reverse ISB fires LONG and the target resolves to that level.
func isbReverseFixture(t *testing.T) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.HTFGateNewsOnly = false // these pins exercise the direction gate itself (all-day)
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.EMALocationTFMinutes = 0
	cfg.ISBReverseEMA9Enabled = true
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	cfg.RoomMultiple = 0.5
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{}
	for i := 0; i < 40; i++ {
		bars = append(bars, mk(i, 101, 101.5, 100.5, 101))
	}
	bars = append(bars,
		mk(40, 106, 106.5, 99.5, 100), // red candle 1 → ISB direction short
		mk(41, 102, 104.5, 99.5, 104), // inside bar 2
	)
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

// TestReverseISBEmitsWithTarget (item 26 / R81): a reverse ISB emits WITH a
// target (the next level beyond in the trade direction), so the trader geometry
// gate no longer refuses it. Mutant: drop the target assignment → RED.
func TestReverseISBEmitsWithTarget(t *testing.T) {
	e, bars, now := isbReverseFixture(t)
	e.Cfg.RoomMultiple = 0 // option B: this pin tests the target assignment, not the room halving
	ins := e.Tick(bars, now)
	var reverse *Intent
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == SideLong {
			if reverse == nil {
				reverse = in
			} else {
				t.Fatalf("two long ISB entries on one tick: %+v", ins)
			}
		}
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == SideShort {
			t.Fatalf("R85: the counter-trend short ISB must not arm beside the reverse: %+v", ins)
		}
	}
	if reverse == nil {
		t.Fatalf("the reverse ISB must fire (LONG); got intents %+v refusals %v", ins, e.State.Refusals)
	}
	if reverse.Target <= 0 || reverse.Target != 110 {
		t.Fatalf("reverse ISB target = %.2f, want the next level beyond (110); intents %+v", reverse.Target, ins)
	}
	if reverse.Stop <= 0 || reverse.Price <= 0 {
		t.Fatalf("reverse ISB geometry must be complete: %+v", reverse)
	}
}

// TestReverseISBObeysThe4hAgainstANormalISB (R85 call-site pin, CTO release
// #4): the reverse ISB now runs through the normal ISB gates, the 4h side
// included. With the 4h SHORT, the normal ISB's short arms and the reverse
// ISB's LONG (5m-trend side, against the 4h) is REFUSED isbrev_htf_side_mismatch
// — the 4h governs, so two opposite orders on one candle cannot both pass.
// (The same-pair suppression stays as defence in depth; its helper is pinned
// by TestSuppressSamePairCounterISB.) Mutant: drop the reverse HTF side gate →
// the long fires → RED.
func TestReverseISBObeysThe4hAgainstANormalISB(t *testing.T) {
	e, bars, now := isbReverseFixture(t)
	e.State.ISBBox = &ISBBox{High: 110, Low: 95, Dir: SideShort, AtTime: bars[0].OpenTime}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideShort, Price: 120}}

	ins := e.Tick(bars, now)
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == SideLong {
			t.Fatalf("a reverse ISB against the 4h must not fire; intents %+v", ins)
		}
	}
	if e.State.Refusals["isbrev_htf_side_mismatch"] < 1 {
		t.Fatalf("want isbrev_htf_side_mismatch counted; refusals %v", e.State.Refusals)
	}
}

// R85 (CTO, release #4): the reverse ISB obeys the twenties stop skip like the
// normal ISB [D4.1 p1 @ 05:41]. The fixture's inside candle gives a 5-pt stop;
// a twenties window moved to [4, 14) puts it inside → refused
// isbrev_stop_twenties. Mutant: drop the gate → the reverse fires → RED.
func TestReverseISBSkipsTheTwenties(t *testing.T) {
	e, bars, now := isbReverseFixture(t)
	e.Cfg.ISBTwentiesPts = 4
	ins := e.Tick(bars, now)
	for i := range ins {
		if in := &ins[i]; (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" && in.Side == SideLong {
			t.Fatalf("a reverse ISB with a stop in the twenties window must not fire; intents %+v", ins)
		}
	}
	if e.State.Refusals["isbrev_stop_twenties"] < 1 {
		t.Fatalf("want isbrev_stop_twenties counted; refusals %v", e.State.Refusals)
	}
}
