package api

import (
	"path/filepath"
	"testing"
	"time"

	"vl/market"
	"vl/store"
)

// F1.1 (2026-09-14) — a thin coarse-TF ring gets older CLOSED buckets
// aggregated from a deep finer rung, capped at the ask, and a ring the finer
// rung cannot extend passes through unchanged.
func TestKlinesAggregatedDepthPrependsOlderClosedBuckets(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	base := []market.Kline{
		{OpenTime: now.Add(-6 * time.Hour).UnixMilli(), Open: 10, High: 11, Low: 9, Close: 10.5},
		{OpenTime: now.Add(-2 * time.Hour).UnixMilli(), Open: 11, High: 12, Low: 10, Close: 11.5},
	}
	one := func(h int) market.Kline {
		return market.Kline{OpenTime: now.Add(-time.Duration(h) * time.Hour).UnixMilli(), Open: float64(h), High: float64(h) + 1, Low: float64(h) - 1, Close: float64(h) + 0.5, Volume: 1}
	}
	ring1h := []market.Kline{one(10), one(9), one(8), one(7), one(6), one(5)}
	provider := func(symbol, tf string, count int) []market.Kline {
		if tf == "1h" {
			return ring1h
		}
		return base
	}
	out := klinesWithAggregatedDepth(base, provider, "MNQ", "4h", 10, now)
	if len(out) != 3 {
		t.Fatalf("served %d bars, want 3 (1 aggregated closed 4h bucket + 2 base)", len(out))
	}
	wantOldest := now.Add(-10 * time.Hour).Truncate(4 * time.Hour).UnixMilli()
	if out[0].OpenTime != wantOldest {
		t.Fatalf("oldest served %d, want the 08:00-12:00 aggregated bucket %d", out[0].OpenTime, wantOldest)
	}
	// The AGGREGATED prefix must be closed (the base's own tail may hold the
	// forming bucket — the chart shows it, the splice never ADDS one).
	if out[0].OpenTime+4*3600_000 > now.UnixMilli() {
		t.Fatalf("a forming 4h bucket was served: %+v", out[0])
	}
}

func TestKlinesAggregatedDepthFallsBackWhenNoOlder(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	base := []market.Kline{
		{OpenTime: now.Add(-6 * time.Hour).UnixMilli(), Open: 10, High: 11, Low: 9, Close: 10.5},
	}
	// The finer rung holds NOTHING older than the base's oldest bar.
	provider := func(symbol, tf string, count int) []market.Kline {
		if tf == "1h" {
			return []market.Kline{{OpenTime: now.Add(-1 * time.Hour).UnixMilli(), Close: 5}}
		}
		return base
	}
	out := klinesWithAggregatedDepth(base, provider, "MNQ", "4h", 10, now)
	if len(out) != 1 {
		t.Fatalf("a rung with no older bars must not change the series: %d bars", len(out))
	}
}

// F1 (2026-09-14), RE-POINTED by dispatch 101 D2 (2026-09-16, owner ruling:
// "the chart MUST show stored history across contract rolls"). The dashboard
// klines path deepens from the store on the CURRENT contract first; when the
// ask is still short, PRIOR contracts fill strictly before the current
// contract's first live row — LABELLED, prices untouched. What this test still
// pins from 09-14: a ring (live) bar is never replaced by a stored one, the
// current contract's rows come first, and a historical_import snapshot is never
// rendered where a live row exists.
func TestKlinesNinjaTraderStoreDepthContractFiltered(t *testing.T) {
	// W-ROLL-DAY-CHART — this test pins the PRE-WAVE wire ("prices untouched").
	// The derive+adjust default shifts the prior segment by the measured basis,
	// which is exactly the fix this wave ships; the byte-identical legacy path
	// is what this fixture must keep exercising.
	oldKnob := chartRollStitchDerive
	chartRollStitchDerive = false
	defer func() { chartRollStitchDerive = oldKnob }()

	orig := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = orig }()

	st, err := store.New(filepath.Join(t.TempDir(), "klines.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}

	// Ring: 2 live bars at 10:00 / 10:01, current contract.
	ringStart := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{
			{OpenTime: ringStart.UnixMilli(), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 1},
			{OpenTime: ringStart.Add(time.Minute).UnixMilli(), Open: 101, High: 102, Low: 100, Close: 101.5, Volume: 1},
		}
	}

	// Store: three older bars on the CURRENT contract, and three OLDER-STILL
	// bars on a retired contract whose Close carries a sentinel (-1) so a leak
	// is visible at a glance.
	older := func(i int) int64 { return ringStart.Add(-time.Duration(i) * time.Minute).UnixMilli() }
	var rows []store.BarHistoryDB
	for i := 1; i <= 3; i++ {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: older(i), O: 90, H: 91, L: 89, C: 90.5, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceLive})
	}
	for i := 4; i <= 6; i++ {
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: older(i), O: -1, H: -1, L: -1, C: -1, V: 1, Contract: "MNQ 09-26", Source: store.BarSourceLive})
	}
	// A wave-101 bulk-import snapshot OLDER than everything else, sentinel
	// Close=50: honest store history, but the DISPLAY chart must skip it.
	// (Imports ride ImportBars — InsertBars refuses historical_import.)
	snapshot := store.BarHistoryDB{Symbol: "MNQ", TF: "1m", OpenTimeMs: older(7), O: 50, H: 50, L: 50, C: 50, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceHistoricalImport}
	if err := st.BarHistory().InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.BarHistory().ImportBars([]store.BarHistoryDB{snapshot}); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	out, _ := s.getKlinesFromNinjaTrader("MNQ", "1m", 6)
	// 6 asked: 2 ring + 3 stored current-contract = 5, and ONE prior-contract
	// bar (older(4), 09-26) fills behind the current contract's first live row
	// — labelled, its sentinel close untouched. Under the 09-14 rule this was 5.
	if len(out) != 6 {
		t.Fatalf("served %d bars, want 6 (2 ring + 3 current-contract + 1 prior-contract behind them)", len(out))
	}
	if out[0].OpenTime != older(4) || out[0].Contract != "MNQ 09-26" || out[0].Close != -1 {
		t.Fatalf("oldest served must be the prior contract's newest bar, labelled, price untouched: %+v", out[0])
	}
	if out[1].OpenTime != older(3) || out[1].Contract != "MNQ 12-26" {
		t.Fatalf("the current contract's oldest stored bar must follow, labelled: %+v", out[1])
	}
	if got := out[len(out)-1].OpenTime; got != ringStart.Add(time.Minute).UnixMilli() {
		t.Fatalf("newest served %d — the ring's live tail must survive untouched", got)
	}
	for _, k := range out {
		if k.Contract == "" {
			t.Fatalf("an unlabelled kline: %+v", k)
		}
		if k.Close == 50 {
			t.Fatalf("a historical_import snapshot rendered on the display chart: %+v", k)
		}
	}
	// flag OFF restores the 09-14 behaviour exactly: 5, current contract only
	prev := chartAcrossRoll
	chartAcrossRoll = false
	t.Cleanup(func() { chartAcrossRoll = prev })
	if off, _ := s.getKlinesFromNinjaTrader("MNQ", "1m", 6); len(off) != 5 || off[0].OpenTime != older(3) {
		t.Fatalf("with VL_CHART_ACROSS_ROLL=off the 09-14 behaviour must hold: got %d bars, oldest %d", len(off), off[0].OpenTime)
	}
}

// F1.2 (2026-09-14) — an EMPTY native ring for the requested TF is aggregated
// from the finer live rung (closed buckets only). Measured live: after a
// re-subscribe NT8 returned zero 2h/4h bars while the 1h ring held 1,500.
func TestKlinesAggregatedDepthFillsEmptyBase(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	one := func(h int) market.Kline {
		return market.Kline{OpenTime: now.Add(-time.Duration(h) * time.Hour).UnixMilli(), Open: float64(h), High: float64(h) + 1, Low: float64(h) - 1, Close: float64(h) + 0.5, Volume: 1}
	}
	// 1h rung: 09:00,10:00,11:00,12:00 → one closed 4h bucket (08:00-12:00) and
	// one bucket (12:00-16:00, closed too). At 18:00 both are closed.
	ring1h := []market.Kline{one(9), one(8), one(7), one(6)}
	provider := func(symbol, tf string, count int) []market.Kline {
		if tf == "1h" {
			return ring1h
		}
		return nil // the requested TF's own ring is EMPTY
	}
	out := klinesWithAggregatedDepth(nil, provider, "MNQ", "4h", 10, now)
	if len(out) != 2 {
		t.Fatalf("empty base + deep finer rung must aggregate: got %d 4h bars, want 2", len(out))
	}
	if out[0].OpenTime != now.Add(-10*time.Hour).Truncate(4*time.Hour).UnixMilli() {
		t.Fatalf("oldest aggregated bucket %d, want 08:00", out[0].OpenTime)
	}
	for _, k := range out {
		if k.OpenTime+4*3600_000 > now.UnixMilli() {
			t.Fatalf("a forming 4h bucket was served from an empty base: %+v", k)
		}
	}
}

// F1 (2026-09-14) — NT8's BarsRequest seed carries the sparse wave-101 import
// snapshots INTO the ring itself; the store-side splice filter never sees
// them. The display seam drops them by open time, so the chart's left edge
// matches NT8's own chart (nothing rendered there).
func TestKlinesNinjaTraderDisplayDropsRingImportSnapshots(t *testing.T) {
	orig := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = orig }()

	st, err := store.New(filepath.Join(t.TempDir(), "klines-ring.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}

	snap := time.Date(2026, 9, 7, 17, 0, 0, 0, time.UTC)
	live := snap.Add(60 * time.Minute)
	// The ring holds BOTH the sparse snapshot (Close sentinel 50) and a live
	// bar an HOUR later — the snapshot is isolated in the merged series, which
	// is what makes it droppable; a dense import is not (separate pin below).
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{
			{OpenTime: snap.UnixMilli(), Open: 50, High: 50, Low: 50, Close: 50, Volume: 1},
			{OpenTime: live.UnixMilli(), Open: 101, High: 102, Low: 100, Close: 101.5, Volume: 1},
		}
	}
	// The same snapshot exists in the store as an import row (ImportBars — the
	// only writer that accepts historical_import).
	if _, _, err := st.BarHistory().ImportBars([]store.BarHistoryDB{{
		Symbol: "MNQ", TF: "1m", OpenTimeMs: snap.UnixMilli(),
		O: 50, H: 50, L: 50, C: 50, V: 1, Contract: "MNQ 12-26",
		Source: store.BarSourceHistoricalImport,
	}}); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	out, _ := s.getKlinesFromNinjaTrader("MNQ", "1m", 5)
	if len(out) != 1 {
		t.Fatalf("served %d bars, want 1 — the ring's import snapshot must be dropped from the display chart: %+v", len(out), out)
	}
	if out[0].OpenTime != live.UnixMilli() {
		t.Fatalf("served bar at %d, want the live bar at %d", out[0].OpenTime, live.UnixMilli())
	}
	for _, k := range out {
		if k.Close == 50 {
			t.Fatalf("a historical_import snapshot rendered on the display chart: %+v", k)
		}
	}
}

// F1 (2026-09-14) — the dense history import is the SAME source flag as the
// sparse wave-101 snapshots: a source filter can no longer tell them apart.
// The display seam keeps dense import fills (neighbors within 3×TF) and drops
// only isolated ones — otherwise the 09-11 hole fill would be invisible.
func TestKlinesNinjaTraderDisplayKeepsDenseImportHistory(t *testing.T) {
	orig := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = orig }()

	st, err := store.New(filepath.Join(t.TempDir(), "klines-dense.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{{OpenTime: now.UnixMilli(), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 1}}
	}
	// Dense import fill: three consecutive minutes strictly older than the
	// ring's oldest bar.
	var rows []store.BarHistoryDB
	for i := 3; i >= 1; i-- {
		rows = append(rows, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", OpenTimeMs: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			O: 90, H: 91, L: 89, C: 90.5, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceHistoricalImport,
		})
	}
	if _, _, err := st.BarHistory().ImportBars(rows); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	out, _ := s.getKlinesFromNinjaTrader("MNQ", "1m", 5)
	if len(out) != 4 {
		t.Fatalf("served %d bars, want 4 (3 dense import + 1 ring): %+v", len(out), out)
	}
	if out[0].OpenTime != now.Add(-3*time.Minute).UnixMilli() {
		t.Fatalf("oldest served %d, want the dense import's oldest minute %d", out[0].OpenTime, now.Add(-3*time.Minute).UnixMilli())
	}
	for i := 1; i < len(out); i++ {
		if out[i].OpenTime != out[i-1].OpenTime+60000 {
			t.Fatalf("dense import rendered with a hole at index %d: %+v", i, out)
		}
	}
}

// F1 (2026-09-14) — NT8's local seed has INTERIOR holes (NT8 was off 09-11
// 09:06-12:45) that the history import filled in the STORE. The display seam
// interleaves store bars into ring holes so the chart is continuous — the
// ring keeps every timestamp it holds, the store only fills the gaps.
func TestKlinesNinjaTraderDisplayFillsRingHoles(t *testing.T) {
	orig := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = orig }()

	st, err := store.New(filepath.Join(t.TempDir(), "klines-hole.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	// The ring has a 5-minute interior hole: 10:00 and 10:05.
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{
			{OpenTime: base.UnixMilli(), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 1},
			{OpenTime: base.Add(5 * time.Minute).UnixMilli(), Open: 105, High: 106, Low: 104, Close: 105.5, Volume: 1},
		}
	}
	// The store holds the missing minutes (dense import fill).
	var rows []store.BarHistoryDB
	for i := 1; i <= 4; i++ {
		rows = append(rows, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", OpenTimeMs: base.Add(time.Duration(i) * time.Minute).UnixMilli(),
			O: 101, H: 102, L: 100, C: 101.5, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceHistoricalImport,
		})
	}
	if _, _, err := st.BarHistory().ImportBars(rows); err != nil {
		t.Fatal(err)
	}

	s := &Server{store: st}
	out, _ := s.getKlinesFromNinjaTrader("MNQ", "1m", 10)
	if len(out) != 6 {
		t.Fatalf("served %d bars, want 6 (2 ring + 4 store fill): %+v", len(out), out)
	}
	for i := 1; i < len(out); i++ {
		if out[i].OpenTime != out[i-1].OpenTime+60000 {
			t.Fatalf("ring hole not filled at index %d: %+v", i, out)
		}
	}
	if out[0].OpenTime != base.UnixMilli() || out[5].OpenTime != base.Add(5*time.Minute).UnixMilli() {
		t.Fatalf("ring edges displaced: %+v", out)
	}
}

// F1 — no store attached: the handler serves the ring exactly as before.
func TestKlinesNinjaTraderNoStoreServesRing(t *testing.T) {
	orig := market.FuturesBarsProvider
	defer func() { market.FuturesBarsProvider = orig }()
	market.FuturesBarsProvider = func(symbol, tf string, count int) []market.Kline {
		return []market.Kline{{OpenTime: 1, Close: 5}}
	}
	s := &Server{}
	out, _ := s.getKlinesFromNinjaTrader("MNQ", "1m", 100)
	if len(out) != 1 || out[0].Close != 5 {
		t.Fatalf("without a store the ring must pass through unchanged: %+v", out)
	}
}

// F1 (2026-09-14) — the dashboard's 5,000-bar ask must SURVIVE the handler's
// limit parsing. The old global 1500 clamp was a removed-provider constraint
// that was silently also capping the ninjatrader path, so the store splice
// could never fire on a warm ring.
func TestResolveKlinesLimitPerExchange(t *testing.T) {
	cases := []struct {
		exchange string
		limit    string
		want     int
	}{
		{"ninjatrader", "5000", 5000},    // F1 dashboard ask survives
		{"ninjatrader", "999999", 20000}, // ninjatrader ceiling, not the removed cap's
		{"NinjaTrader", "5000", 5000},    // case-insensitive
		{"alpaca", "5000", 5000},         // non-NT exchanges pass through
		{"", "5000", 5000},               // default exchange passes through
		{"alpaca", "abc", 1000},          // bad value → default
		{"ninjatrader", "0", 1000},       // non-positive → default
		{"", "", 1000},                   // missing → default
	}
	for _, tc := range cases {
		if got := resolveKlinesLimit(tc.exchange, tc.limit); got != tc.want {
			t.Errorf("resolveKlinesLimit(%q, %q) = %d, want %d", tc.exchange, tc.limit, got, tc.want)
		}
	}
}
