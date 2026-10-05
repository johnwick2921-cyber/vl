package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestISBOneArmFiveCandleSequence — CTO parity ruling 1791008332386 #1: ONE
// active ISB arm per side; the reference is the FIRST ISB. A 5-candle inside
// sequence gives exactly ONE arm, extended twice (the 2nd and 3rd inside
// candles), then cancelled (the 4th).
func TestISBOneArmFiveCandleSequence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // item 25: this fixture pins arm stacking, not the room rule
	// real-UTC epochs (EPOCH RULING): RTH bars on 2026-09-15 CT.
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := auditMs(2026, 9, 15, 9, 0, 0) + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c}
	}
	head := mk(0, 121, 121.5, 120.5, 120)
	mother := mk(1, 98, 106, 97, 105) // green
	c1 := mk(2, 103, 104, 99, 100)    // red, body inside mother
	c2 := mk(3, 100, 101.5, 99.5, 101)
	c3 := mk(4, 100, 101.5, 99.5, 101)
	c4 := mk(5, 100, 101.5, 99.5, 101)
	bars := []market.Kline{head, mother, c1, c2, c3, c4}

	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(head.OpenTime), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(head.OpenTime).In(ctime())), Verdict: DayTrade}

	placements, extends, cancels := 0, 0, 0
	for i := 2; i <= len(bars); i++ {
		now := bars[i-1].CloseTime + 1
		for _, in := range e.Tick(bars[:i], now) {
			switch {
			case in.Action == PlaceStopLimitEntry:
				placements++
			case in.Action == ExtendArm:
				extends++
			case in.Action == CancelArm && strings.HasPrefix(in.Reason, "ISB stacking"):
				cancels++
			}
		}
	}
	if placements != 1 {
		t.Fatalf("placements = %d, want exactly ONE arm for the sequence", placements)
	}
	if extends != 2 {
		t.Fatalf("extends = %d, want 2 (the 2nd and 3rd inside candles)", extends)
	}
	if cancels != 1 {
		t.Fatalf("cancels = %d, want 1 (the 4th inside candle)", cancels)
	}
}
