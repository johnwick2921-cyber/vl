// levelstats-backfill — Level-truth wave (2026-08-27) T1 backfill + nightly
// proof. Re-evaluates EVERY session-day that has persisted 1m bars AND plans
// into the level_stats forward-validation table (idempotent UPSERT).
//
// Run (read-write on level_stats only — everything else is read):
//
//	go run ./cmd/levelstats-backfill [-days 2026-08-24,2026-08-25] [-trader <id>]
//
// With no -days, the bar-coverage span is scanned from the bars table and each
// covered session-day is re-evaluated. Default trader = the hoang day-plan
// trader (env VL_TRADER_ID overrides). DB path: env VL_DB_PATH, default
// data/data.db.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"vl/kernel"
	"vl/store"
	nt "vl/trader/ninjatrader"
)

const defaultTraderID = "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265"

func main() {
	daysFlag := flag.String("days", "", "comma-separated YYYY-MM-DD session-days to backfill (default: full bar-coverage span)")
	traderFlag := flag.String("trader", "", "trader id (default: hoang day-plan trader)")
	flag.Parse()

	dbPath := os.Getenv("VL_DB_PATH") // R5: VL_ only
	if dbPath == "" {
		dbPath = "data/data.db"
	}
	traderID := *traderFlag
	if traderID == "" {
		traderID = os.Getenv("VL_TRADER_ID") // R5: VL_ only
	}
	if traderID == "" {
		traderID = defaultTraderID
	}

	st, err := store.NewWithConfig(store.DBConfig{Type: store.DBTypeSQLite, Path: dbPath})
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}

	var days []string
	if *daysFlag != "" {
		days = strings.Split(*daysFlag, ",")
	} else {
		days = coveredSessionDays(st)
	}
	if len(days) == 0 {
		fmt.Println("no days to backfill (no bar coverage / no -days)")
		os.Exit(1)
	}

	out, err := nt.BackfillLevelStats(st, st.LevelStats(), traderID, days)
	if err != nil {
		fmt.Println("backfill:", err)
		os.Exit(1)
	}
	for _, d := range days {
		fmt.Printf("  %s: %d rows\n", d, out[d])
	}
	n, _ := st.LevelStats().Count()
	fmt.Printf("level_stats total rows: %d\n", n)
	agg, _ := st.LevelStats().AggregateByGrade()
	fmt.Println("by grade:")
	for _, a := range agg {
		fmt.Printf("  %s rows=%d touched=%d reacted=%d broke=%d chopped=%d\n", a.Grade, a.Rows, a.Touched, a.Reacted, a.BrokeClean, a.Chopped)
	}
	fam, _ := st.LevelStats().AggregateByFamily()
	fmt.Println("by family:")
	for _, a := range fam {
		fmt.Printf("  %s rows=%d touched=%d reacted=%d broke=%d\n", a.Family, a.Rows, a.Touched, a.Reacted, a.BrokeClean)
	}
}

// coveredSessionDays scans the bars table's open-time span and returns every
// CME session-day (17:00 CT) with at least one persisted 1m bar.
func coveredSessionDays(st *store.Store) []string {
	bars, err := st.BarHistory().BarsBetween("MNQ", "1m", 0, time.Now().Add(24*time.Hour).UnixMilli())
	if err != nil || len(bars) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, b := range bars {
		seen[kernel.CMESessionDayKey(time.UnixMilli(b.OpenTimeMs))] = true
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
