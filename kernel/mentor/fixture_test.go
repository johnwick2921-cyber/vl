package mentor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// fixture is the on-disk shape written by testdata/extract_fixtures.py:
// recorded MNQ bars copied READ-ONLY from the DB copy (the bot's own tape).
type fixture struct {
	Symbol   string                  `json:"symbol"`
	Contract string                  `json:"contract"`
	Bars     map[string][][5]float64 `json:"bars"` // tf -> [ot,o,h,l,c]
}

// LoadCSVBars loads a recorded-bar CSV fixture (CTO fixtures format,
// 2026-10-03): kernel/mentor/testdata/<rule>/<name>.csv, one bar per line —
// ts_utc,open,high,low,close,volume,tf — with '#' header comments naming the
// source query + date. EPOCH RULING (CTO 2026-10-03 12:59Z): ONE convention =
// REAL UTC epoch ms everywhere (store, BarCache, Go mentor, harness,
// fixtures). ts_utc is kept as-is (real UTC); the old wall-as-epoch
// conversion fed Go wall epochs it converted AGAIN (the 5h day-gate shift).
// This is THE pattern DS-105/DS-106 copy for their fixtures.
func LoadCSVBars(t *testing.T, path string, wantTF int) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	var out []market.Kline
	for ln, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 7 {
			t.Fatalf("%s:%d: want 7 fields, got %d", path, ln+1, len(f))
		}
		ts, err := time.Parse(time.RFC3339, f[0])
		if err != nil {
			t.Fatalf("%s:%d: bad ts %q: %v", path, ln+1, f[0], err)
		}
		open := mustFloat(t, f[1])
		high := mustFloat(t, f[2])
		low := mustFloat(t, f[3])
		closeP := mustFloat(t, f[4])
		vol := mustFloat(t, f[5])
		tf, err := strconv.Atoi(f[6])
		if err != nil || tf != wantTF {
			t.Fatalf("%s:%d: tf %q, want %d", path, ln+1, f[6], wantTF)
		}
		// ts_utc → epoch millis, kept REAL UTC (epoch ruling 2026-10-03 12:59Z).
		ctMs := ts.UnixMilli()
		out = append(out, market.Kline{
			OpenTime:  ctMs,
			CloseTime: ctMs + 59_999,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeP,
			Volume:    vol,
		})
	}
	return out
}

func mustFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		t.Fatalf("bad number %q: %v", s, err)
	}
	return v
}

// TestLoadCSVBarsExampleFixture — the loader round-trips the ONE example CSV
// fixture (the pattern for DS-105/DS-106): 20 recorded MNQ 1m bars,
// 08:40–09:00 CT on 2026-09-15, first bar open 29430.00 (the DB copy's value).
func TestLoadCSVBarsExampleFixture(t *testing.T) {
	bars := LoadCSVBars(t, "testdata/touch/example_touch.csv", 1)
	// the loader round-trips the DB copy's 08:40 bar. The CT-time assertion
	// returns when DS-105 switches LoadCSVBars to real UTC (EPOCH RULING):
	// the wall loader's epoch is CT-wall, so ctOf currently reads 03:40 on
	// CDT — pinning that would pin the WRONG frame.
	if len(bars) != 20 {
		t.Fatalf("bars = %d, want 20", len(bars))
	}
	if bars[0].Open != 29430.00 || bars[0].High != 29434.75 || bars[0].Low != 29409.50 || bars[0].Close != 29431.50 {
		t.Fatalf("first bar = %+v, want the DB copy's 08:40 bar", bars[0])
	}
	// the loader keeps REAL UTC (EPOCH RULING 2026-10-03 12:59Z): the DB
	// copy's 08:40 CT bar must read 08:40 through the zone.
	ct := time.UnixMilli(bars[0].OpenTime).In(ctime())
	if ct.Hour() != 8 || ct.Minute() != 40 {
		t.Fatalf("first bar CT time = %02d:%02d, want 08:40", ct.Hour(), ct.Minute())
	}
}

// loadFixture reads a recorded fixture and returns the given timeframe's bars
// as market.Kline (canon 53: tests run on the bot's REAL recorded bars, not
// synthetic inputs).
func loadFixture(t *testing.T, name, tf string) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	rows, ok := f.Bars[tf]
	if !ok {
		t.Fatalf("fixture %s: no tf %q", name, tf)
	}
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{
			OpenTime:  int64(r[0]),
			Open:      r[1],
			High:      r[2],
			Low:       r[3],
			Close:     r[4],
			CloseTime: int64(r[0]) + 59_999,
		})
	}
	return out
}
