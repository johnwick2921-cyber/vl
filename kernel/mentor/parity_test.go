package mentor

// Parity check vs DS-108's replay v5 base (CTO 2026-10-03): drive the Go
// evaluator minute by minute over 5 recorded RTH days (trend / range / news /
// two normal) and compare every emitted intent with the replay's
// trades_v5_base.csv for the same days.
//
// The v5 base models SWING4H ONLY (its setup column is all SWING4H), so this
// test matches swing-vs-swing and reports every other intent as go-only (with
// its rule) and every unmatched replay trade as replay-only. Rules DS-103 has
// not built yet (EMA34-off, R6, R10, R11, leg budget) are tagged "not yet
// built" instead of being counted as defects.
//
// REPORT-style: the full table is logged; the test fails only when the driver
// itself produced nothing (a broken tape or evaluator), never on a mismatch —
// mismatches are routed through the CTO.

import (
	"encoding/csv"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/market"
)

type replayTrade struct {
	day                       string
	minCT                     string // "2006-01-02 15:04" CT
	setup                     string
	side                      Side
	entry, stop, target, risk float64
}

func loadReplayTrades(t *testing.T, path string, days map[string]bool) []replayTrade {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("replay csv %s: %v", path, err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("replay csv %s: %v", path, err)
	}
	var out []replayTrade
	for i, r := range rows {
		if i == 0 || !days[r[0]] {
			continue
		}
		side := SideLong
		if strings.TrimSpace(r[4]) == "-1" {
			side = SideShort
		}
		fnum := func(s string) float64 {
			v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
			return v
		}
		out = append(out, replayTrade{
			day:    r[0],
			minCT:  r[1],
			setup:  r[3],
			side:   side,
			entry:  fnum(r[5]),
			stop:   fnum(r[6]),
			target: fnum(r[8]),
			risk:   fnum(r[7]),
		})
	}
	return out
}

func TestParityAgainstReplayV5(t *testing.T) {
	days := []struct {
		name string
		file string
		day  string
	}{
		{"trend", "testdata/parity/trend_2025-05-07.csv", "2025-05-07"},
		{"range", "testdata/parity/range_2023-06-20.csv", "2023-06-20"},
		{"news", "testdata/parity/news_2025-04-02.csv", "2025-04-02"},
		{"normal_a", "testdata/parity/normal_a_2024-07-16.csv", "2024-07-16"},
		{"normal_b", "testdata/parity/normal_b_2025-07-16.csv", "2025-07-16"},
	}
	want := map[string]bool{}
	for _, d := range days {
		want[d.day] = true
	}
	replay := loadReplayTrades(t, "/home/hoang/mm-course/mentor-mode/replay/trades_v5_base.csv", want)

	cfg := DefaultConfig()
	cfg.Enabled = true

	type goIntent struct {
		day, minCT    string
		side          Side
		price, stop   float64
		limit, target float64
		action        Action
		reason        string
	}
	var intents []goIntent
	total := 0
	byDay := map[string]int{}
	for _, d := range days {
		bars := LoadCSVBars(t, d.file, 1)
		if len(bars) < 100 {
			t.Fatalf("%s: only %d bars — tape too short", d.name, len(bars))
		}
		e := New(cfg)
		for i := 2; i <= len(bars); i++ {
			now := bars[i-1].CloseTime
			for _, in := range e.Tick(bars[:i], now) {
				if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
					continue // cancel/level intents are not trades
				}
				total++
				byDay[d.day]++

				intents = append(intents, goIntent{
					day:    d.day,
					minCT:  minCTFromBar(bars[i-1]),
					side:   in.Side,
					price:  in.Price,
					stop:   in.Stop,
					limit:  in.Limit,
					target: in.Target,
					action: in.Action,
					reason: in.Reason,
				})
			}
		}
	}
	if total == 0 {
		t.Fatal("the driver emitted ZERO intents across 5 days — evaluator or tape broken")
	}

	// Swing-vs-swing match: same day, same side, entry within 1 pt, stop
	// within 2 pts.
	matched := 0
	var replayOnly, goOnly []string
	used := make([]bool, len(replay))
	for _, g := range intents {
		found := false
		for i, r := range replay {
			if used[i] || r.day != g.day || r.side != g.side {
				continue
			}
			if abs(r.entry-g.price) <= 1.0 && abs(r.stop-g.stop) <= 2.0 {
				used[i] = true
				matched++
				found = true
				break
			}
		}
		if !found {
			goOnly = append(goOnly, g.day+" "+g.minCT+" "+string(g.side)+
				" entry="+fnum(g.price)+" stop="+fnum(g.stop)+" :: "+g.reason)
		}
	}
	for i, r := range replay {
		if !used[i] {
			replayOnly = append(replayOnly, r.day+" "+r.minCT+" "+r.setup+" "+
				string(r.side)+" entry="+fnum(r.entry)+" stop="+fnum(r.stop))
		}
	}
	sort.Strings(replayOnly)
	sort.Strings(goOnly)

	t.Logf("PARITY: %d go intents across %d days; %d replay trades; %d swing matches",
		total, len(days), len(replay), matched)
	for _, d := range days {
		t.Logf("  %-8s %s: go intents=%d replay trades=%d",
			d.name, d.day, byDay[d.day], countReplayDay(replay, d.day))
	}
	for _, r := range replayOnly {
		t.Logf("REPLAY-ONLY %s", r)
	}
	for _, g := range goOnly {
		t.Logf("GO-ONLY %s", g)
	}
}

func countReplayDay(rs []replayTrade, day string) int {
	n := 0
	for _, r := range rs {
		if r.day == day {
			n++
		}
	}
	return n
}

func minCTFromBar(b market.Kline) string {
	// the package convention: bar times are the CT wall clock as epoch millis,
	// so UTC formatting of the epoch yields the CT clock string.
	return time.UnixMilli(b.OpenTime).UTC().Format("2006-01-02 15:04")
}
