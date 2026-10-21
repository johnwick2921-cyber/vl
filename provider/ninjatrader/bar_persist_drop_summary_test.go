package ninjatrader

import (
	"bytes"
	"strings"
	"testing"

	"vl/logger"
)

// LOG-NOISE-2 (2026-10-08): the "bars: ingest drop summary" WARN used to fire
// every minute even when every drop counter was 0, and it read the SESSION peak
// without resetting it. Now: zero-drop minute silent, one-drop minute logs ONE
// WARN with THAT minute's peak (reset after the line), the next zero minute is
// silent again.

// captureIngestSummaryWarns swaps the global logger output and returns the
// "bars: ingest drop summary" WARN lines the call emitted.
func captureIngestSummaryWarns(t *testing.T, fn func()) []string {
	t.Helper()
	var buf bytes.Buffer
	orig := logger.Log.Out
	logger.Log.SetOutput(&buf)
	defer logger.Log.SetOutput(orig)
	fn()
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "bars: ingest drop summary") {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestIngestDropSummaryZeroDropMinuteSilent — a minute with no drops logs
// nothing. Named RED: revert the zero-drop early return → the WARN fires.
func TestIngestDropSummaryZeroDropMinuteSilent(t *testing.T) {
	resetIngestCounters()
	lines := captureIngestSummaryWarns(t, ingestDropSummary)
	if len(lines) != 0 {
		t.Fatalf("zero-drop minute must be silent, got %d WARN line(s): %v", len(lines), lines)
	}
}

// TestIngestDropSummaryOneDropWarnsWithThatMinutesPeak — one drop logs exactly
// ONE WARN carrying that minute's peak, and the peak is reset after the line;
// the following zero-drop minute is silent. Named RED: revert the peak Swap(0)
// back to Load() → the peak is not reset (and a reverted zero-drop guard makes
// the next minute noisy).
func TestIngestDropSummaryOneDropWarnsWithThatMinutesPeak(t *testing.T) {
	resetIngestCounters()
	ingestDropOld.Add(1)
	ingestPeakDepth.Store(4096)

	lines := captureIngestSummaryWarns(t, ingestDropSummary)
	if len(lines) != 1 {
		t.Fatalf("one-drop minute must log exactly ONE WARN, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "intrabar_dropped=1") {
		t.Fatalf("WARN must report the drop: %s", lines[0])
	}
	if !strings.Contains(lines[0], "peak_depth=4096/4096") {
		t.Fatalf("WARN must report THAT minute's peak: %s", lines[0])
	}
	// The peak is reset after the line (per-minute, not the stale session peak).
	if got := ingestPeakDepth.Load(); got != 0 {
		t.Fatalf("peak must reset after the line, got %d", got)
	}
	// The NEXT zero-drop minute is silent again.
	ingestLastSum.Store(0)
	lines2 := captureIngestSummaryWarns(t, ingestDropSummary)
	if len(lines2) != 0 {
		t.Fatalf("the next zero-drop minute must be silent, got %d: %v", len(lines2), lines2)
	}
}
