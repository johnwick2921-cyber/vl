package mentor

import (
	"testing"
	"time"

	"vl/market"
)

// m425b S4b: boxEntryIntent used to drop a box return through 7 silent
// `return nil` paths. Each now carries a NAMED refusal that the evaluator
// counts, and the trigger-side drop is counted box_trigger_side (it used to be
// mislabelled isb_trigger_side). The Tick fixture is the production call site
// (same tape as TestEvaluatorBoxPathEveryReturnTrades: two rejecting returns).

func m425bBoxRig(seed []Level, trig TriggerLine) (*Evaluator, []market.Kline, int64) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 0
	cfg.EMALocationTFMinutes = 0
	cfg.RoomMultiple = 0.05
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{
		mk(0, 100, 101, 99, 100), mk(1, 100, 102, 99, 101), mk(2, 101, 103, 95, 96),
		mk(3, 96.5, 97, 96, 96.8), mk(4, 96.8, 97, 96.2, 96.8), mk(5, 96.8, 97, 95.5, 96),
		mk(6, 96.2, 97, 95.8, 96.8), mk(7, 96.8, 97, 96.2, 96.8), mk(8, 96.8, 97, 96.3, 96.9),
		mk(9, 96.9, 97, 96.4, 96.9), mk(10, 96.5, 97.1, 95.9, 97), mk(11, 97.5, 97.9, 97.1, 97.6),
		mk(12, 96.5, 97.2, 95.9, 97),
	}
	e := New(cfg)
	now := bars[12].OpenTime + 59_999
	e.seeded = true
	e.State.Seed1mWatermark = maxInt64
	e.State.Seed1HWatermark = maxInt64
	e.State.SeedLevels = seed
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 93}}
	trig.LastBucket = t0 + 5*60_000
	e.State.Trigger = trig
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

// MUTANT: revert the `return nil, "box_no_target"` to a silent drop / drop the
// e.refuse(boxWhy) in Tick → the counter stays 0 → RED.
func TestM425bBoxNoTargetIsCounted(t *testing.T) {
	e, bars, now := m425bBoxRig(nil, TriggerLine{Dir: SideLong, Price: 80})
	e.Tick(bars, now)
	if got := e.State.Refusals["box_no_target"]; got == 0 {
		t.Fatalf("a box return with no level beyond must be counted box_no_target, ledger=%v", e.State.Refusals)
	}
}

// MUTANT: restore e.refuse("isb_trigger_side") at the box trigger-side drop →
// box_trigger_side stays 0 and isb_trigger_side is non-zero → RED.
func TestM425bBoxTriggerSideHasItsOwnName(t *testing.T) {
	// a SELL trigger above the entry: TriggerVerdict passes with side=short, the
	// long box entry is on the wrong side. The 5m-trend gate reads the swing
	// structure (not the trigger dir) so it does not pre-empt the drop.
	e, bars, now := m425bBoxRig([]Level{{Key: "far", Kind: KindKeyLevel, Price: 125}}, TriggerLine{Dir: SideShort, Price: 120})
	e.Cfg.D43TrendSource = D43TrendStructure
	e.State.HTF = HTF{}
	e.Cfg.TriggerSchool = 2
	e.Tick(bars, now)
	if e.State.Refusals["box_trigger_side"] == 0 {
		t.Fatalf("box trigger-side drop must be counted box_trigger_side, ledger=%v", e.State.Refusals)
	}
	if e.State.Refusals["box_trigger_side"] != 2 {
		t.Fatalf("both box returns must be booked box_trigger_side (not isb_trigger_side), ledger=%v", e.State.Refusals)
	}
}

// Every silent path of boxEntryIntent now returns a stable name.
func TestM425bBoxEntryIntentWhyNames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0.05
	floor := Box{Kind: FTGL, Top: 100, Bottom: 90, Key: "ftgl:100.00:90.00"}
	ref := market.Kline{High: 101, Low: 99, Close: 101}
	far := []Level{{Key: "k", Kind: KindKeyLevel, Price: 160}}
	buy := TriggerLine{Dir: SideLong, Price: 95}
	why := func(name string, b Box, boxes []Box, r market.Kline, lv []Level, trig TriggerLine, c Config, want string) {
		t.Helper()
		out, got := boxEntryIntentWhy(r, b, boxes, lv, trig, nil, c)
		if len(out) != 0 || got != want {
			t.Fatalf("%s: got %d intents, why=%q, want none + %q", name, len(out), got, want)
		}
	}
	why("close_inside", floor, []Box{floor}, market.Kline{High: 101, Low: 99, Close: 95}, far, buy, cfg, "box_close_inside")
	why("no_target", floor, []Box{floor}, ref, nil, buy, cfg, "box_no_target")
	c2 := cfg
	c2.TriggerSchool = 2
	why("trigger_side", floor, []Box{floor}, ref, far, TriggerLine{Dir: SideShort, Price: 120}, c2, "box_trigger_side")
	c3 := cfg
	c3.StopCeilingPts = 0.5
	why("stop_ceiling", floor, []Box{floor}, ref, far, buy, c3, "box_stop_ceiling")
	c4 := cfg
	c4.RoomMultiple = 100
	why("room", floor, []Box{floor}, ref, far, buy, c4, "box_room")
	ceil := Box{Kind: FTGH, Top: 210, Bottom: 140, Key: "ftgh:210.00:140.00"}
	why("ping_pong", floor, []Box{floor, ceil}, ref, far, buy, cfg, "box_ping_pong")
	other := Box{Kind: FTGH, Top: 130, Bottom: 98, Key: "ftgh:130.00:98.00"}
	why("inside_box", floor, []Box{floor, other}, ref, far, buy, cfg, "box_inside_box")
	if out, w := boxEntryIntentWhy(ref, floor, []Box{floor}, far, buy, nil, cfg); len(out) != 1 || w != "" {
		t.Fatalf("control: emitted intent must carry no refusal, got %d / %q", len(out), w)
	}
}
