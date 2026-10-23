package ninjatrader

import (
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// TestRehydrateRingFromStoreFillsInterior1mHole is the m395 production-call-
// site pin (10-22: ema_series had a 458-minute hole, 00:11→07:50 CT, while the
// store held every minute). The boot-shape 1m ring is NT8's replay tail + live
// head with an INTERIOR hole; the store holds the complete 1m tape the previous
// run persisted. After the boot rehydrate the 1m ring must be CONTIGUOUS — the
// mentor tick's window (FuturesBarsProvider → this ring) feeds EMA 9/34, the
// trigger line, levels, trendlines and the chart EMA curve. MUTANT: use
// RehydrateOlder for tf=="1m" in rehydrateRingFromStoreWith → the interior
// hole remains → RED.
func TestRehydrateRingFromStoreFillsInterior1mHole(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "rehydrate-1m-hole.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	const (
		symbol   = "MNQ"
		contract = "MNQ 12-26"
		min      = int64(60_000)
	)
	var oneMin []store.BarHistoryDB
	for m := int64(0); m <= 600; m++ {
		c := 100 + 0.05*float64(m)
		oneMin = append(oneMin, store.BarHistoryDB{Symbol: symbol, TF: "1m", OpenTimeMs: m * min, O: c, H: c + 0.2, L: c - 0.2, C: c, V: 1, Contract: contract, Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(oneMin); err != nil {
		t.Fatal(err)
	}
	// Ring: replay tail 0..100, live head 560..600, interior hole 101..559.
	// SeedHistorical stamps close→open (T -= 1m), so hand close-stamped times.
	server := ntwire.NewTCPServer(nil)
	var ring []ntwire.Bar
	for m := int64(0); m <= 600; m++ {
		if m <= 100 || m >= 560 {
			ring = append(ring, ntwire.Bar{T: (m + 1) * min, O: 100, H: 102, L: 98, C: 101})
		}
	}
	server.BarCache().SeedHistorical(symbol, "1m", ring)

	rehydrateRingFromStore(bh, server, time.Now())

	got := server.BarCache().Get(symbol, "1m")
	if len(got) != 601 {
		t.Fatalf("1m ring holds %d bars after rehydrate, want 601 (interior hole filled)", len(got))
	}
	for i := 1; i < len(got); i++ {
		if d := got[i].T - got[i-1].T; d != min {
			t.Fatalf("interior 1m hole remains at index %d: gap %d min", i, d/min)
		}
	}
}
