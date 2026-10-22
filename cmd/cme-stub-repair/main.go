// Command cme-stub-repair re-anchors the OLD 2h/4h 16:00-close stub rows in
// the bars table (m169 DS-108 item 1).
//
// Before the m169 fix, the NT8-native 2h/4h session-close stub was stored ONE
// HOUR EARLY: the 4h 13:00 bucket sat at open_time_ms 12:00 CT and the 2h
// 15:00 bucket at 14:00 CT. The m169 fix re-anchors at wire ingress, so NEW
// rows are correct — but rows written by any pre-fix binary are still wrong.
// This command shifts ONLY those wrong rows +1h. It never touches a correct
// row (a bar whose open+step is not 16:00 CT), so it is IDEMPOTENT: a second
// run finds nothing to do.
//
// OWNER-RUN ONLY. It is NOT wired to boot, and it never runs implicitly.
// Procedure:
//
//  1. STOP the bot (a running pre-fix binary will re-write wrong rows).
//  2. go run ./cmd/cme-stub-repair -db data/data.db            (dry-run)
//  3. go run ./cmd/cme-stub-repair -db data/data.db -confirm   (backup + fix)
//  4. start the NEW binary (wire-ingress re-anchor).
//
// -confirm copies the database file to <db>.cme-stub-repair-backup-<ts> before
// the first write. It refuses to run if the backup copy fails.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"vl/kernel"
	"vl/store"
)

type wrongRow struct {
	Symbol   string
	TF       string
	Contract string
	OpenMs   int64
	WantMs   int64
}

func main() {
	dbPath := flag.String("db", "data/data.db", "path to the sqlite database")
	confirm := flag.Bool("confirm", false, "actually write (default: dry-run preview)")
	flag.Parse()

	st, err := store.New(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	var rows []store.BarHistoryDB
	if err := st.GormDB().Where("tf IN ?", []string{"2h", "4h"}).
		Order("symbol, tf, contract, open_time_ms").Find(&rows).Error; err != nil {
		fmt.Fprintf(os.Stderr, "list bars: %v\n", err)
		os.Exit(1)
	}

	var wrong []wrongRow
	for _, r := range rows {
		if want := kernel.ReanchorCMEStub(r.TF, r.OpenTimeMs); want != r.OpenTimeMs {
			wrong = append(wrong, wrongRow{Symbol: r.Symbol, TF: r.TF, Contract: r.Contract, OpenMs: r.OpenTimeMs, WantMs: want})
		}
	}
	if len(wrong) == 0 {
		fmt.Println("bars: no wrong 2h/4h stub rows. Nothing to repair.")
		return
	}

	fmt.Printf("wrong 2h/4h stub rows (stored 1h early): %d\n", len(wrong))
	for _, w := range wrong {
		fmt.Printf("  %-6s %-3s %-14s %s → %s\n", w.Symbol, w.TF, w.Contract,
			time.UnixMilli(w.OpenMs).UTC().Format("2006-01-02 15:04"), time.UnixMilli(w.WantMs).UTC().Format("2006-01-02 15:04"))
	}

	if !*confirm {
		fmt.Println("\nDRY-RUN — no writes. Add -confirm to back up and execute.")
		return
	}

	// Backup FIRST: refuse the write if the backup cannot be made.
	backup := fmt.Sprintf("%s.cme-stub-repair-backup-%s", *dbPath, time.Now().Format("20060102T150405"))
	if err := copyFile(*dbPath, backup); err != nil {
		fmt.Fprintf(os.Stderr, "backup to %s: %v — NOT writing\n", backup, err)
		os.Exit(1)
	}
	fmt.Printf("\nbackup: %s\n", backup)

	// WHERE-scoped, idempotent: each wrong row is shifted +1h by its exact key.
	// If a corrected row already exists for the same (symbol, tf, contract,
	// want) key, the UPDATE would collide — report it and leave it (the stale
	// wrong row is never deleted; the owner resolves it by hand).
	fixed, skipped := 0, 0
	for _, w := range wrong {
		var exists int64
		st.GormDB().Raw("SELECT COUNT(1) FROM bars WHERE symbol=? AND tf=? AND contract=? AND open_time_ms=?",
			w.Symbol, w.TF, w.Contract, w.WantMs).Scan(&exists)
		if exists > 0 {
			fmt.Printf("  SKIP %-6s %-3s %-14s %s: corrected row already exists — leaving the stale row untouched\n",
				w.Symbol, w.TF, w.Contract, time.UnixMilli(w.OpenMs).UTC().Format("15:04"))
			skipped++
			continue
		}
		res := st.GormDB().Exec("UPDATE bars SET open_time_ms=? WHERE symbol=? AND tf=? AND contract=? AND open_time_ms=?",
			w.WantMs, w.Symbol, w.TF, w.Contract, w.OpenMs)
		if res.Error != nil {
			fmt.Fprintf(os.Stderr, "  FAIL %-6s %-3s %-14s %s: %v\n", w.Symbol, w.TF, w.Contract, time.UnixMilli(w.OpenMs).UTC().Format("15:04"), res.Error)
			continue
		}
		fixed++
	}
	fmt.Printf("\n✅ fixed %d row(s); skipped %d (collision, left in place).\n", fixed, skipped)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Sync()
}
