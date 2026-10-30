package pep

import (
	"reflect"
	"testing"
	"time"

	"vl/market"
)

const dayCST = "2026-01-12" // a Monday, CST (UTC-6)

func TestDefaultConfigIsValidAndOwnerNumbers(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Contracts != 3 || c.BoxStartMin != 510 || c.FlatMin != 900 || c.TickSize != 0.25 || c.MaxTradesPerDay != 1 || c.FeePerContractRT != 1 || c.MaxBoxPoints != 0 {
		t.Fatalf("shipped numbers drifted: %+v", c)
	}
	if c.A.BoxMinutes != 5 || c.A.SLFraction != 1 || c.A.TPFraction != 1 {
		t.Fatalf("style A numbers: %+v", c.A)
	}
	if c.E.BoxMinutes != 2 || c.E.SLFraction != 0.5 || c.E.TPFraction != 1 || c.E.EMAPeriod != 9 || c.E.EMATFMin != 2 || c.E.Source != SourceEMA {
		t.Fatalf("style E numbers: %+v", c.E)
	}
}

func TestConfigValidateRefuses(t *testing.T) {
	mut := func(f func(*Config)) Config { c := DefaultConfig(); f(&c); return c }
	for name, c := range map[string]Config{
		"style":       mut(func(c *Config) { c.Style = "Z" }),
		"contracts0":  mut(func(c *Config) { c.Contracts = 0 }),
		"contracts11": mut(func(c *Config) { c.Contracts = 11 }),
		"tick":        mut(func(c *Config) { c.TickSize = 0 }),
		"flat<=box":   mut(func(c *Config) { c.FlatMin = 500 }),
		"maxtrades":   mut(func(c *Config) { c.MaxTradesPerDay = 0 }),
		"tf":          mut(func(c *Config) { c.E.EMATFMin = 7 }),
		"period":      mut(func(c *Config) { c.E.EMAPeriod = 1 }),
		"source":      mut(func(c *Config) { c.E.Source = "x" }),
		"bothedges":   mut(func(c *Config) { c.A.BothEdges = "x" }),
		"slfrac":      mut(func(c *Config) { c.A.SLFraction = 0 }),
	} {
		if c.Validate() == nil {
			t.Errorf("%s: want a validation error", name)
		}
		r := Evaluate(nil, ctms(t, dayCST, "09:00"), c, State{})
		wantRefusal(t, r, CodeConfigInvalid, SevBlock)
	}
}

func TestTickRounding(t *testing.T) {
	near(t, "ceil 18.375", CeilTick(18.375, 0.25), 18.5)
	near(t, "ceil 23.125", CeilTick(23.125, 0.25), 23.25)
	near(t, "ceil exact 18.0 stays", CeilTick(18.0, 0.25), 18.0)
	near(t, "ceil 11.75 stays", CeilTick(11.75, 0.25), 11.75)
	near(t, "ceil float noise 0.1*3", CeilTick(0.30000000000000004, 0.1), 0.3)
	near(t, "round 86.75", RoundTick(86.75, 0.25), 86.75)
	near(t, "round 86.8", RoundTick(86.8, 0.25), 86.75)
}

// STYLE A — break up: entry at the high edge, SL = full box below, TP = full
// box above. [PDF A: "SL cả box, TP cả box"]
func TestA_BreakUp(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041.25, 25029, 25040}})
	r := Evaluate(bars, nowAfter(t, dayCST, "08:37"), cfgA(), State{})
	in := mustOne(t, r)
	if in.Kind != KindEnter || in.Side != SideLong || in.Style != StyleA || in.Contracts != 3 {
		t.Fatalf("intent %+v", in)
	}
	near(t, "entry", in.Entry, 25040)
	near(t, "stop", in.Stop, 25000)
	near(t, "target", in.Target, 25080)
	near(t, "box", in.BoxSize, 40)
	near(t, "stopDist", in.StopDist, 40)
	near(t, "targetDist", in.TargetDist, 40)
	if in.SignalOpen != ctms(t, dayCST, "08:37") || in.Day != dayCST {
		t.Fatalf("signal %d day %s", in.SignalOpen, in.Day)
	}
	if !r.Box.Drawn || r.Box.High != 25040 || r.Box.Low != 25000 {
		t.Fatalf("box %+v", r.Box)
	}
}

func TestA_BreakDown(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:41": {25005, 25010, 24998.75, 25001}})
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:41"), cfgA(), State{}))
	if in.Side != SideShort {
		t.Fatalf("side %s", in.Side)
	}
	near(t, "entry", in.Entry, 25000)
	near(t, "stop", in.Stop, 25040)
	near(t, "target", in.Target, 24960)
}

// A print AT the edge is not a trade THROUGH it.
func TestA_TouchIsNotABreak(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25040, 25000, 25020}})
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:37"), cfgA(), State{}), CodeNoBreakYet, SevWait)
}

// The FIRST side to break wins and the day is then locked: a later break of the
// other edge does not flip it, and a second evaluation after the entry is
// recorded refuses.
func TestA_FirstSideWinsAndOneTradeADay(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{
		"08:37": {25030, 25041, 25029, 25040},
		"08:50": {25030, 25031, 24990, 24995},
	})
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:37"), cfgA(), State{}))
	if in.Side != SideLong {
		t.Fatalf("side %s", in.Side)
	}
	st := State{}.AfterEntry(dayCST)
	st.PositionOpen = false // the trade closed
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:50"), cfgA(), st), CodeMaxTrades, SevWait)
	// Even with the counter lost, the opposite break 13 minutes later is a STALE
	// signal for the first break's slot... but the first break is the one found:
	r := Evaluate(bars, nowAfter(t, dayCST, "08:50"), cfgA(), State{})
	wantRefusal(t, r, CodeStaleSignal, SevBlock)
}

// One 1m bar through BOTH edges: the 1m bar cannot say which came first.
func TestA_WickThroughBothEdges(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25010, 25045, 24995, 25020}})
	now := nowAfter(t, dayCST, "08:37")
	r := Evaluate(bars, now, cfgA(), State{})
	wantRefusal(t, r, CodeBothEdges, SevBlock)
	if !r.Diag.BreakBothEdges {
		t.Fatal("diag should flag the both-edges bar")
	}
	c := cfgA()
	c.A.BothEdges = BothEdgesNearestOpen
	// open 25010: 30 from the high, 10 from the low -> the low broke first.
	in := mustOne(t, Evaluate(bars, now, c, State{}))
	if in.Side != SideShort {
		t.Fatalf("nearest-open should pick the low, got %s", in.Side)
	}
}

// A bar that OPENS beyond an edge broke it at the open: the side is certain even
// when the same bar later reaches the other edge, and the entry is the open.
func TestA_GapOpenBeyondEdge(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25044, 25046, 24990, 25000}})
	in := mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:37"), cfgA(), State{}))
	if in.Side != SideLong {
		t.Fatalf("side %s", in.Side)
	}
	near(t, "entry = the open", in.Entry, 25044)
	near(t, "stop", in.Stop, 25004)
	near(t, "target", in.Target, 25084)
}

func TestA_BoxNotCompleteYet(t *testing.T) {
	bars := aDay(t, dayCST, nil)
	wantRefusal(t, Evaluate(bars[:0], nowAfter(t, dayCST, "08:33"), cfgA(), State{}), CodeBoxNotComplete, SevWait)
	// at 08:34 the 08:34 bar has just closed -> 08:35:00 = box end: complete
	r := Evaluate(bars, nowAfter(t, dayCST, "08:34"), cfgA(), State{})
	if !r.Box.Drawn {
		t.Fatalf("box should be drawn at 08:35:00: %+v", r.Refusals)
	}
	wantRefusal(t, r, CodeNoBreakYet, SevWait)
}

// A missing minute INSIDE the box fails closed with a named reason (owner:
// "fail closed on any missing minute in the box").
func TestA_MissingBoxMinuteFailsClosed(t *testing.T) {
	for _, miss := range []string{"08:30", "08:32", "08:34"} {
		bars := aDay(t, dayCST, map[string]ohlc{"08:40": {25030, 25050, 25030, 25050}}, miss)
		r := Evaluate(bars, nowAfter(t, dayCST, "08:40"), cfgA(), State{})
		wantRefusal(t, r, CodeBoxMissingMin, SevBlock)
		if r.Box.Drawn {
			t.Fatalf("%s missing: box must not be drawn", miss)
		}
	}
}

// A hole after the box and BEFORE any break hides which edge went first.
func TestA_GapAfterBoxBeforeBreak(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:40": {25030, 25050, 25030, 25050}}, "08:37")
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:40"), cfgA(), State{}), CodeGapAfterBox, SevBlock)
}

// A break the bot did not see in time is not entered late.
func TestA_StaleSignalNotEnteredLate(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:41"), cfgA(), State{}), CodeStaleSignal, SevBlock)
	// ... but a break whose bar closed a minute ago is inside the 2-minute window
	mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:39"), cfgA(), State{}))
}

func TestA_StaleFeedBlocks(t *testing.T) {
	bars := aDay(t, dayCST, nil)[:100] // ends ~08:39 on the 07:00 base
	wantRefusal(t, Evaluate(bars, ctms(t, dayCST, "11:00"), cfgA(), State{}), CodeBarsStale, SevBlock)
}

func TestA_MaxBoxPointsRefuses(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})
	c := cfgA()
	c.MaxBoxPoints = 39.75
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:37"), c, State{}), CodeBoxTooWide, SevBlock)
	c.MaxBoxPoints = 40
	mustOne(t, Evaluate(bars, nowAfter(t, dayCST, "08:37"), c, State{}))
}

func TestA_PositionOpenAndMaxTrades(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})
	now := nowAfter(t, dayCST, "08:37")
	wantRefusal(t, Evaluate(bars, now, cfgA(), State{Day: dayCST, Trades: 1, PositionOpen: true}), CodePositionOpen, SevWait)
	wantRefusal(t, Evaluate(bars, now, cfgA(), State{Day: dayCST, Trades: 1}), CodeMaxTrades, SevWait)
	// a counter from ANOTHER day reads as zero
	mustOne(t, Evaluate(bars, now, cfgA(), State{Day: "2026-01-09", Trades: 1}))
}

// THE TIME EXIT: flat at/after 15:00 CT even if the data feed is gone, and a
// position carried over from an earlier day is flattened on sight.
func TestFlatTimeIntent(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})[:200]
	open := State{Day: dayCST, Trades: 1, PositionOpen: true}
	if r := Evaluate(bars, ctms(t, dayCST, "14:59"), cfgA(), open); len(r.Intents) != 0 {
		t.Fatalf("14:59 must not flat: %+v", r.Intents)
	}
	r := Evaluate(bars, ctms(t, dayCST, "15:00"), cfgA(), open)
	in := mustOne(t, r)
	if in.Kind != KindFlat || in.Contracts != 3 {
		t.Fatalf("intent %+v", in)
	}
	// carried-over position flattens at any time of day
	r = Evaluate(bars, ctms(t, dayCST, "09:00"), cfgA(), State{Day: "2026-01-09", Trades: 1, PositionOpen: true})
	if mustOne(t, r).Kind != KindFlat {
		t.Fatal("carried-over position must flat")
	}
	// no new entry at/after the flat time
	full := aDay(t, dayCST, map[string]ohlc{"14:58": {25030, 25100, 25029, 25090}})
	wantRefusal(t, Evaluate(full, ctms(t, dayCST, "15:00"), cfgA(), State{}), CodeAfterFlat, SevWait)
}

// DST: the box starts at 08:30 CT whatever the UTC offset: 14:30Z in CST,
// 13:30Z in CDT, on either side of both transitions.
func TestDST_BoxStartsAt0830CT(t *testing.T) {
	for _, tc := range []struct {
		date string
		utc  string
	}{
		{"2026-03-06", "14:30"}, // Fri, CST
		{"2026-03-09", "13:30"}, // Mon after spring-forward, CDT
		{"2025-10-31", "13:30"}, // Fri, CDT
		{"2025-11-03", "14:30"}, // Mon after fall-back, CST
	} {
		bars := aDay(t, tc.date, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})
		r := Evaluate(bars, nowAfter(t, tc.date, "08:37"), cfgA(), State{})
		in := mustOne(t, r)
		if got := time.UnixMilli(r.Box.Start).UTC().Format("15:04"); got != tc.utc {
			t.Errorf("%s: box starts %s UTC, want %s", tc.date, got, tc.utc)
		}
		if in.Day != tc.date {
			t.Errorf("day %s want %s", in.Day, tc.date)
		}
	}
}

// A half day (data ends 12:00 CT): the trade resolves normally; later the stale
// feed blocks entries and the flat intent still fires at 15:00.
func TestHalfDay(t *testing.T) {
	d := "2025-11-28"
	bars := session(t, d, "07:00", "12:00", 25020, map[string]ohlc{
		"08:30": {25010, 25040, 25000, 25020}, "08:31": {25020, 25035, 25005, 25030},
		"08:32": {25030, 25036, 25010, 25015}, "08:33": {25015, 25030, 25008, 25025},
		"08:34": {25025, 25034, 25012, 25020}, "10:00": {25030, 25041, 25029, 25040},
	})
	mustOne(t, Evaluate(bars, nowAfter(t, d, "10:00"), cfgA(), State{}))
	wantRefusal(t, Evaluate(bars, ctms(t, d, "13:00"), cfgA(), State{}), CodeBarsStale, SevBlock)
	if mustOne(t, Evaluate(bars, ctms(t, d, "15:00"), cfgA(), State{Day: d, Trades: 1, PositionOpen: true})).Kind != KindFlat {
		t.Fatal("flat must fire on a half day")
	}
}

// PURE: same inputs, same output; the input slice is never touched.
func TestEvaluateIsPure(t *testing.T) {
	bars := aDay(t, dayCST, map[string]ohlc{"08:37": {25030, 25041, 25029, 25040}})
	cp := append(bars[:0:0], bars...)
	now := nowAfter(t, dayCST, "08:37")
	a := Evaluate(bars, now, cfgA(), State{})
	b := Evaluate(bars, now, cfgA(), State{})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two calls differ")
	}
	if !reflect.DeepEqual(bars, cp) {
		t.Fatal("input bars were mutated")
	}
	// A forming bar (CloseTime >= now) is ignored: appending one changes nothing.
	forming := kl(ctms(t, dayCST, "08:38"), ohlc{25040, 25200, 25040, 25200})
	upto := 0
	for upto < len(bars) && bars[upto].CloseTime < now {
		upto++
	}
	d := Evaluate(append(append([]market.Kline{}, bars[:upto]...), forming), now, cfgA(), State{})
	if d.Diag.Dropped != 1 || !reflect.DeepEqual(mustOne(t, d), mustOne(t, a)) {
		t.Fatalf("forming bar leaked: dropped=%d", d.Diag.Dropped)
	}
}

func TestUnorderedBarsRefused(t *testing.T) {
	bars := aDay(t, dayCST, nil)
	bars[50], bars[51] = bars[51], bars[50]
	wantRefusal(t, Evaluate(bars, nowAfter(t, dayCST, "08:40"), cfgA(), State{}), CodeBarsUnordered, SevBlock)
	dup := aDay(t, dayCST, nil)
	dup[60] = dup[59]
	wantRefusal(t, Evaluate(dup, nowAfter(t, dayCST, "08:40"), cfgA(), State{}), CodeBarsUnordered, SevBlock)
}
