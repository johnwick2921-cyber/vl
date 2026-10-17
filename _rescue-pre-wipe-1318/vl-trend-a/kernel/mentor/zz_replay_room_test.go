package mentor

// DS-104 REPLAY-ROOM-RULE driver (READ-ONLY, temp — never committed). Drives the
// evaluator exactly as DS-105's sliding harness does (Seed once, then Tick with a
// 2000-bar capped window), emitting the 9-field entries_list (for profit_v3
// outcome_v3) AND the full refusals ledger (for the "room" vs
// rr_floor/room_vs_target/rr_confluence counts). Same bars, same defaults, only
// the tree differs.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"vl/market"
)

const rrTickCap = 2000 // kernel.AISVPBarCount — the live Tick slice cap

func TestReplayRoomRule(t *testing.T) {
	path := os.Getenv("REPLAY_BARS_JSON")
	if path == "" {
		path = "/home/hoang/mm-course/mentor-mode/reread/CTO-REREAD-1004/mnq_1m_65sessions.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bars: %v", err)
	}
	var doc struct {
		SeedEnd int         `json:"seed_end"`
		Bars    [][]float64 `json:"bars"`
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
	if os.Getenv("REPLAY_HTF_NEWS_ONLY") == "1" {
		cfg.HTFGateNewsOnly = true // variant D: news-only 4h/1h gate
	}
	seedNow := bars[seedEnd-1].CloseTime + 1
	e := New(cfg)
	missing := Seed(e, bars[:seedEnd], seedNow)

	setupCount := map[string]int{}
	var entriesList [][]any
	var totalEntries int
	refByMin := map[string]map[string]int{}
	htfByMin := map[string]string{}
	dayByMin := map[string]string{}
	prevRefs := map[string]int{}

	for i := seedEnd + 1; i <= len(bars); i++ {
		lo := i - rrTickCap
		if lo < 0 {
			lo = 0
		}
		outs := e.Tick(bars[lo:i], bars[i-1].CloseTime+1)
		d := sessionKeyCT(bars[i-1].OpenTime)
		t0 := bars[i-1].OpenTime
		// per-tick refusal delta, bucketed by CT minute
		min := minuteKeyCT(t0)
		if refByMin[min] == nil {
			refByMin[min] = map[string]int{}
		}
		htfByMin[min] = string(e.State.HTF.FourH.Dir) + "/" + string(e.State.HTF.OneH.Dir)
		dayByMin[min] = map[DayVerdict]string{DayTrade: "DayTrade", DaySpent: "DaySpent", DayOff: "DayOff", DayNotMeasured: "DayNotMeasured"}[e.State.Day.Verdict]
		for k, v := range e.State.Refusals {
			if dlt := v - prevRefs[k]; dlt > 0 {
				refByMin[min][k] += dlt
			}
		}
		prevRefs = make(map[string]int, len(e.State.Refusals))
		for k, v := range e.State.Refusals {
			prevRefs[k] = v
		}
		var nbH, nbL float64
		if i < len(bars) {
			nbH, nbL = bars[i].High, bars[i].Low
		}
		for _, in := range outs {
			if in.Action == PlaceStopEntry || in.Action == PlaceStopLimitEntry {
				totalEntries++
				s := in.Setup
				if strings.HasPrefix(in.Reason, "reverse ISB at EMA 9") {
					s = "reverse"
				} else if s == "PLH" {
					s = "PHL"
				}
				setupCount[s]++
				entriesList = append(entriesList, []any{s, string(in.Side), in.Price, in.Stop, in.Target, d, t0, nbH, nbL, in.Confluence})
			}
		}
	}

	refs := make(map[string]int, len(e.State.Refusals))
	for k, v := range e.State.Refusals {
		refs[k] = v
	}

	out := map[string]any{
		"total_entries":    totalEntries,
		"entries_by_setup": setupCount,
		"entries_list":     entriesList,
		"all_refusals":     refs,
		"refusals_by_minute": refByMin,
		"htf_by_minute": htfByMin,
		"day_by_minute": dayByMin,
		"day_recheck": map[string]any{
			"key":          e.State.DayRecheck.Key,
			"cleared_dir":  string(e.State.DayRecheck.ClearedDir),
			"cleared_at":   e.State.DayRecheck.ClearedAt,
			"clears":       e.State.DayRecheck.Clears,
			"last_1h_close": e.State.DayRecheck.Last1HClose,
		},
		"seed_missing":     missing,
	}
	b, _ := json.Marshal(out)
	fmt.Println("REPLAY_STATS_JSON=" + string(b))
	t.Logf("room replay: %d ticks, %d entries, refusals=%d keys",
		len(bars)-seedEnd, totalEntries, len(refs))
}

// minuteKeyCT buckets a bar open time to its CT minute "MM-DD HH:MM".
func minuteKeyCT(ms int64) string {
	return time.UnixMilli(ms).In(ctime()).Format("01-02 15:04")
}
