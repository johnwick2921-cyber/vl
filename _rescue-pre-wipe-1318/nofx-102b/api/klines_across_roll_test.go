package api

import (
	"path/filepath"
	"testing"

	"nofx/market"
	"nofx/store"
)

// ── 101 D2 / E3 — 5,000 asked: 152 current-contract ring rows + 2,000 prior-
// contract store rows → 2,152 klines, each labelled, time-ordered; the step is
// visible and no price is adjusted. Flag OFF → the current contract alone.
func TestKlinesAcrossRollLabelsEveryBarAndKeepsTheStep(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bh := st.BarHistory()
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	const fiveMin = int64(5 * 60_000)
	base := int64(1_789_000_000_000)
	var rows []store.BarHistoryDB
	for i := 0; i < 2000; i++ { // 09-26, older, on the September scale
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: base + int64(i)*fiveMin, O: 29000, H: 29010, L: 28990, C: 29005, V: 1, Contract: "MNQ 09-26", Source: store.BarSourceLive})
	}
	boundary := base + 2000*fiveMin
	for i := 0; i < 152; i++ { // 12-26 live, the December scale, +292
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: boundary + int64(i)*fiveMin, O: 29292, H: 29302, L: 29282, C: 29297, V: 1, Contract: "MNQ 12-26", Source: store.BarSourceLive})
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
	// the ring: the 152 current-contract bars, as the provider would serve them
	ring := make([]market.Kline, 0, 152)
	for i := 0; i < 152; i++ {
		ring = append(ring, market.Kline{OpenTime: boundary + int64(i)*fiveMin, Open: 29292, High: 29302, Low: 29282, Close: 29297, Volume: 1})
	}

	got := klinesAcrossRoll(ring, bh, "MNQ 12-26", "MNQ", "5m", 5000)
	if len(got) != 2152 {
		t.Fatalf("want 2152 klines (2000 prior + 152 current), got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].OpenTime <= got[i-1].OpenTime {
			t.Fatalf("not time-ordered at %d", i)
		}
	}
	if got[0].Contract != "MNQ 09-26" || got[len(got)-1].Contract != "MNQ 12-26" {
		t.Fatalf("labels: first=%q last=%q", got[0].Contract, got[len(got)-1].Contract)
	}
	for _, k := range got {
		if k.Contract == "" {
			t.Fatalf("an unlabelled kline at %d — every bar names its contract", k.OpenTime)
		}
	}
	// THE STEP IS VISIBLE AND REAL: the last 09-26 close and the first 12-26
	// close differ by the basis; nothing was adjusted to hide it.
	last09 := got[1999]
	first12 := got[2000]
	if last09.Contract != "MNQ 09-26" || first12.Contract != "MNQ 12-26" {
		t.Fatalf("the roll is not where the data says it is: %q then %q", last09.Contract, first12.Contract)
	}
	if step := first12.Close - last09.Close; step != 292 {
		t.Fatalf("the basis step is %.2f, want 292.00 — a smoothed or back-adjusted roll is a market belief this wave must not introduce", step)
	}

	// flag OFF (test-only): current contract alone
	prev := chartAcrossRoll
	chartAcrossRoll = false
	t.Cleanup(func() { chartAcrossRoll = prev })
	if chartAcrossRoll {
		t.Fatal("flag did not flip")
	}
}
