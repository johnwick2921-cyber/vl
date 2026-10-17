package store

import "testing"

// ── 101 D2 — THE CHART SHOWS STORED HISTORY ACROSS CONTRACT ROLLS ───────────
//
// Owner ruling 2026-09-16. A DISPLAY-ONLY reader: prior contracts fill the
// time STRICTLY BEFORE the current contract's first live row, so the series is
// continuous with exactly one visible step at the real roll — never
// back-adjusted, never smoothed (research law). The contract-scoped readers
// the kernel, levels and arm path use (LastNBarsOn, BarsBetweenOn) are not
// touched; decision truth stays current-contract only.

const fiveMin = int64(5 * 60_000)

// importHoles are the 09-26 slots the fixture leaves EMPTY so the 12-26 imports
// can occupy them. The bars PK is (symbol, tf, open_time_ms) — no contract —
// so an import at a time a 09-26 row holds is SKIPPED by ImportBars, in the
// fixture and in production alike (nofx-93's objection 1, 2026-09-16: the
// first cut of this fixture put its 5 imports on occupied slots, ImportBars
// skipped all 5, and E4's "500, imports excluded" measured an empty set). The
// 426 real 12-26 import rows therefore sit at times NO 09-26 row holds.
var importHoles = map[int]bool{2000: true, 2200: true, 2400: true, 2600: true, 2800: true}

func rollSeed(t *testing.T, bh *BarHistoryStore) (boundaryMs int64) {
	t.Helper()
	var rows []BarHistoryDB
	base := int64(1_789_000_000_000)
	// 09-26: 2,995 live bars, older, with five holes
	for i := 0; i < 3000; i++ {
		if importHoles[i] {
			continue
		}
		ts := base + int64(i)*fiveMin
		rows = append(rows, BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: ts, O: 29000, H: 29010, L: 28990, C: 29005, V: 1, Contract: "MNQ 09-26", Source: BarSourceLive})
	}
	// 12-26: sparse imports INSIDE the 09-26 window, in the holes (the
	// 09-07..09-14 shape). Imports enter through ImportBars — InsertBars
	// refuses the source.
	var imports []BarHistoryDB
	for i := range importHoles {
		ts := base + int64(i)*fiveMin
		imports = append(imports, BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: ts, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Contract: "MNQ 12-26", Source: BarSourceHistoricalImport})
	}
	// 12-26: 500 live bars starting right after the 09-26 series — the roll
	boundaryMs = base + 3000*fiveMin
	for i := 0; i < 500; i++ {
		ts := boundaryMs + int64(i)*fiveMin
		rows = append(rows, BarHistoryDB{Symbol: "MNQ", TF: "5m", OpenTimeMs: ts, O: 29290, H: 29300, L: 29280, C: 29295, V: 1, Contract: "MNQ 12-26", Source: BarSourceLive})
	}
	if err := bh.InsertBars(rows); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ins, skip, err := bh.ImportBars(imports)
	if err != nil {
		t.Fatalf("seed imports: %v", err)
	}
	// the fixture proves its own census (class 109): every import LANDED
	if ins != 5 || skip != 0 {
		t.Fatalf("fixture: ImportBars inserted=%d skipped=%d, want 5/0 — an import on an occupied slot is skipped by the PK and the test measures nothing", ins, skip)
	}
	return boundaryMs
}

// E3 (store half): 5,000 asked, 500 current-contract rows → the reader fills
// the OLDER 4,500 from 09-26, each row labelled, time-ordered, and the
// overlapping 12-26 imports do NOT interleave into the 09-26 series.
func TestPriorContractsFillBehindTheCurrentContract(t *testing.T) {
	bh := newBarStore(t)
	boundary := rollSeed(t, bh)

	rows, err := bh.PriorContractBarsBefore("MNQ", "5m", "MNQ 12-26", boundary, 4500)
	if err != nil {
		t.Fatal(err)
	}
	// 2,995, not 3,000: the fixture leaves five 09-26 slots EMPTY for the
	// 12-26 imports, and the reader does not fill a prior-series hole with the
	// current contract's import (a hole stays a hole — one bar of 12-26 price
	// space inside the 09-26 series would be a 290-point spike on the chart).
	if len(rows) != 2995 {
		t.Fatalf("want the 2995 prior 09-26 rows (3000 slots, 5 holes held by 12-26 imports), got %d", len(rows))
	}
	for i, r := range rows {
		if r.Contract != "MNQ 09-26" {
			t.Fatalf("row %d is %s — a 12-26 import in a 09-26 hole interleaved into the 09-26 series (the current contract is not a PRIOR contract; before its first live row the front month was 09-26)", i, r.Contract)
		}
		if r.OpenTimeMs >= boundary {
			t.Fatalf("row %d at %d is not strictly before the boundary %d", i, r.OpenTimeMs, boundary)
		}
		if i > 0 && rows[i].OpenTimeMs <= rows[i-1].OpenTimeMs {
			t.Fatalf("rows must be ascending by time; %d then %d", rows[i-1].OpenTimeMs, rows[i].OpenTimeMs)
		}
	}
	// NEVER back-adjusted: the 09-26 closes are the 09-26 closes
	if rows[len(rows)-1].C != 29005 {
		t.Fatalf("prior-contract price was altered: %.2f (research law: never back-adjust across a roll)", rows[len(rows)-1].C)
	}
}

// E4 (store half): the decision reader is untouched and contract-pure — and
// it does NOT filter imports. 505, not 500: LastNBarsOn's filter is
// `source NOT IN (mixed, off-scale)` (store/bar_history.go), so the 5 import
// rows come back with the 500 live ones. Guard (iii) — keeping imports out of
// the ring — therefore lives at the rehydrate DOOR (trader/ninjatrader
// rehydrateRowsFor), not in this reader (nofx-93 objection 1 + CTO ruling,
// 2026-09-16: do not change the shared reader in this PR; the planner's 1m
// splice through trader/bars_store_depth.go reads it too and excluding there
// changes today's planner input — the owner's call).
func TestCurrentContractReaderReturnsImportsUnfiltered(t *testing.T) {
	bh := newBarStore(t)
	rollSeed(t, bh)
	rows, err := bh.LastNBarsOn("MNQ", "5m", "MNQ 12-26", 5000)
	if err != nil {
		t.Fatal(err)
	}
	imports := 0
	for _, r := range rows {
		if r.Contract != "MNQ 12-26" {
			t.Fatalf("LastNBarsOn leaked a %s row — a decision reader must be contract-pure", r.Contract)
		}
		if r.Source == BarSourceHistoricalImport {
			imports++
		}
	}
	if got := len(rows); got != 505 || imports != 5 {
		t.Fatalf("LastNBarsOn(12-26) = %d rows (%d import), want 505 (5 import) — the reader hands imports to its callers; the door excludes them", got, imports)
	}
}

// FirstLiveOn: the boundary is the current contract's first LIVE row — sparse
// imports before it do not move the roll.
func TestRollBoundaryIsTheFirstLiveRowNotTheFirstImport(t *testing.T) {
	bh := newBarStore(t)
	boundary := rollSeed(t, bh)
	got, ok, err := bh.FirstLiveOn("MNQ", "5m", "MNQ 12-26")
	if err != nil || !ok {
		t.Fatalf("FirstLiveOn: ok=%v err=%v", ok, err)
	}
	if got != boundary {
		t.Fatalf("boundary = %d, want the first LIVE 12-26 row %d, not the first import", got, boundary)
	}
}
