package mentor

import (
	"testing"

	"vl/market"
)

// ── OLD-EXTREME-CLEANUP (course 5.3 @15:59–16:50) — the DRAW set is cleaned,
//    TRADING keeps reading the RAW set. These tests pin both halves. ─────────

// rawVsDisplay is the shared scenario: a KEY level at 100 and an OLD extreme at
// 101.5 (within KeyLevelPrunePts=20 of the key, so DisplayLevels folds it into
// the key). TRADING reads the raw set (the old extreme stays); the PANEL draws
// the cleaned set (the old extreme is gone, folded into the key).
func rawVsDisplay() (raw, display []Level) {
	raw = []Level{
		{Kind: KindKeyLevel, Price: 100, Key: "key:100"},
		{Kind: KindOldExtreme, Price: 101.5, Key: "old_extreme:101.50", AtTime: 10},
	}
	display = DisplayLevels(raw, 20)
	return raw, display
}

// TestTradingISBFlagReadsRawOldExtreme — the ISB size-3 flag (touchesOldExtreme,
// eval.go:552 via isbFlagsFor) reads the RAW set: an old extreme folded into a
// key for the DRAW still fires the flag. Mutant (feed DisplayLevels to the flag)
// → the second assertion flips and this test goes RED.
func TestTradingISBFlagReadsRawOldExtreme(t *testing.T) {
	raw, display := rawVsDisplay()
	cur := market.Kline{High: 102, Low: 100}
	if !touchesOldExtreme(cur, raw) {
		t.Fatal("the raw old extreme within ±2 must fire the ISB size-3 flag")
	}
	if touchesOldExtreme(cur, display) {
		t.Fatal("the DRAW set (old extreme folded into the key) must NOT fire the flag — the flag reads the raw set")
	}
}

// TestTradingTargetReadsRawOldExtreme — nextLevelBeyond (eval.go:718) returns the
// RAW old extreme as the obstacle; the DRAW set moves the obstacle to the key.
func TestTradingTargetReadsRawOldExtreme(t *testing.T) {
	raw, display := rawVsDisplay()
	if got := nextLevelBeyond(raw, 100, SideLong); got != 101.5 {
		t.Fatalf("raw obstacle = %.2f, want 101.5 (the raw old extreme)", got)
	}
	if got := nextLevelBeyond(display, 100, SideLong); got != 0 && got != 101.5 {
		t.Fatalf("display obstacle = %.2f — the old extreme is folded into the key (100 is not beyond 100)", got)
	}
}

// TestTradingLegExtremeReadsRawOldExtreme — nearestOldExtreme (limits.go:689)
// returns the RAW old extreme as the G1 leg extreme / G2 anchor.
func TestTradingLegExtremeReadsRawOldExtreme(t *testing.T) {
	raw, display := rawVsDisplay()
	if got := nearestOldExtreme(raw, 100, SideLong); got != 101.5 {
		t.Fatalf("raw leg extreme = %.2f, want 101.5", got)
	}
	if got := nearestOldExtreme(display, 100, SideLong); got != 0 {
		t.Fatalf("display leg extreme = %.2f, want 0 (folded into the key)", got)
	}
}

// TestTradingLocationReadsRawOldExtreme — LocationVerdict (location.go:99) reads
// the RAW set: an old extreme within ±2 of a key is the location
// "old_extreme@key_level". The DRAW set still admits via the key level, so a
// location never flips pass↔refuse — only the WHERE string would change.
func TestTradingLocationReadsRawOldExtreme(t *testing.T) {
	raw, _ := rawVsDisplay()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cur := market.Kline{High: 102, Low: 100.5, Close: 101}
	ok, where := LocationVerdict(cur, raw, TriggerLine{}, nil, cfg)
	if !ok {
		t.Fatal("a candle at an old extreme within ±2 of a key must be a location")
	}
	if where != "old_extreme@key_level" {
		t.Fatalf("location where = %q, want old_extreme@key_level (the raw set)", where)
	}
}

// TestLevelsReturnsRawOldExtremes — the TRADING level source (Levels) returns
// every swing pivot un-cleaned: no dedupe, no merge. DisplayLevels is the ONLY
// place the cleanup runs. This pins the boundary the four readers rely on.
func TestLevelsReturnsRawOldExtremes(t *testing.T) {
	// Five 5m bars with a swing HIGH at bar 2 (105) — the detector's 2*k+1=5
	// bar floor, so SwingPointLevels emits exactly one 5m swing high.
	bars := []market.Kline{
		mk5(0, 99, 100, 99, 99.5),
		mk5(1, 100, 102, 99, 101),
		mk5(2, 101, 105, 100, 104),
		mk5(3, 102, 103, 101, 102),
		mk5(4, 103, 104, 102, 103),
	}
	now := bars[len(bars)-1].CloseTime + 1
	levels := Levels(bars, DefaultConfig(), now)
	raw := swingOldExtremeLevels(bars, now)
	if len(raw) == 0 {
		t.Fatal("fixture must emit a swing high")
	}
	var oldPrices []float64
	for _, l := range levels {
		if l.Kind == KindOldExtreme {
			oldPrices = append(oldPrices, l.Price)
		}
	}
	if len(raw) != len(oldPrices) {
		t.Fatalf("Levels old extremes = %d, want raw %d (no cleanup in the trading set)", len(oldPrices), len(raw))
	}
	for i := range raw {
		if raw[i].Price != oldPrices[i] {
			t.Fatalf("Levels old extreme %d = %.2f, want raw %.2f", i, oldPrices[i], raw[i].Price)
		}
	}
}

// mk5 builds a 5m bar (OpenTime in 5m buckets) for the swing detector.
func mk5(i int64, o, h, l, c float64) market.Kline {
	ot := i * 5 * 60_000
	return market.Kline{OpenTime: ot, Open: o, High: h, Low: l, Close: c, CloseTime: ot + 5*60_000 - 1}
}

// TestDisplayLevelsFixture1012 — the 10-12 08:35 level set (07-LEVELS-CHECK-1012.md)
// with explicit recency so the tie-break is deterministic. Invariants: no two
// drawn old extremes <20 apart, no duplicate prices, no old extreme within 20
// of a key level, and every dropped price appears in exactly one kept line's
// MergedFrom.
func TestDisplayLevelsFixture1012(t *testing.T) {
	keys := []Level{
		{Kind: KindKeyLevel, Key: "key:31117.75", Price: 31117.75},
		{Kind: KindKeyLevel, Key: "key:31088.50", Price: 31088.50},
		{Kind: KindKeyLevel, Key: "key:31034.50", Price: 31034.50},
		{Kind: KindKeyLevel, Key: "key:31241.25", Price: 31241.25},
	}
	// old extremes in time order (AtTime ascending = older → newer).
	old := []Level{
		{Kind: KindOldExtreme, Key: "oe:31089.25a", Price: 31089.25, AtTime: 1},
		{Kind: KindOldExtreme, Key: "oe:31089.25b", Price: 31089.25, AtTime: 2}, // duplicate
		{Kind: KindOldExtreme, Key: "oe:31087.75", Price: 31087.75, AtTime: 3},
		{Kind: KindOldExtreme, Key: "oe:31116.50", Price: 31116.50, AtTime: 4},
		{Kind: KindOldExtreme, Key: "oe:31058.00", Price: 31058.00, AtTime: 5},
		{Kind: KindOldExtreme, Key: "oe:31050.00", Price: 31050.00, AtTime: 6},
		{Kind: KindOldExtreme, Key: "oe:31043.00", Price: 31043.00, AtTime: 7},
		{Kind: KindOldExtreme, Key: "oe:31030.00", Price: 31030.00, AtTime: 8},
		{Kind: KindOldExtreme, Key: "oe:31018.00a", Price: 31018.00, AtTime: 9},
		{Kind: KindOldExtreme, Key: "oe:31018.00b", Price: 31018.00, AtTime: 10}, // duplicate
		{Kind: KindOldExtreme, Key: "oe:31214.75", Price: 31214.75, AtTime: 11},
	}
	raw := append(append([]Level{}, keys...), old...)
	drawn := DisplayLevels(raw, 20)

	var drawnOld []Level
	mergedAll := map[float64]int{} // price -> number of times it appears in some MergedFrom
	for _, l := range drawn {
		if l.Kind == KindOldExtreme {
			drawnOld = append(drawnOld, l)
		}
		for _, p := range l.MergedFrom {
			mergedAll[p]++
		}
	}
	// No two drawn old extremes <20 apart.
	for i := 0; i < len(drawnOld); i++ {
		for j := i + 1; j < len(drawnOld); j++ {
			if abs(drawnOld[i].Price-drawnOld[j].Price) < 20 {
				t.Fatalf("drawn old extremes %.2f and %.2f are <20 apart", drawnOld[i].Price, drawnOld[j].Price)
			}
		}
		// No drawn old extreme within 20 of a key level.
		for _, k := range keys {
			if abs(drawnOld[i].Price-k.Price) < 20 {
				t.Fatalf("drawn old extreme %.2f is within 20 of key %.2f", drawnOld[i].Price, k.Price)
			}
		}
	}
	// No duplicate drawn prices.
	seen := map[float64]bool{}
	for _, l := range drawn {
		if l.Kind == KindOldExtreme {
			if seen[l.Price] {
				t.Fatalf("duplicate drawn price %.2f", l.Price)
			}
			seen[l.Price] = true
		}
	}
	// Every dropped raw old line's price appears exactly once in some kept
	// line's MergedFrom (a duplicate dropped twice appears twice).
	droppedCount := map[float64]int{}
	for _, o := range old {
		kept := false
		for _, l := range drawn {
			if l.Kind == KindOldExtreme && l.Price == o.Price {
				kept = true
				break
			}
		}
		if !kept {
			droppedCount[o.Price]++
		}
	}
	for price, want := range droppedCount {
		if mergedAll[price] != want {
			t.Fatalf("price %.2f dropped %d time(s) but appears %d time(s) in merged_from", price, want, mergedAll[price])
		}
	}
}
