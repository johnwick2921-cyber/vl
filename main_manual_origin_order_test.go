package main

import (
	"os"
	"strings"
	"testing"
)

// RT-10 (release #24, N4) — THE MANUAL-ORIGIN MIGRATION RUNS BEFORE THE
// TRADERS LOAD. On the 10-14 boot it ran ~10 s after LoadTradersFromStore, so
// hoang's first decision cycle read the unmigrated rows (track record
// -4226.18/168 instead of -1024.18/164). main() cannot be driven in a test
// (it boots the whole process), so this pins the ORDER of the two production
// call sites in main.go. MUTANT: put the block back after the load → RED.
func TestManualOriginMigrationRunsBeforeTradersLoad(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	mig := strings.Index(src, "st.Position().RunManualOriginMigration()")
	load := strings.Index(src, "traderManager.LoadTradersFromStore(st)")
	if mig < 0 || load < 0 {
		t.Fatalf("call sites not found (migration at %d, load at %d)", mig, load)
	}
	if strings.Count(src, "st.Position().RunManualOriginMigration()") != 1 {
		t.Fatal("the manual-origin migration must have exactly one call site in main.go")
	}
	if mig > load {
		t.Fatal("the manual-origin migration runs AFTER LoadTradersFromStore: the first cycle of every trader reads the unmigrated bot record")
	}
}
