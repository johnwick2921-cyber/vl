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
	// Complete 1m tape, minutes 0..304 (open-stamped), all live rows — 61
	// complete 5m buckets.
	var oneMin []store.BarHistoryDB
	for m := int64(0); m <= 304; m++ {
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

// TestRehydrateRingFromStoreKeepsOlderNativeDepthWhen1mShort is the m149 P2
// native-fallback pin: a 1h ring whose 1m tape covers only the LAST minutes
// still keeps its OLDER native 1h depth — the union order is live > 1m-aggregate
// (where 1m coverage exists) > native rows for older history. MUTANT: `continue`
// when the 1m tape yields no aggregate → the native rows are skipped and the
// older depth is lost → RED.
func TestRehydrateRingFromStoreKeepsOlderNativeDepthWhen1mShort(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "rehydrate-native.db"))
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
	// 1m tape: only minutes 300..304 (the last 5 minutes) — too little to build
	// any complete 1h bucket, and nothing older.
	var oneMin []store.BarHistoryDB
	for m := int64(300); m <= 304; m++ {
		oneMin = append(oneMin, store.BarHistoryDB{Symbol: symbol, TF: "1m", OpenTimeMs: m * min, O: 100, H: 101, L: 99, C: 100.5, V: 1, Contract: contract, Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(oneMin); err != nil {
		t.Fatal(err)
	}
	// Native 1h rows OLDER than the 1m window: minutes 0, 60, 120, 180.
	var oneH []store.BarHistoryDB
	for m := int64(0); m < 300; m += 60 {
		oneH = append(oneH, store.BarHistoryDB{Symbol: symbol, TF: "1h", OpenTimeMs: m * min, O: 200, H: 201, L: 199, C: 200.5, V: 1, Contract: contract, Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(oneH); err != nil {
		t.Fatal(err)
	}

	server := ntwire.NewTCPServer(nil)
	// Live 1h ring: the newest bucket (open 300) only. SeedHistorical stamps
	// close→open, so hand close-stamped (open+60m).
	server.BarCache().SeedHistorical(symbol, "1h", []ntwire.Bar{{T: (300 + 60) * min, O: 300, H: 301, L: 299, C: 300.5}})
	rehydrateRingFromStore(bh, server, time.Now())

	got := server.BarCache().Get(symbol, "1h")
	have := map[int64]bool{}
	for _, b := range got {
		have[b.T/min] = true
	}
	for _, m := range []int64{0, 60, 120, 180, 300} {
		if !have[m] {
			t.Fatalf("older native 1h depth lost at minute %d (1m tape too short, native rows skipped): %v", m, have)
		}
	}
}

// TestRehydrateRingFromStoreFills1mInteriorHole is the m395 production-call-site
// pin: the 1m ring ITSELF keeps an interior hole when NT8's replay tail is short
// (the m395 10-22 00:11→07:50 CT hole — the store held every minute, the ring
// did not). The pre-fix path rehydrated 1m with RehydrateOlder, which extends
// only the OLDER end and cannot reach an interior hole. MUTANT: the 1m branch of
// rehydrateRingFromStoreWith back on RehydrateOlder leaves the hole → RED.
func TestRehydrateRingFromStoreFills1mInteriorHole(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "rehydrate-1m-union.db"))
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
	// Complete 1m tape, minutes 0..304 (open-stamped), all live rows — every
	// minute of the previous run is in the store, exactly like the m395 DB.
	var oneMin []store.BarHistoryDB
	for m := int64(0); m <= 304; m++ {
		c := 100 + 0.05*float64(m)
		oneMin = append(oneMin, store.BarHistoryDB{Symbol: symbol, TF: "1m", OpenTimeMs: m * min, O: c, H: c + 0.2, L: c - 0.2, C: c, V: 1, Contract: contract, Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(oneMin); err != nil {
		t.Fatal(err)
	}

	// Boot-shape 1m ring: NT8's replay tail minutes 0..70, live head 200..304,
	// with the interior hole 71..199 (the previous run's bars NT8 did not
	// replay). SeedHistorical stamps CLOSE→OPEN (T -= 1m), so hand close-stamped
	// times (open+1m) to land on the open grid.
	server := ntwire.NewTCPServer(nil)
	var ring []ntwire.Bar
	for m := int64(0); m <= 304; m++ {
		if m <= 70 || m >= 200 {
			ring = append(ring, ntwire.Bar{T: (m + 1) * min, O: 100, H: 102, L: 98, C: 101})
		}
	}
	server.BarCache().SeedHistorical(symbol, "1m", ring)

	rehydrateRingFromStore(bh, server, time.Now()) // `now` only drives the boot-line horizon

	got := server.BarCache().Get(symbol, "1m")
	if len(got) != 305 { // (304-0)/1 + 1
		t.Fatalf("1m ring holds %d bars after rehydrate, want 305 (hole filled)", len(got))
	}
	for i := 1; i < len(got); i++ {
		if d := got[i].T - got[i-1].T; d != min {
			t.Fatalf("interior hole remains at %d: gap %dms", i, d)
		}
	}
	// The live tail (200..304) must NOT have been replaced by store bars
	// (store C is 100+0.05m; the ring seed C=101). A merge that let the store
	// win on the live tail would show C ≈ 115 at minute 304.
	if tail := got[len(got)-1]; tail.C != 101 {
		t.Fatalf("live tail was replaced by the store: C=%.2f want 101", tail.C)
	}
}
