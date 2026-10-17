package mentor

// DS-105 REL10-SEED-HTF-REPLAY harness (READ-ONLY analysis, temp file — never
// committed). Drives the evaluator as trader/mentor_tick.go does.
//
// Variant (a) "once": Seed once at the start, then Tick forward with a 2000-bar
// capped rolling window (kernel.AISVPBarCount), now = bar.CloseTime + 1.
//
// Variant (b) "reseed": boot at the start of EVERY 17:00 CT session — a fresh
// New()+Seed with ALL history up to that instant (the persisted store), then
// tick that session with a GROWING window from the session's first bar (the
// BarCache fills from the boot instant). This models a daily process boot.
//
// Fill/R uses the first-touch exit model (fork-B, mirror of outcome_first_touch
// .py): wire = kernel ± 0.5, R = |wire−stop|, fill on the NEXT bar, two 50/50
// legs (leg1 at 1:1; runner to target/breakeven, stop-first same-bar).

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

type entryRec struct {
	Setup  string
	Side   string
	Price  float64
	Stop   float64
	Target float64
	Day    string
	T0     int64
}

type outcomeRec struct {
	Kind   string
	R      float64
	Filled bool
}

type dayStatT struct{ intents, entries int }

type kvp struct {
	K string
	V int
}

func TestReplayStackStats(t *testing.T) {
	path := os.Getenv("REPLAY_BARS_JSON")
	if path == "" {
		path = "/home/hoang/mm-course/mentor-mode/reread/CTO-REREAD-1004/mnq_1m_65sessions.json"
	}
	variant := os.Getenv("REPLAY_VARIANT")
	if variant == "" {
		variant = "once"
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

	barIdx := make(map[int64]int, len(bars))
	for i, b := range bars {
		barIdx[b.OpenTime] = i
	}

	var (
		perDay       = map[string]*dayStatT{}
		setupCount   = map[string]int{}
		actionCount  = map[string]int{}
		refusalTot   = map[string]int{}
		entriesList  []entryRec
		totalIntents int
		totalEntries int
		reboots      int
	)

	record := func(i int, outs []Intent) {
		cur := bars[i-1]
		d := sessionKeyCT(cur.OpenTime)
		ds := perDay[d]
		if ds == nil {
			ds = &dayStatT{}
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
				entriesList = append(entriesList, entryRec{
					Setup: s, Side: string(in.Side), Price: in.Price,
					Stop: in.Stop, Target: in.Target, Day: d, T0: cur.OpenTime,
				})
			}
		}
	}

	if variant == "once" {
		seedNow := bars[seedEnd-1].CloseTime + 1
		e := New(cfg)
		Seed(e, bars[:seedEnd], seedNow)
		for i := seedEnd + 1; i <= len(bars); i++ {
			lo := i - liveTickCap
			if lo < 0 {
				lo = 0
			}
			outs := e.Tick(bars[lo:i], bars[i-1].CloseTime+1)
			record(i, outs)
		}
		for k, v := range e.State.Refusals {
			refusalTot[k] = v
		}
	} else {
		// reseed: boot at every 17:00 CT session boundary; tick the session
		// with a growing window (the BarCache fills from the boot instant).
		starts := []int{seedEnd}
		for i := seedEnd + 1; i < len(bars); i++ {
			if sessionKeyCT(bars[i].OpenTime) != sessionKeyCT(bars[i-1].OpenTime) {
				starts = append(starts, i)
			}
		}
		for k := 0; k < len(starts); k++ {
			s := starts[k]
			eNext := len(bars)
			if k+1 < len(starts) {
				eNext = starts[k+1]
			}
			e := New(cfg)
			Seed(e, bars[:s], bars[s].OpenTime)
			reboots++
			for j := s + 1; j <= eNext; j++ {
				outs := e.Tick(bars[s:j], bars[j-1].CloseTime+1)
				record(j, outs)
			}
			for kk, v := range e.State.Refusals {
				refusalTot[kk] = v
			}
		}
	}

	const off = 0.5
	outcomes := make([]outcomeRec, len(entriesList))
	fills := 0
	for idx, en := range entriesList {
		o := firstTouchOutcome(en, off, bars, barIdx)
		outcomes[idx] = o
		if o.Filled {
			fills++
		}
	}
	totalR := 0.0
	bySetupR := map[string]float64{}
	for idx, en := range entriesList {
		totalR += outcomes[idx].R
		bySetupR[en.Setup] += outcomes[idx].R
	}

	var refs []kvp
	for k, v := range refusalTot {
		refs = append(refs, kvp{k, v})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].V != refs[j].V {
			return refs[i].V > refs[j].V
		}
		return refs[i].K < refs[j].K
	})
	htfFamily := map[string]int{}
	for _, k := range []string{"isb_htf_blocked", "isbrev_htf_blocked", "box_htf_blocked", "phl_htf_blocked"} {
		htfFamily[k] = refusalTot[k]
	}

	entriesDump := make([][]any, 0, len(entriesList))
	for idx, en := range entriesList {
		entriesDump = append(entriesDump, []any{
			en.Setup, en.Side, en.Price, en.Stop, en.Target, en.Day, en.T0,
			outcomes[idx].Kind, round2(outcomes[idx].R), outcomes[idx].Filled,
		})
	}

	out := map[string]any{
		"variant":         variant,
		"seed_first":      doc.SeedFirst,
		"seed_last":       doc.SeedLast,
		"replay_first":    doc.ReplayFirst,
		"replay_last":     doc.ReplayLast,
		"bars":            len(bars),
		"seed_end":        seedEnd,
		"days":            len(perDay),
		"reboots":         reboots,
		"total_intents":   totalIntents,
		"total_entries":   totalEntries,
		"fills":           fills,
		"total_r":         round2(totalR),
		"entries_by_setup": setupCount,
		"r_by_setup":      bySetupR,
		"entries_list":    entriesDump,
		"htf_family":      htfFamily,
		"top_refusals":    refs,
	}
	b, _ := json.Marshal(out)
	outStr := "REPLAY_STATS_JSON=" + string(b)
	if p := os.Getenv("REPLAY_OUT"); p != "" {
		_ = os.WriteFile(p, []byte(outStr+"\n"), 0o644)
	}
	fmt.Println(outStr)
	t.Logf("replay done: variant=%s entries=%d fills=%d R=%.2f reboots=%d",
		variant, totalEntries, fills, totalR, reboots)
}

func round2(x float64) float64 {
	return float64(int(x*100+0.5)) / 100
}

func firstTouchOutcome(en entryRec, off float64, bars []market.Kline, barIdx map[int64]int) outcomeRec {
	side := en.Side
	wire := en.Price + off
	if side == "short" {
		wire = en.Price - off
	}
	R := absFloat(wire - en.Stop)
	if R <= 0 {
		return outcomeRec{"degenerate", 0, false}
	}
	targetRR := absFloat(en.Target-wire) / R
	idx := barIdx[en.T0]
	if idx < 0 || idx+1 >= len(bars) {
		return outcomeRec{"open", 0, true}
	}
	nb := bars[idx+1]
	filled := false
	if side == "long" {
		filled = nb.High >= wire
	} else {
		filled = nb.Low <= wire
	}
	if !filled {
		return outcomeRec{"no-fill", 0, false}
	}
	T1 := wire + R
	BE := wire
	if side == "short" {
		T1 = wire - R
	}
	leg1Done := false
	leg1 := 0.0
	runner := 0.0
	for j := idx + 1; j < len(bars); j++ {
		h, l := bars[j].High, bars[j].Low
		if side == "long" {
			if !leg1Done {
				if l <= en.Stop {
					return outcomeRec{"stop", -1.0, true}
				}
				if h >= T1 {
					leg1Done = true
					leg1 = 1.0
					if l <= BE {
						runner = 0
						break
					}
					if h >= en.Target {
						runner = targetRR
						break
					}
				}
			} else {
				if l <= BE {
					runner = 0
					break
				}
				if h >= en.Target {
					runner = targetRR
					break
				}
			}
		} else {
			if !leg1Done {
				if h >= en.Stop {
					return outcomeRec{"stop", -1.0, true}
				}
				if l <= T1 {
					leg1Done = true
					leg1 = 1.0
					if h >= BE {
						runner = 0
						break
					}
					if l <= en.Target {
						runner = targetRR
						break
					}
				}
			} else {
				if h >= BE {
					runner = 0
					break
				}
				if l <= en.Target {
					runner = targetRR
					break
				}
			}
		}
	}
	if leg1Done {
		return outcomeRec{"ok", 0.5*leg1 + 0.5*runner, true}
	}
	return outcomeRec{"open", 0, true}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
