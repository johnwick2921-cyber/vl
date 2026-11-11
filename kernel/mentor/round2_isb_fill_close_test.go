package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// D2-G3 (oct round 2): OctD2.2-R35/R37 [Oct D2.2 @ 28:40, 30:03] - the ISB gives its
// own first target, the fill candle's close ("target đầu tiên... bán bớt... khi cây
// nến trend phiêu lệnh... đóng"); R34 @ 28:12 puts the 1:1 theory and level targets
// aside ("not taught yet"). So an ISB with no level beyond (isb_missing_target) or
// only a level inside 1R (isb_target_below_floor) is no longer refused: the first
// target is the leg-1 TP (+1R) the trader owns and the partial runs at the fill
// candle's close. The fixtures are the production Tick fixtures of
// TestISBMissingTargetSkipsOnlyThatISB / TestISBTargetBelowFloorRefused.
func d2g3Run(t *testing.T, ema34 float64, closeFirst float64, mut func(*Config)) (*Evaluator, []Intent) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	// the target-rule fixtures exercise this stage only: no later-lesson 4h/1h gate.
	cfg.HTFGateNewsOnly = true
	if mut != nil {
		mut(&cfg)
	}
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 90}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 90}}
	bars := []market.Kline{
		rthBars(1, 99, 106, 98.5, closeFirst),
		rthBars(2, 101, 102.1, 100.9, 102),
	}
	now := bars[1].CloseTime + 1
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.EMA34 = ema34
	e.State.EMA9 = 0
	return e, e.Tick(bars, now)
}

func d2g3Entry(ins []Intent) *Intent {
	for i := range ins {
		if ins[i].Action == PlaceStopLimitEntry || ins[i].Action == PlaceStopEntry {
			return &ins[i]
		}
	}
	return nil
}

func TestD2G3ISBBasicTargetIsTheFillCandleClose(t *testing.T) {
	if DefaultConfig().ISBTargetFillClose {
		t.Fatal("D2-G3 (review): ISBTargetFillClose must default OFF - the course does not say a no-target ISB may be traded [B]")
	}
	cases := []struct {
		name, refusal string
		ema34, close1 float64
	}{
		{"no level beyond (isb_missing_target)", "isb_missing_target", 0, 100},
		{"level inside 1R (isb_target_below_floor)", "isb_target_below_floor", 106, 106},
	}
	for _, c := range cases {
		e, ins := d2g3Run(t, c.ema34, c.close1, func(cf *Config) { cf.ISBTargetFillClose = true })
		in := d2g3Entry(ins)
		if in == nil {
			t.Fatalf("%s: October trades the ISB; refusals %v", c.name, e.State.Refusals)
		}
		risk := in.Price - in.Stop
		if in.Target != in.Price+risk {
			t.Fatalf("%s: first target must be the +1R leg-1 TP %.2f, got %.2f", c.name, in.Price+risk, in.Target)
		}
		e, ins = d2g3Run(t, c.ema34, c.close1, nil)
		if d2g3Entry(ins) != nil || e.State.Refusals[c.refusal] == 0 {
			t.Fatalf("%s: default (knob isb_target_fill_close off = September) refuses %s; intents %+v ledger %v", c.name, c.refusal, ins, e.State.Refusals)
		}
	}
}
