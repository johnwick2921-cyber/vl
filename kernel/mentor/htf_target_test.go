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
	cfg.KeyLevelTFMinutes = 1
	cfg.EMAPeriod34 = 0
	cfg.EMAPeriod9 = 9
	cfg.ISBReverseEMA9Enabled = false
	cfg.ISBBufferPts = 0
	cfg.LocTriggerFilter = false
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
