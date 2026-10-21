package ninjatrader

import (
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// TestRehydrateTFMinutes pins the epoch-aligned rebuild selection (m134 a):
// only 3m/5m/15m/30m/1h rebuild from 1m; 1m and the session-anchored buckets
// (2h/4h/6h/8h/12h/1d/3d/1w) keep the older-only store path.
func TestRehydrateTFMinutes(t *testing.T) {
	want := map[string]int{"3m": 3, "5m": 5, "15m": 15, "30m": 30, "1h": 60}
	for tf, min := range want {
		if got := rehydrateTFMinutes(tf); got != min {
			t.Fatalf("rehydrateTFMinutes(%q) = %d, want %d", tf, got, min)
		}
	}
	for _, tf := range []string{"1m", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w", "bogus"} {
		if got := rehydrateTFMinutes(tf); got != 0 {
			t.Fatalf("rehydrateTFMinutes(%q) = %d, want 0 (older-only path)", tf, got)
		}
	}
}

// TestRehydrateRingFromStoreRebuildsHigherTFFrom1m is the m134 production-call-
// site pin: the boot-shape ring (replay tail + live head with an interior hole)
// is rehydrated from the complete stored 1m tape, and the 5m ring comes back
// CONTIGUOUS. MUTANT: RehydrateUnion → RehydrateOlder in bar_persist_wire.go
// leaves the interior hole → RED.
func TestRehydrateRingFromStoreRebuildsHigherTFFrom1m(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "rehydrate-union.db"))
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
	// Complete 1m tape, minutes 0..300 (open-stamped), all live rows.
	var oneMin []store.BarHistoryDB
	for m := int64(0); m <= 300; m++ {
		c := 100 + 0.05*float64(m)
		oneMin = append(oneMin, store.BarHistoryDB{Symbol: symbol, TF: "1m", OpenTimeMs: m * min, O: c, H: c + 0.2, L: c - 0.2, C: c, V: 1, Contract: contract, Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(oneMin); err != nil {
		t.Fatal(err)
	}

	// Boot-shape 5m ring: replay tail buckets 0..70, live head 200..300, with
	// the interior hole 75..195. SeedHistorical stamps CLOSE→OPEN (T -= 5m), so
	// hand close-stamped times (open+5m) to land on the open grid.
	server := ntwire.NewTCPServer(nil)
	var ring []ntwire.Bar
	for m := int64(0); m <= 300; m += 5 {
		if m <= 70 || m >= 200 {
			ring = append(ring, ntwire.Bar{T: (m + 5) * min, O: 100, H: 102, L: 98, C: 101})
		}
	}
	server.BarCache().SeedHistorical(symbol, "5m", ring)

	rehydrateRingFromStore(bh, server, time.Now()) // `now` only drives the boot-line horizon

	got := server.BarCache().Get(symbol, "5m")
	if len(got) != 61 { // (300-0)/5 + 1
		t.Fatalf("5m ring holds %d bars after rehydrate, want 61 (hole filled)", len(got))
	}
	for i := 1; i < len(got); i++ {
		if d := got[i].T - got[i-1].T; d != 5*min {
			t.Fatalf("interior hole remains at %d: gap %dms", i, d)
		}
	}
}
