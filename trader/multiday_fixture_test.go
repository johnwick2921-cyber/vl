package trader

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"
)

// TestMultiDayFixtureLoads pins the multi-day replay fixture (m186 DS-108,
// 10-13 → 10-21): the recorded MNQ 1m + native 1h bars, in the SAME shape the
// full-day replay reads, with a seed at the 10-13 session open and a window
// through the 10-21 RTH close. MUTANT: truncate the 1m series before the seed
// → the EMA34 depth is short and the replay seeds shallow → RED.
func TestMultiDayFixtureLoads(t *testing.T) {
	raw, err := os.ReadFile("testdata/mnq_1m_2026-10-21_multiday.json.gz")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	var doc struct {
		Bars        map[string][][]json.RawMessage `json:"bars"`
		SeedNowCTMs int64                          `json:"seed_now_ct_ms"`
		WindowCTMs  []int64                        `json:"window_ct_ms"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if got := len(doc.Bars["1m"]); got < 130_000 {
		t.Fatalf("1m bars = %d, want >= 130000 (warm-up + the 10-13→10-21 window)", got)
	}
	if got := len(doc.Bars["1h"]); got < 3_800 {
		t.Fatalf("1h bars = %d, want >= 3800 (native 1h retention)", got)
	}
	if len(doc.WindowCTMs) != 2 || doc.WindowCTMs[0] != doc.SeedNowCTMs {
		t.Fatalf("window/seed mismatch: seed=%d window=%v", doc.SeedNowCTMs, doc.WindowCTMs)
	}
	// The last 1m bar must reach the 10-21 RTH close (16:00 CT = window end).
	var lastOT float64
	if rows := doc.Bars["1m"]; len(rows) > 0 {
		if len(rows[len(rows)-1]) > 0 {
			_ = json.Unmarshal(rows[len(rows)-1][0], &lastOT)
		}
	}
	if lastOT < float64(doc.WindowCTMs[1]-120_000) {
		t.Fatalf("last 1m open %d is far short of the window end %d", int64(lastOT), doc.WindowCTMs[1])
	}
}
