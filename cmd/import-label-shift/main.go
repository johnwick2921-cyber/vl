// Command import-label-shift re-stamps the OLD historical_import bars from the
// bar's CLOSE to the bar's OPEN (m460, 2026-10-30).
//
// THE DEFECT. NT8's Bars.GetTime(i) is the bar's CLOSE. The bars table is keyed
// by the bar's OPEN. The wave-101 importer stored the AddOn's stamp raw, so
// every imported row of 2022-2026 is labelled one full timeframe late (1m
// volume peaks at label 08:31 CT, live at 08:30; the 5m label 08:30 is the sum
// of the 1m labels 08:26..08:30). The importer is fixed (it open-stamps and
// writes Convention=epoch_floor_open); this tool repairs the rows already on
// disk. It is NOT wired to boot and NEVER runs implicitly.
//
// WHAT IT TOUCHES. Exactly the rows with source=historical_import AND
// convention=epoch_floor (the old, close-stamped ones), for the chosen
// timeframes (default 1m,5m,15m,1h: the imported 5m/15m/1h are close-stamped
// from the same pull, and leaving them behind would desynchronise them from the
// shifted 1m). Each moves back by ITS OWN timeframe (1m -60 s, 5m -300 s, ...)
// and is re-tagged convention=epoch_floor_open. That tag is the per-row
// idempotency guard: a second run finds nothing, and a row the fixed importer
// wrote can never be shifted.
//
// COLLISIONS. If the target stamp already holds a row that is not itself
// moving (a live or historical row for the same contract, as at 10 minutes on
// 09-26 and 12-26), the live tape wins, as everywhere else in the store: the
// import row is COPIED to bars_import_shift_conflicts (full row, why, run id)
// and PARKED in place by UPDATE (contract=ContractMixed, source=
// historical_import:shift-conflict), where every contract-scoped reader skips
// it. Nothing is deleted; the dry-run report counts them first and says whether
// each is an identical duplicate of the row that stays.
//
// NEVER DELETE / DROP / TRUNCATE (CLAUDE.md DB rules). The shift is UPDATE only,
// in two phases inside one transaction per contract: every moving row first goes
// to a far, collision-free key range, then lands on its final key. Row counts
// are asserted equal before and after, or the transaction rolls back.
//
// PROCEDURE (owner/CTO, under the main-tree lock):
//
//  1. go run ./cmd/import-label-shift -db data/data.db                 (dry-run, read-only)
//  2. stop vl.service          (the tool REFUSES while it runs; -allow-service-running overrides)
//  3. go run ./cmd/import-label-shift -db data/data.db -apply          (backup first, then shift)
//  4. start vl.service
//
// -apply copies the database to <backup-root>/<timestamp>/ (default
// ~/vl-backups) with VACUUM INTO and VERIFIES the copy's row count before the
// first write; it refuses to write if that fails.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vl/store"
	"vl/store/sqlitedriver"
)

const (
	shiftVersion = "m460-open-stamp-v1"
	// legacyConvention is what every close-stamped import row carries.
	legacyConvention = "epoch_floor"
	// parkedSource labels a parked conflict row (never readable).
	parkedSource = "historical_import:shift-conflict"
	// phaseGap is the far key range phase 1 parks moving rows in (2^52 ms,
	// ~142 000 years): no real stamp, no parked (negative-free) key reaches it.
	phaseGap = int64(1) << 52
)

var tfDurMs = map[string]int64{"1m": 60_000, "5m": 300_000, "15m": 900_000, "1h": 3_600_000}

// serviceActive reports whether the service must be treated as RUNNING (true =
// refuse to write). It is a var so tests can stand in for the whole guard.
// FAIL CLOSED: only a positively "stopped" answer from systemctl clears the
// guard. "activating", "reloading", "deactivating", an empty answer, a systemctl
// error (no dbus / no systemd) and a missing binary all count as running.
var serviceActive = func(unit string) bool {
	out, err := exec.Command("systemctl", "is-active", unit).Output()
	return serviceMustBeTreatedAsRunning(string(out), err)
}

// serviceMustBeTreatedAsRunning classifies one `systemctl is-active` answer.
// is-active exits non-zero (3, or 4 for an unknown unit) for the stopped states
// while still printing the state, so the exit code alone cannot be the signal:
// the printed state is, and it must be one of the three stopped words.
func serviceMustBeTreatedAsRunning(stdout string, runErr error) bool {
	switch strings.TrimSpace(stdout) {
	case "inactive", "failed", "unknown":
		var ee *exec.ExitError
		// a stopped word with a clean exit never happens in practice, but is
		// harmless; a stopped word with systemctl's own non-zero exit is the
		// normal case. Anything else (exec failure, signal) is not an answer.
		if runErr == nil || errors.As(runErr, &ee) {
			return false
		}
	}
	return true
}

var nowFn = time.Now

type config struct {
	db, symbol, contract, tfs, unit, backupRoot string
	apply, allowRunning                         bool
}

func main() {
	var c config
	flag.StringVar(&c.db, "db", "data/data.db", "path to the sqlite database")
	flag.StringVar(&c.symbol, "symbol", "MNQ", "symbol")
	flag.StringVar(&c.contract, "contract", "", "limit to one contract (default: all)")
	flag.StringVar(&c.tfs, "tfs", "1m,5m,15m,1h", "timeframes to re-stamp (each shifts by its own length)")
	flag.StringVar(&c.unit, "service", "vl.service", "systemd unit that must not be running while writing")
	flag.StringVar(&c.backupRoot, "backup-root", "", "backup parent dir (default ~/vl-backups)")
	flag.BoolVar(&c.apply, "apply", false, "write (default: dry-run report, read-only)")
	flag.BoolVar(&c.allowRunning, "allow-service-running", false, "write even though the service is active (UNSAFE: blocks live bar writes)")
	flag.Parse()
	if err := run(c, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "import-label-shift: %v\n", err)
		os.Exit(1)
	}
}

type group struct {
	Contract, TF string
	N            int64
	First, Last  int64
	Dur          int64
}

type conflict struct {
	Contract, TF   string
	Rowid, OpenMs  int64
	ExistingRowid  int64
	ExistingSource string
	ExistingConv   string
	Identical      bool
}

func ct(ms int64) string {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
	}
	return time.UnixMilli(ms).In(loc).Format("2006-01-02 15:04 MST")
}

func openDB(path string, readOnly bool) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	db, err := sqlitedriver.Open("file:" + abs + "?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // pragmas and BEGIN IMMEDIATE stick to ONE connection
	if readOnly {
		if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func parseTFs(s string) ([]string, error) {
	var out []string
	for _, tf := range strings.Split(s, ",") {
		tf = strings.TrimSpace(tf)
		if tf == "" {
			continue
		}
		if _, ok := tfDurMs[tf]; !ok {
			return nil, fmt.Errorf("timeframe %q is not one the importer pulls (1m,5m,15m,1h)", tf)
		}
		out = append(out, tf)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no timeframes")
	}
	return out, nil
}

func eligibleWhere(alias string) string {
	p := ""
	if alias != "" {
		p = alias + "."
	}
	return fmt.Sprintf("%ssymbol=? AND %stf=? AND %scontract=? AND %ssource='%s' AND %sconvention='%s'",
		p, p, p, p, store.BarSourceHistoricalImport, p, legacyConvention)
}

func groups(db *sql.DB, c config, tfs []string) ([]group, error) {
	q := fmt.Sprintf(`SELECT contract, tf, COUNT(*), MIN(open_time_ms), MAX(open_time_ms) FROM bars
		WHERE symbol=? AND source=? AND convention=? AND tf IN (%s)`, placeholders(len(tfs)))
	args := []any{c.symbol, store.BarSourceHistoricalImport, legacyConvention}
	for _, tf := range tfs {
		args = append(args, tf)
	}
	if c.contract != "" {
		q += " AND contract=?"
		args = append(args, c.contract)
	}
	q += " GROUP BY contract, tf ORDER BY contract, tf"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.Contract, &g.TF, &g.N, &g.First, &g.Last); err != nil {
			return nil, err
		}
		g.Dur = tfDurMs[g.TF]
		out = append(out, g)
	}
	return out, rows.Err()
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

const conflictSelect = `SELECT a.rowid, a.open_time_ms, b.rowid, COALESCE(b.source,''), COALESCE(b.convention,''),
	(a.o IS b.o AND a.h IS b.h AND a.l IS b.l AND a.c IS b.c AND a.v IS b.v)
	FROM bars a JOIN bars b ON b.symbol=a.symbol AND b.tf=a.tf AND b.contract=a.contract AND b.open_time_ms=a.open_time_ms-?
	WHERE %s AND NOT (b.source='` + store.BarSourceHistoricalImport + `' AND b.convention='` + legacyConvention + `')
	ORDER BY a.open_time_ms`

func conflicts(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, symbol string, g group) ([]conflict, error) {
	rows, err := q.Query(fmt.Sprintf(conflictSelect, eligibleWhere("a")), g.Dur, symbol, g.TF, g.Contract)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []conflict
	for rows.Next() {
		cf := conflict{Contract: g.Contract, TF: g.TF}
		var ident int
		if err := rows.Scan(&cf.Rowid, &cf.OpenMs, &cf.ExistingRowid, &cf.ExistingSource, &cf.ExistingConv, &ident); err != nil {
			return nil, err
		}
		cf.Identical = ident == 1
		out = append(out, cf)
	}
	return out, rows.Err()
}

// alignment probes up to n 5m rows of a contract against the 1m rows of the
// SAME import: how many equal the 1m labels [t-4..t] (close-stamped) and how
// many [t..t+4] (open-stamped). Read-only; n==0 or no 5m/1m rows -> (0,0,0).
func alignment(db *sql.DB, symbol, contract string, n int) (tested, closeDom, openDom int) {
	rows, err := db.Query(`SELECT open_time_ms, h, l, v FROM bars WHERE symbol=? AND tf='5m' AND contract=? AND source=?
		ORDER BY open_time_ms LIMIT ? OFFSET (SELECT COUNT(*)/2 FROM bars WHERE symbol=? AND tf='5m' AND contract=? AND source=?)`,
		symbol, contract, store.BarSourceHistoricalImport, n, symbol, contract, store.BarSourceHistoricalImport)
	if err != nil {
		return
	}
	type r5 struct {
		t       int64
		h, l, v float64
	}
	var sample []r5
	for rows.Next() {
		var x r5
		if rows.Scan(&x.t, &x.h, &x.l, &x.v) == nil {
			sample = append(sample, x)
		}
	}
	rows.Close()
	agg := func(a, b int64) (cnt int, v, h, l float64) {
		_ = db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(v),0), COALESCE(MAX(h),0), COALESCE(MIN(l),0) FROM bars
			WHERE symbol=? AND tf='1m' AND contract=? AND source=? AND open_time_ms>=? AND open_time_ms<=?`,
			symbol, contract, store.BarSourceHistoricalImport, a, b).Scan(&cnt, &v, &h, &l)
		return
	}
	for _, x := range sample {
		tested++
		if c, v, h, l := agg(x.t-4*60_000, x.t); c == 5 && v == x.v && h == x.h && l == x.l {
			closeDom++
		}
		if c, v, h, l := agg(x.t, x.t+4*60_000); c == 5 && v == x.v && h == x.h && l == x.l {
			openDom++
		}
	}
	return
}

func run(c config, out io.Writer) error {
	tfs, err := parseTFs(c.tfs)
	if err != nil {
		return err
	}
	if _, err := os.Stat(c.db); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if c.apply && !c.allowRunning && serviceActive(c.unit) {
		return fmt.Errorf("%s is ACTIVE or its state could not be read (fail-closed): the shift holds the write lock for minutes and would starve the live bar writer. Stop it first, or pass -allow-service-running (UNSAFE)", c.unit)
	}
	db, err := openDB(c.db, !c.apply)
	if err != nil {
		return err
	}
	defer db.Close()

	gs, err := groups(db, c, tfs)
	if err != nil {
		return fmt.Errorf("list groups: %w", err)
	}
	mode := "DRY-RUN (read-only)"
	if c.apply {
		mode = "APPLY"
	}
	fmt.Fprintf(out, "import-label-shift %s  version=%s  db=%s  symbol=%s  tfs=%s\n", mode, shiftVersion, c.db, c.symbol, c.tfs)
	if len(gs) == 0 {
		fmt.Fprintln(out, "nothing to do: no historical_import rows with convention=epoch_floor in scope (already shifted, or none imported).")
		return nil
	}

	var totalRows, totalConf int64
	allConf := map[string][]conflict{}
	fmt.Fprintf(out, "\n%-12s %-4s %9s %7s  %-22s %-22s  samples (rowid open_ms -> new_open_ms)\n", "contract", "tf", "rows", "shift", "first (CT)", "last (CT)")
	for _, g := range gs {
		cf, err := conflicts(db, c.symbol, g)
		if err != nil {
			return fmt.Errorf("conflicts %s %s: %w", g.Contract, g.TF, err)
		}
		allConf[g.Contract+"|"+g.TF] = cf
		totalRows += g.N
		totalConf += int64(len(cf))
		fmt.Fprintf(out, "%-12s %-4s %9d %6ds  %-22s %-22s", g.Contract, g.TF, g.N, g.Dur/1000, ct(g.First), ct(g.Last))
		for _, pick := range []struct{ name, ord string }{{"first", "ASC"}, {"last", "DESC"}} {
			var rowid, ms int64
			if err := db.QueryRow(fmt.Sprintf(`SELECT rowid, open_time_ms FROM bars WHERE %s ORDER BY open_time_ms %s LIMIT 1`, eligibleWhere(""), pick.ord),
				c.symbol, g.TF, g.Contract).Scan(&rowid, &ms); err == nil {
				fmt.Fprintf(out, "  %s: #%d %d -> %d", pick.name, rowid, ms, ms-g.Dur)
			}
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "\nTOTAL rows to shift: %d   target-occupied conflicts (park + quarantine): %d\n", totalRows, totalConf)
	for _, g := range gs {
		cf := allConf[g.Contract+"|"+g.TF]
		if len(cf) == 0 {
			continue
		}
		ident := 0
		for _, x := range cf {
			if x.Identical {
				ident++
			}
		}
		fmt.Fprintf(out, "  conflicts %s %s: %d (%d identical OHLCV duplicates of the row that stays)\n", g.Contract, g.TF, len(cf), ident)
		for i, x := range cf {
			if i >= 20 {
				fmt.Fprintf(out, "    ... %d more\n", len(cf)-i)
				break
			}
			fmt.Fprintf(out, "    import #%d @%d (%s) would land on #%d [%s/%s] identical=%v\n", x.Rowid, x.OpenMs, ct(x.OpenMs), x.ExistingRowid, x.ExistingSource, x.ExistingConv, x.Identical)
		}
	}
	// ALIGNMENT PROOF, BEFORE: the imported 5m against its own 1m.
	seen := map[string]bool{}
	for _, g := range gs {
		if g.TF != "5m" || seen[g.Contract] {
			continue
		}
		seen[g.Contract] = true
		t, cd, od := alignment(db, c.symbol, g.Contract, 200)
		fmt.Fprintf(out, "alignment BEFORE %s: %d sampled 5m rows -> equal to 1m labels [t-4..t] (close-stamped) %d, [t..t+4] (open-stamped) %d\n", g.Contract, t, cd, od)
	}
	// Other imported timeframes that are NOT covered by -tfs.
	var other int64
	_ = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM bars WHERE symbol=? AND source=? AND convention=? AND tf NOT IN (%s)`, placeholders(len(tfs))),
		append([]any{c.symbol, store.BarSourceHistoricalImport, legacyConvention}, tfsAny(tfs)...)...).Scan(&other)
	if other > 0 {
		fmt.Fprintf(out, "WARNING: %d close-stamped imported rows in timeframes outside -tfs are NOT covered by this run.\n", other)
	}
	if !c.apply {
		fmt.Fprintln(out, "\nDRY-RUN: no writes. Stop vl.service, then add -apply (backup is taken first).")
		return nil
	}

	// ---- APPLY ----
	var before int64
	if err := db.QueryRow("SELECT COUNT(*) FROM bars").Scan(&before); err != nil {
		return err
	}
	backup, err := backupDB(db, c, before, out)
	if err != nil {
		return fmt.Errorf("backup failed, NOT writing: %w", err)
	}
	runID := nowFn().UTC().Format("20060102T150405Z")
	if err := ensureTables(db); err != nil {
		return fmt.Errorf("create bookkeeping tables: %w", err)
	}
	byContract := map[string][]group{}
	var order []string
	for _, g := range gs {
		if _, ok := byContract[g.Contract]; !ok {
			order = append(order, g.Contract)
		}
		byContract[g.Contract] = append(byContract[g.Contract], g)
	}
	var shifted, parked int64
	for _, contract := range order {
		s, p, err := applyContract(db, c, runID, backup, byContract[contract])
		if err != nil {
			return fmt.Errorf("contract %s rolled back (earlier contracts stay shifted; re-run is safe): %w", contract, err)
		}
		shifted += s
		parked += p
		fmt.Fprintf(out, "  %s: shifted %d, parked %d  (committed)\n", contract, s, p)
	}
	var after int64
	_ = db.QueryRow("SELECT COUNT(*) FROM bars").Scan(&after)
	fmt.Fprintf(out, "\nDONE run=%s shifted=%d parked=%d  bars rows before=%d after=%d (must be equal)  backup=%s\n", runID, shifted, parked, before, after, backup)
	if before != after {
		return fmt.Errorf("row count changed %d -> %d", before, after)
	}
	seen = map[string]bool{}
	for _, g := range gs {
		if g.TF != "5m" || seen[g.Contract] {
			continue
		}
		seen[g.Contract] = true
		t, cd, od := alignment(db, c.symbol, g.Contract, 200)
		fmt.Fprintf(out, "alignment AFTER  %s: %d sampled 5m rows -> close-stamped %d, open-stamped %d\n", g.Contract, t, cd, od)
	}
	return nil
}

func tfsAny(tfs []string) []any {
	var a []any
	for _, t := range tfs {
		a = append(a, t)
	}
	return a
}

func backupDB(db *sql.DB, c config, rows int64, out io.Writer) (string, error) {
	root := c.backupRoot
	if root == "" {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return "", fmt.Errorf("no home dir for the default backup root; pass -backup-root")
		}
		root = filepath.Join(h, "vl-backups")
	}
	dir := filepath.Join(root, "import-label-shift-"+nowFn().Format("20060102T150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "data.db")
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("%s already exists", dst)
	}
	// VACUUM INTO is a transaction-consistent snapshot (safe even under
	// -allow-service-running) and refuses an existing target.
	if _, err := db.Exec("VACUUM INTO ?", dst); err != nil {
		return "", err
	}
	bdb, err := openDB(dst, true)
	if err != nil {
		return "", err
	}
	defer bdb.Close()
	var n int64
	if err := bdb.QueryRow("SELECT COUNT(*) FROM bars").Scan(&n); err != nil {
		return "", err
	}
	if n != rows {
		return "", fmt.Errorf("backup holds %d bars rows, source %d", n, rows)
	}
	fmt.Fprintf(out, "\nbackup: %s (%d bars rows, verified)\n", dst, n)
	return dst, nil
}

const ddlRuns = `CREATE TABLE IF NOT EXISTS bars_import_label_shift (
	version TEXT NOT NULL, run_id TEXT NOT NULL, symbol TEXT NOT NULL, contract TEXT NOT NULL, tf TEXT NOT NULL,
	shift_ms INTEGER NOT NULL, rows_shifted INTEGER NOT NULL, rows_parked INTEGER NOT NULL,
	first_before_ms INTEGER, last_before_ms INTEGER, backup TEXT, applied_at TEXT NOT NULL,
	PRIMARY KEY (version, run_id, symbol, contract, tf))`

const ddlConf = `CREATE TABLE IF NOT EXISTS bars_import_shift_conflicts (
	version TEXT NOT NULL, run_id TEXT NOT NULL, orig_rowid INTEGER, symbol TEXT NOT NULL, tf TEXT NOT NULL, contract TEXT NOT NULL,
	open_time_ms INTEGER NOT NULL, o REAL, h REAL, l REAL, c REAL, v REAL, convention TEXT, source TEXT,
	would_land_ms INTEGER NOT NULL, existing_rowid INTEGER, existing_source TEXT, existing_convention TEXT, identical INTEGER NOT NULL,
	parked_at TEXT NOT NULL, PRIMARY KEY (version, run_id, symbol, tf, contract, open_time_ms))`

func ensureTables(db *sql.DB) error {
	for _, q := range []string{ddlRuns, ddlConf} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func applyContract(db *sql.DB, c config, runID, backup string, gs []group) (shifted, parked int64, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var before int64
	if err = tx.QueryRow("SELECT COUNT(*) FROM bars").Scan(&before); err != nil {
		return
	}
	now := nowFn().UTC().Format(time.RFC3339)
	type result struct {
		g              group
		shifted, parks int64
	}
	var results []result
	for _, g := range gs {
		var cfs []conflict
		if cfs, err = conflicts(tx, c.symbol, g); err != nil {
			return
		}
		for _, x := range cfs {
			if _, err = tx.Exec(`INSERT INTO bars_import_shift_conflicts
				SELECT ?, ?, rowid, symbol, tf, contract, open_time_ms, o, h, l, c, v, convention, source, ?, ?, ?, ?, ?, ? FROM bars WHERE rowid=?`,
				shiftVersion, runID, x.OpenMs-g.Dur, x.ExistingRowid, x.ExistingSource, x.ExistingConv, boolInt(x.Identical), now, x.Rowid); err != nil {
				return
			}
			// PARK in place (UPDATE, never DELETE): the key leaves the live range,
			// the contract label is the one every contract-scoped reader skips.
			if _, err = tx.Exec(`UPDATE bars SET contract=?, source=?, convention='parked' WHERE rowid=?`,
				store.ContractMixed, parkedSource, x.Rowid); err != nil {
				return
			}
		}
		// phase 1: every still-eligible row of the group to the far range
		res, e := tx.Exec(fmt.Sprintf(`UPDATE bars SET open_time_ms = open_time_ms - ? - ? WHERE %s`, eligibleWhere("")),
			g.Dur, phaseGap, c.symbol, g.TF, g.Contract)
		if e != nil {
			err = e
			return
		}
		n1, _ := res.RowsAffected()
		// phase 2: land on the final key, re-tag
		res, e = tx.Exec(`UPDATE bars SET open_time_ms = open_time_ms + ?, convention=? WHERE symbol=? AND tf=? AND contract=? AND source=? AND convention=? AND open_time_ms < ?`,
			phaseGap, store.ConventionEpochFloorOpen, c.symbol, g.TF, g.Contract, store.BarSourceHistoricalImport, legacyConvention, -phaseGap/2)
		if e != nil {
			err = e
			return
		}
		n2, _ := res.RowsAffected()
		if n1 != n2 || n1+int64(len(cfs)) != g.N {
			err = fmt.Errorf("%s %s: group had %d eligible, parked %d, phase1 moved %d, phase2 landed %d", g.Contract, g.TF, g.N, len(cfs), n1, n2)
			return
		}
		results = append(results, result{g, n1, int64(len(cfs))})
		shifted += n1
		parked += int64(len(cfs))
	}
	for _, r := range results {
		if _, err = tx.Exec(`INSERT INTO bars_import_label_shift VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			shiftVersion, runID, c.symbol, r.g.Contract, r.g.TF, r.g.Dur, r.shifted, r.parks, r.g.First, r.g.Last, backup, now); err != nil {
			return
		}
	}
	var after int64
	if err = tx.QueryRow("SELECT COUNT(*) FROM bars").Scan(&after); err != nil {
		return
	}
	if after != before {
		err = fmt.Errorf("bars rows %d -> %d inside the transaction", before, after)
		return
	}
	err = tx.Commit()
	return
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
