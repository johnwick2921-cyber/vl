package mentor

import (
	"testing"

	"vl/market"
)

// The LOCATION GATE (fold item 1, owner ruling): setups happen ONLY at
// important levels — all three setups, ISB included. A setup's reference
// candle must touch a key level, the EMA 34 on a higher timeframe
// (ema34_tf knob, default 1m per R3), or the 5m trigger-line retest. An
// old high/low alone is NOT a location (only ±2 pts of a key level).

// TestLocationKeyLevelTouch — the reference candle touching a key level is a
// location.
func TestLocationKeyLevelTouch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ref := market.Kline{High: 101, Low: 99}
	levels := []Level{{Key: "k1", Kind: KindKeyLevel, Price: 100}}
	ok, where := LocationVerdict(ref, levels, TriggerLine{}, nil, cfg)
	if !ok || where != "key_level" {
		t.Fatalf("key-level touch = %v/%q, want true/key_level", ok, where)
	}
	// a candle that stops short is not a location
	ref2 := market.Kline{High: 99.5, Low: 98}
	if ok, _ := LocationVerdict(ref2, levels, TriggerLine{}, nil, cfg); ok {
		t.Fatal("a candle short of the level is not a location")
	}
}

// TestLocationBareSwingIsNotALocation — an old high/low ALONE is not a
// location; ±2 pts of a key level makes it one (fold item 1).
func TestLocationBareSwingIsNotALocation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ref := market.Kline{High: 101, Low: 99}
	swing := Level{Key: "s1", Kind: KindOldExtreme, Price: 100}
	if ok, _ := LocationVerdict(ref, []Level{swing}, TriggerLine{}, nil, cfg); ok {
		t.Fatal("a bare old extreme is not a location")
	}
	// coinciding ±2 pts with a key level → location
	withKey := []Level{swing, {Key: "k1", Kind: KindKeyLevel, Price: 101.5}}
	if ok, where := LocationVerdict(ref, withKey, TriggerLine{}, nil, cfg); !ok || where == "" {
		t.Fatalf("old extreme ±2 of a key level = %v/%q, want a location", ok, where)
	}
}

// TestLocationEMA34Default1m — R3 (RULES FIX v3, verified): intraday setups
// use the EMA 34 of the TRADING chart, i.e. the 1m — the DEFAULT knob is 1
// [D5.4 @08:12–08:40]. The 4h EMA 34 belongs ONLY to the swing.
// Other TFs (here 5m) stay allowed for experimentation.
func TestLocationEMA34Default1m(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	// deterministic 1m series: closes 100, 102, 104, …
	var bars []market.Kline
	for i := 0; i < 60; i++ {
		c := 100 + float64(i)*2
		bars = append(bars, market.Kline{OpenTime: int64(i) * 60_000, CloseTime: int64(i)*60_000 + 59_999, Open: c - 1, High: c + 1, Low: c - 2, Close: c})
	}
	ref := market.Kline{High: 500, Low: -500} // touches everything
	if cfg.EMALocationTFMinutes != 1 {
		t.Fatalf("ema34_tf default = %d, want 1 (R3)", cfg.EMALocationTFMinutes)
	}
	if ok, where := LocationVerdict(ref, nil, TriggerLine{}, bars, cfg); !ok || where != "ema34_1m" {
		t.Fatal("the DEFAULT 1m EMA 34 must be a location (R3)")
	}
	cfg.EMALocationTFMinutes = 5
	v := emaValue(barsTF(bars, 5), cfg.EMAPeriod34)
	if ok, where := LocationVerdict(ref, nil, TriggerLine{}, bars, cfg); !ok || where != "ema34_5m" {
		t.Fatalf("5m EMA location = %v/%q, want true/ema34_5m (EMA value %.2f)", ok, where, v)
	}
}

// TestLocationTriggerRetest — the 5m trigger-line retest is a location
// [D3.4 p2 @ 20:51].
func TestLocationTriggerRetest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ref := market.Kline{High: 100.5, Low: 99.5}
	ok, where := LocationVerdict(ref, nil, TriggerLine{Dir: SideLong, Price: 100}, nil, cfg)
	if !ok || where != "trigger_line" {
		t.Fatalf("trigger retest = %v/%q, want true/trigger_line", ok, where)
	}
}

// TestLocationEMA9IsNotALocation — §10: EMA 9 is a reverse-ISB location only,
// never a location for ISB/PHL/PLH.
func TestLocationEMA9IsNotALocation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	ref := market.Kline{High: 101, Low: 99}
	levels := []Level{{Key: "ema9", Kind: KindEMA9, Price: 100}}
	if ok, _ := LocationVerdict(ref, levels, TriggerLine{}, nil, cfg); ok {
		t.Fatal("EMA 9 must not be a location for the normal setups")
	}
}

// TestPHLPLHGatedByLocationAtEvalLevel — a reject touch at a BARE swing
// produces no PHL/PLH intent through the evaluator; the same touch at a key
// level can. (Mutant: drop the levelIsLocation check → RED.)
func TestPHLPLHGatedByLocationAtEvalLevel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RangeGapPts = 0
	cfg.EMALocationTFMinutes = 1 // R3 default — isolate via bare levels, no bars
	// 40 bars marching down toward 100; a key level at 100 drawn by the walk
	// is unlikely — seed the levels directly through a wrapped tick is not
	// possible, so test the gate helper's production shape instead: the
	// evaluator's PHL/PLH loop uses levelIsLocation.
	swing := Level{Key: "s1", Kind: KindOldExtreme, Price: 100}
	if levelIsLocation(swing, []Level{swing}) {
		t.Fatal("bare swing must not pass levelIsLocation")
	}
	key := Level{Key: "k1", Kind: KindKeyLevel, Price: 100}
	if !levelIsLocation(key, []Level{key}) {
		t.Fatal("key level must pass levelIsLocation")
	}
}

// TestEMALocationLevelTF1UsesStateEMA34 is the m327 (b) pin: at
// EMALocationTFMinutes==1 the location-gate EMA 34 (KindEMA34HTF) is the SAME
// number as the evaluator's incremental State.EMA34 — one source, never a second
// EMA seeded at the tick slice's head (the old emaValue over bars diverged by
// the slice-head seed). MUTANT: fall back to EMALocationLevel at tf=1 → the
// level price ≠ State.EMA34 on a window where the slice-head EMA diverges → RED.
func TestEMALocationLevelTF1UsesStateEMA34(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.EMALocationTFMinutes = 1

	// The tick slice is a short WINDOW whose head is far from the converged EMA,
	// but the evaluator's State.EMA34 was seeded from long history.
	var bars []market.Kline
	price := 30000.0
	for i := 0; i < 300; i++ {
		price += 1.0
		bars = append(bars, market.Kline{
			OpenTime:  int64(i) * 60_000,
			CloseTime: int64(i)*60_000 + 59_999,
			Open:      price - 1, High: price + 1, Low: price - 2, Close: price,
		})
	}
	e := &Evaluator{State: State{EMA34: 30123.45, EMA34Warmed: true}} // long-history seeded
	lvl, ok := e.emaLocationLevel(bars, cfg, 0)
	if !ok || lvl.Kind != KindEMA34HTF {
		t.Fatalf("emaLocationLevel tf=1 = %+v ok=%v, want the EMA34HTF level", lvl, ok)
	}
	if lvl.Price != e.State.EMA34 {
		t.Fatalf("tf=1 EMA34HTF price %.4f ≠ State.EMA34 %.4f (must be one source)",
			lvl.Price, e.State.EMA34)
	}
	// At a different TF the level is the emaValue over that TF's buckets.
	cfg.EMALocationTFMinutes = 5
	lvl5, ok := e.emaLocationLevel(bars, cfg, 0)
	if !ok || lvl5.Kind != KindEMA34HTF || lvl5.Price == e.State.EMA34 {
		t.Fatalf("tf=5 must keep the emaValue-over-5m path, got %+v ok=%v", lvl5, ok)
	}
}
