package mentor

// DS-104 REL10-FULLHISTORY-REPLAY driver (READ-ONLY, scratch worktree — never
// committed). Faithful to DS-105's sliding harness (zz_replay_stack_test.go),
// with one addition: a FULL-HISTORY variant that builds the stitched key-level
// history exactly as production does (store.BarHistoryStore → NewRollStitcher,
// the same path trader/mentor_tick.go mentorFullKeyLevelHistory walks), cuts it
// at the seed instant, and calls SeedFull — compared against plain Seed() with
// everything else identical.
//
// Run with REPLAY_MODE=plain|full (default plain). Emits REPLAY_STATS_JSON= so
// the validated profit_v3/v4 scorer consumes the same marker.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
	"vl/store"
)

const fullhistTickCap = 2000 // kernel.AISVPBarCount — the live Tick slice cap

func fullhistRowsToKlines(rows []store.BarHistoryDB, tfMs int64) []market.Kline {
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		out = append(out, market.Kline{
			OpenTime:  r.OpenTimeMs,
			Open:      r.O,
			High:      r.H,
			Low:       r.L,
			Close:     r.C,
			Volume:    r.V,
			CloseTime: r.OpenTimeMs + tfMs - 1,
		})
	}
	return out
}

func fullhistCutAt(series []market.Kline, now int64) []market.Kline {
	out := make([]market.Kline, 0, len(series))
	for _, b := range series {
		if b.CloseTime <= now {
			out = append(out, b)
		}
	}
	return out
}

// fullhistContracts mirrors trader.AutoTrader.mentorFullKeyLevelHistory: read
// contracts newest→oldest, stop at the first unreadable span (FIX 5).
func fullhistContracts(t *testing.T, dbPath string) (contracts []Contract1M, stitched1m, full1HRTH []market.Kline, gaps []RollGap, stoppedAt string) {
	db, err := store.InitGorm(dbPath)
	if err != nil {
		t.Fatalf("open db copy: %v", err)
	}
	bh := store.NewBarHistoryStore(db)
	spans, err := bh.ContractSpans("MNQ", "1m")
	if err != nil || len(spans) == 0 {
		t.Fatalf("contract spans: err=%v n=%d", err, len(spans))
	}
	read := func(contract string) (Contract1M, bool) {
		rows, err := bh.AllBarsOn("MNQ", "1m", contract)
		if err != nil || len(rows) == 0 {
			return Contract1M{}, false
		}
		c := Contract1M{Contract: contract, Bars: fullhistRowsToKlines(rows, 60_000)}
		if rows1h, err := bh.AllBarsOn("MNQ", "1h", contract); err == nil {
			c.Bars1H = fullhistRowsToKlines(rows1h, 3600_000)
		}
		return c, true
	}
	newest, ok := read(spans[len(spans)-1].Contract)
	if !ok {
		t.Fatalf("newest contract unreadable: %s", spans[len(spans)-1].Contract)
	}
	s := NewRollStitcher(newest)
	contracts = append(contracts, newest)
	for i := len(spans) - 2; i >= 0; i-- {
		c, ok := read(spans[i].Contract)
		if !ok {
			break
		}
		if !s.Add(c) {
			break
		}
		contracts = append(contracts, c)
	}
	stitched1m, full1HRTH, gaps, stoppedAt = s.Result()
	return
}

func TestReplayFullHistStats(t *testing.T) {
	path := os.Getenv("REPLAY_BARS_JSON")
	if path == "" {
		path = "/home/hoang/mm-course/mentor-mode/reread/CTO-REREAD-1004/mnq_1m_65sessions.json"
	}
	mode := os.Getenv("REPLAY_MODE")
	if mode == "" {
		mode = "plain"
	}
	dbPath := os.Getenv("REPLAY_DB")
	if dbPath == "" {
		dbPath = "/home/hoang/vl-fullhist/data-copy.db"
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bars: %v", err)
	}
	var doc struct {
		SeedEnd     int         `json:"seed_end"`
		SeedFirst   string      `json:"seed_first"`
		SeedLast    string      `json:"seed_last"`
		ReplayFirst string      `json:"replay_first"`
		ReplayLast  string      `json:"replay_last"`
		Bars        [][]float64 `json:"bars"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse bars: %v", err)
	}
	bars := make([]market.Kline, 0, len(doc.Bars))
	for _, b := range doc.Bars {
		bars = append(bars, market.Kline{
			OpenTime:  int64(b[0]),
			Open:      b[1],
			High:      b[2],
			Low:       b[3],
			Close:     b[4],
			CloseTime: int64(b[0]) + 59_000,
		})
	}
	seedEnd := doc.SeedEnd
	if seedEnd <= 0 || seedEnd >= len(bars) {
		seedEnd = len(bars) - 1
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	seedNow := bars[seedEnd-1].CloseTime + 1
	e := New(cfg)

	var missing []string
	levelsCount := 0
	var gaps []RollGap
	stoppedAt := ""
	stitchedFrom := ""

	switch mode {
	case "full":
		var stitched1m, full1HRTH []market.Kline
		var contracts []Contract1M
		contracts, stitched1m, full1HRTH, gaps, stoppedAt = fullhistContracts(t, dbPath)
		_ = contracts
		if len(stitched1m) > 0 {
			stitchedFrom = fmt.Sprintf("%s..%s",
				time.UnixMilli(stitched1m[0].OpenTime).In(ctime()).Format("2006-01-02"),
				time.UnixMilli(stitched1m[len(stitched1m)-1].OpenTime).In(ctime()).Format("2006-01-02"))
		}
		stitched1m = fullhistCutAt(stitched1m, seedNow)
		full1HRTH = fullhistCutAt(full1HRTH, seedNow)
		missing = SeedFull(e, stitched1m, full1HRTH, seedNow)
	default: // plain
		missing = Seed(e, bars[:seedEnd], seedNow)
	}
	levelsCount = len(e.State.SeedLevels)
	levelsList := make([][]any, 0, levelsCount)
	for _, l := range e.State.SeedLevels {
		levelsList = append(levelsList, []any{l.Key, string(l.Kind), l.Price, l.AtTime})
	}
	seed1hCount := len(e.State.Seed1HBars)

	type dayStat struct {
		intents int
		entries int
	}
	perDay := map[string]*dayStat{}
	setupCount := map[string]int{}
	actionCount := map[string]int{}
	var entriesList [][]any
	var totalIntents, totalEntries int

	for i := seedEnd + 1; i <= len(bars); i++ {
		lo := i - fullhistTickCap
		if lo < 0 {
			lo = 0
		}
		outs := e.Tick(bars[lo:i], bars[i-1].CloseTime+1)
		d := sessionKeyCT(bars[i-1].OpenTime)
		t0 := bars[i-1].OpenTime
		var nbH, nbL float64
		if i < len(bars) {
			nbH, nbL = bars[i].High, bars[i].Low
		}
		ds := perDay[d]
		if ds == nil {
			ds = &dayStat{}
			perDay[d] = ds
		}
		for _, in := range outs {
			totalIntents++
			ds.intents++
			actionCount[string(in.Action)]++
			if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
				totalEntries++
				ds.entries++
				s := in.Setup
				if strings.HasPrefix(in.Reason, "reverse ISB at EMA 9") {
					s = "reverse"
				} else if s == "PLH" {
					s = "PHL"
				}
				setupCount[s]++
				// 9-field entry: setup, side, kernel, stop, target, day, t0,
				// next-bar high, next-bar low — the exact shape profit_v3.py's
				// outcome_v3 consumes (entry[:9]).
				entriesList = append(entriesList, []any{s, string(in.Side), in.Price, in.Stop, in.Target, d, t0, nbH, nbL})
			}
		}
	}

	days := make([]string, 0, len(perDay))
	for d := range perDay {
		days = append(days, d)
	}
	sort.Strings(days)

	out := map[string]any{
		"mode":             mode,
		"seed_first":       doc.SeedFirst,
		"seed_last":        doc.SeedLast,
		"replay_first":     doc.ReplayFirst,
		"replay_last":      doc.ReplayLast,
		"bars":             len(bars),
		"days":             len(days),
		"seed_missing":     missing,
		"levels_count":     levelsCount,
		"levels_list":      levelsList,
		"seed_1h_count":    seed1hCount,
		"stitched_from":    stitchedFrom,
		"stitch_stopped":   stoppedAt,
		"gaps":             gaps,
		"total_intents":    totalIntents,
		"total_entries":    totalEntries,
		"entries_by_setup": setupCount,
		"entries_list":     entriesList,
		"intent_actions":   actionCount,
	}
	b, _ := json.Marshal(out)
	fmt.Println("REPLAY_STATS_JSON=" + string(b))
	t.Logf("mode=%s: %d ticks, %d intents, %d entries, levels=%d, missing=%v, stitch=%s",
		mode, len(bars)-seedEnd, totalIntents, totalEntries, levelsCount, missing, stoppedAt)
}
