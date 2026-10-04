package mentor

import (
	"encoding/json"
	"os"
	"testing"

	"vl/market"
)

// loadMNQ1m loads a testdata MNQ 1m fixture (rows: [openTime, open, high, low,
// close]) into evaluator Klines with the bucket-close convention CloseTime =
// OpenTime + 60_000 - 1.
func loadMNQ1m(t *testing.T, path string) []market.Kline {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var doc struct {
		Bars map[string][][5]float64 `json:"bars"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	rows := doc.Bars["1m"]
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		ot := int64(r[0])
		out = append(out, market.Kline{
			Open:      r[1],
			High:      r[2],
			Low:       r[3],
			Close:     r[4],
			OpenTime:  ot,
			CloseTime: ot + 60_000 - 1,
		})
	}
	return out
}

// TestSeedIncrementalMatchesRebuildRecordedDay — binding point 2 on REAL data:
// seed on day 1 (2026-09-15 RTH), advance tick-by-tick through day 2
// (2026-09-16 RTH), and the resulting state must EQUAL a fresh seed over the
// whole two-day tape: same 1H level set, same 1m EMA 34/9.
func TestSeedIncrementalMatchesRebuildRecordedDay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	day1 := loadMNQ1m(t, "testdata/mnq_1m_2026-09-15_rth.json")
	day2 := loadMNQ1m(t, "testdata/mnq_1m_2026-09-16_rth.json")
	full := append(append([]market.Kline(nil), day1...), day2...)
	seedTime := day1[len(day1)-1].CloseTime

	// Incremental: seed day 1, then tick day 2 closed-bar by closed-bar.
	inc := New(cfg)
	Seed(inc, day1, seedTime)
	for i := len(day1) + 1; i <= len(full); i++ {
		now := full[i-1].CloseTime + 1
		inc.Tick(full[:i], now)
	}

	// Rebuild: seed the whole tape at the end time.
	endTime := full[len(full)-1].CloseTime + 1 // match Tick: now advances one millisecond after the final closed bar
	reb := New(cfg)
	Seed(reb, full, endTime)

	if len(inc.State.SeedLevels) != len(reb.State.SeedLevels) {
		t.Fatalf("levels: incremental %d, rebuild %d",
			len(inc.State.SeedLevels), len(reb.State.SeedLevels))
	}
	for i := range reb.State.SeedLevels {
		a, b := inc.State.SeedLevels[i], reb.State.SeedLevels[i]
		if a.Price != b.Price || a.Kind != b.Kind {
			t.Fatalf("level %d: incremental %+v, rebuild %+v", i, a, b)
		}
	}
	if abs(inc.State.EMA34-reb.State.EMA34) > 1e-9 {
		t.Fatalf("EMA34: incremental %.6f, rebuild %.6f", inc.State.EMA34, reb.State.EMA34)
	}
	if abs(inc.State.EMA9-reb.State.EMA9) > 1e-9 {
		t.Fatalf("EMA9: incremental %.6f, rebuild %.6f", inc.State.EMA9, reb.State.EMA9)
	}
	if inc.State.Seed1HWatermark != reb.State.Seed1HWatermark {
		t.Fatalf("1H watermark: incremental %d, rebuild %d",
			inc.State.Seed1HWatermark, reb.State.Seed1HWatermark)
	}
	if inc.State.Seed1HLastColour != reb.State.Seed1HLastColour {
		t.Fatalf("last colour: incremental %v, rebuild %v",
			inc.State.Seed1HLastColour, reb.State.Seed1HLastColour)
	}
}
