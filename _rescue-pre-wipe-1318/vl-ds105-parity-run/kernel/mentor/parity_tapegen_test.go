package mentor

// Tape generator for the strict parity harness (CTO 2026-10-03 07:02Z):
// one tape per parity day with a deep prefix so the per-source warm-up
// floors hold (4h buckets >= 102, 1m bars >= 102). The bars are pulled from
// the replay's db-copy with the SAME semantics the replay world uses
// (data_structs.load_1m): rows ordered by open_time_ms; on a minute where
// two contracts overlap, the LAST row in the query wins.
//
// Guarded by PARITY_GEN=1 so the committed test never touches a database.
//
//	go test -count=1 -run TestGenerateParityTapes -v ./kernel/mentor/
//
// with PARITY_GEN=1 and PARITY_DB=<path to db-copy> (defaults to the
// mentor-mode db-copy).
//
// The written CSV shape is the package fixture shape (LoadCSVBars):
// RFC3339 CT-wall-clock-as-UTC, open, high, low, close, volume, tf.

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
	// prefixDays overrides tapePrefixDays for tapes whose default window is
	// cut by a contract-listing gap (the range day's 06-03..06-10 hole).
	prefixDays int
}

var parityTapeSpecs = []parityTapeSpec{
	{"trend", "testdata/parity/trend_2025-05-07.csv", "2025-05-07", 0},
	{"range", "testdata/parity/range_2023-06-20.csv", "2023-06-20", 33},
	// P7: 2025-04-02 was depth-blocked in the db-copy; 2025-02-07 (NFP)
	// has a full 30-day prefix inside MNQ 03-25.
	{"news", "testdata/parity/news_2025-02-07.csv", "2025-02-07", 0},
	{"normal_a", "testdata/parity/normal_a_2024-07-16.csv", "2024-07-16", 0},
	{"normal_b", "testdata/parity/normal_b_2025-07-16.csv", "2025-07-16", 0},
}

// tapePrefixDays is the prefix length BEFORE the target day. 102 four-hour
// buckets need ~17 trading days; 30 calendar days covers the weekends with
// margin (a 17:00 CT anchor yields ~20 trading days x 6 = 120 buckets).
const tapePrefixDays = 30

func TestGenerateParityTapes(t *testing.T) {
	if os.Getenv("PARITY_GEN") != "1" {
		t.Skip("PARITY_GEN != 1: tape generation is off (the committed test never touches a database)")
	}
	dbPath := os.Getenv("PARITY_DB")
	if dbPath == "" {
		dbPath = "/home/hoang/mm-course/mentor-mode/db-copy/data.db"
	}
	for _, spec := range parityTapeSpecs {
		bars := loadParityDayBars(t, dbPath, spec.day, spec.prefixDays)
		if len(bars) == 0 {
			t.Fatalf("%s: no bars loaded", spec.name)
		}
		if err := os.WriteFile(spec.file, tapeCSV(bars), 0o644); err != nil {
			t.Fatalf("%s: write tape: %v", spec.name, err)
		}
		first := bars[0]
		// depth preview: distinct 4h buckets, so a floor miss is visible at
		// generation time, not at the parity gate.
		fourH := map[int64]bool{}
		for _, b := range bars {
			fourH[b.ts/(240*60_000)] = true
		}
		t.Logf("TAPE %-8s -> %s: %d bars, ~%d 4h buckets, %s .. %s",
			spec.name, spec.file, len(bars), len(fourH),
			time.UnixMilli(first.ts).UTC().Format("2006-01-02 15:04"),
			time.UnixMilli(bars[len(bars)-1].ts).UTC().Format("2006-01-02 15:04"))
	}
}

// loadParityDayBars loads MNQ 1m bars from the db-copy for the window
// [day-prefixDays, day+1d). On a minute where two contracts overlap the
// LAST row in open_time_ms order wins — the same overwrite semantics as the
// replay's load_1m numpy assignment.
func loadParityDayBars(t *testing.T, dbPath, day string, prefixDays int) []parityBar {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()

	start, err := time.ParseInLocation("2006-01-02", day, time.UTC)
	if err != nil {
		t.Fatalf("day %s: %v", day, err)
	}
	if prefixDays == 0 {
		prefixDays = tapePrefixDays
	}
	from := start.AddDate(0, 0, -prefixDays)
	to := start.AddDate(0, 0, 1)

	rows, err := db.Query(
		"SELECT open_time_ms, o, h, l, c, v FROM bars WHERE symbol='MNQ' AND tf='1m' AND open_time_ms >= ? AND open_time_ms < ? ORDER BY open_time_ms",
		from.UnixMilli(), to.UnixMilli())
	if err != nil {
		t.Fatalf("query %s: %v", dbPath, err)
	}
	defer rows.Close()

	// Last row per minute wins (mirrors the replay's per-minute overwrite).
	byMin := map[int64]parityBar{}
	var order []int64
	for rows.Next() {
		var ms int64
		var b parityBar
		if err := rows.Scan(&ms, &b.open, &b.high, &b.low, &b.close, &b.vol); err != nil {
			t.Fatalf("scan %s: %v", dbPath, err)
		}
		if _, seen := byMin[ms]; !seen {
			order = append(order, ms)
		}
		b.ts = ms
		byMin[ms] = b
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %s: %v", dbPath, err)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]parityBar, 0, len(order))
	for _, ms := range order {
		out = append(out, byMin[ms])
	}
	return out
}

type parityBar struct {
	ts              int64
	open, high, low float64
	close, vol      float64
}

func tapeCSV(bars []parityBar) []byte {
	out := make([]byte, 0, len(bars)*64)
	out = append(out, "# MNQ 1m parity tape — CT wall clock as UTC, last-row-per-minute wins\n"...)
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
