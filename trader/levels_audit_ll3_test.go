package trader

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"vl/store"
)

// TestLL3SeedReadsCloseStampedImportsAtTheirOpen — levels audit LL-3 (P2).
// The level is the OPEN of the 1H RTH candle that opens at 08:30, 09:30, …
// [slide 31; OctD5.2-R9 @03:37]. The live DB still holds the wave-101
// historical_import 1m rows close-stamped (convention epoch_floor, the m460
// migration never ran), so the seed's 13:30 candle opened with the 13:29 bar.
// Fixture: the real MNQ 09-26 import rows of 07-29 12:30–14:31 CT exactly as
// stored (raw stamps, source historical_import, convention epoch_floor),
// exported read-only. Through the PRODUCTION seed read (mentorFullKeyLevelHistory
// over store.BarHistoryStore.AllBarsOn — the body mentorSeedFrom runs) the
// 13:30 candle must open with the bar the import stamped 13:31 (27,776.75; on
// the 12-26 scale 28,067.75 — native 28,060.00, the mentor drew 28,062.25
// [OctD5.2-R16 @06:18]), never with the 13:29 bar (27,750.00 → 28,041.00, the
// live 11-03 level).
func TestLL3SeedReadsCloseStampedImportsAtTheirOpen(t *testing.T) {
	at := seedStateTrader(t)
	bh := store.NewBarHistoryStore(at.store.GormDB())
	if err := bh.Migrate(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/ll3_mnq0926_import_1m_20260729_1230ct.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var rows []store.BarHistoryDB
	for _, r := range recs {
		ms, _ := strconv.ParseInt(r[0], 10, 64)
		pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: "MNQ 09-26", OpenTimeMs: ms,
			O: pf(1), H: pf(2), L: pf(3), C: pf(4), V: pf(5), Source: r[6], Convention: r[7]})
	}
	if _, _, err := bh.ImportBars(rows); err != nil {
		t.Fatal(err)
	}
	_, full1H, _, _, _, _ := at.mentorFullKeyLevelHistory(bh)
	ct, _ := time.LoadLocation("America/Chicago")
	at1330 := time.Date(2026, 7, 29, 13, 30, 0, 0, ct).UnixMilli()
	at1230 := time.Date(2026, 7, 29, 12, 30, 0, 0, ct).UnixMilli()
	got := map[int64][2]float64{}
	for _, c := range full1H {
		got[c.OpenTime] = [2]float64{c.Open, c.Close}
	}
	if c, ok := got[at1330]; !ok || c[0] != 27776.75 {
		t.Errorf("07-29 13:30 1H RTH candle open = %v (ok=%v), want 27776.75 — the bar the import stamped 13:31 is the 13:30 bar (27750.00 is the 13:29 bar)", c, ok)
	}
	if c, ok := got[at1230]; !ok || c[0] != 27652.0 {
		t.Errorf("07-29 12:30 candle open = %v (ok=%v), want 27652.00 (the bar stamped 12:31)", c, ok)
	}
}
