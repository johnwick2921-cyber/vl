package ninjatrader

import (
	"testing"

	ntwire "vl/provider/ninjatrader"
)

// TestBarsToKlinesCarriesReplayProvenance — EX-13 review P0 (11-17): the mentor
// bar-close guard judges only bars from a bars_historical replay batch (a live
// boundary final stamped a few ms before its close is a true close), so the
// bridge must carry the cache's Source onto Kline.Historical — true for
// "historical" only, false for "live", "mixed" and an unstamped bar.
func TestBarsToKlinesCarriesReplayProvenance(t *testing.T) {
	in := []ntwire.Bar{
		{T: 0, Final: true, Source: ntwire.BarSourceHistorical},
		{T: 60_000, Final: true, Source: ntwire.BarSourceLive},
		{T: 120_000, Final: true, Source: ntwire.BarSourceMixed},
		{T: 180_000, Final: true},
	}
	out := barsToKlines(in, "1m")
	want := []bool{true, false, false, false}
	for i, k := range out {
		if k.Historical != want[i] {
			t.Errorf("bar %d (Source %q): Historical=%v, want %v", i, in[i].Source, k.Historical, want[i])
		}
	}
}
