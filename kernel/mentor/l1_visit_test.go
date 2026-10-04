package mentor

import (
	"testing"

	"vl/market"
)

func keyLevel(price float64) Level {
	return Level{Key: "kl", Kind: KindKeyLevel, Price: price}
}

// TestLevelVisitReclassifies — L1: the reference is per VISIT. A reject
// reference stands only while candles keep touching; one closed candle that
// did not touch the level ends the visit and the NEXT touching candle is the
// new reference [D5.3 p1 @20:42-23:35; X4 @04:55-05:27].
func TestLevelVisitReclassifies(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	lvl := keyLevel(100)
	var tr Touch

	// Visit 1: approach from below, candle touches and closes back below.
	prev := market.Kline{Close: 99}
	bar1 := market.Kline{Open: 99.4, High: 100.2, Low: 99.3, Close: 99.5}
	visitTick(&tr, lvl, prev, bar1, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("visit 1 outcome = %q, want reject", tr.Outcome)
	}
	ref1 := tr.RefBar

	// Departure: a closed candle that does NOT touch (high below the level).
	bar2 := market.Kline{Open: 99.5, High: 99.8, Low: 99.2, Close: 99.6}
	visitTick(&tr, lvl, bar1, bar2, cfg)
	if tr.Outcome != TouchNone {
		t.Fatalf("after a non-touching candle the visit must end, got %q", tr.Outcome)
	}

	// Visit 2: the next touching candle is the NEW reference.
	bar3 := market.Kline{Open: 99.6, High: 100.1, Low: 99.5, Close: 99.4}
	visitTick(&tr, lvl, bar2, bar3, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("visit 2 outcome = %q, want reject", tr.Outcome)
	}
	if tr.RefBar != bar3 || tr.RefBar == ref1 {
		t.Fatalf("visit 2 reference is not bar3 (old reference kept)")
	}
}

// TestLevelTouchLiteral — L2: touch_tol = 0 at key levels. A candle that
// comes close but never REACHES the level must not be a reference, no matter
// how wide the band is.
func TestLevelTouchLiteral(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TouchBandPts = 0.5 // band loosened — key levels must ignore it
	lvl := keyLevel(100)
	var tr Touch

	prev := market.Kline{Close: 99}
	near := market.Kline{Open: 99.4, High: 99.9, Low: 99.2, Close: 99.5} // within band, never reaches 100
	visitTick(&tr, lvl, prev, near, cfg)
	if tr.Outcome != TouchNone {
		t.Fatalf("near-miss classified as %q — key levels need a LITERAL touch", tr.Outcome)
	}

	hit := market.Kline{Open: 99.5, High: 100.0, Low: 99.4, Close: 99.5}
	visitTick(&tr, lvl, near, hit, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("literal touch (high 100.0) not classified, got %q", tr.Outcome)
	}
}

// TestEMATouchKeepsBand: the band still applies to non-key kinds (the EMA's
// line moves; the strictness knob is only pinned for levels).
func TestEMATouchKeepsBand(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TouchBandPts = 0.5
	lvl := Level{Key: string(KindEMA34), Kind: KindEMA34, Price: 100}
	var tr Touch

	prev := market.Kline{Close: 99}
	near := market.Kline{Open: 99.4, High: 99.9, Low: 99.2, Close: 99.5}
	visitTick(&tr, lvl, prev, near, cfg)
	if tr.Outcome == TouchNone {
		t.Fatalf("EMA within the band must still classify")
	}
}

// TestVisitMinPts: the L1 knob — a departure needs LvlRevisitMinPts of
// close-distance from the level. Below the distance the visit survives.
func TestVisitMinPts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.LvlRevisitMinPts = 0.5
	lvl := keyLevel(100)
	var tr Touch

	prev := market.Kline{Close: 99}
	bar1 := market.Kline{Open: 99.4, High: 100.2, Low: 99.3, Close: 99.5}
	visitTick(&tr, lvl, prev, bar1, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("visit outcome = %q, want reject", tr.Outcome)
	}

	// Non-touching but only 0.3 pts from the level: below the knob.
	bar2 := market.Kline{Open: 99.6, High: 99.9, Low: 99.5, Close: 99.7}
	visitTick(&tr, lvl, bar1, bar2, cfg)
	if tr.Outcome != TouchReject {
		t.Fatalf("departure below LvlRevisitMinPts must keep the visit, got %q", tr.Outcome)
	}

	// Non-touching and 0.7 pts away: past the knob — the visit ends.
	bar3 := market.Kline{Open: 99.5, High: 99.8, Low: 99.0, Close: 99.3}
	visitTick(&tr, lvl, bar2, bar3, cfg)
	if tr.Outcome != TouchNone {
		t.Fatalf("departure past LvlRevisitMinPts must end the visit, got %q", tr.Outcome)
	}
}
