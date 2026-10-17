package mentor

// DS-104 REL10-FULLHISTORY-RESCORE driver (READ-ONLY, scratch worktree — never
// committed). Faithful ONE-SCALE re-score: the IN-period tick series is built
// from the DB copy itself, exactly as production sees it — MNQ 09-26 back-
// adjusted by the stitch's own measured gap (+293.00) up to its last stored bar
// (2026-09-14 10:33 CT), then MNQ 12-26 RAW strictly after that instant. Both
// seed variants run on that same one-scale series at 2026-08-21 16:00 CT; the
// replay window 2026-08-24→10-02 ticks with the sliding 2000-bar window.
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

	"vl/market"
	"vl/store"
)

const rescoreTickCap = 2000
const rescoreGap = 293.0 // the stitch's own measured gap 09-26→12-26 @2026-09-11

func TestReplayFaithfulScale(t *testing.T) {
	mode := os.Getenv("REPLAY_MODE")
	if mode == "" {
		mode = "plain"
	}
	dbPath := os.Getenv("REPLAY_DB")
	if dbPath == "" {
		dbPath = "/home/hoang/vl-fullhist/data-copy.db"
	}

	db, err := store.InitGorm(dbPath)
	if err != nil {
		t.Fatalf("open db copy: %v", err)
	}
	bh := store.NewBarHistoryStore(db)

	rows09, err := bh.AllBarsOn("MNQ", "1m", "MNQ 09-26")
	if err != nil || len(rows09) == 0 {
		t.Fatalf("09-26 1m read: err=%v n=%d", err, len(rows09))
	}
	bars09 := fullhistRowsToKlines(rows09, 60_000)
	for i := range bars09 {
		bars09[i].Open += rescoreGap
		bars09[i].High += rescoreGap
		bars09[i].Low += rescoreGap
		bars09[i].Close += rescoreGap
	}

	rows12, err := bh.AllBarsOn("MNQ", "1m", "MNQ 12-26")
	if err != nil || len(rows12) == 0 {
		t.Fatalf("12-26 1m read: err=%v n=%d", err, len(rows12))
	}
	bars12 := fullhistRowsToKlines(rows12, 60_000)

	last09 := int64(1789399980000) // 09-26's last stored bar (2026-09-14 10:33 CT)
	series := append([]market.Kline{}, bars09...)
	for _, b := range bars12 {
		if b.OpenTime > last09 {
			series = append(series, b)
		}
	}

	seedInst := int64(1787346000000) // 2026-08-21 16:00 CT
	seedEnd := sort.Search(len(series), func(i int) bool { return series[i].OpenTime > seedInst })
	if seedEnd == 0 || seedEnd >= len(series) {
		t.Fatalf("seed instant not found: seedEnd=%d len=%d", seedEnd, len(series))
	}
	seedNow := series[seedEnd-1].CloseTime + 1

	// Write the one-scale series for the scorer (same [t,o,h,l,c] shape
	// profit_v3.load_bars reads from the 65-session file).
	if p := os.Getenv("SERIES_OUT"); p != "" {
		rows := make([][5]float64, 0, len(series))
		for _, b := range series {
			rows = append(rows, [5]float64{float64(b.OpenTime), b.Open, b.High, b.Low, b.Close})
		}
		var doc struct {
			Bars [][5]float64 `json:"bars"`
		}
		doc.Bars = rows
		if b, err := json.Marshal(doc); err == nil {
			_ = os.WriteFile(p, b, 0o644)
		}
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	e := New(cfg)

	var missing []string
	var gaps []RollGap
	stoppedAt := ""

	if mode == "full" {
		_, stitched1m, full1HRTH, g, stop := fullhistContracts(t, dbPath)
		gaps, stoppedAt = g, stop
		stitched1m = fullhistCutAt(stitched1m, seedNow)
		full1HRTH = fullhistCutAt(full1HRTH, seedNow)
		missing = SeedFull(e, stitched1m, full1HRTH, seedNow)
	} else {
		lo := seedEnd - 50000
		if lo < 0 {
			lo = 0
		}
		missing = Seed(e, series[lo:seedEnd], seedNow)
	}

	levelsCount := len(e.State.SeedLevels)
	levelsList := make([][]any, 0, levelsCount)
	for _, l := range e.State.SeedLevels {
		levelsList = append(levelsList, []any{l.Key, string(l.Kind), l.Price, l.AtTime})
	}
	seed1hCount := len(e.State.Seed1HBars)

	perDay := map[string]*dayStat{}
	setupCount := map[string]int{}
	actionCount := map[string]int{}
	var entriesList [][]any
	var totalIntents, totalEntries int

	for i := seedEnd + 1; i <= len(series); i++ {
		lo := i - rescoreTickCap
		if lo < 0 {
			lo = 0
		}
		outs := e.Tick(series[lo:i], series[i-1].CloseTime+1)
		d := sessionKeyCT(series[i-1].OpenTime)
		t0 := series[i-1].OpenTime
		var nbH, nbL float64
		if i < len(series) {
			nbH, nbL = series[i].High, series[i].Low
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
		"series_bars":      len(series),
		"seed_end_index":   seedEnd,
		"seed_now":         seedNow,
		"days":             len(days),
		"seed_missing":     missing,
		"levels_count":     levelsCount,
		"levels_list":      levelsList,
		"seed_1h_count":    seed1hCount,
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
	t.Logf("faithful mode=%s: series=%d, seedEnd=%d, %d ticks, %d intents, %d entries, levels=%d, missing=%v",
		mode, len(series), seedEnd, len(series)-seedEnd, totalIntents, totalEntries, levelsCount, missing)
}

type dayStat struct {
	intents int
	entries int
}
