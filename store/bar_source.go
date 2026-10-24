package store

import (
	"fmt"
	"strings"
)

// ── BAR-SOURCE WAVE (2026-09-10) — WHICH FEED WROTE THIS BAR ─────────────────
//
// THE FINDING. The contract-roll wave separated MNQ 09-26 from MNQ 12-26
// correctly and then booted into a chart that was still discontinuous. The boot
// line said 12-26=15992 where ~40 were expected; the research archive said why.
// For the 22:37 CT one-minute bar, same subscription, same label:
//
//	fact 16516009  bar_update       (live)    open 29360     close 29358.25
//	fact 16518205  bars_historical  (replay)  open 29068.75  close 29068.25
//
// NT8's replay serves the December contract ~290 points below Tradovate's live
// feed for the SAME minutes. A contract column cannot separate one contract from
// itself. At every boot or reconnect the replay repainted the ring and the store
// low, the live feed continued high, and the boot minute became a mixed bar —
// the 22:39 bar read open 29068.25 close 29355.25, the exact shape the 21:15
// bar had before it.
//
// THE DAMAGE. Go's upsert was unconditional, so that boot's replay overwrote
// 186 live bars across both symbols and six timeframes. Restored from the 22:15
// backup under owner authorisation (markers ac76b47b, 33fee48e).
//
// THE RULE. Every bar names its feed. Live is the minute as it traded and
// overwrites anything. Historical is a replay and fills only what live never
// wrote — in the store (InsertBars' upsert WHERE) and in the ring
// (BarCache.SeedHistorical keeps an existing live bar over an incoming
// historical one). Readers prefer live. A boot minute whose open came from a
// replay and whose close came from live is marked mixed and no reader accepts
// it.
//
// The NT8 side — whatever merge/back-adjust policy makes the replay sit on
// another scale — is filed for the AddOn wave with the two facts above as the
// evidence. This wave makes Go survive it.

// offScaleWindow is one MEASURED span of rows written by a replay on another
// price scale than the live feed, before the replay hold existed. Read-only
// queries on data.db, 2026-09-10 23:3x CDT, before this wave's migration:
//
//	MNQ 12-26, 21:15–22:38 CT open times, o AND c below 29200 → 51 rows
//	  (1m 30 · 3m 9 · 5m 5 · 15m 5 · 30m 2; rowids 464781–465008)
//	ES  12-26, same window, o AND c below 7630 → 47 rows
//	  (1m 26 · 3m 9 · 5m 5 · 15m 5 · 30m 2; rowids 464775–465010)
//
// Live December closes in that window are 29350–29421 / 7660–7677; the
// replay's are 29052–29094 / 7599–7602. The dividing value sits ~150 / ~30
// points from either side. Outside the window the value rule means nothing
// (September traded through 29200 for weeks) and is not applied.
//
// The same windows hold the STRADDLING bars — one side on each scale: the
// roll minutes 21:14–21:17 (whose spans-roll contract labels the 22:39 boot's
// replay overwrote to 12-26, so the roll wave's own rule no longer finds
// them) and the 22:39 boot minute with the aggregates that were open across
// it. 25 rows (MNQ 12, ES 13), labelled mixed by value, never by rowid.
type offScaleWindow struct {
	Symbol, Contract string
	FromMs, ToMs     int64 // open_time_ms, [from, to)
	Below            float64
}

var offScale20260910 = []offScaleWindow{
	{Symbol: "MNQ", Contract: "MNQ 12-26", FromMs: 1789092900000, ToMs: 1789097940000, Below: 29200}, // 21:15–22:39 CT
	{Symbol: "ES", Contract: "ES 12-26", FromMs: 1789092900000, ToMs: 1789097940000, Below: 7630},
}

// straddle20260910 is the wider window the mixed rule scans: 21:00–22:40 CT,
// so the 2h bar opened at 21:00 and the 1h bar opened at 22:00 are seen.
var straddle20260910 = []offScaleWindow{
	{Symbol: "MNQ", FromMs: 1789092000000, ToMs: 1789098000000, Below: 29200},
	{Symbol: "ES", FromMs: 1789092000000, ToMs: 1789098000000, Below: 7630},
}

// migrateSourceColumn adds the column (idempotent) and labels pre-existing rows.
//
// PRE-COLUMN ROWS ARE LABELLED LIVE. The first draft labelled them historical
// and called it conservative — "a later live bar may overwrite this". That
// reasoning covered live-over-historical and missed historical-over-historical:
// a future REPLAY overwrites a historical row, which is exactly the damage of
// 2026-09-10 22:39, and the label would have licensed it again at the very next
// boot. The store is the record of what the bot saw as it traded; the whole
// purpose of this wave is that a replay must not repaint that record. So the
// record is live, and only rows this process itself receives as a replay are
// historical.
//
// EXCEPT WHERE THE TAPE SAYS OTHERWISE, MEASURED. The rows an earlier boot's
// replay wrote on the wrong scale (offScale20260910) are labelled
// replay:off-scale — no reader takes them, and a live bar or a verified replay
// may overwrite them, which is the repair path. Their VALUES are untouched
// (A24); the reconstruction from the research archive's live facts is
// owner-authorised separately. Straddling bars (straddle20260910) and any row
// still carrying the roll wave's spans-roll contract are mixed.
//
// Order matters and is idempotent: the value rules run first on unlabelled
// rows, then everything still unlabelled is live. A second run finds nothing
// unlabelled and changes nothing.
func (s *BarHistoryStore) migrateSourceColumn() error {
	var has int64
	if err := s.db.Raw("SELECT COUNT(*) FROM pragma_table_info('bars') WHERE name='source'").Scan(&has).Error; err != nil {
		return err
	}
	if has == 0 {
		if err := s.db.Exec("ALTER TABLE bars ADD COLUMN source TEXT NOT NULL DEFAULT ''").Error; err != nil {
			return err
		}
	}
	// GORM's AutoMigrate adds the column nullable first; normalise (roll wave lesson).
	if err := s.db.Exec("UPDATE bars SET source = '' WHERE source IS NULL").Error; err != nil {
		return err
	}
	if err := s.db.Exec("UPDATE bars SET source = ? WHERE source = '' AND contract = ?", BarSourceMixed, ContractMixed).Error; err != nil {
		return err
	}
	for _, w := range straddle20260910 {
		if err := s.db.Exec(`UPDATE bars SET source = ? WHERE source = '' AND symbol = ? AND open_time_ms >= ? AND open_time_ms < ?
			AND ((o < ?) <> (c < ?))`, BarSourceMixed, w.Symbol, w.FromMs, w.ToMs, w.Below, w.Below).Error; err != nil {
			return err
		}
	}
	for _, w := range offScale20260910 {
		if err := s.db.Exec(`UPDATE bars SET source = ? WHERE source = '' AND symbol = ? AND contract = ? AND open_time_ms >= ? AND open_time_ms < ?
			AND o < ? AND c < ?`, BarSourceOffScale, w.Symbol, w.Contract, w.FromMs, w.ToMs, w.Below, w.Below).Error; err != nil {
			return err
		}
	}
	// (index creation moved to bar_contract_key.go — the set depends on the key)
	return s.db.Exec("UPDATE bars SET source = ? WHERE source = ''", BarSourceLive).Error
}

// SourceCensus reports, per symbol, how many rows carry each source label —
// the boot line's figure, read not literal.
func (s *BarHistoryStore) SourceCensus(symbol string) (map[string]int64, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	type row struct {
		Source string
		N      int64
	}
	var rows []row
	if err := s.db.Raw("SELECT source, COUNT(*) AS n FROM bars WHERE symbol = ? GROUP BY source", symbol).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Source] = r.N
	}
	return out, nil
}

// IsReadableSource reports whether a bar's source may be handed to a reader.
// Mixed never is: it is two price scales in one candle. Off-scale never is: it
// is one scale, the wrong one.
func IsReadableSource(src string) bool {
	src = strings.TrimSpace(src)
	return src == BarSourceLive || src == BarSourceHistorical || src == BarSourceLivePartial
}

// IsBacktestReadable reports whether a bar may be read by a BACKTEST reader.
// Backtests may hold imported history (wave 101: deliberately pulled named-
// contract tape, a third feed distinct from the live-path replay) beside live
// and historical. Mixed and off-scale never are, and continuous:adjusted rows
// are never persisted at all. Live trading readers keep IsReadableSource, so
// imported rows are invisible to every live path even without a contract
// filter — the current-contract filter remains the seam guard, this is the
// second lock.
func IsBacktestReadable(src string) bool {
	return IsReadableSource(src) || strings.TrimSpace(src) == BarSourceHistoricalImport
}
