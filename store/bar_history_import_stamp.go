package store

import "sort"

// importStampTFMs is the bar length of each timeframe the wave-101 importer
// wrote close-stamped (the same table cmd/import-label-shift shifts by).
var importStampTFMs = map[string]int64{"1m": 60_000, "5m": 300_000, "15m": 900_000, "30m": 1_800_000, "1h": 3_600_000}

// OpenStampImportRows is the READ-SIDE twin of the m460 migration
// (cmd/import-label-shift), applied by AllBarsOn so the mentor key-level seed
// never reads a close-stamped import row at its close (levels audit LL-3).
//
// THE DEFECT. Every historical_import row written before the importer started
// open-stamping carries convention "epoch_floor" and is stamped at the bar's
// CLOSE — one timeframe late. The migration was never run on the live database
// (75,492 MNQ 09-26 1m rows, 06-02..09-02, still plain epoch_floor on
// 2026-11-10), so the seed's "08:30" 1H RTH candle opened with the 08:29 bar:
// 381 of 794 candles had another open/close, 24 changed colour, and the 07-29
// 13:30 key level read 28,041.00 instead of 28,067.75 (MNQ 12-26 native
// 28,060.00; the mentor drew 28,062.25 [OctD5.2-R16 @06:18]).
//
// WHAT IT DOES. Exactly the rows the migration would move — source
// historical_import AND convention != epoch_floor_open, for a timeframe it
// knows — move back by their own timeframe (1m −60 s, 1h −3600 s, …). The tag
// makes it idempotent: once the owner runs the migration the rows read
// epoch_floor_open and nothing moves here. COLLISIONS: a moved import row that
// lands on a minute another (non-moving) row of the same slice already holds
// is dropped — the live tape wins, as in the migration (which parks it).
// rows must be ONE (symbol, tf, contract) slice; the result is ascending by
// OpenTimeMs and never aliases the input when anything moved.
func OpenStampImportRows(rows []BarHistoryDB, tf string) []BarHistoryDB {
	span, ok := importStampTFMs[tf]
	if !ok || len(rows) == 0 {
		return rows
	}
	moving := func(r BarHistoryDB) bool {
		return r.Source == BarSourceHistoricalImport && r.Convention != ConventionEpochFloorOpen
	}
	any := false
	for _, r := range rows {
		if moving(r) {
			any = true
			break
		}
	}
	if !any {
		return rows
	}
	fixed := make(map[int64]bool, len(rows))
	for _, r := range rows {
		if !moving(r) {
			fixed[r.OpenTimeMs] = true
		}
	}
	out := make([]BarHistoryDB, 0, len(rows))
	for _, r := range rows {
		if moving(r) {
			r.OpenTimeMs -= span
			if fixed[r.OpenTimeMs] {
				continue // the live/open-stamped row at that minute wins
			}
			r.Convention = ConventionEpochFloorOpen
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OpenTimeMs < out[j].OpenTimeMs })
	return out
}
