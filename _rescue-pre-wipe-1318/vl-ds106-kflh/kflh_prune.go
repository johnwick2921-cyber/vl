package main
package main

import (
	"fmt"
	"os"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

func kline(r store.BarHistoryDB, tf int64) market.Kline {
	return market.Kline{OpenTime: r.OpenTimeMs, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V, CloseTime: r.OpenTimeMs + tf - 1}
}

func main() {
	db, err := store.InitGorm("/home/hoang/vl-ds106-kflh/data-copy/data.db")
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	bh := store.NewBarHistoryStore(db)
	spans, _ := bh.ContractSpans("MNQ", "1m")
	var contracts []mentor.Contract1M
	for _, sp := range spans {
		rows, err := bh.AllBarsOn("MNQ", "1m", sp.Contract)
		if err != nil || len(rows) == 0 {
			continue
		}
		c := mentor.Contract1M{Contract: sp.Contract}
		for _, r := range rows {
			c.Bars = append(c.Bars, kline(r, 60_000))
		}
		if rows1h, err := bh.AllBarsOn("MNQ", "1h", sp.Contract); err == nil {
			for _, r := range rows1h {
				c.Bars1H = append(c.Bars1H, kline(r, 3600_000))
			}
		}
		contracts = append(contracts, c)
	}
	stitched, _, _ := mentor.StitchKeyLevelHistory(contracts)
	now := time.Now().UnixMilli()

	// order A: prune then delete (what Seed does)
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	e := mentor.New(cfg)
	_ = mentor.SeedFull(e, contracts[len(contracts)-1].Bars, stitched, now)
	fmt.Printf("order=prune→delete  levels=%d deleted=%d surviving=%d\n",
		len(e.State.SeedLevels), len(e.State.DeletedLevels), len(e.State.SeedLevels)-len(e.State.DeletedLevels))
}
