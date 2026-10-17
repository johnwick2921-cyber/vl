package mentor

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"

	"vl/market"
)

// zz_audit_probe_test.go — DS-101 MENTOR-FUNCTION-AUDIT probe (READ-ONLY,
// UNCOMMITTED). Loads the real MNQ 12-26 1m bars for the 17:00 CT 10-12 →
// 16:00 CT 10-13 session, seeds the evaluator at 08:30 CT, then ticks the RTH
// session and reports per-setup intents and the kernel refusal funnel.

func loadMNQAuditBars(t *testing.T) []market.Kline {
	f, err := os.Open("/tmp/ds101-mnq-1m.csv")
	if err != nil {
		t.Fatalf("open bars csv: %v", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read bars csv: %v", err)
	}
	bars := make([]market.Kline, 0, len(rows))
	for _, row := range rows {
		if len(row) < 5 {
			continue
		}
		openMs, _ := strconv.ParseInt(row[0], 10, 64)
		o, _ := strconv.ParseFloat(row[1], 64)
		h, _ := strconv.ParseFloat(row[2], 64)
		l, _ := strconv.ParseFloat(row[3], 64)
		c, _ := strconv.ParseFloat(row[4], 64)
		bars = append(bars, market.Kline{OpenTime: openMs, CloseTime: openMs + 59_999, Open: o, High: h, Low: l, Close: c})
	}
	return bars
}

func auditCtMs(day int, hh, mm int) int64 {
	// 17:00 CT 10-12 = 1791842400000 (verified from the CSV min).
	base := int64(1791842400000)
	// day 12 → 0; day 13 → +24h
	return base + int64(day-12)*24*3600_000 + int64((hh-17+24)%24)*3600_000 + int64(mm)*60_000
}

// TestZZAuditSetupsProbe is the probe: it is NOT a gate test — it prints.
func TestZZAuditSetupsProbe(t *testing.T) {
	bars := loadMNQAuditBars(t)
	t.Logf("bars=%d first=%d last=%d", len(bars), bars[0].OpenTime, bars[len(bars)-1].OpenTime)

	rthOpen := auditCtMs(13, 8, 30) // 08:30 CT 10-13
	idx := 0
	for idx < len(bars) && bars[idx].OpenTime < rthOpen {
		idx++
	}
	seedBars := bars[:idx]
	rth := bars[idx:]
	t.Logf("seed bars=%d (to %d), RTH bars=%d", len(seedBars), rthOpen, len(rth))

	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)
	// Seed with the overnight history so the day gate + levels + trigger are
	// live, exactly as the trader seeds at boot.
	missing := Seed(e, seedBars, rthOpen)
	t.Logf("seed missing=%v", missing)

	setupCounts := map[string]int{}
	totalIntents := 0
	for i := 1; i < len(rth); i++ {
		win := append(append([]market.Kline{}, seedBars...), rth[:i+1]...)
		now := rth[i].CloseTime + 1
		ints := e.Tick(win, now)
		for _, in := range ints {
			if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry || in.Action == PlaceStopLimitEntry {
				setupCounts[in.Setup]++
				totalIntents++
			}
		}
	}
	t.Logf("RTH intents by setup: %v (total %d)", setupCounts, totalIntents)

	keys := make([]string, 0, len(e.State.Refusals))
	for k := range e.State.Refusals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("kernel refusals:")
	for _, k := range keys {
		t.Logf("  %-40s %d", k, e.State.Refusals[k])
	}
	t.Logf("day verdict=%v", e.State.Day.Verdict)
	_ = fmt.Sprintf
}
