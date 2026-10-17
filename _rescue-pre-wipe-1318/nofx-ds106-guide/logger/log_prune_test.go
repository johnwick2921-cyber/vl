package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneOldLogsNeverDeletesTodayOrLive pins P2-2 at the production
// function Init consumes (pruneLogFilesAtInit → pruneOldLogs): retention
// removes only files STRICTLY older than N days, never today's and never the
// running boot's file.
func TestPruneOldLogsNeverDeletesTodayOrLive(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("nofx_2026-09-26.log") // today — never deleted
	write("nofx_2026-09-25.log") // 1 day old — inside a 3-day window
	write("nofx_2026-09-20.log") // 6 days old — outside a 3-day window
	write("nofx_2026-09-19.log") // 7 days old — outside a 3-day window
	write("other.txt")           // not ours — untouched

	removed, err := pruneOldLogs(dir, now, 3, "nofx_2026-09-26.log")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 2 || removed[0] != "nofx_2026-09-19.log" || removed[1] != "nofx_2026-09-20.log" {
		t.Fatalf("want exactly [nofx_2026-09-19.log nofx_2026-09-20.log], got %v", removed)
	}
	for _, keep := range []string{"nofx_2026-09-26.log", "nofx_2026-09-25.log", "other.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s must survive the prune: %v", keep, err)
		}
	}
}

// TestPruneOldLogsOffKeepsEverything pins the default-OFF knob: days<=0 must
// remove nothing (the owner has not ruled on log retention).
func TestPruneOldLogsOffKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("nofx_2020-01-01.log")
	removed, err := pruneOldLogs(dir, time.Now(), 0, "")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("OFF must keep everything, removed %v", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "nofx_2020-01-01.log")); err != nil {
		t.Fatalf("off-state file deleted: %v", err)
	}
}
