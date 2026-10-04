package mentor

// Tape generator for the strict parity harness. Each prefix mirrors the
// production store seed: up to the last 50,000 eligible 1m bars of the
// contract current immediately before midnight CT on the target day.
//
// Guarded by PARITY_GEN=1 so the committed test never touches a database.
//
//	go test -count=1 -run TestGenerateParityTapes -v ./kernel/mentor/
//
// with PARITY_GEN=1 and PARITY_DB=<path to db-copy> (defaults to the
// mentor-mode db-copy).
//
// The written CSV shape is the package fixture shape (LoadCSVBars):
// RFC3339 timestamps, open, high, low, close, volume, tf.

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

type parityTapeSpec struct {
	name string
	file string
	day  string
}

var parityTapeSpecs = []parityTapeSpec{
	{"trend", "testdata/parity/trend_2025-05-07.csv", "2025-05-07"},
	{"range", "testdata/parity/range_2023-06-20.csv", "2023-06-20"},
	{"news", "testdata/parity/news_2025-02-07.csv", "2025-02-07"},
	{"normal_a", "testdata/parity/normal_a_2024-07-16.csv", "2024-07-16"},
	{"normal_b", "testdata/parity/normal_b_2025-07-16.csv", "2025-07-16"},
}

const paritySeedBars = 50000

func TestGenerateParityTapes(t *testing.T) {
	if os.Getenv("PARITY_GEN") != "1" {
		t.Skip("PARITY_GEN != 1: tape generation is off (the committed test never touches a database)")
	}
	dbPath := os.Getenv("PARITY_DB")
	if dbPath == "" {
		dbPath = "/home/hoang/mm-course/mentor-mode/db-copy/data.db"
	}
	for _, spec := range parityTapeSpecs {
		bars, contract, prefixBars := loadParityDayBars(t, dbPath, spec.day)
		if len(bars) == 0 {
			t.Fatalf("%s: no bars loaded", spec.name)
		}
		if err := os.WriteFile(spec.file, tapeCSV(bars, contract), 0o644); err != nil {
			t.Fatalf("%s: write tape: %v", spec.name, err)
		}
		prefixFirst, prefixLast := bars[0], bars[prefixBars-1]
		last := bars[len(bars)-1]
		// depth preview: distinct 4h buckets, so a floor miss is visible at
		// generation time, not at the parity gate.
		fourH := map[int64]bool{}
		for _, b := range bars {
			fourH[b.ts/(240*60_000)] = true
		}
		availability := ""
		if prefixBars < paritySeedBars {
			availability = " (db-copy has fewer eligible contract bars than requested)"
		}
		t.Logf("TAPE %-8s -> %s: contract=%s prefix=%d/%d%s prefix CT=%s .. %s total=%d ~%d 4h buckets last CT=%s",
			spec.name, spec.file, contract, prefixBars, paritySeedBars, availability,
			time.UnixMilli(prefixFirst.ts).In(ctime()).Format("2006-01-02 15:04"),
			time.UnixMilli(prefixLast.ts).In(ctime()).Format("2006-01-02 15:04"),
			len(bars), len(fourH), time.UnixMilli(last.ts).In(ctime()).Format("2006-01-02 15:04"))
	}
}

// loadParityDayBars mirrors LastNBarsCurrentContract at the target day's
// midnight CT, then appends that contract's target-day bars.
func loadParityDayBars(t *testing.T, dbPath, day string) ([]parityBar, string, int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()

	start, err := time.ParseInLocation("2006-01-02", day, ctime())
	if err != nil {
		t.Fatalf("day %s: %v", day, err)
	}
	to := start.AddDate(0, 0, 1)

	const eligible = "symbol='MNQ' AND tf='1m' AND contract<>'' AND COALESCE(source,'') NOT IN ('mixed','replay:off-scale')"
	var contract string
	err = db.QueryRow(
		"SELECT contract FROM bars WHERE "+eligible+" AND open_time_ms < ? ORDER BY open_time_ms DESC LIMIT 1",
		start.UnixMilli(),
	).Scan(&contract)
	if err != nil {
		t.Fatalf("current MNQ contract before %s in %s: %v", day, dbPath, err)
	}

	prefixRows, err := db.Query(
		"SELECT open_time_ms, o, h, l, c, v FROM bars WHERE "+eligible+" AND contract=? AND open_time_ms < ? ORDER BY open_time_ms DESC LIMIT ?",
		contract, start.UnixMilli(), paritySeedBars,
	)
	if err != nil {
		t.Fatalf("query %s: %v", dbPath, err)
	}
	prefix := readParityRows(t, prefixRows, dbPath)
	if err := prefixRows.Close(); err != nil {
		t.Fatalf("close prefix rows %s: %v", dbPath, err)
	}
	sort.Slice(prefix, func(i, j int) bool { return prefix[i].ts < prefix[j].ts })

	dayRows, err := db.Query(
		"SELECT open_time_ms, o, h, l, c, v FROM bars WHERE "+eligible+" AND contract=? AND open_time_ms >= ? AND open_time_ms < ? ORDER BY open_time_ms",
		contract, start.UnixMilli(), to.UnixMilli(),
	)
	if err != nil {
		t.Fatalf("query target day %s in %s: %v", day, dbPath, err)
	}
	targetDay := readParityRows(t, dayRows, dbPath)
	if err := dayRows.Close(); err != nil {
		t.Fatalf("close target-day rows %s: %v", dbPath, err)
	}
	return append(prefix, targetDay...), contract, len(prefix)
}

func readParityRows(t *testing.T, rows *sql.Rows, dbPath string) []parityBar {
	t.Helper()
	var out []parityBar
	for rows.Next() {
		var ms int64
		var b parityBar
		if err := rows.Scan(&ms, &b.open, &b.high, &b.low, &b.close, &b.vol); err != nil {
			t.Fatalf("scan %s: %v", dbPath, err)
		}
		b.ts = ms
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %s: %v", dbPath, err)
	}
	return out
}

type parityBar struct {
	ts              int64
	open, high, low float64
	close, vol      float64
}

func tapeCSV(bars []parityBar, contract string) []byte {
	out := make([]byte, 0, len(bars)*64)
	out = append(out, fmt.Sprintf("# MNQ 1m parity tape — contract %s\n", contract)...)
	for _, b := range bars {
		ts := time.UnixMilli(b.ts).UTC().Format(time.RFC3339)
		out = append(out, fmt.Sprintf("%s,%s,%s,%s,%s,%s,1\n",
			ts,
			formatTapeFloat(b.open), formatTapeFloat(b.high),
			formatTapeFloat(b.low), formatTapeFloat(b.close),
			formatTapeFloat(b.vol))...)
	}
	return out
}

func formatTapeFloat(v float64) string {
	return fnum(v)
}
