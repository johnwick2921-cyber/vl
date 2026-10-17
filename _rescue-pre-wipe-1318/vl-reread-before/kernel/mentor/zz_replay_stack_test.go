package mentor

// DS-105 CTO-REREAD-1004 replay stack driver (READ-ONLY analysis, temp file —
// never committed). Faithful LIVE pattern: Seed with the full warm-up history
// (35 sessions), then Tick forward through the 30-session replay window with a
// 2000-bar capped slice (kernel.AISVPBarCount), exactly as trader/mentor_tick.go
// drives the evaluator. Prints per-day intents, setup breakdown and the refusal
// ledger.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"vl/market"
)

const liveTickCap = 2000 // kernel.AISVPBarCount — the live Tick slice cap

func TestReplayStackStats(t *testing.T) {
	path := os.Getenv("REPLAY_BARS_JSON")
	if path == "" {
		path = "/home/hoang/mm-course/mentor-mode/reread/CTO-REREAD-1004/mnq_1m_65sessions.json"
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
	missing := Seed(e, bars[:seedEnd], seedNow)

	type dayStat struct {
		intents int
		entries int
	}
	perDay := map[string]*dayStat{}
	setupCount := map[string]int{}
	actionCount := map[string]int{}
	invKinds := map[string]int{}
	cancelKinds := map[string]int{}
	refusalByDay := map[string]map[string]int{}
	var entriesList [][]any
	var totalIntents, totalEntries int

	kindOf := func(key string) string {
		if i := strings.IndexByte(key, ':'); i >= 0 {
			return key[:i]
		}
		return key
	}
	snapRefusals := func() map[string]int {
		c := make(map[string]int, len(e.State.Refusals))
		for k, v := range e.State.Refusals {
			c[k] = v
		}
		return c
	}

	for i := seedEnd + 1; i <= len(bars); i++ {
		lo := i - liveTickCap
		if lo < 0 {
			lo = 0
		}
		outs := e.Tick(bars[lo:i], bars[i-1].CloseTime+1)
		d := sessionKeyCT(bars[i-1].OpenTime)
		ds := perDay[d]
		if ds == nil {
			ds = &dayStat{}
			perDay[d] = ds
		}
		for _, in := range outs {
			totalIntents++
			ds.intents++
			actionCount[string(in.Action)]++
			if in.Action == LevelInvalid {
				invKinds[kindOf(in.LevelKey)]++
			}
			if in.Action == CancelArm {
				if in.LevelKey != "" {
					cancelKinds[kindOf(in.LevelKey)]++
				} else {
					cancelKinds["(no level key)"]++
				}
			}
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
				entriesList = append(entriesList, []any{s, string(in.Side), in.Price, in.Stop, in.Target, d})
			}
		}
		refusalByDay[d] = snapRefusals()
	}

	// per-day summary
	days := make([]string, 0, len(perDay))
	for d := range perDay {
		days = append(days, d)
	}
	sort.Strings(days)
	intentsVals := make([]int, 0, len(days))
	entriesVals := make([]int, 0, len(days))
	var zeroEntryDays []string
	for _, d := range days {
		ds := perDay[d]
		intentsVals = append(intentsVals, ds.intents)
		entriesVals = append(entriesVals, ds.entries)
		if ds.entries == 0 {
			zeroEntryDays = append(zeroEntryDays, d)
		}
	}
	meanOf := func(v []int) float64 {
		if len(v) == 0 {
			return 0
		}
		var s int
		for _, x := range v {
			s += x
		}
		return float64(s) / float64(len(v))
	}
	minOf := func(v []int) int {
		if len(v) == 0 {
			return 0
		}
		m := v[0]
		for _, x := range v[1:] {
			if x < m {
				m = x
			}
		}
		return m
	}
	maxOf := func(v []int) int {
		if len(v) == 0 {
			return 0
		}
		m := v[0]
		for _, x := range v[1:] {
			if x > m {
				m = x
			}
		}
		return m
	}

	// refusal ledger — full sorted list + top-10
	type kv struct {
		K string
		V int
	}
	var refs []kv
	for k, v := range e.State.Refusals {
		refs = append(refs, kv{k, v})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].V != refs[j].V {
			return refs[i].V > refs[j].V
		}
		return refs[i].K < refs[j].K
	})
	topRefs := refs
	if len(topRefs) > 10 {
		topRefs = topRefs[:10]
	}

	// per-day refusal deltas for the zero-entry days.
	type zkv struct {
		K string
		V int
	}
	zeroDayRefusals := map[string][]zkv{}
	prev := map[string]int{}
	for _, d := range days {
		cur := refusalByDay[d]
		var delta []zkv
		for k, v := range cur {
			if v > prev[k] {
				delta = append(delta, zkv{k, v - prev[k]})
			}
		}
		sort.Slice(delta, func(i, j int) bool {
			if delta[i].V != delta[j].V {
				return delta[i].V > delta[j].V
			}
			return delta[i].K < delta[j].K
		})
		for _, zd := range zeroEntryDays {
			if d == zd {
				zeroDayRefusals[d] = delta
			}
		}
		prev = cur
	}

	out := map[string]any{
		"seed_first":       doc.SeedFirst,
		"seed_last":        doc.SeedLast,
		"replay_first":     doc.ReplayFirst,
		"replay_last":      doc.ReplayLast,
		"bars":             len(bars),
		"days":             len(days),
		"seed_missing":     missing,
		"total_intents":    totalIntents,
		"total_entries":    totalEntries,
		"intents_per_day":  map[string]any{"mean": meanOf(intentsVals), "min": minOf(intentsVals), "max": maxOf(intentsVals)},
		"entries_per_day":  map[string]any{"mean": meanOf(entriesVals), "min": minOf(entriesVals), "max": maxOf(entriesVals)},
		"entries_by_setup": setupCount,
		"entries_list":     entriesList,
		"intent_actions":   actionCount,
		"level_invalid_kinds": invKinds,
		"cancel_arm_kinds": cancelKinds,
		"zero_entry_days":  zeroEntryDays,
		"zero_day_refusals": zeroDayRefusals,
		"top_refusals":     topRefs,
		"all_refusals":     refs,
	}
	b, _ := json.Marshal(out)
	fmt.Println("REPLAY_STATS_JSON=" + string(b))
	t.Logf("replay done: %d ticks, %d intents, %d entries, %d days, seed_missing=%v",
		len(bars)-seedEnd, totalIntents, totalEntries, len(days), missing)
}
