package mentor

import (
	"testing"
	"time"

	"vl/market"
)

const maxInt64 = int64(^uint64(0) >> 1)

func TestLevelTriggerSideDropIsCounted(t *testing.T) {
	oldHighs := []Level{{Key: "old-high:130", Kind: KindOldExtreme, Price: 130}}
	e, bars, now, _ := b14Fixture(oldHighs, 99.5)
	e.Cfg.LocTriggerFilter = true
	e.Cfg.TriggerSchool = 2
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 120, LastBucket: maxInt64}

	intents := e.Tick(bars, now)
	if e.State.Refusals["isb_trigger_side"] == 0 {
		t.Fatalf("wrong-side level drop was not counted: ledger = %v", e.State.Refusals)
	}
	if n := b14Entries(intents); n != 0 {
		t.Fatalf("wrong-side level emitted %d entries: %+v", n, intents)
	}
}

func TestBoxTriggerSideDropIsCounted(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.RoomMultiple = 0.05
	cfg.TriggerSchool = 2

	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{
			OpenTime:  t0 + int64(i)*60_000,
			CloseTime: t0 + int64(i)*60_000 + 59_000,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     c,
		}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100),
		mk(1, 100, 102, 99, 101),
		mk(2, 101, 103, 95, 96),
		mk(3, 96.5, 97, 96, 96.8), // confirms the extreme (low 96 > 95)
		mk(4, 96.8, 97, 96.2, 96.8),
		mk(5, 96.8, 97, 95.5, 96),   // bottom 2: later confirmed higher low
		mk(6, 96.2, 97, 95.8, 96.8), // confirms bottom 2
		mk(7, 96.5, 97.1, 95.9, 97), // return 1
	}
	e := New(cfg)
	now := bars[len(bars)-1].OpenTime + 59_999
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 110, LastBucket: maxInt64}

	intents := e.Tick(bars, now)
	if len(e.State.BoxRefs) == 0 {
		t.Fatalf("fixture produced no box return visits; refusals = %v", e.State.Refusals)
	}
	if e.State.Refusals["isb_trigger_side"] == 0 {
		t.Fatalf("wrong-side box return was not counted: ledger = %v; intents = %+v", e.State.Refusals, intents)
	}
	for _, in := range intents {
		if in.Action == PlaceStopEntry && in.Setup == "BOX" {
			t.Fatalf("wrong-side box entry emitted: %+v", in)
		}
	}
}

func TestSwingTriggerSideDropIsCounted(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Swing.Respects5mZone = true
	now := time.Date(2026, time.September, 15, 9, 15, 0, 0, ctime()).UnixMilli()
	t0 := time.Date(2026, time.September, 14, 17, 0, 0, 0, ctime()).UnixMilli()
	mk := func(openTime int64, o, h, l, c float64) market.Kline {
		return market.Kline{
			OpenTime:  openTime,
			CloseTime: openTime + 5*60_000 - 1,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     c,
		}
	}
	bars := []market.Kline{
		mk(t0, 100, 101, 99, 100),
		mk(t0+4*60*60_000, 100, 101, 99, 100),
		mk(now-15*60_000, 120, 125, 115, 120),
		mk(now-10*60_000, 120, 122, 104, 106),
	}
	e := New(cfg)
	e.State.Trigger = TriggerLine{Dir: SideShort, Price: 104}

	intents := runSwing(e, bars, now)
	if e.State.Refusals["isb_trigger_side"] == 0 {
		t.Fatalf("wrong-side swing was not counted: ledger = %v; intents = %+v", e.State.Refusals, intents)
	}
	for _, in := range intents {
		if in.Action == PlaceStopEntry {
			t.Fatalf("wrong-side swing entry emitted: %+v", in)
		}
	}
}
