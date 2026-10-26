package mentor

import (
	"math"
	"testing"
	"time"

	"vl/market"
)

// D4.4-15: the 4h trigger line joins the TARGET LADDER — a trade's target can
// be the 4h trigger line when it sits between the entry and the next level
// ("TARGET MÌNH VỀ LẠI 4 GIỜ nè anh chị… CÁI LỆNH TRĂM ĐIỂM của mình LÀ VỀ ĐÂY"
// [D4.4 p2 @05:04–06:02]).

func htfTargetFixture(t *testing.T) (*Evaluator, []market.Kline, int64) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Exec2mAfter30m = false // release #31: the default is ON (2m after 09:00); this pin tests the 1m read
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
	cfg.RoomMultiple = 0 // item 25: this fixture pins TARGET SELECTION, not the room rule
	t0 := time.Date(2026, time.September, 15, 9, 0, 0, 0, ctime()).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		return market.Kline{OpenTime: t0 + int64(i)*60_000, CloseTime: t0 + int64(i)*60_000 + 59_000, Open: o, High: h, Low: l, Close: c}
	}
	bars := []market.Kline{}
	for i := 0; i < 40; i++ {
		bars = append(bars, mk(i, 101, 101.5, 100.5, 101))
	}
	bars = append(bars,
		mk(40, 99.5, 106.5, 99.5, 106), // green candle 1 → ISB long
		mk(41, 102, 104.5, 100.5, 104), // inside bar 2
	)
	now := bars[len(bars)-1].CloseTime
	levels := []Level{
		{Key: "K-below", Kind: KindKeyLevel, Price: 90},
		{Key: "K-above", Kind: KindKeyLevel, Price: 120}, // the next KEY level beyond the entry
	}
	e := New(cfg)
	e.seeded = true
	e.State.Seed1mWatermark = math.MaxInt64
	e.State.Seed1HWatermark = math.MaxInt64
	e.State.SeedLevels = levels
	e.State.ORB = ORB{Day: dayStartCT(now), High: 90, Low: 85, Drawn: true, Escaped: SideLong}
	// The 4h trigger line sits at 110 — between the entry (104.5) and the next
	// key level (120). The 1h is silent.
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 110}}
	e.State.Trigger = TriggerLine{Dir: SideLong, Price: 101}
	e.State.Day = DayLatch{Key: tradingDayKey(time.UnixMilli(now).In(ctime())), Verdict: DayTrade}
	return e, bars, now
}

// TestISBTargetCanBeThe4hTriggerLine — the 4h trigger line between the entry and
// the next level becomes the ISB target. Mutant: don't add the HTF lines to the
// level set → RED (target falls back to the next key level 120).
func TestISBTargetCanBeThe4hTriggerLine(t *testing.T) {
	e, bars, now := htfTargetFixture(t)
	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			isb = in
			break
		}
	}
	if isb == nil {
		t.Fatalf("the long ISB must fire; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 110 {
		t.Fatalf("ISB target = %.2f, want 110 (the 4h trigger line); intents %+v", isb.Target, ins)
	}
}

// TestISBTargetPrefersNearestLevelOver4h — the 4h line is NOT used when a nearer
// key level already sits between the entry and the 4h line (the ladder picks the
// nearest obstacle, the 4h line only fills a gap).
func TestISBTargetPrefersNearestLevelOver4h(t *testing.T) {
	e, bars, now := htfTargetFixture(t)
	// A key level at 109 sits NEARER than the 4h line at 110 (and clears the
	// 1:1 floor: 109 − 104.5 = 4.5 ≥ 4) → target 109, not 110.
	e.State.SeedLevels = append(e.State.SeedLevels, Level{Key: "K-near", Kind: KindKeyLevel, Price: 109})
	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			isb = in
			break
		}
	}
	if isb == nil {
		t.Fatalf("the long ISB must fire; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 109 {
		t.Fatalf("ISB target = %.2f, want 109 (the nearer key level beats the 4h line); intents %+v", isb.Target, ins)
	}
}

// TestHTFTriggerLevelNeverBecomesALocation — the 4h line is target-only: it is
// never a PHL/PLH location (levelIsLocation returns false).
func TestHTFTriggerLevelNeverBecomesALocation(t *testing.T) {
	if levelIsLocation(Level{Key: "htf_4h_trigger", Kind: KindHTFTrigger, Price: 110}, nil) {
		t.Fatal("the 4h trigger line must never be a PHL/PLH location")
	}
}

// Release #52 B5 — the 1H trigger line is NOT a target. The course names the
// 4h line ("Về lại 4 giờ nè anh chị" [D4.4 p2 @05:04–05:44], one worked example)
// and never the 1h. Production call site: Evaluator.Tick (the ladder) and
// SeedChartDisplay (the boot-time chart set).
//
// Mutant (RED, then restored): re-add to eval.go Tick, after the 4h append,
//
//	if e.State.HTF.OneH.Dir != "" && e.State.HTF.OneH.Price != 0 {
//	    levels = append(levels, Level{Key: "htf_1h_trigger", Kind: KindHTFTrigger, Price: e.State.HTF.OneH.Price})
//	}
//
// → TestISBTargetIgnoresThe1hTriggerLine RED (target 110 instead of 120).
func TestISBTargetIgnoresThe1hTriggerLine(t *testing.T) {
	e, bars, now := htfTargetFixture(t)
	// 4h silent; the 1h trigger sits at 110, between the entry (104.5) and the
	// next key level (120).
	e.State.HTF = HTF{OneH: TriggerLine{Dir: SideLong, Price: 110}}
	ins := e.Tick(bars, now)
	var isb *Intent
	for i := range ins {
		in := &ins[i]
		if (in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry) && in.Setup == "ISB" {
			isb = in
			break
		}
	}
	if isb == nil {
		t.Fatalf("the long ISB must fire; intents %+v refusals %v", ins, e.State.Refusals)
	}
	if isb.Target != 120 {
		t.Fatalf("ISB target = %.2f, want 120 (the next key level — the 1h line is not a target); intents %+v", isb.Target, ins)
	}
	for _, l := range e.State.ChartLevels {
		if l.Key == "htf_1h_trigger" {
			t.Fatalf("the chart set still carries the 1h trigger as a target-ladder line: %+v", l)
		}
	}
}

// TestChartSeedCarriesThe4hLineNotThe1h — the boot-time chart set mirrors the
// Tick ladder: the 4h trigger line is drawn, the 1h line is not.
func TestChartSeedCarriesThe4hLineNotThe1h(t *testing.T) {
	e, bars, now := htfTargetFixture(t)
	e.State.HTF = HTF{FourH: TriggerLine{Dir: SideLong, Price: 110}, OneH: TriggerLine{Dir: SideLong, Price: 108}}
	e.SeedChartDisplay(bars, now)
	var has4h, has1h bool
	for _, l := range e.State.ChartLevels {
		switch l.Key {
		case "htf_4h_trigger":
			has4h = true
		case "htf_1h_trigger":
			has1h = true
		}
	}
	if !has4h {
		t.Fatalf("the chart seed lost the 4h trigger line: %+v", e.State.ChartLevels)
	}
	if has1h {
		t.Fatalf("the chart seed still carries the 1h trigger line as a target-ladder level")
	}
}
