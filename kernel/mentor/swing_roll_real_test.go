package mentor

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
)

// loadRollCSV reads one `open_time_ms,o,h,l,c,v` export (read-only DB dump).
func loadRollCSV(t *testing.T, path string, tfMs int64) []market.Kline {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []market.Kline
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ",")
		if len(p) < 6 {
			continue
		}
		ot, _ := strconv.ParseInt(p[0], 10, 64)
		fl := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + tfMs - 1, Open: fl(p[1]), High: fl(p[2]), Low: fl(p[3]), Close: fl(p[4]), Volume: fl(p[5])})
	}
	return out
}

// TestM456RealRollSeamSeedNotStale (m456 defect 2, evidence on real data):
// the REAL 09-26 -> 12-26 roll, read from a read-only export of the store
// (VL_ROLL_REAL_DIR = a dir holding MNQ_09-26_1m/1h.csv and MNQ_12-26_1m/1h.csv),
// stitched by the production RollStitcher and seeded through SeedFull at the
// production call shape. A hole-free-by-the-store tape must never be STALE.
// Skipped when the export is absent (CI has no production DB).
func TestM456RealRollSeamSeedNotStale(t *testing.T) {
	dir := os.Getenv("VL_ROLL_REAL_DIR")
	if dir == "" {
		t.Skip("VL_ROLL_REAL_DIR not set (read-only store export of MNQ 09-26 / 12-26)")
	}
	rd := func(c, tf string, ms int64) []market.Kline {
		return loadRollCSV(t, filepath.Join(dir, c+"_"+tf+".csv"), ms)
	}
	older := Contract1M{Contract: "MNQ 09-26", Bars: rd("MNQ_09-26", "1m", 60_000), Bars1H: rd("MNQ_09-26", "1h", 3_600_000)}
	newest := Contract1M{Contract: "MNQ 12-26", Bars: rd("MNQ_12-26", "1m", 60_000), Bars1H: rd("MNQ_12-26", "1h", 3_600_000)}
	s := NewRollStitcher(newest)
	if !s.Add(older) {
		t.Fatalf("stitch refused")
	}
	stitched, _, _, _ := s.Result()
	seam := s.SeamCovered()
	t.Logf("stitched %d 1m bars, seamCovered %d minutes", len(stitched), len(seam))
	loc := ctime()
	stale, n := 0, 0
	for d := time.Date(2026, 9, 14, 17, 5, 0, 0, loc); d.Before(time.Date(2026, 10, 12, 0, 0, 0, 0, loc)); d = d.Add(6*time.Hour + 5*time.Minute) {
		if !kernel.IsCMEOpen(d) {
			continue
		}
		nm := d.UnixMilli()
		var hist []market.Kline
		for _, b := range stitched {
			if b.CloseTime+1 <= nm {
				hist = append(hist, b)
			}
		}
		e := New(lrProdCfg())
		e.SetSeamCovered(seam)
		SeedFull(e, hist, keyLevel1HBars(hist), nm)
		n++
		if e.State.Swing.LineStale {
			stale++
			t.Errorf("STALE at %s: holed=%v", kernel.FormatCT(d), e.State.Swing.HoledBuckets)
		}
	}
	t.Logf("seeds %d stale %d", n, stale)
}
