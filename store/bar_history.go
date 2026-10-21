package store

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// BAR PERSISTENCE (2026-08-26) — the unblock for replay/calibration.
//
// Every CLOSED OHLCV bar the NT8 BarCache receives is persisted here,
// idempotently (INSERT OR IGNORE on the composite PK). This table feeds:
//   - the volume-levels wave validation replay (VWAP/POC/VAH/VAL/naked-POC),
//   - the swing-k / MSS-FVG / trail-mult calibration queue,
//   - the min-SL replay (joined with structure_json.atr by timestamp).
//
// Retention: BAR_RETENTION_DAYS (env, default 90). 1m MNQ+ES ≈ ~50 bytes/row
// ≈ ~2,900 rows/day → ≈1.5 MB/day → ≈130 MB at the 90-day cap. Trivial.

// BarHistoryDB is one closed OHLCV bar. OpenTimeMs is the bar's OPEN time
// (epoch ms UTC) — the BarCache canonical contract.
type BarHistoryDB struct {
	Symbol     string  `gorm:"column:symbol;primaryKey"`
	TF         string  `gorm:"column:tf;primaryKey"`
	OpenTimeMs int64   `gorm:"column:open_time_ms;primaryKey"`
	O          float64 `gorm:"column:o"`
	H          float64 `gorm:"column:h"`
	L          float64 `gorm:"column:l"`
	C          float64 `gorm:"column:c"`
	V          float64 `gorm:"column:v"`
	// Convention (BAR-SOURCE WAVE 2026-09-02) names the calendar this row's
	// bucket start is stamped on: "epoch_floor" (ours) or "fri_thu" (NT8's
	// native weekly). Empty on rows written before the column existed.
	Convention string `gorm:"column:convention"`
	// Contract (ROLL WAVE 2026-09-10) names the futures contract this bar was
	// received on — the qualified NT8 name the AddOn's subscription ACK
	// carried, e.g. "MNQ 09-26" or "MNQ 12-26". NEVER derived from a date
	// rule: the AddOn's own date-based resolver rolled the subscription
	// mid-ASIA at 21:15 CT on 2026-09-10 and three bars per symbol arrived with
	// one contract's open and the other's close (a ~292-point phantom on MNQ,
	// ~65 on ES). Those rows carry ContractMixed and no reader accepts them.
	//
	// Empty on rows written before the column existed and never backfilled;
	// InsertBars refuses to write a new row without one.
	//
	// W-BARS-CONTRACT-KEY (2026-09-18): PART OF THE PRIMARY KEY. Two contracts
	// may hold the same minute (the new contract's served history beside the
	// old contract's live tape at a roll); a reader that wants one price scale
	// names the contract. The sqlite key order is (symbol, tf, contract,
	// open_time_ms) — see bar_contract_key.go, which owns the DDL.
	Contract string `gorm:"column:contract;primaryKey"`
	// Source (BAR-SOURCE WAVE 2026-09-10) names WHICH FEED this bar came from:
	// BarSourceLive (a bar_update as the minute traded) or BarSourceHistorical
	// (a bars_historical replay after a subscribe/reconnect). On 2026-09-10 NT8's
	// replay served the December contract ~290 points below Tradovate's live feed
	// for the SAME minutes under the SAME label — research facts 16516009 (live,
	// 22:37 close 29358.25) vs 16518205 (replay, same bar, close 29068.25). A
	// replay that can be on a different scale than live must never overwrite
	// live, and a reader must be able to tell which it is holding.
	Source string `gorm:"column:source"`
}

// TableName is the bars table (spec name).
func (BarHistoryDB) TableName() string { return "bars" }

// BarRetentionDays resolves the bars retention window (env BAR_RETENTION_DAYS,
// default 90). A value ≤ 0 means "keep forever".
// tfRetentionDays (BAR-SOURCE WAVE 2026-09-02) — retention is PER TF. The old
// single cutoff was TF-blind: pruning at 90 days would delete the 383 weekly
// bars back to 2019 the moment they were persisted, which is the whole reason
// to persist them. A coarse bar is tiny and irreplaceable; a 1m bar is bulky
// and re-fetchable. 0 = keep forever.
//
// Storage at steady state (measured base: 23,470 rows = 1.34 MB ≈ 60 B/row;
// 2 symbols): 1m 90d ≈ 261k rows ≈ 16 MB · 5m 180d ≈ 104k ≈ 6 MB · 15m 365d
// ≈ 70k ≈ 4 MB · 1h and coarser kept forever ≈ 90k ≈ 5 MB → ≈ 31 MB total
// against a 634 MB database.
var tfRetentionDays = map[string]int{
	"3m": 180, "5m": 180,
	"15m": 365, "30m": 365,
	"1h": 0, "2h": 0, "4h": 0, "6h": 0, "8h": 0, "12h": 0,
	"1d": 0, "3d": 0, "1w": 0,
}

// RetentionDaysFor resolves one TF's retention. 1m follows BAR_RETENTION_DAYS
// (env, default 90) so the existing knob keeps its meaning; every other TF
// reads the table above. 0 = keep forever.
func RetentionDaysFor(tf string) int {
	tf = strings.ToLower(strings.TrimSpace(tf))
	if tf == "1m" {
		return BarRetentionDays()
	}
	if d, ok := tfRetentionDays[tf]; ok {
		return d
	}
	return BarRetentionDays()
}

// PruneByTF applies the per-TF retention and returns rows deleted per TF.
// A TF with retention 0 is never pruned.
func (s *BarHistoryStore) PruneByTF(now time.Time) (map[string]int64, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var tfs []string
	if err := s.db.Raw("SELECT DISTINCT tf FROM bars").Scan(&tfs).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, tf := range tfs {
		days := RetentionDaysFor(tf)
		if days <= 0 {
			continue // keep forever
		}
		cutoff := now.AddDate(0, 0, -days).UnixMilli()
		// HISTORY IMPORT (wave 101): imported history is NEVER pruned — it is
		// irreplaceable tape pulled once from NT8, and the retention sweep
		// exists to bound LIVE churn. The source column carries the exemption:
		// a row marked historical_import survives any cutoff.
		res := s.db.Exec("DELETE FROM bars WHERE tf = ? AND open_time_ms < ? AND source != ?", tf, cutoff, BarSourceHistoricalImport)
		if res.Error != nil {
			return out, res.Error
		}
		if res.RowsAffected > 0 {
			out[tf] = res.RowsAffected
		}
	}
	return out, nil
}

func BarRetentionDays() int {
	if v := os.Getenv("BAR_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 90
}

// Bar sources. Live is the truth of the minute as it traded; historical is a
// replay and may be on a different price scale (NT8 merge/back-adjust policy).
// Mixed marks a bar whose own body spans the two scales — the boot minute when
// a replay seeded its open and a live update supplied its close.
const (
	BarSourceLive       = "live"
	BarSourceHistorical = "historical"
	BarSourceMixed      = "mixed"
	// BarSourceOffScale marks a row MEASURED to hold an unverified replay's
	// values on another price scale than the live feed (the 2026-09-10 boots,
	// before the replay hold existed). Written only by the migration's measured
	// backfill — the wire discards such a replay and never writes it. No reader
	// takes it; a live bar or a verified replay overwrites it.
	BarSourceOffScale = "replay:off-scale"
	// BarSourceHistoricalImport (HISTORY IMPORT, wave 101) marks a bar pulled
	// deliberately from a NAMED expired contract and written by the importer
	// (store.ImportBars), never by the live ingest. It is the third feed:
	// distinct from BarSourceHistorical (a live-path replay). Imported rows are
	// never pruned (PruneByTF skips them) and never overwrite anything (the
	// importer has no upsert at all).
	BarSourceHistoricalImport = "historical_import"
	// BarSourceContinuous marks an ADJUSTED continuous series built deliberately
	// by BuildContinuous with an explicit per-seam basis — never raw tape. The
	// store REFUSES to persist it (ImportBars and InsertBars both reject it), so
	// an adjusted series can never be mistaken for raw bars.
	BarSourceContinuous = "continuous:adjusted"
)

// ContractMixed marks a bar whose OHLC straddles a contract roll: the AddOn
// re-subscribed mid-bar and the frame carried the retired contract's open with
// the new contract's close. It matches NO contract filter by construction, so
// every reader that asks for the current contract skips it — which is the only
// honest thing to do with a bar that is not on any single price scale.
const ContractMixed = "unrecomputable:spans_roll"

// IsUsableContract reports whether a contract label can be read as a single
// price scale. Empty (pre-column, un-backfilled) and MIXED both fail.
func IsUsableContract(c string) bool {
	c = strings.TrimSpace(c)
	return c != "" && c != ContractMixed
}

// BarHistoryStore persists closed bars (gorm-backed, like the other sub-stores).
type BarHistoryStore struct {
	db *gorm.DB
}

// NewBarHistoryStore constructs the sub-store.
func NewBarHistoryStore(db *gorm.DB) *BarHistoryStore { return &BarHistoryStore{db: db} }

// Migrate creates the bars table + the natural-key unique index (additive +
// idempotent). On SQLite it also runs the ONE-SHOT integrity migration:
//
//	(1) a safety copy `bars_pre_dedupe_<date>` of the pre-fix table (if absent),
//	(2) DELETE keeping max(rowid) per (symbol, tf, open_time_ms) — 2026-08-26
//	    live table held 17,695 duplicate revisions (F5, 2026-08-27-london-
//	    drought.md), written by the old INSERT OR IGNORE against a table with
//	    no real unique constraint,
//	(3) CREATE UNIQUE INDEX on the natural key so the constraint is real,
//	(4) DELETE tf != '1m' rows — 5m/15m are DERIVED ON READ from 1m (the
//	    stored NT8 aggregates were inconsistent with their 1m constituents).
//
// Idempotent: the heavy steps run only while the unique index is absent; every
// later boot is a no-op.
//
// W-BARS-CONTRACT-KEY (2026-09-18) — the legacy passes above run ONLY on a
// table still keyed (symbol, tf, open_time_ms); then the key migration
// (bar_contract_key.go) moves the table onto (symbol, tf, contract,
// open_time_ms) behind a whole-database backup, fail-open. The 2026-08-27
// dedupe block is gated on the LEGACY key on purpose: on the contract key a
// roll overlap is two legitimate rows per minute, and "keep max(rowid) per
// (symbol, tf, open_time_ms)" would delete the new contract's history.
// KeyReport carries what this boot did; the caller prints its BootLine.
func (s *BarHistoryStore) Migrate() error {
	_, err := s.MigrateWithReport(time.Now())
	return err
}

// MigrateWithReport is Migrate with the key migration's report returned for
// the boot line (READ values). `now` names the backup file and the renamed
// table.
func (s *BarHistoryStore) MigrateWithReport(now time.Time) (BarsKeyReport, error) {
	if s == nil || s.db == nil {
		return BarsKeyReport{}, fmt.Errorf("store required")
	}
	if s.db.Dialector.Name() != "sqlite" {
		barsKeyed.Store(true)
		if err := s.db.AutoMigrate(&BarHistoryDB{}); err != nil {
			return BarsKeyReport{}, err
		}
		return BarsKeyReport{AlreadyKeyed: true}, s.db.Exec("CREATE INDEX IF NOT EXISTS idx_bars_sym_tf_time ON bars(symbol, tf, open_time_ms DESC)").Error
	}
	// A fresh database gets the contract key from the exact DDL — never from
	// AutoMigrate, whose key order follows struct field order.
	if err := s.db.Exec(fmt.Sprintf(barsCreateDDL, "bars")).Error; err != nil {
		return BarsKeyReport{}, err
	}
	keyed, legacy, err := s.barsKeyState()
	if err != nil {
		return BarsKeyReport{}, err
	}
	if !keyed && legacy {
		hasUnique, err := s.barsIndexOnTable(idxBarsLegacyUnique, "bars")
		if err != nil {
			return BarsKeyReport{}, err
		}
		if !hasUnique {
			// (1) pre-dedupe safety copy (besides the systemd-timer backups).
			today := now.Format("2006-01-02")
			backup := "bars_pre_dedupe_" + today
			if err := s.db.Exec(`CREATE TABLE IF NOT EXISTS "` + backup + `" AS SELECT * FROM bars`).Error; err != nil {
				return BarsKeyReport{}, err
			}
			// (2) keep max(rowid) per natural key.
			if err := s.db.Exec("DELETE FROM bars WHERE rowid NOT IN (SELECT MAX(rowid) FROM bars GROUP BY symbol, tf, open_time_ms)").Error; err != nil {
				return BarsKeyReport{}, err
			}
			// (4) 1m-only storage — aggregates derive on read.
			if err := s.db.Exec("DELETE FROM bars WHERE tf <> '1m'").Error; err != nil {
				return BarsKeyReport{}, err
			}
		}
		// convention column (idempotent ADD; pre-existing rows keep "").
		var hasConv int64
		if err := s.db.Raw("SELECT COUNT(*) FROM pragma_table_info('bars') WHERE name='convention'").Scan(&hasConv).Error; err != nil {
			return BarsKeyReport{}, err
		}
		if hasConv == 0 {
			if err := s.db.Exec("ALTER TABLE bars ADD COLUMN convention TEXT NOT NULL DEFAULT ''").Error; err != nil {
				return BarsKeyReport{}, err
			}
		}
	}
	// ROLL WAVE: the contract column and its one-time backfill. BAR-SOURCE
	// WAVE: which feed wrote each row. Both idempotent (WHERE-scoped to rows
	// still unlabelled), so they are safe on either key.
	if err := s.migrateContractColumn(); err != nil {
		return BarsKeyReport{}, err
	}
	if err := s.migrateSourceColumn(); err != nil {
		return BarsKeyReport{}, err
	}
	// W-BARS-CONTRACT-KEY — the key migration, fail-open.
	rep := s.migrateContractKey(now)
	nowKeyed := rep.Migrated || rep.AlreadyKeyed
	barsKeyed.Store(nowKeyed)
	if nowKeyed {
		if err := s.ensureKeyedIndexes(s.db); err != nil {
			return rep, err
		}
	} else if err := s.ensureLegacyIndexes(); err != nil {
		return rep, err
	}
	// AutoMigrate LAST and only for columns a later wave may add: every column
	// exists, primary-key fields skip its nullable/default checks, so it alters
	// nothing on either key (pinned by TestBarsKeyMigrateIsIdempotent).
	if err := s.db.AutoMigrate(&BarHistoryDB{}); err != nil {
		return rep, err
	}
	return rep, nil
}

// InsertBars upserts closed 1m bars on the natural key — a revision of an
// already-stored bar UPDATES (close/volume move on forming-bar snapshots), and
// the unique index makes duplication impossible (F5: the old INSERT OR IGNORE
// wrote 17,695 duplicate revisions because the table had no real constraint).
// Only tf="1m" rows are stored: 5m/15m aggregates are DERIVED ON READ from 1m.
//
// W-BARS-CONTRACT-KEY: the natural key is (symbol, tf, contract, open_time_ms)
// — the same minute on two contracts is two rows, and the upsert rule below
// applies WITHIN a contract. The conflict target is read from the key the
// table actually has (barsConflictTarget), so a migration that failed open
// leaves a working writer on the legacy key.
func (s *BarHistoryStore) InsertBars(rows []BarHistoryDB) error {
	if s == nil || s.db == nil || len(rows) == 0 {
		return nil
	}
	const batch = 200
	for start := 0; start < len(rows); start += batch {
		end := start + batch
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		placeholders := make([]string, 0, len(chunk))
		args := make([]interface{}, 0, len(chunk)*11)
		for _, r := range chunk {
			if r.TF == "" {
				continue // BAR-SOURCE WAVE 2026-09-02: every TF the cache holds
				// is now persisted (owner ruling) — the old `TF != "1m"` gate
				// threw away 383 weekly / 1500 daily bars on every restart.
			}
			// ROLL WAVE: NOT NULL going forward, enforced HERE because SQLite
			// cannot add a NOT NULL column to a populated table without a
			// default, and a default would be a date rule in disguise. A bar
			// with no contract is a bar on an unknown price scale; refusing it
			// is cheaper than every reader having to guess.
			if strings.TrimSpace(r.Contract) == "" {
				return fmt.Errorf("bars: refusing %s %s @%d with no contract — the subscription's resolved contract must be stamped at write time (roll wave 2026-09-10)", r.Symbol, r.TF, r.OpenTimeMs)
			}
			src := strings.TrimSpace(r.Source)
			if src != BarSourceLive && src != BarSourceHistorical && src != BarSourceMixed {
				return fmt.Errorf("bars: refusing %s %s @%d with source %q — every bar names its feed: live, historical or mixed (bar-source wave 2026-09-10)", r.Symbol, r.TF, r.OpenTimeMs, r.Source)
			}
			placeholders = append(placeholders, "(?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, r.Symbol, r.TF, r.OpenTimeMs, r.O, r.H, r.L, r.C, r.V, r.Convention, r.Contract, src)
		}
		if len(placeholders) == 0 {
			continue
		}
		// THE UPSERT RULE. A REPLAY NEVER OVERWRITES A LIVE BAR.
		//
		// This was an unconditional DO UPDATE — harmless while replay and live
		// shared a scale, and on 2026-09-10 22:39 CT it let a back-adjusted
		// replay overwrite 186 live bars (~290 points each) across both symbols
		// and six timeframes. Restored from backup, owner-authorised, in two
		// WHERE-scoped writes (markers ac76b47b and 33fee48e).
		//
		// Live overwrites anything: it is the minute as it traded. Historical
		// fills only what live never wrote. Mixed is written once and then
		// behaves as live for precedence — it is what the wire delivered for
		// that minute, and hiding it behind a later replay would erase the
		// evidence of the seam.
		q := "INSERT INTO bars(symbol, tf, open_time_ms, o, h, l, c, v, convention, contract, source) VALUES " +
			strings.Join(placeholders, ",") +
			" ON CONFLICT(" + barsConflictTarget() + ") DO UPDATE SET o=excluded.o, h=excluded.h, l=excluded.l, c=excluded.c, v=excluded.v, convention=excluded.convention, contract=excluded.contract, source=excluded.source" +
			" WHERE NOT (bars.source IN ('live','mixed') AND excluded.source = 'historical')"
		if err := s.db.Exec(q, args...).Error; err != nil {
			return err
		}
	}
	return nil
}

// ImportBars writes pulled historical bars under source=historical_import.
// HISTORY IMPORT (wave 101). The rules, all pinned by tests:
//
//   - NO UPSERT, ever: a key collision keeps the existing row's values and is
//     COUNTED as a skip. This is deliberately NOT the InsertBars upsert — the
//     09-10 damage was exactly an import-shaped write overwriting live tape.
//     W-BARS-CONTRACT-KEY: the key includes the contract, so an import of
//     contract B at a minute contract A holds LANDS (it is B's history, not a
//     collision) — a skip now means the SAME contract already holds that bar.
//   - Every row must carry a non-empty contract and source=historical_import;
//     anything else is refused with the row named (A24: an unstamped bar is
//     never written).
//   - Returns (inserted, skipped, error): the three-state import report builds
//     on this, so a caller can say imported/skipped/unavailable with counts.
func (s *BarHistoryStore) ImportBars(rows []BarHistoryDB) (inserted int64, skipped int64, err error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("store required")
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	const batch = 200
	for start := 0; start < len(rows); start += batch {
		end := start + batch
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		placeholders := make([]string, 0, len(chunk))
		args := make([]interface{}, 0, len(chunk)*11)
		for _, r := range chunk {
			if r.TF == "" {
				return inserted, skipped, fmt.Errorf("bars: import refusing %s %s @%d with no timeframe", r.Symbol, r.Contract, r.OpenTimeMs)
			}
			if strings.TrimSpace(r.Contract) == "" {
				return inserted, skipped, fmt.Errorf("bars: import refusing %s %s @%d with no contract — every imported bar names the series it came from (wave 101)", r.Symbol, r.TF, r.OpenTimeMs)
			}
			if r.Source != BarSourceHistoricalImport {
				return inserted, skipped, fmt.Errorf("bars: import refusing %s %s %s @%d with source %q — only historical_import rows enter through this door (wave 101)", r.Symbol, r.TF, r.Contract, r.OpenTimeMs, r.Source)
			}
			placeholders = append(placeholders, "(?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, r.Symbol, r.TF, r.OpenTimeMs, r.O, r.H, r.L, r.C, r.V, r.Convention, r.Contract, r.Source)
		}
		if len(placeholders) == 0 {
			continue
		}
		q := "INSERT INTO bars(symbol, tf, open_time_ms, o, h, l, c, v, convention, contract, source) VALUES " +
			strings.Join(placeholders, ",") +
			" ON CONFLICT(" + barsConflictTarget() + ") DO NOTHING"
		res := s.db.Exec(q, args...)
		if res.Error != nil {
			return inserted, skipped, res.Error
		}
		inserted += res.RowsAffected
		skipped += int64(len(chunk)) - res.RowsAffected
	}
	return inserted, skipped, nil
}

// HistoryHeldRow is one (contract, tf) group of imported history (wave 101).
type HistoryHeldRow struct {
	Contract string
	TF       string
	N        int64
	FirstMs  int64
	LastMs   int64
}

// HistoryHeld reports the imported history the store HOLDS for a symbol,
// per contract × timeframe, oldest contract first. READ, never literal: a
// contract the store has never seen is simply absent.
func (s *BarHistoryStore) HistoryHeld(symbol string) ([]HistoryHeldRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var rows []HistoryHeldRow
	err := s.db.Raw(`SELECT contract, tf, COUNT(*) AS n, MIN(open_time_ms) AS first_ms, MAX(open_time_ms) AS last_ms
		FROM bars WHERE symbol = ? AND source = ? AND contract != '' AND contract != ?
		GROUP BY contract, tf ORDER BY MIN(open_time_ms), contract, tf`,
		symbol, BarSourceHistoricalImport, ContractMixed).Scan(&rows).Error
	return rows, err
}

// ClearSince deletes rows with open_time_ms >= sinceMs for (symbol, tf) — the
// BAR-TRUTH backfill wipes the window BEFORE a deep replay repopulates it, so
// previously-misstamped rows can never survive as spurious extras.
func (s *BarHistoryStore) ClearSince(symbol, tf string, sinceMs int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	res := s.db.Where("symbol = ? AND tf = ? AND open_time_ms >= ?", symbol, tf, sinceMs).
		Delete(&BarHistoryDB{})
	return res.RowsAffected, res.Error
}

// BarsIntegrity returns the nightly integrity triple: duplicate natural-key
// groups (must be 0), the distinct tfs present, and total rows. The tf set is
// REPORTED, not asserted, since 2026-09-02: every cached TF is persisted.
//
// W-BARS-CONTRACT-KEY: the natural key is (symbol, tf, contract, open_time_ms)
// on the contract key. Two contracts on one minute is a ROLL OVERLAP, not a
// duplicate — RollOverlaps counts those separately for the same line.
func (s *BarHistoryStore) BarsIntegrity() (dups int64, tfs []string, total int64, err error) {
	if s == nil || s.db == nil {
		return 0, nil, 0, fmt.Errorf("store required")
	}
	if err = s.db.Raw("SELECT COUNT(*) FROM (SELECT symbol, tf, contract, open_time_ms FROM bars GROUP BY symbol, tf, contract, open_time_ms HAVING COUNT(*) > 1)").Scan(&dups).Error; err != nil {
		return 0, nil, 0, err
	}
	if err = s.db.Raw("SELECT DISTINCT tf FROM bars ORDER BY tf").Scan(&tfs).Error; err != nil {
		return 0, nil, 0, err
	}
	if err = s.db.Model(&BarHistoryDB{}).Count(&total).Error; err != nil {
		return 0, nil, 0, err
	}
	return dups, tfs, total, nil
}

// Count returns the persisted bar count (boot line + growth proof).
func (s *BarHistoryStore) Count() (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store required")
	}
	var n int64
	err := s.db.Model(&BarHistoryDB{}).Count(&n).Error
	return n, err
}

// SymbolTFCount returns the (symbol, tf) pair count (boot line).
func (s *BarHistoryStore) SymbolTFCount() (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store required")
	}
	var n int64
	err := s.db.Raw("SELECT COUNT(*) FROM (SELECT DISTINCT symbol, tf FROM bars)").Scan(&n).Error
	return n, err
}

// PruneOlderThan deletes bars older than cutoffMs (RETENTION). Returns the
// deleted count.
func (s *BarHistoryStore) PruneOlderThan(cutoffMs int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store required")
	}
	res := s.db.Exec("DELETE FROM bars WHERE open_time_ms < ?", cutoffMs)
	return res.RowsAffected, res.Error
}

// BarsBetween is the one-line replay read: bars for (symbol, tf) in
// [fromMs, toMs). Join with structure_json.atr by timestamp in scripts.
func (s *BarHistoryStore) BarsBetween(symbol, tf string, fromMs, toMs int64) ([]BarHistoryDB, error) {
	return s.BarsBetweenOn(symbol, tf, "", fromMs, toMs)
}

// BarsBetweenOn is BarsBetween restricted to ONE contract. An empty contract
// means "unfiltered" and exists only for replay/audit tooling that wants to see
// the seam; every live reader passes the current contract.
func (s *BarHistoryStore) BarsBetweenOn(symbol, tf, contract string, fromMs, toMs int64) ([]BarHistoryDB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var out []BarHistoryDB
	q := s.db.Where("symbol = ? AND tf = ? AND open_time_ms >= ? AND open_time_ms < ? AND COALESCE(source, '') NOT IN (?, ?)", symbol, tf, fromMs, toMs, BarSourceMixed, BarSourceOffScale)
	if c := strings.TrimSpace(contract); c != "" {
		q = q.Where("contract = ?", c)
	}
	err := q.Order("open_time_ms").Find(&out).Error
	return out, err
}

// RetentionCutoffMs is the prune boundary for the resolved retention window.
func RetentionCutoffMs(now time.Time) int64 {
	days := BarRetentionDays()
	if days <= 0 {
		return 0 // keep forever
	}
	return now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
}

// LastNBars returns the NEWEST n bars for (symbol, tf), ASCENDING by open time
// — the same order and shape every ring reader already expects, so a caller can
// splice a store read and a cache read without reordering either.
//
// THE STORE IS THE HORIZON; THE RING IS THE CACHE (owner ruling 2026-09-09).
// The BarCache ring holds DefaultBarCacheMaxBars = 2500 per (symbol, timeframe)
// and is rebuilt from a 2000-bar seed on every Go restart, so three call sites
// asked for depth no market condition could supply: 1m × 12000 (200.0 h) twice
// and 5m × 3000 (250.0 h), against ring ceilings of 41.7 h and 208.3 h.
//
// Retention bounds what this can give — RetentionDaysFor(tf): 1m 90d, 3m/5m
// 180d, 15m/30m 365d, 1h and coarser forever. Nothing has ever been pruned, so
// as measured on 2026-09-09 the bars table held MNQ 1m 20,043 rows back to
// 2026-08-19 10:00 CT (21 days).
//
// n <= 0 returns nil. A read error is returned, never swallowed — the caller
// WARNs and degrades to the ring (A10).
func (s *BarHistoryStore) LastNBars(symbol, tf string, n int) ([]BarHistoryDB, error) {
	return s.LastNBarsOn(symbol, tf, "", n)
}

// LastNBarsOn is LastNBars restricted to ONE contract.
//
// ROLL WAVE (2026-09-10). Before the contract column existed this read
// straddled the roll: a request for the last 2000 1m bars at 21:20 CT returned
// ~1,995 September bars and ~5 December ones, and the ~292-point basis between
// them presented to every consumer — ATR, RANGE, regime, the planner's level
// table — as a real move. Filtering to the current contract means depth is
// bounded by how much of THIS contract has been received, which is the truth:
// the retired contract's history is not history of the thing being traded.
//
// Empty contract = unfiltered, for audit tooling only.
func (s *BarHistoryStore) LastNBarsOn(symbol, tf, contract string, n int) ([]BarHistoryDB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	if n <= 0 {
		return nil, nil
	}
	var desc []BarHistoryDB
	q := s.db.Where("symbol = ? AND tf = ? AND COALESCE(source, '') NOT IN (?, ?)", symbol, tf, BarSourceMixed, BarSourceOffScale)
	if c := strings.TrimSpace(contract); c != "" {
		q = q.Where("contract = ?", c)
	}
	if err := q.Order("open_time_ms DESC").Limit(n).Find(&desc).Error; err != nil {
		return nil, err
	}
	// Reverse in place — ASCENDING is the contract every bar reader in this
	// repo relies on (mergeBarsByTime, AggregateBars, the indicator engine).
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}

// CurrentContract returns the contract of the newest non-mixed row for
// (symbol, tf), or "" on a cold store. Read-only.
func (s *BarHistoryStore) CurrentContract(symbol, tf string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("store required")
	}
	var newest BarHistoryDB
	if err := s.db.Where("symbol = ? AND tf = ? AND contract <> '' AND COALESCE(source, '') NOT IN (?, ?)",
		symbol, tf, BarSourceMixed, BarSourceOffScale).
		Order("open_time_ms DESC").Limit(1).Find(&newest).Error; err != nil {
		return "", err
	}
	return newest.Contract, nil
}

// LastNBarsCurrentContract returns the NEWEST n bars for (symbol, tf) on the
// CURRENT contract only — the contract of the newest non-mixed row. One price
// scale across a roll: rows written before the contract column existed carry
// an empty contract and fall outside the filter (a cold store simply has no
// rows). Read-only; never writes. n <= 0 returns nil.
func (s *BarHistoryStore) LastNBarsCurrentContract(symbol, tf string, n int) ([]BarHistoryDB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	if n <= 0 {
		return nil, nil
	}
	var newest BarHistoryDB
	if err := s.db.Where("symbol = ? AND tf = ? AND contract <> '' AND COALESCE(source, '') NOT IN (?, ?)",
		symbol, tf, BarSourceMixed, BarSourceOffScale).
		Order("open_time_ms DESC").Limit(1).Find(&newest).Error; err != nil {
		return nil, err
	}
	if newest.Contract == "" {
		return nil, nil
	}
	return s.LastNBarsOn(symbol, tf, newest.Contract, n)
}

// AllBarsOn returns EVERY stored bar for (symbol, tf, contract) ascending —
// the full-history read for the mentor key-level seed (KEYLEVEL-FULL-HISTORY).
// No N cap: the caller bounds memory by the symbol's own retention. A read
// error is returned, never swallowed.
func (s *BarHistoryStore) AllBarsOn(symbol, tf, contract string) ([]BarHistoryDB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var asc []BarHistoryDB
	if err := s.db.Where("symbol = ? AND tf = ? AND contract = ? AND COALESCE(source, '') NOT IN (?, ?)",
		symbol, tf, contract, BarSourceMixed, BarSourceOffScale).
		Order("open_time_ms ASC").Find(&asc).Error; err != nil {
		return nil, err
	}
	return asc, nil
}

// ContractSpan is one contract's (symbol, tf) coverage: its name and the
// first/last open time it holds.
type ContractSpan struct {
	Contract string
	FirstMs  int64
	LastMs   int64
	N        int64
}

// ContractSpans lists every contract the store holds for (symbol, tf), OLDEST
// first (by first bar), with its first/last open time and row count. The roll
// order is the span order.
func (s *BarHistoryStore) ContractSpans(symbol, tf string) ([]ContractSpan, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var rows []ContractSpan
	err := s.db.Raw(`SELECT contract, MIN(open_time_ms) AS first_ms, MAX(open_time_ms) AS last_ms, COUNT(*) AS n
		FROM bars WHERE symbol = ? AND tf = ? AND contract != '' AND contract != ? AND COALESCE(source,'') NOT IN (?, ?)
		GROUP BY contract ORDER BY MIN(open_time_ms)`, symbol, tf, ContractMixed, BarSourceMixed, BarSourceOffScale).Scan(&rows).Error
	return rows, err
}

// ImportSnapshotTimes returns the open times of historical_import rows for
// (symbol, tf, contract) — the sparse wave-101 pull snapshots. The DISPLAY
// seam (BarsWithStoreDepthDisplay) uses this set to drop the same sparse bars
// when NT8's own BarsRequest seed served them INTO the ring: the store-side
// splice filter only sees rows it read, but the ring carries the snapshots too
// (measured 2026-09-14: MNQ 12-26 1m's first four ring bars were the import
// snapshots at 09-07 17:00 / 09-08·09·10 21:00Z, where NT8's chart renders
// nothing). Empty contract = unfiltered, mirroring LastNBarsOn.
func (s *BarHistoryStore) ImportSnapshotTimes(symbol, tf, contract string) (map[int64]bool, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store required")
	}
	var times []int64
	q := s.db.Table("bars").Where("symbol = ? AND tf = ? AND source = ?", symbol, tf, BarSourceHistoricalImport)
	if c := strings.TrimSpace(contract); c != "" {
		q = q.Where("contract = ?", c)
	}
	if err := q.Pluck("open_time_ms", &times).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(times))
	for _, t := range times {
		out[t] = true
	}
	return out, nil
}
