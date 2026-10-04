package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// R1a: the pruner treats vl_ and vl_ logs alike — today's file of EITHER
// prefix survives, and old files of both prefixes are removed. // R5 removes
// the vl half.
func TestPruneOldLogsCoversBothPrefixes(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("vl_2026-09-26.log")   // today's vl file — never deleted
	write("vl_2026-09-26.log") // today's vl file — never deleted
	write("vl_2026-09-20.log")   // old vl file — removed
	write("vl_2026-09-19.log") // old vl file — removed
	write("other.txt")           // not ours — untouched

	removed, err := pruneOldLogs(dir, now, 3, "vl_2026-09-26.log")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range removed {
		seen[r] = true
	}
	if len(removed) != 2 || !seen["vl_2026-09-20.log"] || !seen["vl_2026-09-19.log"] {
		t.Fatalf("want exactly the two old files of both prefixes, got %v", removed)
	}
	for _, keep := range []string{"vl_2026-09-26.log", "vl_2026-09-26.log", "other.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s must survive the prune: %v", keep, err)
		}
	}
}
