package logger

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestCompactFormatterRendersCTWallTime pins CT-EVERYWHERE: the log line
// timestamp is America/Chicago wall time no matter which zone the host (and
// therefore the instant's own location) is. At 2026-01-15 06:30:45 UTC the CT
// wall clock is 2026-01-15 00:30:45 (CST, UTC-6). RED at base: the formatter
// rendered the instant's OWN zone ("01-15 06:30:45" under UTC, "01-15 13:30:45"
// under Ho Chi Minh) instead of CT.
func TestCompactFormatterRendersCTWallTime(t *testing.T) {
	instant := time.Date(2026, 1, 15, 6, 30, 45, 0, time.UTC)
	want := "01-15 00:30:45"
	for _, zone := range []string{"UTC", "Asia/Ho_Chi_Minh"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatalf("load %s: %v", zone, err)
		}
		t.Run(zone, func(t *testing.T) {
			entry := &logrus.Entry{Level: logrus.InfoLevel, Message: "x", Time: instant.In(loc)}
			out, err := (&compactFormatter{}).Format(entry)
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			if !strings.Contains(string(out), want) {
				t.Fatalf("rendered %q, want the CT wall time %q", string(out), want)
			}
		})
	}
}

// TestLogFileNameForUsesCTDateAcrossBoundary pins the file-name half of
// CT-EVERYWHERE: at 2026-01-01 02:00 UTC the machine (UTC) date is 2026-01-01
// but the CT date is 2025-12-31 (CST, UTC-6). The log file must be named by the
// CT date so its name and its line times agree. RED at base: the name carried
// the instant's own date ("vl_2026-01-01.log").
func TestLogFileNameForUsesCTDateAcrossBoundary(t *testing.T) {
	instant := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	want := filepath.Join("data", "vl_2025-12-31.log")
	if got := LogFileNameFor("data", instant); got != want {
		t.Fatalf("LogFileNameFor = %q, want %q", got, want)
	}
}
