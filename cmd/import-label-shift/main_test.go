package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/testhome"
	"vl/store"
)

func TestMain(m *testing.M) { os.Exit(testhome.Guard(m)) }

// 2026-10-29 08:30 CT (CDT) = 13:30 UTC.
var open0 = time.Date(2026, 10, 29, 13, 30, 0, 0, time.UTC).UnixMilli()

const (
	min1 = int64(60_000)
	min5 = int64(300_000)
)

type fx struct {
	path string
	db   *sql.DB
}

func ins(t *testing.T, db *sql.DB, tf, contract string, ms int64, v float64, conv, src string) {
	t.Helper()
	// o,h,l,c all derive from ms so a moved row is recognisable
	if _, err := db.Exec(`INSERT INTO bars(symbol,tf,contract,open_time_ms,o,h,l,c,v,convention,source) VALUES ('MNQ',?,?,?,?,?,?,?,?,?,?)`,
		tf, contract, ms, 100.0, 100.0+float64(ms%1000), 99.0, 100.5, v, conv, src); err != nil {
		t.Fatal(err)
	}
}

// fixture: MNQ 12-25 holds a CLOSE-stamped import of the minutes 08:30..08:39
// CT (labels 08:31..08:40, but no label 08:39), the 5m bar over the first five
// (label 08:35), plus a LIVE row at 08:39 that is the same bar as the import's
// last 1m (label 08:40). MNQ 03-26 holds one already-open-stamped import row (the fixed
// importer's output) that the tool must never touch.
func newFixture(t *testing.T) *fx {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.db")
	st, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	db, err := openDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const c = "MNQ 12-25"
	for i := int64(1); i <= 10; i++ {
		if i == 9 {
			// the real store has no import row at this label: ImportBars is
			// no-upsert and the LIVE bar that really opened here took the key, so
			// the import row was silently skipped. The live row is added below.
			continue
		}
		ins(t, db, "1m", c, open0+i*min1, 10, legacyConvention, store.BarSourceHistoricalImport) // CLOSE label
	}
	// 5m over opens 08:30..08:34: volume 50, high = max of its five 1m highs (labels 1..5)
	for k := int64(0); k < 1; k++ {
		var h float64
		_ = db.QueryRow(`SELECT MAX(h) FROM bars WHERE tf='1m' AND contract=? AND open_time_ms>=? AND open_time_ms<=?`, c, open0+k*min5+min1, open0+k*min5+5*min1).Scan(&h)
		if _, err := db.Exec(`INSERT INTO bars(symbol,tf,contract,open_time_ms,o,h,l,c,v,convention,source) VALUES ('MNQ','5m',?,?,100,?,99,100.5,50,?,?)`,
			c, open0+(k+1)*min5, h, legacyConvention, store.BarSourceHistoricalImport); err != nil {
			t.Fatal(err)
		}
	}
	// live row at the true open of the import's last bar, identical OHLCV
	if _, err := db.Exec(`INSERT INTO bars(symbol,tf,contract,open_time_ms,o,h,l,c,v,convention,source)
		SELECT symbol,tf,contract,open_time_ms-?,o,h,l,c,v,'epoch_floor','live' FROM bars WHERE tf='1m' AND contract=? AND open_time_ms=?`,
		min1, c, open0+10*min1); err != nil {
		t.Fatal(err)
	}
	ins(t, db, "1m", "MNQ 03-26", open0+5*min1, 7, store.ConventionEpochFloorOpen, store.BarSourceHistoricalImport)
	return &fx{path: path, db: db}
}

func (f *fx) dump(t *testing.T) string {
	t.Helper()
	rows, err := f.db.Query(`SELECT rowid,symbol,tf,contract,open_time_ms,o,h,l,c,v,COALESCE(convention,''),COALESCE(source,'') FROM bars ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var rid, ms int64
		var sy, tf, c, cv, so string
		var o, h, l, cl, v float64
		_ = rows.Scan(&rid, &sy, &tf, &c, &ms, &o, &h, &l, &cl, &v, &cv, &so)
		fmt.Fprintf(&b, "%d|%s|%s|%s|%d|%v|%v|%v|%v|%v|%s|%s\n", rid, sy, tf, c, ms, o, h, l, cl, v, cv, so)
	}
	return b.String()
}

func cfg(f *fx, apply bool) config {
	return config{db: f.path, symbol: "MNQ", tfs: "1m,5m,15m,1h", unit: "vl.service", backupRoot: filepath.Join(filepath.Dir(f.path), "bk"), apply: apply}
}

func stubService(t *testing.T, active bool) {
	t.Helper()
	old := serviceActive
	serviceActive = func(string) bool { return active }
	t.Cleanup(func() { serviceActive = old })
}

func count(t *testing.T, db *sql.DB, q string, a ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(q, a...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDryRunReportsAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	before := f.dump(t)
	var out bytes.Buffer
	if err := run(cfg(f, false), &out); err != nil {
		t.Fatal(err)
	}
	rep := out.String()
	for _, want := range []string{"DRY-RUN", "MNQ 12-25", "TOTAL rows to shift: 10", "conflicts MNQ 12-25 1m: 1 (1 identical OHLCV duplicates",
		"close-stamped) 1, [t..t+4] (open-stamped) 0"} {
		if !strings.Contains(rep, want) {
			t.Fatalf("report missing %q:\n%s", want, rep)
		}
	}
	if strings.Contains(rep, "MNQ 03-26") {
		t.Fatalf("an already-open-stamped contract must not be listed:\n%s", rep)
	}
	if f.dump(t) != before {
		t.Fatalf("dry-run changed the database")
	}
	if count(t, f.db, `SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'bars_import_%'`) != 0 {
		t.Fatalf("dry-run created bookkeeping tables")
	}
}

func TestApplyRefusesWhileServiceRuns(t *testing.T) {
	f := newFixture(t)
	stubService(t, true)
	before := f.dump(t)
	err := run(cfg(f, true), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "ACTIVE") {
		t.Fatalf("want a refusal naming the running service, got %v", err)
	}
	if f.dump(t) != before {
		t.Fatalf("a refused run wrote")
	}
	if _, e := os.Stat(filepath.Join(filepath.Dir(f.path), "bk")); e == nil {
		t.Fatalf("a refused run made a backup dir")
	}
}

func TestApplyShiftsParksAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	stubService(t, false)
	nowFn = func() time.Time { return time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC) }
	defer func() { nowFn = time.Now }()
	total := count(t, f.db, "SELECT COUNT(*) FROM bars")
	untouched := count(t, f.db, `SELECT open_time_ms FROM bars WHERE contract='MNQ 03-26'`)

	var out bytes.Buffer
	if err := run(cfg(f, true), &out); err != nil {
		t.Fatalf("apply: %v\n%s", err, out.String())
	}
	// every row survives
	if got := count(t, f.db, "SELECT COUNT(*) FROM bars"); got != total {
		t.Fatalf("rows %d -> %d: the tool must keep everything", total, got)
	}
	// 1m: labels 08:31..08:38 moved to opens 08:30..08:37; the 08:40 label parked
	for i := int64(0); i < 8; i++ {
		if count(t, f.db, `SELECT COUNT(*) FROM bars WHERE tf='1m' AND contract='MNQ 12-25' AND source='historical_import' AND convention=? AND open_time_ms=?`,
			store.ConventionEpochFloorOpen, open0+i*min1) != 1 {
			t.Fatalf("1m open %d missing after the shift", i)
		}
	}
	// 5m: label 08:35 -> open 08:30 (its OWN length, 300 s)
	for k := int64(0); k < 1; k++ {
		if count(t, f.db, `SELECT COUNT(*) FROM bars WHERE tf='5m' AND source='historical_import' AND convention=? AND open_time_ms=?`, store.ConventionEpochFloorOpen, open0+k*min5) != 1 {
			t.Fatalf("5m open %d missing after the shift", k)
		}
	}
	// the conflicting row: parked, quarantined, live row untouched
	if count(t, f.db, `SELECT COUNT(*) FROM bars WHERE source=? AND contract=?`, parkedSource, store.ContractMixed) != 1 {
		t.Fatalf("the conflicting import row was not parked")
	}
	if count(t, f.db, `SELECT COUNT(*) FROM bars_import_shift_conflicts WHERE contract='MNQ 12-25' AND identical=1 AND open_time_ms=?`, open0+10*min1) != 1 {
		t.Fatalf("the parked row is not quarantined with its original key")
	}
	if count(t, f.db, `SELECT COUNT(*) FROM bars WHERE source='live' AND open_time_ms=?`, open0+9*min1) != 1 {
		t.Fatalf("the live row was disturbed")
	}
	// the already-open-stamped row is never touched
	if got := count(t, f.db, `SELECT open_time_ms FROM bars WHERE contract='MNQ 03-26'`); got != untouched {
		t.Fatalf("an open-stamped row was shifted: %d -> %d", untouched, got)
	}
	// marker rows + backup exist, backup verified
	if count(t, f.db, `SELECT SUM(rows_shifted) FROM bars_import_label_shift`) != 9 {
		t.Fatalf("marker rows_shifted wrong")
	}
	bk := filepath.Join(filepath.Dir(f.path), "bk", "import-label-shift-20260930T030000", "data.db")
	if strings.Contains(out.String(), "backup:") == false {
		t.Fatalf("no backup line:\n%s", out.String())
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(f.path), "bk", "import-label-shift-*", "data.db"))
	if len(matches) != 1 {
		t.Fatalf("backup file missing (%s): %v", bk, matches)
	}
	if !strings.Contains(out.String(), "alignment AFTER  MNQ 12-25: 1 sampled 5m rows -> close-stamped 0, open-stamped 1") {
		t.Fatalf("the 5m/1m alignment proof does not show open-stamped after:\n%s", out.String())
	}
	// IDEMPOTENT: the second run finds nothing and changes nothing
	after := f.dump(t)
	var out2 bytes.Buffer
	if err := run(cfg(f, true), &out2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.String(), "nothing to do") {
		t.Fatalf("second run should find nothing:\n%s", out2.String())
	}
	if f.dump(t) != after {
		t.Fatalf("second run changed the database (not idempotent)")
	}
}

// A failure inside one contract rolls that contract back whole.
func TestApplyRollsBackAContractOnError(t *testing.T) {
	f := newFixture(t)
	stubService(t, false)
	// squat on the park key so the park UPDATE collides
	ins(t, f.db, "1m", store.ContractMixed, open0+10*min1, 1, "x", "mixed")
	before := f.dump(t)
	err := run(cfg(f, true), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want a rollback error, got %v", err)
	}
	if f.dump(t) != before {
		t.Fatalf("a failed contract left partial changes")
	}
}

func TestContractFilterAndTimeframeValidation(t *testing.T) {
	f := newFixture(t)
	c := cfg(f, false)
	c.tfs = "7m"
	if err := run(c, &bytes.Buffer{}); err == nil {
		t.Fatalf("an unknown timeframe must be refused")
	}
	c = cfg(f, false)
	c.contract = "MNQ 03-26"
	var out bytes.Buffer
	if err := run(c, &out); err != nil || !strings.Contains(out.String(), "nothing to do") {
		t.Fatalf("an open-stamped contract has nothing to do: %v\n%s", err, out.String())
	}
}
