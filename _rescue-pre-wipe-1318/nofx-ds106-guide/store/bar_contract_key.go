package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"gorm.io/gorm"

	"vl/internal/envcompat"
)

// ── W-BARS-CONTRACT-KEY (2026-09-18) — THE CONTRACT JOINS THE PRIMARY KEY ────
//
// WHAT WAS WRONG. The bars table was keyed (symbol, tf, open_time_ms) with
// `contract` OUTSIDE the key, and both writers resolved a collision on that
// key alone: InsertBars upserted whole rows, ImportBars did nothing. At every
// quarterly roll NT8 serves the NEW contract's history (~2,000 bars per TF at
// subscribe) for minutes the OLD contract already holds, so the new contract's
// overlap was silently dropped wherever the old one had a row and silently
// LANDED wherever it did not (holidays, Sunday evenings, NT8-off windows) —
// the stray-row shape behind CLASS 143's three-day chart hole. A bar store
// keyed without the contract drops the new contract's overlap at every roll.
//
// THE KEY. PRIMARY KEY (symbol, tf, contract, open_time_ms); contract NOT NULL
// with '' for a legacy row that never got one. A secondary index on
// (symbol, tf, open_time_ms) serves the readers that ask by time across
// contracts (the chart's prior-contract reader, the audit tooling). The
// December history rows now land BESIDE the September rows for the same
// minute, each under its own label, and every decision reader — LastNBarsOn,
// BarsBetweenOn, the NT8-only planner doors — already names ONE contract.
//
// THE MIGRATION is a guarded DB write and runs at boot, once, idempotently:
//   (a) detect the old key from pragma_table_info;
//   (b) BACKUP the whole database (VACUUM INTO — SQLite's online, consistent
//       copy) to ~/vl-backups/pre-bars-key-<stamp>.db, verified by row
//       count; a backup that cannot be written REFUSES the migration;
//   (c) CREATE bars_v2 with the new key and INSERT … SELECT every row (no
//       dedupe: the old key guaranteed uniqueness on a subset of the new one);
//   (d) rename bars → bars_pre_contract_key_<date>, bars_v2 → bars;
//   (e) verify COUNT(*) equal and say what happened, with READ values.
// (c)–(e) run in ONE transaction; any error rolls back and the bot keeps
// running on the old key — the writers pick their conflict target from the
// key the table ACTUALLY has (barsKeyed), never from what this file intends.
//
// THE OLD TABLE KEEPS ITS INDEXES (idx_bars_sym_tf_time_unique among them).
// SQLite carries an index with its table through a rename, so the backup table
// stays exactly what a pre-migration binary expects; renaming it back is a
// clean rollback and that binary's own Migrate finds its unique index and does
// NOTHING. Had the indexes been dropped, that binary would find no unique
// index, run its 2026-08-27 dedupe block, and DELETE every tf <> '1m' row.
// The new table's indexes therefore carry NEW names (idx_bars_v2_*).
//
// A PRE-MIGRATION BINARY MUST NEVER BOOT ON THE MIGRATED TABLE: its upsert
// names ON CONFLICT(symbol, tf, open_time_ms), which is no longer a unique
// constraint (SQLite refuses the statement — persistence dies), and its
// Migrate runs the destructive dedupe block described above. deploy/RESTORE.md
// carries the rename-back and the restore-from-backup runbooks.

// barsKeyColumns is the new key, in order.
var barsKeyColumns = []string{"symbol", "tf", "contract", "open_time_ms"}

// barsKeyLegacyColumns is the key every row was written under before this wave.
var barsKeyLegacyColumns = []string{"symbol", "tf", "open_time_ms"}

// barsCreateDDL is the exact sqlite shape of a bars table on the contract key.
// Non-key columns stay nullable TEXT/REAL exactly as GORM created them, so
// AutoMigrate's nullable/default checks (which skip primary-key fields) see
// nothing to alter and never trigger the sqlite driver's table recreate.
const barsCreateDDL = "CREATE TABLE IF NOT EXISTS `%s` (" +
	"`symbol` text NOT NULL, `tf` text NOT NULL, `contract` text NOT NULL DEFAULT '', `open_time_ms` integer NOT NULL, " +
	"`o` real, `h` real, `l` real, `c` real, `v` real, `convention` text, `source` text, " +
	"PRIMARY KEY (`symbol`, `tf`, `contract`, `open_time_ms`))"

// Index names on the NEW table. Distinct from the legacy names on purpose (see
// the file comment: the renamed old table keeps the legacy names).
const (
	idxBarsV2Time   = "idx_bars_v2_time"   // (symbol, tf, open_time_ms) — time-across-contracts readers
	idxBarsV2Source = "idx_bars_v2_source" // (symbol, tf, source, open_time_ms)
)

// Legacy index names (on the pre-wave table shape).
const (
	idxBarsLegacyUnique   = "idx_bars_sym_tf_time_unique"
	idxBarsLegacyTime     = "idx_bars_sym_tf_time"
	idxBarsLegacyContract = "idx_bars_contract"
	idxBarsLegacySource   = "idx_bars_source"
)

// barsPreKeyTablePrefix names the renamed old table: bars_pre_contract_key_<date>.
const barsPreKeyTablePrefix = "bars_pre_contract_key_"

// barsKeyed is the key the bars table ACTUALLY has, as read by Migrate. The
// writers build their ON CONFLICT target from it, so a migration that failed
// open leaves a working writer on the old key rather than a statement SQLite
// refuses. Process-wide because the store is (one bars table per process).
var barsKeyed atomic.Bool

// BarsKeyHasContract reports whether the bars table this process runs on is
// keyed with the contract — read from the table at Migrate, never assumed.
func BarsKeyHasContract() bool { return barsKeyed.Load() }

// barsConflictTarget is the ON CONFLICT column list matching the live key.
func barsConflictTarget() string {
	if barsKeyed.Load() {
		return strings.Join(barsKeyColumns, ", ")
	}
	return strings.Join(barsKeyLegacyColumns, ", ")
}

// barsPKColumns reads the primary-key columns of a table, in key order.
func (s *BarHistoryStore) barsPKColumns(table string) ([]string, error) {
	type col struct {
		Name string
		PK   int
	}
	var cols []col
	if err := s.db.Raw("SELECT name, pk FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk", table).Scan(&cols).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out, nil
}

// barsTableExists reports whether a table of that name exists.
func (s *BarHistoryStore) barsTableExists(name string) (bool, error) {
	var n int64
	if err := s.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// barsIndexOnTable reports whether an index of that name exists ON THAT TABLE.
// Index names are global in SQLite, and after the rename the legacy names
// belong to the backup table — a name-only check would answer for the wrong
// table.
func (s *BarHistoryStore) barsIndexOnTable(index, table string) (bool, error) {
	var n int64
	if err := s.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=? AND tbl_name=?", index, table).Scan(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func sameColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// barsKeyState reads which key the bars table has. keyed=true is the contract
// key; keyed=false with legacy=true is the pre-wave key; anything else is a
// shape this code does not know and refuses to migrate.
func (s *BarHistoryStore) barsKeyState() (keyed, legacy bool, err error) {
	pk, err := s.barsPKColumns("bars")
	if err != nil {
		return false, false, err
	}
	return sameColumns(pk, barsKeyColumns), sameColumns(pk, barsKeyLegacyColumns), nil
}

// BarsKeyReport is what one boot's key migration DID, with every value read
// from the database afterwards — the boot line prints it, never a literal.
type BarsKeyReport struct {
	// Migrated is true when THIS boot moved the table onto the contract key.
	Migrated bool
	// AlreadyKeyed is true when the table was found on the contract key already.
	AlreadyKeyed bool
	// Rows is COUNT(*) of the new table after the copy (Migrated) or of the
	// table as found (AlreadyKeyed).
	Rows int64
	// Backup is the path of the whole-database backup taken before the copy.
	Backup string
	// OldTable is the renamed pre-migration table (Migrated), or the newest
	// such table found in sqlite_master (AlreadyKeyed; "" on a fresh database).
	OldTable string
	// MigratedOn is the date in OldTable's name, "" on a fresh database.
	MigratedOn string
	// Err is the failure that left the table on the old key (fail-open).
	Err error
}

// BootLine renders the report as the 🗄 line the owner reads at boot.
func (r BarsKeyReport) BootLine() string {
	switch {
	case r.Err != nil:
		return fmt.Sprintf("🗄 bars: migration FAILED — %v; old table intact (bot runs on the legacy key (symbol,tf,open_time_ms); the new contract's roll overlap is still dropped)", r.Err)
	case r.Migrated:
		return fmt.Sprintf("🗄 bars: key migrated to (symbol,tf,contract,open_time_ms) — rows=%d backup=%s old_table=%s", r.Rows, r.Backup, r.OldTable)
	case r.AlreadyKeyed && r.MigratedOn != "":
		return fmt.Sprintf("🗄 bars: key=(symbol,tf,contract,open_time_ms) (migrated %s) rows=%d", r.MigratedOn, r.Rows)
	case r.AlreadyKeyed:
		return fmt.Sprintf("🗄 bars: key=(symbol,tf,contract,open_time_ms) (created on this key) rows=%d", r.Rows)
	default:
		return "🗄 bars: key state unknown"
	}
}

// barsKeyBackupDir is where the pre-migration backup goes: BARS_KEY_BACKUP_DIR
// if set (tests and failure injection), else envcompat.BackupRoot() — the
// vl-backups tree, or nofx-backups only while ~/nofx exists (R5 removes).
func barsKeyBackupDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("BARS_KEY_BACKUP_DIR")); v != "" {
		return v, nil // BARS_KEY_BACKUP_DIR still wins: an explicit dir is explicit
	}
	if root := envcompat.BackupRoot(); root != "" {
		return root, nil
	}
	return "", fmt.Errorf("resolve home for the backup dir")
}

// barsKeyRequiredFree is the free space the backup dir must have before
// VACUUM INTO runs, as a function of the database's size (review fix 2:
// 2× the file — the copy plus headroom). A test overrides it to inject a
// limit no disk can meet.
var barsKeyRequiredFree = func(dbBytes int64) int64 { return 2 * dbBytes }

// barsKeyFreeBytes reports the free space available to this user on the
// filesystem holding dir.
func barsKeyFreeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// backupBeforeKeyMigration writes a consistent whole-database copy with
// VACUUM INTO (SQLite's online backup as a statement: a read transaction on
// the source, nothing locked for writers) and verifies it by comparing the
// bars row count of the copy with the source. Any failure is returned and
// the migration is refused — a schema change with no backup is not a
// guarded write.
//
// REVIEW FIX 1 (2026-09-18): the whole sequence — VACUUM INTO, ATTACH, COUNT,
// DETACH — runs on ONE *sql.Conn. ATTACH is connection-scoped, and the gorm
// pool (MaxOpenConns 4) hands connections out per call: under live API
// traffic the COUNT could land on a connection that never saw the ATTACH
// ("no such table: barskey_backup.bars") and refuse a good backup. VACUUM
// INTO is not allowed inside a transaction, so a dedicated connection is
// the pin, not a Transaction.
func (s *BarHistoryStore) backupBeforeKeyMigration(now time.Time) (path string, srcRows int64, err error) {
	dir, err := barsKeyBackupDir()
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("backup dir %s: %w", dir, err)
	}
	path = filepath.Join(dir, "pre-bars-key-"+now.Format("20060102-150405")+".db")
	if _, err := os.Stat(path); err == nil {
		return "", 0, fmt.Errorf("backup %s already exists — refusing to overwrite", path)
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return "", 0, fmt.Errorf("sql.DB: %w", err)
	}
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("dedicated connection for the backup: %w", err)
	}
	defer conn.Close()
	// REVIEW FIX 2: disk-space precheck — the copy needs the database's size
	// again, plus headroom; a backup that would run the disk out is refused
	// before a byte is written.
	var pageCount, pageSize int64
	if err := conn.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return "", 0, fmt.Errorf("page_count: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return "", 0, fmt.Errorf("page_size: %w", err)
	}
	dbBytes := pageCount * pageSize
	free, err := barsKeyFreeBytes(dir)
	if err != nil {
		return "", 0, fmt.Errorf("free space on %s: %w", dir, err)
	}
	if need := barsKeyRequiredFree(dbBytes); free < need {
		return "", 0, fmt.Errorf("insufficient disk for the backup: %s has %d bytes free, the database is %d bytes and the backup needs %d (2× the file)", dir, free, dbBytes, need)
	}
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM bars").Scan(&srcRows); err != nil {
		return "", 0, fmt.Errorf("count source rows: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path) // never leave a partial copy that looks complete
		return "", 0, fmt.Errorf("VACUUM INTO %s: %w", path, err)
	}
	copyRows, err := verifyBackupOnConn(ctx, conn, path)
	if err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	if copyRows != srcRows {
		_ = os.Remove(path)
		return "", 0, fmt.Errorf("backup verification: copy holds %d bars rows, source %d", copyRows, srcRows)
	}
	return path, srcRows, nil
}

// barsBackupConn is the subset of *sql.Conn the verification uses — ONE
// connection, so ATTACH is visible to the COUNT that follows it.
type barsBackupConn interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// verifyBackupOnConn attaches the copy ON THE GIVEN CONNECTION, counts its
// bars rows there, and detaches — three statements that only make sense
// together on one connection.
func verifyBackupOnConn(ctx context.Context, conn barsBackupConn, path string) (int64, error) {
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS barskey_backup", path); err != nil {
		return 0, fmt.Errorf("attach backup for verification: %w", err)
	}
	var copyRows int64
	verr := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM barskey_backup.bars").Scan(&copyRows)
	if _, derr := conn.ExecContext(ctx, "DETACH DATABASE barskey_backup"); derr != nil && verr == nil {
		verr = derr
	}
	if verr != nil {
		return 0, fmt.Errorf("verify backup: %w", verr)
	}
	return copyRows, nil
}

// migrateContractKey moves a legacy-keyed bars table onto the contract key.
// Idempotent: a table already on the contract key is reported and left alone.
// Fail-open: every error is returned IN the report (Err) after the transaction
// rolled back; the caller keeps running on the old key.
func (s *BarHistoryStore) migrateContractKey(now time.Time) BarsKeyReport {
	var rep BarsKeyReport
	keyed, legacy, err := s.barsKeyState()
	if err != nil {
		rep.Err = fmt.Errorf("read bars key: %w", err)
		return rep
	}
	if keyed {
		rep.AlreadyKeyed = true
		_ = s.db.Raw("SELECT COUNT(*) FROM bars").Scan(&rep.Rows).Error
		var old string
		_ = s.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE ? ORDER BY name DESC LIMIT 1", barsPreKeyTablePrefix+"%").Scan(&old).Error
		rep.OldTable = old
		rep.MigratedOn = strings.TrimPrefix(old, barsPreKeyTablePrefix)
		return rep
	}
	if !legacy {
		pk, _ := s.barsPKColumns("bars")
		rep.Err = fmt.Errorf("bars has an unknown primary key %v — neither the legacy (symbol,tf,open_time_ms) nor the contract key; refusing to touch it", pk)
		return rep
	}
	oldTable := barsPreKeyTablePrefix + now.Format("2006-01-02")
	if exists, err := s.barsTableExists(oldTable); err != nil {
		rep.Err = err
		return rep
	} else if exists {
		rep.Err = fmt.Errorf("%s already exists — a migration ran today and was rolled back by renaming? resolve by hand (deploy/RESTORE.md)", oldTable)
		return rep
	}
	if exists, err := s.barsTableExists("bars_v2"); err != nil {
		rep.Err = err
		return rep
	} else if exists {
		rep.Err = fmt.Errorf("bars_v2 already exists — an earlier attempt left it behind; resolve by hand (deploy/RESTORE.md)")
		return rep
	}
	backup, srcRows, err := s.backupBeforeKeyMigration(now)
	if err != nil {
		rep.Err = fmt.Errorf("backup refused: %w", err)
		return rep
	}
	rep.Backup = backup
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var n0 int64
		if err := tx.Raw("SELECT COUNT(*) FROM bars").Scan(&n0).Error; err != nil {
			return err
		}
		if n0 != srcRows {
			return fmt.Errorf("bars changed under the migration: %d rows at backup, %d now", srcRows, n0)
		}
		if err := tx.Exec(fmt.Sprintf(barsCreateDDL, "bars_v2")).Error; err != nil {
			return fmt.Errorf("create bars_v2: %w", err)
		}
		if err := tx.Exec("INSERT INTO bars_v2 (symbol, tf, contract, open_time_ms, o, h, l, c, v, convention, source) " +
			"SELECT symbol, tf, COALESCE(contract, ''), open_time_ms, o, h, l, c, v, convention, source FROM bars").Error; err != nil {
			return fmt.Errorf("copy rows: %w", err)
		}
		var n1 int64
		if err := tx.Raw("SELECT COUNT(*) FROM bars_v2").Scan(&n1).Error; err != nil {
			return err
		}
		if n1 != n0 {
			return fmt.Errorf("row count after copy %d != before %d", n1, n0)
		}
		if err := tx.Exec("ALTER TABLE bars RENAME TO `" + oldTable + "`").Error; err != nil {
			return fmt.Errorf("rename bars → %s: %w", oldTable, err)
		}
		if err := tx.Exec("ALTER TABLE bars_v2 RENAME TO bars").Error; err != nil {
			return fmt.Errorf("rename bars_v2 → bars: %w", err)
		}
		if err := s.ensureKeyedIndexes(tx); err != nil {
			return err
		}
		var n2 int64
		if err := tx.Raw("SELECT COUNT(*) FROM bars").Scan(&n2).Error; err != nil {
			return err
		}
		if n2 != n0 {
			return fmt.Errorf("row count after rename %d != before %d", n2, n0)
		}
		rep.Rows = n2
		return nil
	})
	if err != nil {
		rep.Err = err
		rep.Rows = 0
		return rep
	}
	rep.Migrated = true
	rep.OldTable = oldTable
	rep.MigratedOn = strings.TrimPrefix(oldTable, barsPreKeyTablePrefix)
	return rep
}

// ensureKeyedIndexes creates the new table's secondary indexes (idempotent).
// Index names are global: a name already held by ANOTHER table (a rollback's
// renamed copy that kept them) would make CREATE INDEX IF NOT EXISTS a silent
// no-op and leave bars unindexed — so that case is an error naming the table.
func (s *BarHistoryStore) ensureKeyedIndexes(tx *gorm.DB) error {
	for _, ix := range []struct{ name, ddl string }{
		{idxBarsV2Time, "CREATE INDEX IF NOT EXISTS " + idxBarsV2Time + " ON bars(symbol, tf, open_time_ms)"},
		{idxBarsV2Source, "CREATE INDEX IF NOT EXISTS " + idxBarsV2Source + " ON bars(symbol, tf, source, open_time_ms)"},
	} {
		var holder string
		if err := tx.Raw("SELECT tbl_name FROM sqlite_master WHERE type='index' AND name=?", ix.name).Scan(&holder).Error; err != nil {
			return err
		}
		if holder != "" && holder != "bars" {
			return fmt.Errorf("index %s already belongs to table %s — drop it there first (deploy/bars-key-rollback.sh does)", ix.name, holder)
		}
		if err := tx.Exec(ix.ddl).Error; err != nil {
			return fmt.Errorf("index %s: %w", ix.name, err)
		}
	}
	return nil
}

// ensureLegacyIndexes is the pre-wave index set, kept for a table still on
// the legacy key (a migration that failed open). Unchanged from the roll and
// bar-source waves, only moved here so the two shapes sit side by side.
func (s *BarHistoryStore) ensureLegacyIndexes() error {
	if err := s.db.Exec("DROP INDEX IF EXISTS " + idxBarsLegacyTime).Error; err != nil {
		return err
	}
	if err := s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS " + idxBarsLegacyUnique + " ON bars(symbol, tf, open_time_ms)").Error; err != nil {
		return err
	}
	if err := s.db.Exec("CREATE INDEX IF NOT EXISTS " + idxBarsLegacyContract + " ON bars(symbol, tf, contract, open_time_ms)").Error; err != nil {
		return err
	}
	return s.db.Exec("CREATE INDEX IF NOT EXISTS " + idxBarsLegacySource + " ON bars(symbol, tf, source, open_time_ms)").Error
}

// RollOverlaps counts the (symbol, tf, open_time_ms) slots held by MORE THAN
// ONE contract — the rows the old key used to drop. Expected non-zero after a
// roll (the new contract's served history over the old contract's minutes);
// always zero on the legacy key by construction. The nightly integrity line
// prints it, read.
func (s *BarHistoryStore) RollOverlaps() (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store required")
	}
	var n int64
	err := s.db.Raw("SELECT COUNT(*) FROM (SELECT symbol, tf, open_time_ms FROM bars GROUP BY symbol, tf, open_time_ms HAVING COUNT(DISTINCT contract) > 1)").Scan(&n).Error
	return n, err
}

// ── contract tie-breaks for the time-only "which contract" readers ─────────
//
// LatestContract / ContractAt ask "the contract of the newest usable 1m bar
// (at or before t)". On the contract key two contracts can hold the SAME
// newest minute (the old contract's live bar and the new contract's served
// history), and ORDER BY open_time_ms DESC LIMIT 1 would pick one at random.
// The rule: the row that TRADED wins (live, then mixed, then replay, then
// import); among equal sources the later expiry, parsed from the broker's own
// label ("MNQ 12-26" → 2026-12). This orders two labels the broker gave us;
// it never decides when a roll happens.

type contractRow struct {
	Contract string
	Source   string
}

// contractExpiry parses "<root> MM-YY" into (year, month). ok=false for any
// other shape (the label is then ordered lexically as a last resort).
func contractExpiry(label string) (year, month int, ok bool) {
	f := strings.Fields(strings.TrimSpace(label))
	if len(f) < 2 {
		return 0, 0, false
	}
	mmyy := f[len(f)-1]
	parts := strings.Split(mmyy, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	m, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || m < 1 || m > 12 {
		return 0, 0, false
	}
	return 2000 + y, m, true
}

// contractNewer reports whether a expires after b.
func contractNewer(a, b string) bool {
	ay, am, aok := contractExpiry(a)
	by, bm, bok := contractExpiry(b)
	if aok && bok {
		if ay != by {
			return ay > by
		}
		return am > bm
	}
	return a > b
}

// preferContract picks the winner among rows that share one open time.
func preferContract(rows []contractRow) (string, bool) {
	best, ok := contractRow{}, false
	for _, r := range rows {
		if !IsUsableContract(r.Contract) {
			continue
		}
		if !ok {
			best, ok = r, true
			continue
		}
		ra, rb := priorSourceRank(r.Source), priorSourceRank(best.Source)
		if ra < rb || (ra == rb && contractNewer(r.Contract, best.Contract)) {
			best = r
		}
	}
	return best.Contract, ok
}
