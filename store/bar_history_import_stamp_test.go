package store

import "testing"

// TestLL3AllBarsOnOpenStampsCloseStampedImports — levels audit LL-3: AllBarsOn
// (the mentor key-level seed's read) moves a close-stamped historical_import
// row (convention epoch_floor) back by its own timeframe, leaves an
// epoch_floor_open import row and every live row where they are, drops a moved
// import row that lands on a live row's minute (the live tape wins, as in
// cmd/import-label-shift), and is idempotent once the migration has run.
func TestLL3AllBarsOnOpenStampsCloseStampedImports(t *testing.T) {
	st := newBarStore(t)
	imp := func(ms int64, c float64, conv string) BarHistoryDB {
		return BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: "MNQ 09-26", OpenTimeMs: ms, O: c, H: c, L: c, C: c, V: 1, Source: BarSourceHistoricalImport, Convention: conv}
	}
	const m = 60_000
	if _, _, err := st.ImportBars([]BarHistoryDB{
		imp(10*m, 10, "epoch_floor"), // the 9th-minute bar, stamped at its close
		imp(11*m, 11, "epoch_floor"),
		imp(13*m, 13, "epoch_floor"),            // lands on the live 12 → dropped
		imp(20*m, 20, ConventionEpochFloorOpen), // already open-stamped
	}); err != nil {
		t.Fatal(err)
	}
	live := mkBar("MNQ", "1m", 12*m, 99)
	if err := st.InsertBars([]BarHistoryDB{live}); err != nil {
		t.Fatal(err)
	}
	got, err := st.AllBarsOn("MNQ", "1m", "MNQ 09-26")
	if err != nil {
		t.Fatal(err)
	}
	type tc struct {
		ms int64
		c  float64
	}
	want := []tc{{9 * m, 10}, {10 * m, 11}, {12 * m, 99}, {20 * m, 20}}
	if len(got) != len(want) {
		t.Fatalf("AllBarsOn returned %d rows %+v, want %+v", len(got), got, want)
	}
	for i, w := range want {
		if got[i].OpenTimeMs != w.ms || got[i].C != w.c {
			t.Errorf("row %d = (%d, %.0f), want (%d, %.0f)", i, got[i].OpenTimeMs, got[i].C, w.ms, w.c)
		}
	}
	// idempotent: re-stamping an already re-stamped read moves nothing
	again := OpenStampImportRows(got, "1m")
	for i := range again {
		if again[i].OpenTimeMs != got[i].OpenTimeMs {
			t.Fatalf("a second re-stamp moved row %d: %d → %d", i, got[i].OpenTimeMs, again[i].OpenTimeMs)
		}
	}
	// a timeframe the importer never wrote is returned untouched
	if out := OpenStampImportRows([]BarHistoryDB{imp(10*m, 1, "epoch_floor")}, "4h"); out[0].OpenTimeMs != 10*m {
		t.Fatalf("unknown tf must not move: %d", out[0].OpenTimeMs)
	}
}
