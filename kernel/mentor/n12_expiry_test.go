package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestISBIntentCarriesNextCandleExpiry — N12 (CTO 1791007969871): a single ISB
// fills by the close of the NEXT 1m candle or it is cancelled ("cancel if the
// next candle does not fill" [D1.4 p1 @ 18:32–18:45]).
func TestISBIntentCarriesNextCandleExpiry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// a head bar with a high close so the EMA target clears the E-2 1:1
	// floor ("the target is never smaller than the stop" [D1.2 p1 @ 07:48]).
	head := market.Kline{Open: 121, High: 121.5, Low: 120.5, Close: 120, OpenTime: auditMs(2026, 9, 15, 9, 0, 0), CloseTime: auditMs(2026, 9, 15, 9, 0, 0) + 59_999}
	prev := market.Kline{Open: 99, High: 106, Low: 98.5, Close: 106, OpenTime: auditMs(2026, 9, 15, 9, 1, 0), CloseTime: auditMs(2026, 9, 15, 9, 1, 0) + 59_999}
	cur := market.Kline{Open: 101, High: 102.1, Low: 100.9, Close: 102, OpenTime: auditMs(2026, 9, 15, 9, 2, 0), CloseTime: auditMs(2026, 9, 15, 9, 2, 0) + 59_999}
	now := cur.CloseTime + 1
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	ints := e.Tick([]market.Kline{head, prev, cur}, now)
	found := false
	for _, in := range ints {
		if in.Action == PlaceStopLimitEntry {
			found = true
			if in.ExpiryMs != cur.CloseTime+60_000 {
				t.Fatalf("ISB expiry = %d, want %d (close of the next 1m candle)", in.ExpiryMs, cur.CloseTime+60_000)
			}
		}
	}
	if !found {
		t.Fatalf("no ISB intent: %+v", ints)
	}
}

// TestStackingExtendsAndCancelsAtFourth — N12 + stacking [D4.2 p2 @08:21–16:05]:
// while the candles stay inside the mother candle the expiry is pushed forward
// (ExtendArm) and the 4th inside candle cancels the arm.
func TestStackingExtendsAndCancelsAtFourth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	e.State.ORB = ORB{Day: 0, High: 200, Low: 50, Drawn: true, Escaped: SideLong}
	mother := market.Kline{Open: 95, Close: 105, High: 110, Low: 90} // body [95,105]
	e.State.ISBArms["a"] = ISBArm{FirstBar: mother, Inside: 0}       // fresh arm
	// prev/cur are NOT an ISB pair (cur's body outside prev's range) so no new
	// arm is placed; cur stays inside the MOTHER candle.
	prev := market.Kline{Open: 95, High: 96, Low: 94, Close: 95, CloseTime: 120_000}
	cur := market.Kline{Open: 100, High: 105, Low: 95, Close: 100, CloseTime: 179_999}
	bars := []market.Kline{prev, cur}
	now := cur.CloseTime + 1

	extends, cancels := 0, 0
	for i := 0; i < 4; i++ { // Inside 1, 2 (holds -> extends), 3 (cancel), then gone
		for _, in := range e.Tick(bars, now) {
			switch in.Action {
			case ExtendArm:
				extends++
				if in.ExpiryMs != cur.CloseTime+60_000 {
					t.Fatalf("extend expiry = %d, want the next 1m close", in.ExpiryMs)
				}
			case CancelArm:
				if strings.HasPrefix(in.Reason, "ISB stacking") {
					cancels++
				}
			}
		}
	}
	if extends != 2 {
		t.Fatalf("extends = %d, want 2 (holds at the 2nd and 3rd inside candles)", extends)
	}
	if cancels != 1 {
		t.Fatalf("cancels = %d, want 1 (the 4th inside candle)", cancels)
	}
}
