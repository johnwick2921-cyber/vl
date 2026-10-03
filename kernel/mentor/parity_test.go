package mentor

// Parity check vs DS-108's replay (CTO 2026-10-03): drive the Go evaluator
// minute by minute over 5 recorded days (trend / range / news / two normal)
// and compare every emitted intent with the replay's trades for the same days.
//
// Compared against trades_v5_nolimits.csv for now: trades_v5_base.csv currently
// has 0 intraday trades (a DS-108 daily-limits bug being fixed). The Go side
// therefore runs with the daily limits OFF (window, done-after-win) too. When
// DS-108 posts the fixed v5_base, re-point this test at it and turn the limits
// back on.
//
// The replay records FILLS (with A/B/C exits); the evaluator emits RESTING
// arms. The match rule is the closest arm per replay trade (same day, same
// side, entry within 1 pt, stop within 2 pts). Rules DS-103 has not built yet
// (EMA34-off, R6, R10, R11, leg budget) are tagged "not yet built".
//
// REPORT-style: the full table is logged; the test fails only when the driver
// itself produced nothing (a broken tape or evaluator), never on a mismatch —
// mismatches are routed through the CTO.

import (
	"encoding/csv"
	"encoding/json"
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

// Warm-up seam (CTO P0 plan, point 1 and 3): one constant per source. The
// binding numbers are DS-103's Seed PR; until the Seed API lands, the ticked
// history prefix is the proxy for all of them and the test only checks the
// aggregate depth. When the Seed lands, replace the tick warm-up below with a
// Seed call and move these per-source values into the seed inputs.
const (
	warmupHistoryDays  = 3 // distinct prefix days currently extracted into each tape
	warmup1mBarsPerDay = 1440

	// Per-source requirements (documented, not yet individually enforced —
	// enforced together via warmupHistoryDays):
	warmupFourHEMA34Bars = 34   // 4h EMA34, derived from 1h, 17:00 CT anchor
	warmupOneMEMA34Bars  = 34   // 1m EMA34
	warmupOneHRTHAll     = true // 1H RTH level set: FULL stored history, no cap (Seed-owned)
	warmupSessionDays    = 1    // today's session for boxes / ORB / day latch
	warmupClosed15mBars  = 1
)

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
	replay := loadReplayTrades(t, "/home/hoang/mm-course/mentor-mode/replay/trades_v5_nolimits.csv", want)

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
	reasonCounts := map[string]int{}
	for _, d := range days {
		bars := LoadCSVBars(t, d.file, 1)
		// Warm-up depth check: the tape must carry at least warmupHistoryDays
		// distinct calendar days BEFORE the first target-day bar (the first
		// prefix day is typically a partial globex session), or the evaluator
		// runs on a cold prefix (cold EMA / truncated levels) and the
		// comparison is meaningless. Fail-closed, like the P0 requires of the
		// live driver. DS-103's Seed PR replaces this aggregate check with the
		// per-source numbers above.
		firstTarget := -1
		for i, b := range bars {
			if time.UnixMilli(b.OpenTime).UTC().Format("2006-01-02") == d.day {
				firstTarget = i
				break
			}
		}
		if firstTarget < 0 {
			t.Fatalf("%s: no target-day bars in the tape", d.name)
		}
		prefixDays := map[string]bool{}
		for i := 0; i < firstTarget; i++ {
			prefixDays[time.UnixMilli(bars[i].OpenTime).UTC().Format("2006-01-02")] = true
		}
		if len(prefixDays) < warmupHistoryDays {
			t.Fatalf("%s: only %d distinct warm-up days before %s; need %d (cold EMA / truncated levels)",
				d.name, len(prefixDays), d.day, warmupHistoryDays)
		}
		t.Logf("WARMUP %s: %d bars / %d days before %s", d.name, firstTarget, len(prefixDays), d.day)
		e := New(cfg)
		for i := 2; i <= len(bars); i++ {
			now := bars[i-1].CloseTime
			// Tick sees the full history; only the TARGET day is recorded (the
			// prefix is warm-up per the CTO's routing).
			if time.UnixMilli(bars[i-1].OpenTime).UTC().Format("2006-01-02") != d.day {
				for _, in := range e.Tick(bars[:i], now) {
					reasonCounts[in.Reason]++
				}
				continue
			}
			for _, in := range e.Tick(bars[:i], now) {
				reasonCounts[in.Reason]++
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
		// Per-day evaluator state at the end of the target day: live ISB arms,
		// the HTF latch and the day gate (refusals are mostly fail-closed
		// latches, so this is how a silent day explains itself).
		t.Logf("STATE %-8s %s: isb_arms=%d htf=%s day=%s",
			d.name, d.day, len(e.State.ISBArms),
			mustJSON(e.State.HTF), mustJSON(e.State.Day))
	}
	if total == 0 {
		t.Fatal("the driver emitted ZERO intents across 5 days — evaluator or tape broken")
	}
	type rc struct {
		k string
		n int
	}
	var ranks []rc
	for k, n := range reasonCounts {
		ranks = append(ranks, rc{k, n})
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i].n > ranks[j].n })
	for _, r := range ranks {
		if r.n > 0 {
			t.Logf("REASON %4d x %s", r.n, r.k)
		}
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

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(b)
}
