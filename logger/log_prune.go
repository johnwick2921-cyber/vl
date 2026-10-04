package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// logRetentionDays resolves LOG_RETENTION_DAYS. 0 = OFF (keep every file) —
// the owner has not ruled on log retention, and deleting files behind a knob
// that defaults ON is forbidden. A negative/invalid value also reads OFF.
func logRetentionDays() int {
	v := strings.TrimSpace(os.Getenv("LOG_RETENTION_DAYS"))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// pruneOldLogs (P2-2) deletes data/vl_YYYY-MM-DD.log files strictly
// older than `days` calendar days, EXCEPT today's file and the currently-open
// process file (a running boot's file is never deleted). Returns the removed
// names (basenames) and the first error. Pure in (dir, now, days, current) so
// the pin drives the production path — Init calls it once after opening the
// day's file.
func pruneOldLogs(dir string, now time.Time, days int, current string) ([]string, error) {
	if days <= 0 {
		return nil, nil // OFF: keep everything
	}
	cutoff := now.AddDate(0, 0, -days).Format("2006-01-02")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		name := e.Name()
		prefix := ""
		if strings.HasPrefix(name, "vl_") {
			prefix = "vl_"
		} else {
			continue
		}
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		datePart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".log")
		if _, perr := time.Parse("2006-01-02", datePart); perr != nil {
			continue // not our daily naming — leave it alone
		}
		if name == current || datePart == now.Format("2006-01-02") {
			continue // today's or the live file: never deleted
		}
		if datePart >= cutoff {
			continue // inside the retention window
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// pruneLogFilesAtInit runs the retention prune for the data/ dir after the
// day's file is open, and prints the boot line that READS the resolved knob
// and what it did — never a literal.
func pruneLogFilesAtInit() {
	days := logRetentionDays()
	if days <= 0 {
		Infof("🧹 log retention: off (keep all) · knob LOG_RETENTION_DAYS")
		return
	}
	removed, err := pruneOldLogs("data", time.Now(), days, currentLogName())
	if err != nil {
		Warnf("🧹 log retention: prune FAILED (%v) · knob LOG_RETENTION_DAYS=%d", err, days)
		return
	}
	Infof("🧹 log retention: keep %d days · pruned %d file(s) at init%s", days, len(removed), fmt.Sprintf(" (%v)", removed))
}

// currentLogName returns the basename of the currently-open log file ("" when
// stdout-only).
func currentLogName() string {
	if logFile == nil {
		return ""
	}
	return filepath.Base(logFile.Name())
}
