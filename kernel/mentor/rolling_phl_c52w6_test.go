package mentor

import (
	"strings"
	"testing"

	"vl/market"
)

// c52 W6 (plan A8): the rolling PHL/PLH gets (1) the pullback precondition —
// the ladder starts only on a counter-trend coloured candle (red in an
// uptrend, green in a downtrend) [D2.2 p1 @01:52–01:57, @10:47–10:51] — and
// (2) the 5m trigger-side gate [X8 part1 @06:25, @08:37–08:48]. Both are
// pinned through e.Tick, the production call site.

func rollingKline(t0 int64, i int, o, h, l, c float64) market.Kline {
	return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
}

// TestC52W6RollingNeedsCounterTrendCandle — an uptrend whose newest closed
// candle is GREEN (with the trend, lower high than the old extreme) is not a
// pullback: no rolling stop is armed. The same tape with a RED newest candle
// fires (control, = TestRollingPHLPullbackFires).
// Mutant: drop the rollingPullbackCandle gate → the green candle arms → RED.
func TestC52W6RollingNeedsCounterTrendCandle(t *testing.T) {
	e, bars, now := rollingFixture()
	t0 := bars[0].OpenTime
	// bar 5 turned GREEN (open 113 → close 117.5), high 118, low 112.
	bars[5] = rollingKline(t0, 5, 113, 118, 112, 117.5)
	if got := rollingPlaceEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("a with-trend (green) newest candle armed a rolling PHL: %+v", got)
	}
	if e.State.RollingPHL.ArmID != "" {
		t.Fatalf("no rolling state may start on a with-trend candle, got %+v", e.State.RollingPHL)
	}
	// control: the red tape fires.
	e2, bars2, now2 := rollingFixture()
	if got := rollingPlaceEntries(e2.Tick(bars2, now2)); len(got) != 1 {
		t.Fatalf("control: the red pullback tape must arm exactly once, got %+v", got)
	}
}

// TestC52W6RollingKeepsRestingOrderThroughWithTrendCandle — a resting roll is
// NOT cancelled, replaced or duplicated by a with-trend candle that does not
// fill it; the next RED candle rolls it again.
// Mutant: make the colour gate cancel (rollingCancel) instead of returning nil
// → the resting arm is cancelled → RED.
func TestC52W6RollingKeepsRestingOrderThroughWithTrendCandle(t *testing.T) {
	e, bars, now := rollingFixture()
	if len(rollingPlaceEntries(e.Tick(bars, now))) != 1 {
		t.Fatal("setup: the first roll must arm")
	}
	firstArm := e.State.RollingPHL.ArmID
	t0 := bars[0].OpenTime
	// bar 6: GREEN, high 118.5 < the resting entry 119 → does not fill it.
	next := append(append([]market.Kline{}, bars...), rollingKline(t0, 6, 113, 118.5, 112.5, 118))
	ins := e.Tick(next, next[6].OpenTime+59_999)
	for _, in := range ins {
		if in.Action == CancelArm || in.Action == PlaceStopEntry {
			t.Fatalf("a with-trend candle must leave the resting roll alone, got %+v", in)
		}
	}
	if e.State.RollingPHL.ArmID != firstArm {
		t.Fatalf("resting arm changed %q -> %q", firstArm, e.State.RollingPHL.ArmID)
	}
}

// TestC52W6RollingShortNeedsGreenBounce — the mirror: in a downtrend a RED
// newest candle (with the trend) does not start a rolling sell stop.
// Mutant: gate on `Close < Open` for shorts too → the green bounce is refused
// and this (and the existing short mirror test) go RED.
func TestC52W6RollingShortNeedsGreenBounce(t *testing.T) {
	if !rollingPullbackCandle(market.Kline{Open: 128, Close: 130}, SideShort) {
		t.Fatal("a green candle is the short's pullback")
	}
	if rollingPullbackCandle(market.Kline{Open: 130, Close: 128}, SideShort) {
		t.Fatal("a red candle is NOT a short's pullback")
	}
	if rollingPullbackCandle(market.Kline{Open: 128, Close: 128}, SideShort) || rollingPullbackCandle(market.Kline{Open: 128, Close: 128}, SideLong) {
		t.Fatal("a doji is no pullback")
	}
}

// TestC52W6RollingRefusedAgainstTrigger — LocTriggerFilter on, the 5m trigger
// line points the other way (sell side while the 4h trend is long): the roll
// is refused and counted; with the trigger on the roll's side it fires.
// Mutant: delete the TriggerVerdict block → the long roll arms against the sell
// trigger → RED.
func TestC52W6RollingRefusedAgainstTrigger(t *testing.T) {
	e, bars, now := rollingFixture()
	e.Cfg.LocTriggerFilter = true
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 200}
	if got := rollingPlaceEntries(e.Tick(bars, now)); len(got) != 0 {
		t.Fatalf("a rolling long armed against the 5m sell trigger: %+v", got)
	}
	n := 0
	for k, v := range e.State.Refusals {
		if k == "rolling_phl_trigger_side" {
			n += v
		}
	}
	if n != 1 {
		t.Fatalf("want rolling_phl_trigger_side counted once, ledger=%v", e.State.Refusals)
	}
	// control: trigger on the long side, price above the buy line.
	e2, bars2, now2 := rollingFixture()
	e2.Cfg.LocTriggerFilter = true
	e2.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	if got := rollingPlaceEntries(e2.Tick(bars2, now2)); len(got) != 1 {
		t.Fatalf("control: trigger agrees, the roll must arm, got %+v", got)
	}
}

// TestC52W6RollingCancelsRestingWhenTriggerFlips — a resting roll is cancelled
// the candle the 5m trigger flips against it.
// Mutant: return nil instead of rollingCancel in the trigger block → the
// resting stop survives the flip → RED.
func TestC52W6RollingCancelsRestingWhenTriggerFlips(t *testing.T) {
	e, bars, now := rollingFixture()
	e.Cfg.LocTriggerFilter = true
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	if len(rollingPlaceEntries(e.Tick(bars, now))) != 1 {
		t.Fatal("setup: the roll must arm")
	}
	firstArm := e.State.RollingPHL.ArmID
	t0 := bars[0].OpenTime
	next := append(append([]market.Kline{}, bars...), rollingKline(t0, 6, 113, 114, 108, 109))
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 200}
	ins := e.Tick(next, next[6].OpenTime+59_999)
	var cancelled bool
	for _, in := range ins {
		if in.Action == CancelArm && in.ArmID == firstArm && strings.Contains(in.Reason, "5m trigger") {
			cancelled = true
		}
		if in.Action == PlaceStopEntry && in.Setup == "PHL" {
			t.Fatalf("no new roll against the flipped trigger: %+v", in)
		}
	}
	if !cancelled {
		t.Fatalf("the resting roll must be cancelled on the trigger flip: %+v", ins)
	}
}
