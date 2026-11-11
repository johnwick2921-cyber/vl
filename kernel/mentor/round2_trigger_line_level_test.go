package mentor

import (
	"testing"

	"vl/market"
)

// reread D43A-U1 (oct round 2), verified [A] already correct: Mon 02 Nov 2026, the
// mentor's 5m sell-line demo [Oct D4.3 @ 07:56-08:52, frame s_00478]. The candle whose
// low was broken is the peak green candle O 28,859.00 H 28,891.50 L 28,834.25
// C 28,886.75; the line is drawn where the candle broke the BOTTOM, i.e. at that
// candle's wick low 28,834.25 - never at the breaking candle's own low (the demo
// screen's 28,828.50-28,831.50). applyBreak sets the price from the broken candle
// (before.Low); this pins it through the production TriggerTick.
func TestD43AU1TriggerLineSitsOnTheBrokenCandleLow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	at := func(m int) int64 { return 1_793_000_000_000 - 1_793_000_000_000%300_000 + int64(m)*300_000 }
	peak := market.Kline{OpenTime: at(0), CloseTime: at(1) - 1, Open: 28_859, High: 28_891.5, Low: 28_834.25, Close: 28_886.75}
	breaker := market.Kline{OpenTime: at(1), CloseTime: at(2) - 1, Open: 28_880, High: 28_884, Low: 28_828.5, Close: 28_830.25}
	next := market.Kline{OpenTime: at(2), CloseTime: at(3) - 1, Open: 28_830, High: 28_835, Low: 28_826, Close: 28_829}
	got := TriggerTick(TriggerLine{}, []market.Kline{peak, breaker, next}, 5, cfg)
	if got.Dir != SideShort || got.Price != 28_834.25 {
		t.Fatalf("the sell line must sit on the broken candle's low 28834.25, got %+v", got)
	}
}
