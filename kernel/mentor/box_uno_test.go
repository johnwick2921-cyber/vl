package mentor

import (
	"strings"
	"testing"
	"time"

	"vl/market"
)

// D14 "Uno Reverse" [slide 17; X2 @02:36–03:25]: a broken FTGH becomes
// SUPPORT, a broken FTGL becomes RESISTANCE. "Kháng cự bị phá → sẽ thành hỗ
// trợ khi backtest. Hỗ trợ bị phá → sẽ thành kháng cự khi backtest."
// A broken FTGH re-approached from ABOVE must trade LONG (the flipped side)
// with the same reject rule and the same gates — before the flip, no return
// was ever counted from above (box_trade.go counted only the original
// approach side).
func TestEvaluatorUnoReverseBrokenFTGHTradesLong(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 105, 100, 104), // swing high @2 — pairs with @4 (nearest)
		mk(3, 103, 104, 102, 103),
		mk(4, 102, 106, 101, 105),       // swing high @4 — the extreme; FTGH [104, 106]
		mk(5, 105, 105.8, 99, 100),      // confirms bar 4 (high 105.8 < 106)
		mk(6, 106.2, 106.5, 105, 106.4), // ESCAPE: whole body above 106 → role flips
		// bar 7 blocks bar 6 from confirmation (106.6 > 106.5) and is the ONE
		// return: it re-approaches the broken ceiling from ABOVE, touches 106
		// (low 105.9), and closes back above it (106.3 > 106) = the reject.
		mk(7, 106.4, 106.6, 105.9, 106.3),
		// bar 8: the target key level (colour flip → open 106.8); its high
		// 106.9 also blocks bar 7, so neither escape candle becomes the new
		// extreme and the box stays [104, 106].
		mk(8, 106.8, 106.9, 106.6, 106.85),
	}
	e := New(cfg)
	now := bars[8].OpenTime + 59_999
	// ORB preset (the §7 gate is drawn+escaped long; the tape alone never
	// draws an ORB and the gate would refuse every intraday entry).
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	// B9 presets: box entries obey the HTF/day gates — 4h long (1h silent),
	// a normal measured day.
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	boxIntents := func(ins []Intent) []Intent {
		var out []Intent
		for _, in := range ins {
			if strings.HasPrefix(in.Reason, "box edge return") {
				out = append(out, in)
			}
		}
		return out
	}
	first := boxIntents(e.Tick(bars, now))
	if len(first) != 1 {
		t.Fatalf("flipped FTGH return = %d box intents (%+v), want 1 — a broken FTGH re-approached from above trades LONG [D14, slide 17; X2 @02:36–03:25]", len(first), first)
	}
	in := first[0]
	if in.Side != SideLong || in.Price != 106.6 || in.Stop != 105.9 || in.Target != 106.8 {
		t.Fatalf("flipped FTGH intent = %+v, want LONG entry 106.6 / stop 105.9 / target 106.8", in)
	}
}
