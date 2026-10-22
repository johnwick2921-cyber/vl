package mentor

import (
	"testing"

	"vl/market"
)

// TestKeyLevelsPruneParity — m202 #28: the LIVE key-level path must rebuild the
// kept set with keyLevelsFromCandles (newest-first against the FINAL kept set),
// not the old incremental prune, so a running bot and a fresh restart agree.
// Chain with level opens 100, 110, 125 and prune 20: |100-110|=10 < 20,
// |110-125|=15 < 20, |100-125|=25 >= 20 → the full walk keeps [100 125]; the old
// incremental path dropped 100 the moment 110 arrived and keeps only [125].
func TestKeyLevelsPruneParity(t *testing.T) {
	candles := []market.Kline{
		{Open: 90, Close: 95, OpenTime: 0, CloseTime: 1},   // green
		{Open: 100, Close: 96, OpenTime: 2, CloseTime: 3},  // red  → level 100
		{Open: 110, Close: 115, OpenTime: 4, CloseTime: 5}, // green → level 110
		{Open: 125, Close: 121, OpenTime: 6, CloseTime: 7}, // red  → level 125
	}
	full := keyLevelsFromCandles(candles, 20)
	if got := keyLevelPrices(full); len(got) != 2 || got[0] != 100 || got[1] != 125 {
		t.Fatalf("full walk kept %v, want [100 125]", got)
	}

	// The OLD incremental path (keyLevelsAppend) diverges: it keeps only [125].
	var inc []Level
	last := candleColour(candles[0])
	for i := 1; i < len(candles); i++ {
		inc, last = keyLevelsAppend(inc, candles[i], last, 20)
	}
	if got := keyLevelPrices(inc); len(got) != 1 || got[0] != 125 {
		t.Fatalf("old incremental path kept %v, want [125] (the divergence the fix removes)", got)
	}
}

func keyLevelPrices(levels []Level) []float64 {
	out := make([]float64, len(levels))
	for i, l := range levels {
		out[i] = l.Price
	}
	return out
}
