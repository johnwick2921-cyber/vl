//go:build s2replay

// Command s2-replay reproduces the 2026-09-16 16:31 CT "🗺️ G2 HTF detection"
// level set from the PERSISTED NT8 bar history (read-only) and grades each
// level two ways:
//
//	OFF — the W7 persisted level_state 1m-touch grade (AgedFreshness), the
//	      production path today;
//	ON  — kernel.LevelFreshnessByTF, the S2 grader on the level's OWN TF bars.
//
// This is the read-only replay table the S4 measurement gate consumes. The
// live detection ran over the in-memory ring; this replay runs over the store's
// bar_history for the same contract — a close proxy, not a byte-identical
// reproduction (deltas vs the log's 244 are reported, never hidden).
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vl/kernel"
	"vl/market"
	"vl/store"
	"vl/store/sqlitedriver" // the ONE sqlite registration site (DS-102 fold, CTO 1790305899255)
)

const (
	dbPath     = "/home/hoang/nofx/data/data.db"
	symbol     = "MNQ"
	traderID   = "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"
	levelDate  = "2026-09-16"
	nowHourMin = "16:31"
)

func main() {
	now := mustTime(time.Parse("2006-01-02 15:04", levelDate+" "+nowHourMin))
	dsn := fmt.Sprintf("file:%s?mode=ro", dbPath)
	db, err := gorm.Open(sqlitedriver.GormDialector(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		panic(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	st, err := store.NewFromGorm(db)
	if err != nil {
		panic(err)
	}
	bh := st.BarHistory()
	contract, ok := bh.LatestContract(symbol)
	if !ok || contract == "" {
		fmt.Fprintf(os.Stderr, "no latest contract (ok=%v)\n", ok)
		os.Exit(1)
	}
	fmt.Printf("# S2 replay — %s %s CT · contract=%s · detector TFs=%v\n", levelDate, nowHourMin, contract, kernel.DefaultHTFDetectionTFs)

	// Per-TF persisted bars: current contract first, then the PREVIOUS
	// contract's deeper series as the historical leg (the same combination the
	// nPOC provider uses). In the overlap window the current contract wins.
	tfBars := map[string][]market.Kline{}
	contracts := []string{contract}
	var secondNewest string
	var ranked []struct{ Contract string }
	if err := db.Raw(`SELECT contract FROM bars WHERE symbol=? GROUP BY contract ORDER BY MAX(open_time_ms) DESC LIMIT 2`, symbol).
		Scan(&ranked).Error; err == nil && len(ranked) == 2 {
		secondNewest = ranked[1].Contract
	}
	if secondNewest != "" {
		contracts = append(contracts, secondNewest)
	}
	for _, tf := range kernel.DefaultHTFDetectionTFs {
		seen := map[int64]bool{}
		for _, ct := range contracts {
			rows, err := bh.BarsBetweenFromNT8On(symbol, tf, ct, 0, now.UnixMilli())
			if err != nil {
				fmt.Fprintf(os.Stderr, "bars %s/%s: %v\n", tf, ct, err)
				continue
			}
			for _, b := range rows {
				if seen[b.OpenTimeMs] {
					continue // overlap window: current-contract row already kept
				}
				seen[b.OpenTimeMs] = true
				tfBars[tf] = append(tfBars[tf], market.Kline{
					OpenTime: b.OpenTimeMs, // CloseTime 0: the grader never reads it (F2)
					Open:     b.O, High: b.H, Low: b.L, Close: b.C, Volume: b.V,
				})
			}
		}
		sort.Slice(tfBars[tf], func(i, j int) bool { return tfBars[tf][i].OpenTime < tfBars[tf][j].OpenTime })
	}

	fetch := func(tf string, count int) []market.Kline {
		bars := tfBars[tf]
		if len(bars) > count {
			bars = bars[len(bars)-count:]
		}
		return bars
	}
	levels, rep := kernel.DetectHTFLevelsExport(fetch, kernel.DefaultHTFDetectionTFs, symbol, now)
	_ = rep

	ls := st.LevelState()
	type row struct {
		Label  string
		Kind   string
		TF     string
		Price  float64
		HTF    bool
		Off    string
		On     string
		N      int
		Origin string
	}
	rows := make([]row, 0, len(levels))
	byKindTF := map[string]int{}
	for _, l := range levels {
		off := ""
		key := store.MakeLevelKey(traderID, symbol, kernel.LevelTypeFromLabel(l.Label), "", kernel.LevelBinIndex(l.Price))
		if cur, err := ls.Get(key); err == nil && cur != nil {
			off = store.AgedFreshness(cur, now)
		}
		on, n, origin := "fresh", 0, "n/a"
		if l.HTF && kernel.IsHTFFreshTF(l.TF) {
			on, n, origin = kernel.LevelFreshnessByTF(l, now, tfBars[l.TF])
		}
		rows = append(rows, row{l.Label, string(l.Kind), l.TF, l.Price, l.HTF, off, on, n, origin})
		byKindTF[l.Label]++
	}

	// Summary vs the log map (16:31:29 G2).
	fmt.Printf("detected=%d (log said 244) · by kind·TF:\n", len(levels))
	ks := make([]string, 0, len(byKindTF))
	for k := range byKindTF {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("  %-16s %3d\n", k, byKindTF[k])
	}

	// Grade distribution OFF vs ON over HTF-in-set levels only.
	var offDist, onDist map[string]int = map[string]int{}, map[string]int{}
	var htfN int
	for _, r := range rows {
		if !r.HTF || !kernel.IsHTFFreshTF(r.TF) {
			continue
		}
		htfN++
		offDist[displayGrade(r.Off)]++
		onDist[r.On]++
	}
	fmt.Printf("HTF grades (TF∈S2 set, n=%d): OFF=%v ON=%v\n", htfN, offDist, onDist)

	fmt.Println("# per-level table: label | kind | tf | price | OFF | ON | nTests | origin")
	for _, r := range rows {
		fmt.Printf("%-16s %-10s %-3s %8.2f  off=%-8s on=%-9s n=%d origin=%s\n", r.Label, r.Kind, r.TF, r.Price, display(r.Off), r.On, r.N, r.Origin)
	}
}

func display(s string) string {
	if s == "" {
		return "fresh(∅)"
	}
	return s
}

// displayGrade mirrors kernel.freshLabel's normalization (unexported upstream).
func displayGrade(f string) string {
	switch f {
	case "", "a", "A", "fresh":
		return "fresh"
	case "b", "B":
		return "B"
	case "c", "C", "tested":
		return "tested"
	case "done", "consumed":
		return "flipped"
	default:
		return f
	}
}

func mustTime(t time.Time, err error) time.Time {
	if err != nil {
		panic(err)
	}
	return t
}
